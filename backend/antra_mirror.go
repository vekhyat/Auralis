package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	req = req.WithContext(ActiveDownloadContext())
	req.Header.Set("Accept", "application/json, audio/*, */*")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	client := &http.Client{Timeout: timeout}
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
	isrc = strings.ToUpper(strings.TrimSpace(isrc))
	if isrc == "" {
		return antraSearchHit{}, fmt.Errorf("empty ISRC")
	}
	resp, err := antraMirrorGet(service, "/api/search/isrc/"+url.PathEscape(isrc), nil, 15*time.Second)
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
	query = strings.TrimSpace(query)
	if query == "" {
		return antraSearchHit{}, fmt.Errorf("empty search query")
	}
	resp, err := antraMirrorGet(service, "/api/search", url.Values{"q": {query}}, 15*time.Second)
	if err != nil {
		return antraSearchHit{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return antraSearchHit{}, fmt.Errorf("%s text search HTTP %d", service, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return antraSearchHit{}, err
	}
	if hit, err := parseAntraSearchHit(body); err == nil && hit.TrackID != "" {
		return hit, nil
	}
	var payload struct {
		Results []json.RawMessage `json:"results"`
		Items   []json.RawMessage `json:"items"`
		Tracks  []json.RawMessage `json:"tracks"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return antraSearchHit{}, fmt.Errorf("%s text search: %w", service, err)
	}
	for _, raw := range append(append(payload.Results, payload.Items...), payload.Tracks...) {
		hit, err := parseAntraSearchHit(raw)
		if err == nil && hit.TrackID != "" {
			return hit, nil
		}
	}
	return antraSearchHit{}, fmt.Errorf("%s text search returned no track", service)
}

func parseAntraSearchHit(body []byte) (antraSearchHit, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return antraSearchHit{}, err
	}
	hit := antraSearchHit{
		TrackID:    firstAntraString(raw, "track_id", "trackId", "id"),
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
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return antraTrackInfo{}, err
	}
	info := antraTrackInfo{
		TrackID:       firstAntraString(raw, "track_id", "trackId", "id", "asin"),
		StreamURL:     firstAntraString(raw, "streamUrl", "stream_url", "url"),
		DecryptionKey: firstAntraString(raw, "decryptionKey", "decryption_key", "key"),
		Codec:         strings.ToLower(firstAntraString(raw, "codec")),
		BitDepth:      firstAntraInt(raw, "bitDepth", "bit_depth"),
		SampleRate:    firstAntraInt(raw, "sampleRate", "sample_rate"),
	}
	if info.TrackID == "" {
		info.TrackID = trackID
	}
	return info, nil
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
	if quality == "" {
		return query
	}
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "tidal":
		query.Set("quality", mapTidalQualityToCommunity(quality))
	case "qobuz":
		query.Set("quality", quality)
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
	resp, err := antraMirrorGet(service, "/api/stream/"+url.PathEscape(trackID), query, 4*time.Minute)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s stream HTTP %d", service, resp.StatusCode)
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
	_, copyErr := io.Copy(pw, resp.Body)
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
	}
	finalPath := replaceAudioExtension(destPath, ext)
	if err := os.Rename(partPath, finalPath); err != nil {
		_ = os.Remove(partPath)
		return "", err
	}
	fmt.Printf("Downloaded via %s mirror: %s (%.2f MB)\n", service, filepath.Base(finalPath), float64(pw.GetTotal())/(1024*1024))
	return finalPath, nil
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
	req = req.WithContext(ActiveDownloadContext())
	client := &http.Client{Timeout: 4 * time.Minute}
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
	_, copyErr := io.Copy(pw, resp.Body)
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
