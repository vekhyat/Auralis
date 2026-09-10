package backend

import (
	"crypto/des"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
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

const (
	jioSaavnOfficialAPI  = "https://www.jiosaavn.com/api.php"
	jioSaavnCommunityAPI = "https://saavn.dev/api"
)

var jioSaavnFeatRE = regexp.MustCompile(`(?i)\s*[\(\[](feat\.?|ft\.?|with|featuring)\s[^\)\]]+[\)\]]`)

func downloadJioSaavnTrack(p ExtraDownloadParams, destPath string) (string, string, error) {
	songID, sourceURL, err := searchJioSaavn(p)
	if err != nil {
		return "", "", err
	}
	streamURL, err := jioSaavnStreamURL(songID)
	if err != nil {
		return "", sourceURL, err
	}
	path, err := antraDownloadURLToFile(streamURL, destPath)
	if err != nil {
		return "", sourceURL, err
	}
	if ext := filepath.Ext(path); ext == ".flac" && looksLikeLossyURL(streamURL) {
		renamed := replaceAudioExtension(path, inferJioSaavnExt(streamURL))
		if renamed != path {
			if renameErr := os.Rename(path, renamed); renameErr == nil {
				path = renamed
			}
		}
	}
	return path, sourceURL, nil
}

func searchJioSaavn(p ExtraDownloadParams) (songID, sourceURL string, err error) {
	queries := jioSaavnQueries(p.TrackName, p.ArtistName)
	var lastErr error
	for _, query := range queries {
		if id, src, searchErr := jioSaavnOfficialSearch(query); searchErr == nil && id != "" {
			return id, src, nil
		} else if searchErr != nil {
			lastErr = searchErr
		}
		if id, src, searchErr := jioSaavnCommunitySearch(query); searchErr == nil && id != "" {
			return id, src, nil
		} else if searchErr != nil {
			lastErr = searchErr
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("jiosaavn: no match")
	}
	return "", "", lastErr
}

func jioSaavnQueries(title, artist string) []string {
	title = strings.TrimSpace(title)
	artist = GetFirstArtist(artist)
	variants := []string{title}
	stripped := strings.TrimSpace(jioSaavnFeatRE.ReplaceAllString(title, " "))
	stripped = regexp.MustCompile(`\s*[\(\[].*?[\)\]]\s*`).ReplaceAllString(stripped, " ")
	stripped = strings.Join(strings.Fields(stripped), " ")
	if stripped != "" && !strings.EqualFold(stripped, title) {
		variants = append(variants, stripped)
	}
	var queries []string
	seen := map[string]struct{}{}
	add := func(q string) {
		q = strings.TrimSpace(q)
		if q == "" {
			return
		}
		key := strings.ToLower(q)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		queries = append(queries, q)
	}
	for _, v := range variants {
		add(strings.TrimSpace(v + " " + artist))
		add(v)
	}
	return queries
}

func jioSaavnOfficialSearch(query string) (string, string, error) {
	params := url.Values{
		"__call":          {"autocomplete.get"},
		"_format":         {"json"},
		"_marker":         {"0"},
		"cc":              {"in"},
		"includeMetaTags": {"1"},
		"query":           {query},
	}
	req, err := NewRequestWithDefaultHeaders(http.MethodGet, jioSaavnOfficialAPI+"?"+params.Encode(), nil)
	if err != nil {
		return "", "", err
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("jiosaavn search HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", "", err
	}
	var payload struct {
		Songs struct {
			Data []map[string]any `json:"data"`
		} `json:"songs"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", err
	}
	if len(payload.Songs.Data) == 0 {
		return "", "", fmt.Errorf("jiosaavn: empty official results")
	}
	return jioSaavnResultFromList(payload.Songs.Data, query)
}

func jioSaavnCommunitySearch(query string) (string, string, error) {
	req, err := NewRequestWithDefaultHeaders(http.MethodGet, jioSaavnCommunityAPI+"/search/songs?query="+url.QueryEscape(query)+"&page=1&limit=8", nil)
	if err != nil {
		return "", "", err
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("saavn.dev HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", "", err
	}
	var payload struct {
		Data struct {
			Results []map[string]any `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", err
	}
	if len(payload.Data.Results) == 0 {
		return "", "", fmt.Errorf("jiosaavn: empty community results")
	}
	return jioSaavnResultFromList(payload.Data.Results, query)
}

func jioSaavnStreamURL(songID string) (string, error) {
	if url, err := jioSaavnOfficialStreamURL(songID); err == nil && url != "" {
		return url, nil
	}
	return jioSaavnCommunityStreamURL(songID)
}

func jioSaavnOfficialStreamURL(songID string) (string, error) {
	params := url.Values{
		"__call":  {"song.getDetails"},
		"cc":      {"in"},
		"_format": {"json"},
		"_marker": {"0"},
		"pids":    {songID},
	}
	req, err := NewRequestWithDefaultHeaders(http.MethodGet, jioSaavnOfficialAPI+"?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	song := jioSaavnExtractSong(payload, songID)
	if song == nil {
		return "", fmt.Errorf("jiosaavn: song details missing")
	}
	if media := jioSaavnString(song, "media_url"); media != "" {
		return normalizeJioSaavnMediaURL(media), nil
	}
	enc := jioSaavnString(song, "encrypted_media_url")
	if enc == "" {
		enc = jioSaavnString(song, "encrypted_drm_media_url")
	}
	if enc == "" {
		return "", fmt.Errorf("jiosaavn: no media url")
	}
	decrypted, err := decryptJioSaavnURL(enc)
	if err != nil {
		return "", err
	}
	return normalizeJioSaavnMediaURL(decrypted), nil
}

func jioSaavnCommunityStreamURL(songID string) (string, error) {
	req, err := NewRequestWithDefaultHeaders(http.MethodGet, jioSaavnCommunityAPI+"/songs/"+url.PathEscape(songID), nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	var payload struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	var songs []map[string]any
	if json.Unmarshal(payload.Data, &songs) != nil {
		var one map[string]any
		if json.Unmarshal(payload.Data, &one) == nil {
			songs = []map[string]any{one}
		}
	}
	if len(songs) == 0 {
		return "", fmt.Errorf("jiosaavn: community song missing")
	}
	rawURLs, _ := songs[0]["downloadUrl"].([]any)
	var last string
	for _, entry := range rawURLs {
		item, _ := entry.(map[string]any)
		u, _ := item["url"].(string)
		if u != "" {
			last = u
			if q, _ := item["quality"].(string); q == "320kbps" {
				return strings.ReplaceAll(u, "http://", "https://"), nil
			}
		}
	}
	if last != "" {
		return strings.ReplaceAll(last, "http://", "https://"), nil
	}
	return "", fmt.Errorf("jiosaavn: no community download url")
}

func jioSaavnExtractSong(payload map[string]any, songID string) map[string]any {
	if direct, ok := payload[songID].(map[string]any); ok {
		return direct
	}
	if songs, ok := payload["songs"].([]any); ok && len(songs) > 0 {
		if song, ok := songs[0].(map[string]any); ok {
			return song
		}
	}
	return nil
}

func decryptJioSaavnURL(enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(enc))
	if err != nil {
		return "", err
	}
	block, err := des.NewCipher([]byte("38346591"))
	if err != nil {
		return "", err
	}
	if len(raw)%des.BlockSize != 0 {
		return "", fmt.Errorf("jiosaavn: encrypted url length")
	}
	out := make([]byte, len(raw))
	for i := 0; i < len(raw); i += des.BlockSize {
		block.Decrypt(out[i:i+des.BlockSize], raw[i:i+des.BlockSize])
	}
	out = pkcs7Unpad(out)
	decoded := strings.TrimSpace(string(out))
	decoded = strings.ReplaceAll(decoded, "http://", "https://")
	if decoded == "" {
		return "", fmt.Errorf("jiosaavn: decrypted url empty")
	}
	return decoded, nil
}

func pkcs7Unpad(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	pad := int(data[len(data)-1])
	if pad <= 0 || pad > len(data) {
		return bytesTrimNull(data)
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return bytesTrimNull(data)
		}
	}
	return data[:len(data)-pad]
}

func bytesTrimNull(data []byte) []byte {
	i := len(data)
	for i > 0 && data[i-1] == 0 {
		i--
	}
	return data[:i]
}

func normalizeJioSaavnMediaURL(raw string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), "http://", "https://")
	for _, quality := range []string{"12", "48", "96", "160"} {
		normalized = strings.ReplaceAll(normalized, "_"+quality+".mp4", "_320.mp4")
		normalized = strings.ReplaceAll(normalized, "_"+quality+".mp3", "_320.mp3")
		normalized = strings.ReplaceAll(normalized, "_"+quality+".m4a", "_320.m4a")
	}
	return normalized
}

func inferJioSaavnExt(streamURL string) string {
	lower := strings.ToLower(streamURL)
	switch {
	case strings.Contains(lower, ".m4a"):
		return ".m4a"
	case strings.Contains(lower, ".mp3"):
		return ".mp3"
	default:
		return ".m4a"
	}
}

func looksLikeLossyURL(streamURL string) bool {
	lower := strings.ToLower(streamURL)
	return strings.Contains(lower, ".m4a") || strings.Contains(lower, ".mp3") || strings.Contains(lower, ".mp4")
}

func jioSaavnResultFromList(items []map[string]any, query string) (string, string, error) {
	best, score := jioSaavnPickBest(items, query)
	if best == nil || score <= 0 {
		return "", "", fmt.Errorf("jiosaavn: no close match")
	}
	id := jioSaavnString(best, "id")
	if id == "" {
		return "", "", fmt.Errorf("jiosaavn: missing song id")
	}
	return id, "https://www.jiosaavn.com/song/" + id, nil
}

func jioSaavnPickBest(items []map[string]any, query string) (map[string]any, int) {
	want := strings.ToLower(strings.TrimSpace(query))
	if want == "" {
		return nil, 0
	}
	var best map[string]any
	bestScore := 0
	for _, item := range items {
		score := jioSaavnScore(item, want)
		if score > bestScore {
			bestScore = score
			best = item
		}
	}
	return best, bestScore
}

func jioSaavnScore(item map[string]any, query string) int {
	title := strings.ToLower(jioSaavnString(item, "title"))
	if title == "" {
		title = strings.ToLower(jioSaavnString(item, "name"))
	}
	artist := strings.ToLower(strings.Join([]string{
		jioSaavnString(item, "primaryArtists"),
		jioSaavnString(item, "singers"),
		jioSaavnString(item, "subtitle"),
		jioSaavnString(item, "artist"),
	}, " "))
	if more, ok := item["more_info"].(map[string]any); ok {
		artist += " " + strings.ToLower(jioSaavnString(more, "singers"))
	}
	haystack := strings.TrimSpace(title + " " + artist)
	if haystack == "" {
		return 0
	}
	score := 0
	for _, token := range strings.Fields(query) {
		if len(token) < 2 {
			continue
		}
		if strings.Contains(haystack, token) {
			score += 2
		}
		if strings.Contains(title, token) {
			score += 3
		}
	}
	if title != "" && strings.Contains(query, title) {
		score += 4
	}
	return score
}

func jioSaavnString(item map[string]any, key string) string {
	value, _ := item[key].(string)
	value = html.UnescapeString(strings.TrimSpace(value))
	if value != "" {
		return value
	}
	switch n := item[key].(type) {
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case json.Number:
		return n.String()
	}
	return ""
}

func JioSaavnHealth() bool {
	req, err := NewRequestWithDefaultHeaders(http.MethodGet, jioSaavnOfficialAPI+"?__call=autocomplete.get&_format=json&query=test", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}
