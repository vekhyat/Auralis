package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type SourceTrack struct {
	ID         string
	ServiceURL string
	Title      string
	Artist     string
	ISRC       string
	Duration   int
}

type communityAccessError struct{ Status int }

func (e *communityAccessError) Error() string {
	return fmt.Sprintf("source requires an authorized session or verification (HTTP %d)", e.Status)
}

func sourceResponse(source CommunitySource, method, path string, query url.Values, payload any, ctx context.Context) ([]byte, http.Header, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, err
		}
		body = bytes.NewReader(encoded)
	}
	raw := strings.TrimRight(source.BaseURL, "/") + path
	if len(query) > 0 {
		raw += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, body)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid source request")
	}
	req.Header.Set("Accept", "application/json, text/html")
	req.Header.Set("User-Agent", DefaultDownloaderUserAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := applyCommunityRequestCredentials(req, source); err != nil {
		return nil, nil, err
	}
	resp, err := NewSignedHTTPClient(8 * time.Second).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, communityContextError(ctx)
		}
		return nil, nil, fmt.Errorf("source network request failed: %w", safeSourceNetworkError(ctx, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 428 {
		return nil, nil, &communityAccessError{Status: resp.StatusCode}
	}
	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		return nil, nil, fmt.Errorf("source HTTP %d", resp.StatusCode)
	}
	data, err := readBoundedBody(resp.Body, 2<<20)
	if err != nil {
		return nil, nil, fmt.Errorf("source response was unreadable or too large")
	}
	headers := resp.Header.Clone()
	headers.Set("X-Auralis-Status", strconv.Itoa(resp.StatusCode))
	return data, headers, nil
}

func safeSourceNetworkError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("request timed out")
	}
	return fmt.Errorf("connection failed")
}
func communityContextError(ctx context.Context) error {
	if ActiveDownloadContext().Err() != nil {
		return WrapDownloadCancelled(ActiveDownloadContext().Err())
	}
	return fmt.Errorf("community source operation timed out or was cancelled")
}

func sourceURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("source did not return an HTTP audio URL")
	}
	return u.String(), nil
}

func downloadCommunitySource(source CommunitySource, track SourceTrack, quality, dest string) (string, error) {
	if strings.TrimSpace(track.ID) == "" && source.Protocol != "subsonic" && source.Protocol != "dab" {
		return "", fmt.Errorf("track ID is required")
	}
	ctx, cancel := context.WithTimeout(ActiveDownloadContext(), 45*time.Second)
	defer cancel()
	var raw string
	var err error
	switch source.Protocol {
	case "hifi":
		raw, err = resolveHiFiSource(ctx, source, track.ID, quality)
	case "qobuz-rest", "qobuz-dl":
		raw, err = resolveQobuzSource(ctx, source, track.ID, quality)
	case "dab":
		raw, err = resolveDABSource(ctx, source, track, quality)
	case "lucida":
		raw, err = resolveLucidaSource(ctx, source, track, quality)
	case "subsonic":
		raw, err = resolveSubsonicSource(ctx, source, track, quality)
	default:
		return "", fmt.Errorf("unsupported community adapter")
	}
	if err != nil {
		return "", err
	}
	if source.Protocol == "hifi" {
		t := NewTidalDownloader(source.BaseURL)
		t.communitySource = &source
		err = t.DownloadFile(raw, dest, quality)
		if err != nil {
			cleanupTidalDownloadArtifacts(dest)
		}
		if err != nil {
			err = fmt.Errorf("source media request failed: %s", redactCommunityMessage(err.Error(), communitySecretValues(source, nil)...))
		}
		return dest, err
	}
	raw, err = sourceURL(raw)
	if err != nil {
		return "", err
	}
	return downloadCommunityMedia(source, raw, dest)
}

// downloadCommunityMedia keeps site cookies on the source origin and on a
// redirect only when that cookie's domain still applies. Environment cookies
// and API keys never follow a different host.
func downloadCommunityMedia(source CommunitySource, rawURL, dest string) (string, error) {
	header, err := communityCookieHeader(source, rawURL)
	if err != nil {
		return "", err
	}
	if header == "" {
		return antraDownloadURLToFile(rawURL, dest)
	}
	return downloadCredentialedMedia(source, rawURL, dest, header)
}

func downloadCredentialedMedia(source CommunitySource, rawURL, dest, header string) (string, error) {
	req, err := NewRequestWithDefaultHeaders(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", header)
	if err := applyCommunityRequestCredentials(req, source); err != nil {
		return "", err
	}
	req = WithDownloadContext(req)
	client := newMediaHTTPClient()
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		next.Header.Del("Cookie")
		next.Header.Del("Authorization")
		next.Header.Del("X-API-Key")
		hop, hopErr := communityCookieHeader(source, next.URL.String())
		if hopErr != nil {
			return hopErr
		}
		if hop != "" {
			next.Header.Set("Cookie", hop)
		}
		return applyCommunityRequestCredentials(next, source)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("source media request failed: %s", redactCommunityMessage(err.Error(), header))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stream HTTP %d", resp.StatusCode)
	}
	partPath := dest + ".part"
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil && filepath.Dir(dest) != "." {
		return "", err
	}
	out, err := os.Create(partPath)
	if err != nil {
		return "", err
	}
	_, copyErr := copyDownloadBody(NewProgressWriter(out), resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(partPath)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(partPath)
		return "", closeErr
	}
	ext := extensionFromContentType(resp.Header.Get("Content-Type"))
	if sniff, sniffErr := sniffAudioExtension(partPath); sniffErr == nil && sniff != "" {
		ext = sniff
	}
	if ext == "" {
		ext = filepath.Ext(dest)
		if ext == "" {
			ext = ".flac"
		}
	}
	finalPath := replaceAudioExtension(dest, ext)
	if err := os.Rename(partPath, finalPath); err != nil {
		_ = os.Remove(partPath)
		return "", err
	}
	return finalPath, nil
}

func resolveQobuzSource(ctx context.Context, source CommunitySource, id, quality string) (string, error) {
	if sourceQuality(quality) == "atmos" {
		return "", fmt.Errorf("this Qobuz adapter has no Atmos selection")
	}
	qual := "6"
	switch sourceQuality(quality) {
	case "24":
		qual = "27"
	}
	if quality == "7" {
		qual = "7"
	}
	path := "/api/download-music"
	params := url.Values{"track_id": {id}, "quality": {qual}}
	if source.Protocol == "qobuz-rest" {
		path = "/download-url/" + url.PathEscape(id)
		q := "flac"
		if qual == "27" {
			q = "hi96"
		} else if qual == "7" {
			q = "hi24"
		}
		params = url.Values{"quality": {q}}
	}
	body, _, err := sourceResponse(source, http.MethodGet, path, params, nil, ctx)
	if err != nil {
		return "", err
	}
	if err := expectSourceJSON(body); err != nil {
		return "", err
	}
	var p struct {
		URL     string      `json:"url"`
		TrackID json.Number `json:"track_id"`
		Success *bool       `json:"success"`
		Data    struct {
			URL string `json:"url"`
		} `json:"data"`
		BitDepth int `json:"bit_depth"`
	}
	if json.Unmarshal(body, &p) != nil {
		return "", fmt.Errorf("source returned invalid download JSON")
	}
	if p.Success != nil && !*p.Success {
		return "", fmt.Errorf("source reported an unavailable track")
	}
	if p.TrackID != "" && p.TrackID.String() != id {
		return "", fmt.Errorf("source returned a different track")
	}
	if sourceQuality(quality) == "24" && p.BitDepth > 0 && p.BitDepth <= 16 {
		return "", fmt.Errorf("source returned lower bit depth")
	}
	if p.URL == "" {
		p.URL = p.Data.URL
	}
	return sourceURL(p.URL)
}

func resolveHiFiSource(ctx context.Context, source CommunitySource, id, quality string) (raw string, resultErr error) {
	params := url.Values{"id": {id}, "quality": {mapTidalQualityToZarz(quality)}}
	path := "/track/"
	if isTidalAtmosQuality(quality) {
		path = "/trackManifests/"
		params = url.Values{"id": {id}, "formats": {"EAC3_JOC"}, "adaptive": {"true"}, "manifestType": {"MPEG_DASH"}, "uriScheme": {"DATA"}, "usage": {"PLAYBACK"}}
	}
	body, headers, err := sourceResponse(source, http.MethodGet, path, params, nil, ctx)
	if err != nil {
		return "", err
	}
	if communityChallengeHTML(body) {
		return "", &communityAccessError{Status: 403}
	}
	var cancelPath string
	defer func() {
		if cancelPath != "" && resultErr != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _, _ = sourceResponse(source, http.MethodDelete, cancelPath, nil, nil, cleanupCtx)
		}
	}()
	for headers.Get("X-Auralis-Status") == "202" {
		var job struct {
			StatusURL string `json:"statusUrl"`
			CancelURL string `json:"cancelUrl"`
		}
		if json.Unmarshal(body, &job) != nil {
			return "", fmt.Errorf("invalid queued playback response")
		}
		poll, err := sameSourcePath(source.BaseURL, job.StatusURL)
		if err != nil {
			return "", err
		}
		cancelPath, err = sameSourcePath(source.BaseURL, job.CancelURL)
		if err != nil {
			return "", err
		}
		wait := time.Second
		if n, _ := strconv.Atoi(headers.Get("Retry-After")); n > 1 {
			wait = time.Duration(min(n, 10)) * time.Second
		}
		select {
		case <-ctx.Done():
			return "", communityContextError(ctx)
		case <-time.After(wait):
		}
		body, headers, err = sourceResponse(source, http.MethodGet, poll, nil, nil, ctx)
		if err != nil {
			return "", err
		}
		if communityChallengeHTML(body) {
			return "", &communityAccessError{Status: 403}
		}
	}
	var p TidalAPIResponseV2
	if json.Unmarshal(body, &p) == nil && p.Data.Manifest != "" {
		if p.Data.TrackID != 0 && strconv.FormatInt(p.Data.TrackID, 10) != id {
			return "", fmt.Errorf("source returned a different track")
		}
		if !strings.EqualFold(p.Data.AssetPresentation, "FULL") {
			return "", fmt.Errorf("source returned a preview or unknown asset")
		}
		if err := validateUnprotectedManifest(p.Data.Manifest); err != nil {
			return "", err
		}
		return "MANIFEST:" + p.Data.Manifest, nil
	}
	if isTidalAtmosQuality(quality) {
		var p TidalManifestAPIResponse
		if json.Unmarshal(body, &p) != nil {
			return "", fmt.Errorf("invalid Atmos response")
		}
		a := p.Data.Data.Attributes
		if !containsString(a.Formats, "EAC3_JOC") {
			return "", fmt.Errorf("Atmos is unavailable")
		}
		prefix := "data:application/dash+xml;base64,"
		if !strings.HasPrefix(a.URI, prefix) {
			return "", fmt.Errorf("missing inline Atmos manifest")
		}
		manifest := strings.TrimPrefix(a.URI, prefix)
		if err := validateUnprotectedManifest(manifest); err != nil {
			return "", err
		}
		return "MANIFEST:" + manifest, nil
	}
	return "", fmt.Errorf("source did not return a full-track playback manifest")
}

func validateUnprotectedManifest(encoded string) error {
	body, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("invalid playback manifest")
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "<") {
		if strings.Contains(strings.ToLower(string(body)), "contentprotection") {
			return fmt.Errorf("protected playback is not supported by this adapter")
		}
		return nil
	}
	var manifest struct {
		Encryption string   `json:"encryptionType"`
		URLs       []string `json:"urls"`
	}
	if json.Unmarshal(body, &manifest) != nil || len(manifest.URLs) == 0 {
		return fmt.Errorf("invalid audio manifest")
	}
	if !strings.EqualFold(manifest.Encryption, "NONE") {
		return fmt.Errorf("protected playback is not supported by this adapter")
	}
	for _, raw := range manifest.URLs {
		if _, err := sourceURL(raw); err != nil {
			return err
		}
	}
	return nil
}

func sameSourcePath(base, raw string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid source URL")
	}
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return "", fmt.Errorf("missing playback job URL")
	}
	u = b.ResolveReference(u)
	if u.Scheme != b.Scheme || u.Host != b.Host || u.User != nil || !strings.HasPrefix(u.Path, "/playback/requests/") {
		return "", fmt.Errorf("playback job URL changed source origin")
	}
	return u.RequestURI(), nil
}

var sourceTitleStrip = regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*(remaster|mix)[^\)\]]*[\)\]]`)

func normalizedSourceTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(sourceTitleStrip.ReplaceAllString(s, "")), " "))
}

func resolveDABSource(ctx context.Context, source CommunitySource, track SourceTrack, quality string) (string, error) {
	if sourceQuality(quality) == "atmos" {
		return "", fmt.Errorf("DAB does not expose an Atmos selection")
	}
	if track.Title == "" || track.Artist == "" {
		return "", fmt.Errorf("DAB requires title and artist to match its own catalog")
	}
	body, _, err := sourceResponse(source, http.MethodGet, "/api/search", url.Values{"q": {track.Title + " " + track.Artist}, "type": {"track"}, "limit": {"10"}}, nil, ctx)
	if err != nil {
		return "", err
	}
	if err := expectSourceJSON(body); err != nil {
		return "", err
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&payload) != nil {
		return "", fmt.Errorf("invalid DAB search response")
	}
	items, _ := payload["tracks"].([]any)
	if len(items) == 0 {
		items, _ = payload["results"].([]any)
	}
	match := ""
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		title, _ := row["title"].(string)
		artist, _ := row["artist"].(string)
		isrc, _ := row["isrc"].(string)
		if track.ISRC != "" && isrc != "" && !strings.EqualFold(track.ISRC, isrc) {
			continue
		}
		if normalizedSourceTitle(title) != normalizedSourceTitle(track.Title) || !strings.EqualFold(strings.TrimSpace(artist), strings.TrimSpace(track.Artist)) {
			continue
		}
		switch id := row["id"].(type) {
		case string:
			match = id
		case json.Number:
			match = id.String()
		}
		if match != "" {
			break
		}
	}
	if match == "" {
		return "", fmt.Errorf("DAB did not match the requested recording")
	}
	qual := "6"
	if sourceQuality(quality) == "24" {
		qual = "27"
	}
	body, _, err = sourceResponse(source, http.MethodGet, "/api/stream", url.Values{"trackId": {match}, "quality": {qual}}, nil, ctx)
	if err != nil {
		return "", err
	}
	if err := expectSourceJSON(body); err != nil {
		return "", err
	}
	var result struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(body, &result) != nil {
		return "", fmt.Errorf("invalid DAB stream response")
	}
	return sourceURL(result.URL)
}
