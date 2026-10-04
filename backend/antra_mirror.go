package backend

import (
	"encoding/json"
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

var regexpAmazonASIN = regexp.MustCompile(`(B[0-9A-Z]{9})`)

// antraAmazonDecrypt and antraAmazonRemux are the community Amazon pipeline.
// Tests replace them to check key passthrough without ffmpeg.
var (
	antraAmazonDecrypt = decryptWithMP4FF
	antraAmazonRemux   = amazonRemuxWithFFmpeg
)

type antraSearchHit struct {
	TrackID    string
	Title      string
	Artist     string
	Album      string
	DurationMS int
	BitDepth   int
	SampleRate int
	ISRC       string
}

type antraTrackInfo struct {
	TrackID       string
	StreamURL     string
	DecryptionKey string
	Codec         string
	BitDepth      int
	SampleRate    int
	ContentType   string
}

func antraMirrorGet(service, path string, query url.Values, timeout time.Duration) (*http.Response, error) {
	base, apiKey, err := antraMirrorBase(service)
	if err != nil {
		return nil, err
	}
	raw := base + path
	if len(query) > 0 {
		raw += "?" + query.Encode()
	}
	req, err := NewRequestWithDefaultHeaders(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req = WithDownloadContext(req)
	req.Header.Set("Accept", "application/json, audio/*, */*")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	// Stream calls pass a multi-minute timeout. That used to cap the whole
	// body read, so a quiet socket looked stuck until the deadline. Header
	// timeout plus the idle watchdog covers that without cutting off a long file.
	client := &http.Client{Timeout: timeout}
	if timeout > time.Minute {
		client = newMediaHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, WrapDownloadCancelled(err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		return nil, fmt.Errorf("%s mirror rate limited (429)", service)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		return nil, fmt.Errorf("%s mirror rejected API key (%d)", service, resp.StatusCode)
	}
	return resp, nil
}

func antraSearchByISRC(service, isrc string) (antraSearchHit, error) {
	return antraSearchByISRCHint(service, isrc, "", "", 0)
}

func antraSearchByISRCHint(service, isrc, title, artist string, durationMS int) (antraSearchHit, error) {
	isrc = strings.ToUpper(strings.TrimSpace(isrc))
	if isrc == "" {
		return antraSearchHit{}, fmt.Errorf("empty ISRC")
	}
	var query url.Values
	if strings.EqualFold(strings.TrimSpace(service), "amazon") {
		query = url.Values{}
		if title = strings.TrimSpace(title); title != "" {
			query.Set("title", title)
		}
		if artist = strings.TrimSpace(artist); artist != "" {
			query.Set("artist", artist)
		}
		if durationMS > 0 {
			query.Set("duration_ms", strconv.Itoa(durationMS))
		}
		if len(query) == 0 {
			query = nil
		}
	}
	resp, err := antraMirrorGet(service, "/api/search/isrc/"+url.PathEscape(isrc), query, 15*time.Second)
	if err != nil {
		return antraSearchHit{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return antraSearchHit{}, fmt.Errorf("%s ISRC search HTTP %d", service, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return antraSearchHit{}, err
	}
	hit, err := parseAntraSearchHit(body)
	if err != nil {
		return antraSearchHit{}, err
	}
	if hit.TrackID == "" {
		return antraSearchHit{}, fmt.Errorf("%s ISRC search returned no track", service)
	}
	return hit, nil
}

func antraSearchText(service, query string) (antraSearchHit, error) {
	return antraSearchCatalog(service, query, "")
}

func antraSearchCatalog(service, title, artist string) (antraSearchHit, error) {
	title = strings.TrimSpace(title)
	artist = strings.TrimSpace(artist)
	if title == "" && artist == "" {
		return antraSearchHit{}, fmt.Errorf("empty search query")
	}
	serviceKey := strings.ToLower(strings.TrimSpace(service))
	path, query, err := antraTextSearchRequest(serviceKey, title, artist)
	if err != nil {
		return antraSearchHit{}, err
	}
	resp, err := antraMirrorGet(serviceKey, path, query, 15*time.Second)
	if err != nil {
		return antraSearchHit{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return antraSearchHit{}, fmt.Errorf("%s text search HTTP %d", serviceKey, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return antraSearchHit{}, err
	}
	hit, err := pickAntraSearchHit(strings.TrimSpace(title+" "+artist), body)
	if err != nil {
		return antraSearchHit{}, fmt.Errorf("%s text search: %w", serviceKey, err)
	}
	return hit, nil
}

func antraTextSearchRequest(service, title, artist string) (string, url.Values, error) {
	query := url.Values{}
	switch service {
	case "tidal":
		query.Set("s", strings.TrimSpace(strings.TrimSpace(title+" "+artist)))
		return "/search/", query, nil
	case "qobuz", "deezer":
		if title != "" {
			query.Set("title", title)
		}
		if artist != "" {
			query.Set("artist", artist)
		}
		query.Set("limit", "5")
		return "/api/search", query, nil
	case "apple":
		if title != "" {
			query.Set("title", title)
		}
		if artist != "" {
			query.Set("artist", artist)
		}
		return "/api/search", query, nil
	case "amazon":
		return "", nil, fmt.Errorf("amazon mirror has no text search; use ISRC or spotify/apple resolve")
	default:
		return "", nil, fmt.Errorf("unsupported antra service %s", service)
	}
}

func parseAntraSearchHit(body []byte) (antraSearchHit, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return antraSearchHit{}, err
	}
	hit := antraSearchHit{
		TrackID:    firstAntraString(raw, "track_id", "trackId", "id", "asin"),
		Title:      firstAntraString(raw, "title", "name"),
		Artist:     firstAntraString(raw, "artist", "artistName"),
		Album:      firstAntraString(raw, "album", "albumTitle"),
		DurationMS: firstAntraInt(raw, "duration_ms", "durationMs"),
		BitDepth:   firstAntraInt(raw, "bitDepth", "bit_depth"),
		SampleRate: firstAntraInt(raw, "sampleRate", "sample_rate"),
		ISRC:       strings.ToUpper(firstAntraString(raw, "isrc")),
	}
	if hit.DurationMS == 0 {
		if seconds := firstAntraInt(raw, "duration"); seconds > 0 && seconds < 36000 {
			hit.DurationMS = seconds * 1000
		}
	}
	if nested, ok := raw["album"].(map[string]any); ok && hit.Album == "" {
		hit.Album = firstAntraString(nested, "title", "name")
	}
	if nested, ok := raw["artist"].(map[string]any); ok && hit.Artist == "" {
		hit.Artist = firstAntraString(nested, "name")
	}
	return hit, nil
}

func pickAntraSearchHit(query string, body []byte) (antraSearchHit, error) {
	hits := parseAntraSearchList(body)
	if len(hits) == 0 {
		return antraSearchHit{}, fmt.Errorf("returned no track")
	}
	best := hits[0]
	bestScore := antraHitScore(query, best)
	for _, hit := range hits[1:] {
		if score := antraHitScore(query, hit); score > bestScore {
			best = hit
			bestScore = score
		}
	}
	return best, nil
}

func parseAntraSearchList(body []byte) []antraSearchHit {
	var hits []antraSearchHit
	seen := map[string]struct{}{}
	add := func(hit antraSearchHit) {
		if hit.TrackID == "" {
			return
		}
		if _, ok := seen[hit.TrackID]; ok {
			return
		}
		seen[hit.TrackID] = struct{}{}
		hits = append(hits, hit)
	}
	if hit, err := parseAntraSearchHit(body); err == nil {
		add(hit)
	}
	var payload struct {
		Results []json.RawMessage `json:"results"`
		Items   []json.RawMessage `json:"items"`
		Tracks  []json.RawMessage `json:"tracks"`
		Data    struct {
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return hits
	}
	raws := append(append(append([]json.RawMessage{}, payload.Results...), payload.Items...), payload.Tracks...)
	raws = append(raws, payload.Data.Items...)
	for _, raw := range raws {
		hit, err := parseAntraSearchHit(raw)
		if err == nil {
			add(hit)
		}
	}
	return hits
}

func antraHitScore(query string, hit antraSearchHit) int {
	q := antraTokens(query)
	score := 0
	titleMatched := 0
	for token := range antraTokens(hit.Title) {
		if _, ok := q[token]; ok {
			score += 3
			titleMatched++
		}
	}
	if titleMatched == 0 {
		return 0
	}
	artistTokens := antraTokens(hit.Artist)
	artistSubset := len(artistTokens) > 0
	for token := range artistTokens {
		if _, ok := q[token]; ok {
			score += 2
		} else {
			artistSubset = false
		}
	}
	if artistSubset {
		score += 2
	}
	return score
}

func antraTokens(value string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		switch part {
		case "", "the", "a", "of", "and":
			continue
		}
		out[part] = struct{}{}
	}
	return out
}

func antraGetTrackInfo(service, trackID string) (antraTrackInfo, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return antraTrackInfo{}, fmt.Errorf("empty track id")
	}
	resp, err := antraMirrorGet(service, "/api/track/"+url.PathEscape(trackID), nil, 30*time.Second)
	if err != nil {
		return antraTrackInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return antraTrackInfo{}, fmt.Errorf("%s track HTTP %d", service, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return antraTrackInfo{}, err
	}
	info, err := parseAntraTrackPayload(body)
	if err != nil {
		return antraTrackInfo{}, err
	}
	if info.TrackID == "" {
		info.TrackID = trackID
	}
	return info, nil
}

func parseAntraTrackPayload(body []byte) (antraTrackInfo, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return antraTrackInfo{}, err
	}
	return antraTrackInfo{
		TrackID:       firstAntraString(raw, "track_id", "trackId", "id", "asin"),
		StreamURL:     firstAntraString(raw, "streamUrl", "stream_url", "url"),
		DecryptionKey: firstAntraString(raw, "decryptionKey", "decryption_key"),
		Codec:         strings.ToLower(firstAntraString(raw, "codec")),
		BitDepth:      firstAntraInt(raw, "bitDepth", "bit_depth"),
		SampleRate:    firstAntraInt(raw, "sampleRate", "sample_rate"),
	}, nil
}

func antraResolveSpotify(service, spotifyID string) (string, error) {
	spotifyID = strings.TrimSpace(spotifyID)
	if spotifyID == "" {
		return "", fmt.Errorf("empty spotify id")
	}
	resp, err := antraMirrorGet(service, "/api/resolve/spotify/"+url.PathEscape(spotifyID), nil, 45*time.Second)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s resolve HTTP %d", service, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	var raw map[string]any
	if json.Unmarshal(body, &raw) == nil {
		id := firstAntraString(raw, "track_id", "trackId", "id", "asin", "apple_id")
		if id != "" {
			return id, nil
		}
	}
	text := strings.Trim(strings.TrimSpace(string(body)), "\"")
	if text != "" && !strings.HasPrefix(text, "{") {
		return text, nil
	}
	return "", fmt.Errorf("%s resolve returned no id", service)
}

func antraQualityQuery(service, quality string) url.Values {
	query := url.Values{}
	quality = strings.TrimSpace(quality)
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "tidal":
		if quality == "" {
			return query
		}
		switch mapTidalQualityToCommunity(quality) {
		case "atmos":
			query.Set("format", "atmos")
		case "24":
			query.Set("strict_24", "1")
		default:
			query.Set("prefer_16", "1")
		}
	case "qobuz":
		switch quality {
		case "27", "7", "24":
			query.Set("strict_24", "1")
		}
	case "amazon":
		if amazonCommunityNormalizeQuality(quality) == "atmos" {
			query.Set("format", "atmos")
		}
	}
	return query
}

func antraStreamToFile(service, trackID, destPath string, query url.Values) (string, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return "", fmt.Errorf("empty track id")
	}
	if strings.EqualFold(strings.TrimSpace(service), "amazon") {
		return antraAmazonTrackToFile(trackID, destPath, query)
	}
	partPath := destPath + ".part"
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil && filepath.Dir(destPath) != "." {
		return "", err
	}
	var resp *http.Response
	var pw *ProgressWriter
	var lastErr error
	for attempt := 1; attempt <= downloadRetryLimit; attempt++ {
		if attempt > 1 {
			fmt.Printf("%s stream stalled, retrying (%d/%d): %v\n", service, attempt, downloadRetryLimit, lastErr)
			if sleepErr := SleepWithDownloadContext(time.Duration(attempt-1) * time.Second); sleepErr != nil {
				return "", sleepErr
			}
		}
		resp, lastErr = antraMirrorGet(service, "/api/stream/"+url.PathEscape(trackID), query, 4*time.Minute)
		if lastErr != nil {
			if IsDownloadCancelledError(lastErr) || !retryableDownloadError(lastErr) {
				return "", lastErr
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			detail := ""
			if resp.StatusCode == http.StatusConflict {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
				detail = antraRedactDetail(string(body))
			}
			resp.Body.Close()
			if detail != "" {
				return "", fmt.Errorf("%s stream HTTP %d: %s", service, resp.StatusCode, detail)
			}
			return "", fmt.Errorf("%s stream HTTP %d", service, resp.StatusCode)
		}
		if antraContentTypeIsJSON(resp.Header.Get("Content-Type")) {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			resp.Body.Close()
			if readErr != nil {
				return "", readErr
			}
			info, parseErr := parseAntraTrackPayload(body)
			if parseErr != nil || info.StreamURL == "" {
				return "", fmt.Errorf("%s stream returned JSON, not audio", service)
			}
			return antraMaterializeStream(info, destPath)
		}
		out, createErr := os.Create(partPath)
		if createErr != nil {
			resp.Body.Close()
			return "", createErr
		}
		pw = NewProgressWriter(out)
		_, copyErr := copyDownloadBody(pw, resp.Body)
		closeErr := out.Close()
		if copyErr != nil {
			_ = os.Remove(partPath)
			lastErr = copyErr
			if IsDownloadCancelledError(copyErr) || !retryableDownloadError(copyErr) || attempt == downloadRetryLimit {
				return "", copyErr
			}
			continue
		}
		if closeErr != nil {
			_ = os.Remove(partPath)
			return "", closeErr
		}
		lastErr = nil
		break
	}
	if lastErr != nil || resp == nil || pw == nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("%s stream failed", service)
		}
		return "", lastErr
	}
	defer resp.Body.Close()

	ext := extensionFromContentType(resp.Header.Get("Content-Type"))
	if sniff, sniffErr := sniffAudioExtension(partPath); sniffErr == nil && sniff != "" {
		ext = sniff
	}
	if ext == "" {
		ext = filepath.Ext(destPath)
	}
	finalPath := replaceAudioExtension(destPath, ext)
	if err := os.Rename(partPath, finalPath); err != nil {
		_ = os.Remove(partPath)
		return "", err
	}
	fmt.Printf("Downloaded via %s mirror: %s (%.2f MB)\n", service, filepath.Base(finalPath), float64(pw.GetTotal())/(1024*1024))
	return finalPath, nil
}

func antraAmazonTrackToFile(trackID, destPath string, query url.Values) (string, error) {
	resp, err := antraMirrorGet("amazon", "/api/track/"+url.PathEscape(trackID), query, 45*time.Second)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("amazon track HTTP %d", resp.StatusCode)
	}
	if !antraContentTypeIsJSON(resp.Header.Get("Content-Type")) {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("amazon track returned no json metadata")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if antraBodyLooksAudio(body, resp.Header.Get("Content-Type")) {
		return "", fmt.Errorf("amazon track returned no json metadata")
	}
	info, err := parseAntraTrackPayload(body)
	if err != nil || info.StreamURL == "" {
		return "", fmt.Errorf("amazon track returned no stream url")
	}
	if info.TrackID == "" {
		info.TrackID = trackID
	}
	atmos := query != nil && strings.EqualFold(query.Get("format"), "atmos")
	return antraMaterializeAmazon(info, destPath, atmos)
}

func antraAmazonStreamInfo(rawURL, quality string) (antraTrackInfo, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return antraTrackInfo{}, fmt.Errorf("empty amazon source")
	}
	lower := strings.ToLower(rawURL)
	if strings.Contains(lower, "spotify.com") || strings.Contains(lower, "music.apple.com") {
		query := url.Values{}
		query.Set("url", rawURL)
		return antraReadMirrorTrack("amazon", "/api/resolve", query)
	}
	asin := regexpAmazonASIN.FindString(strings.ToUpper(rawURL))
	if asin == "" {
		return antraTrackInfo{}, fmt.Errorf("failed to extract Amazon ASIN")
	}
	query := url.Values{}
	if amazonCommunityNormalizeQuality(quality) == "atmos" {
		query.Set("format", "atmos")
	}
	info, err := antraReadMirrorTrack("amazon", "/api/track/"+url.PathEscape(asin), query)
	if err != nil {
		return antraTrackInfo{}, err
	}
	if info.TrackID == "" {
		info.TrackID = asin
	}
	return info, nil
}

func antraReadMirrorTrack(service, path string, query url.Values) (antraTrackInfo, error) {
	resp, err := antraMirrorGet(service, path, query, 45*time.Second)
	if err != nil {
		return antraTrackInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return antraTrackInfo{}, fmt.Errorf("%s track HTTP %d", service, resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if antraContentTypeIsAudio(contentType) {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return antraTrackInfo{}, fmt.Errorf("%s track returned no json metadata", service)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return antraTrackInfo{}, err
	}
	if antraBodyLooksAudio(body, contentType) {
		return antraTrackInfo{}, fmt.Errorf("%s track returned no json metadata", service)
	}
	info, err := parseAntraTrackPayload(body)
	if err != nil {
		return antraTrackInfo{}, err
	}
	if info.StreamURL == "" {
		return antraTrackInfo{}, fmt.Errorf("%s track returned no stream url", service)
	}
	return info, nil
}

func antraMaterializeStream(info antraTrackInfo, destPath string) (string, error) {
	return antraMaterializeAmazon(info, destPath, false)
}

func antraAmazonCodecIsSpatial(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "eac3", "ec-3", "ec3", "ac-3", "ac3", "ac4", "ac-4", "atmos":
		return true
	default:
		return false
	}
}

func antraMaterializeAmazon(info antraTrackInfo, destPath string, atmos bool) (string, error) {
	if atmos && !antraAmazonCodecIsSpatial(info.Codec) {
		return "", fmt.Errorf("amazon mirror did not return atmos")
	}
	downloaded, err := antraDownloadURLToFile(info.StreamURL, destPath)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(info.DecryptionKey) == "" {
		return downloaded, nil
	}
	decryptedPath := downloaded + ".decrypted.mp4"
	if err := antraAmazonDecrypt([]string{info.DecryptionKey}, downloaded, decryptedPath); err != nil {
		_ = os.Remove(downloaded)
		_ = os.Remove(decryptedPath)
		return "", fmt.Errorf("protected stream was not readable audio")
	}
	_ = os.Remove(downloaded)
	targetExt := ".flac"
	codec := strings.ToLower(strings.TrimSpace(info.Codec))
	if atmos || codec == "eac3" || codec == "ec-3" || codec == "ac-3" {
		targetExt = ".m4a"
	}
	finalPath := replaceAudioExtension(destPath, targetExt)
	if err := antraAmazonRemux(decryptedPath, finalPath, targetExt); err != nil {
		_ = os.Remove(decryptedPath)
		_ = os.Remove(finalPath)
		return "", err
	}
	_ = os.Remove(decryptedPath)
	infoStat, statErr := os.Stat(finalPath)
	if statErr != nil || infoStat.Size() == 0 {
		_ = os.Remove(finalPath)
		return "", fmt.Errorf("remuxed file missing or empty")
	}
	return finalPath, nil
}

func antraContentTypeIsJSON(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "json")
}

func antraContentTypeIsAudio(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "audio") || strings.Contains(ct, "octet-stream") || strings.Contains(ct, "mp4")
}

func antraBodyLooksAudio(body []byte, contentType string) bool {
	if len(body) == 0 || body[0] == '{' || body[0] == '[' {
		return false
	}
	if antraContentTypeIsAudio(contentType) {
		return true
	}
	if len(body) >= 4 && string(body[:4]) == "fLaC" {
		return true
	}
	if len(body) >= 8 && string(body[4:8]) == "ftyp" {
		return true
	}
	return len(body) >= 3 && string(body[:3]) == "ID3"
}

func antraRedactDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	var b strings.Builder
	for _, field := range strings.Fields(detail) {
		if strings.Contains(field, "://") {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(field)
		if b.Len() > 180 {
			break
		}
	}
	return b.String()
}

func antraDownloadURLToFile(downloadURL, destPath string) (string, error) {
	downloadURL = strings.TrimSpace(downloadURL)
	if downloadURL == "" {
		return "", fmt.Errorf("empty stream url")
	}
	req, err := NewRequestWithDefaultHeaders(http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	req = WithDownloadContext(req)
	client := newMediaHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", WrapDownloadCancelled(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stream HTTP %d", resp.StatusCode)
	}
	partPath := destPath + ".part"
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil && filepath.Dir(destPath) != "." {
		return "", err
	}
	out, err := os.Create(partPath)
	if err != nil {
		return "", err
	}
	pw := NewProgressWriter(out)
	_, copyErr := copyDownloadBody(pw, resp.Body)
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
		ext = filepath.Ext(destPath)
		if ext == "" {
			ext = ".flac"
		}
	}
	finalPath := replaceAudioExtension(destPath, ext)
	if err := os.Rename(partPath, finalPath); err != nil {
		_ = os.Remove(partPath)
		return "", err
	}
	return finalPath, nil
}

func firstAntraString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := raw[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		case float64:
			if value != 0 {
				return strconv.FormatInt(int64(value), 10)
			}
		case json.Number:
			if s := strings.TrimSpace(value.String()); s != "" && s != "0" {
				return s
			}
		}
	}
	return ""
}

func firstAntraInt(raw map[string]any, keys ...string) int {
	for _, key := range keys {
		switch value := raw[key].(type) {
		case float64:
			return int(value)
		case json.Number:
			n, _ := value.Int64()
			return int(n)
		case string:
			n, _ := strconv.Atoi(strings.TrimSpace(value))
			return n
		}
	}
	return 0
}

func extensionFromContentType(contentType string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "flac"):
		return ".flac"
	case strings.Contains(ct, "mpeg") || strings.Contains(ct, "mp3"):
		return ".mp3"
	case strings.Contains(ct, "mp4") || strings.Contains(ct, "m4a") || strings.Contains(ct, "aac"):
		return ".m4a"
	default:
		return ""
	}
}

func sniffAudioExtension(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	header := make([]byte, 12)
	n, err := f.Read(header)
	if err != nil && n == 0 {
		return "", err
	}
	header = header[:n]
	switch {
	case n >= 4 && string(header[:4]) == "fLaC":
		return ".flac", nil
	case n >= 8 && string(header[4:8]) == "ftyp":
		return ".m4a", nil
	case n >= 3 && string(header[:3]) == "ID3":
		return ".mp3", nil
	case n >= 2 && header[0] == 0xff && (header[1]&0xe0) == 0xe0:
		return ".mp3", nil
	default:
		return "", nil
	}
}

func replaceAudioExtension(path, ext string) string {
	if ext == "" {
		return path
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	current := filepath.Ext(path)
	if current == "" {
		return path + ext
	}
	return strings.TrimSuffix(path, current) + ext
}

func AntraMirrorHealth(service string) bool {
	resp, err := antraMirrorGet(service, "/", nil, 8*time.Second)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
