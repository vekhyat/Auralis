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
	"unicode"

	"golang.org/x/text/unicode/norm"
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
	if err := CheckDownloadCancelled(); err != nil {
		return "", "", err
	}
	want := jioSaavnWantFrom(p)
	queries := jioSaavnQueries(p.TrackName, p.ArtistName)
	var lastErr error
	for _, query := range queries {
		if err := CheckDownloadCancelled(); err != nil {
			return "", "", err
		}
		if id, src, searchErr := jioSaavnOfficialSearch(query, want); searchErr == nil && id != "" {
			return id, src, nil
		} else if IsDownloadCancelledError(searchErr) {
			return "", "", searchErr
		} else if searchErr != nil {
			lastErr = searchErr
		}
		if err := CheckDownloadCancelled(); err != nil {
			return "", "", err
		}
		if id, src, searchErr := jioSaavnCommunitySearch(query, want); searchErr == nil && id != "" {
			return id, src, nil
		} else if IsDownloadCancelledError(searchErr) {
			return "", "", searchErr
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

func jioSaavnOfficialSearch(query string, want jioSaavnWant) (string, string, error) {
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
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(WithDownloadContext(req))
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
	return jioSaavnResultFromList(payload.Songs.Data, want)
}

func jioSaavnCommunitySearch(query string, want jioSaavnWant) (string, string, error) {
	req, err := NewRequestWithDefaultHeaders(http.MethodGet, jioSaavnCommunityAPI+"/search/songs?query="+url.QueryEscape(query)+"&page=1&limit=8", nil)
	if err != nil {
		return "", "", err
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(WithDownloadContext(req))
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
	return jioSaavnResultFromList(payload.Data.Results, want)
}

func jioSaavnStreamURL(songID string) (string, error) {
	var lastErr error
	for _, id := range RankedSourceIDs([]string{"jiosaavn-official", "jiosaavn-community"}, "lossy") {
		var raw string
		var err error
		if id == "jiosaavn-official" {
			raw, err = jioSaavnOfficialStreamURL(songID)
		} else {
			raw, err = jioSaavnCommunityStreamURL(songID)
		}
		if err == nil && raw != "" {
			return raw, nil
		}
		if IsDownloadCancelledError(err) {
			return "", err
		}
		lastErr = err
	}
	return "", lastErr
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
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(WithDownloadContext(req))
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
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(WithDownloadContext(req))
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

// jioSaavnWant is the caller's original request. Search queries may drop the
// artist or a version suffix; selection still has to satisfy this request.
type jioSaavnWant struct {
	Title    string
	Artist   string
	Album    string
	ISRC     string
	Duration int
}

func jioSaavnWantFrom(p ExtraDownloadParams) jioSaavnWant {
	return jioSaavnWant{
		Title:    p.TrackName,
		Artist:   p.ArtistName,
		Album:    p.AlbumName,
		ISRC:     p.ISRC,
		Duration: p.DurationSeconds,
	}
}

func jioSaavnResultFromList(items []map[string]any, want jioSaavnWant) (string, string, error) {
	best, score := jioSaavnPickBest(items, want)
	if best == nil || score <= 0 {
		return "", "", fmt.Errorf("jiosaavn: no close match")
	}
	id := jioSaavnString(best, "id")
	if id == "" {
		return "", "", fmt.Errorf("jiosaavn: missing song id")
	}
	return id, "https://www.jiosaavn.com/song/" + id, nil
}

func jioSaavnPickBest(items []map[string]any, want jioSaavnWant) (map[string]any, int) {
	if strings.TrimSpace(want.Title) == "" {
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

func jioSaavnScore(item map[string]any, want jioSaavnWant) int {
	titleScore, titleOK := jioSaavnTitleScore(item, want.Title)
	if !titleOK {
		return 0
	}
	artistScore, artistOK := jioSaavnArtistScore(item, want.Artist)
	if strings.TrimSpace(want.Artist) != "" && !artistOK {
		return 0
	}
	if jioSaavnDisallowedVariant(item, want) && !jioSaavnISRCMatches(item, want.ISRC) {
		return 0
	}
	score := titleScore + artistScore
	score += jioSaavnISRCScore(item, want.ISRC)
	score += jioSaavnDurationScore(item, want.Duration)
	score += jioSaavnAlbumScore(item, want.Album)
	if score < 1 {
		return 0
	}
	return score
}

func jioSaavnTitleScore(item map[string]any, wantTitle string) (int, bool) {
	got := jioSaavnItemTitle(item)
	wantTitle = strings.TrimSpace(wantTitle)
	if wantTitle == "" || strings.TrimSpace(got) == "" {
		return 0, false
	}
	if jioSaavnExactTitleKey(wantTitle) == jioSaavnExactTitleKey(got) {
		return 1000, true
	}
	wantBase := jioSaavnBaseTitleKey(wantTitle)
	gotBase := jioSaavnBaseTitleKey(got)
	if wantBase != "" && wantBase == gotBase {
		return 700, true
	}
	return 0, false
}

func jioSaavnArtistScore(item map[string]any, wantArtist string) (int, bool) {
	wantArtist = strings.TrimSpace(wantArtist)
	if wantArtist == "" {
		return 0, true
	}
	primary, extra := jioSaavnArtistNames(item)
	all := append(append([]string{}, primary...), extra...)
	if len(all) == 0 {
		return 0, false
	}
	allKeys := jioSaavnArtistKeys(all)
	primaryKeys := jioSaavnArtistKeys(primary)
	if len(primaryKeys) == 0 {
		primaryKeys = allKeys
	}
	wantKey := jioSaavnMatchKey(wantArtist)
	if jioSaavnKeyHit(allKeys, wantKey) && jioSaavnKeyHit(primaryKeys, wantKey) {
		return 500, true
	}
	credits := jioSaavnCredits(wantArtist)
	if len(credits) == 0 {
		return 0, false
	}
	matched := 0
	for _, credit := range credits {
		if jioSaavnKeyHit(allKeys, jioSaavnMatchKey(credit)) {
			matched++
		}
	}
	first := jioSaavnMatchKey(credits[0])
	if matched == len(credits) && jioSaavnKeyHit(primaryKeys, first) {
		return 480, true
	}
	if len(credits) == 1 && jioSaavnKeyHit(primaryKeys, first) {
		return 500, true
	}
	return 0, false
}

func jioSaavnItemTitle(item map[string]any) string {
	for _, key := range []string{"title", "name", "song"} {
		if value := jioSaavnString(item, key); value != "" {
			return value
		}
	}
	return ""
}

func jioSaavnArtistNames(item map[string]any) (primary []string, extra []string) {
	if item == nil {
		return nil, nil
	}
	jioSaavnCollectNameList(&primary, item["primaryArtists"])
	jioSaavnCollectNameList(&primary, item["primary_artists"])
	jioSaavnCollectNameList(&extra, item["singers"])
	jioSaavnCollectNameList(&extra, item["featuredArtists"])
	jioSaavnCollectNameList(&extra, item["featured_artists"])
	jioSaavnCollectNameList(&extra, item["artist"])
	if subtitle := jioSaavnString(item, "subtitle"); subtitle != "" && !strings.EqualFold(subtitle, jioSaavnItemTitle(item)) {
		extra = append(extra, subtitle)
	}
	if artist, _ := jioSaavnDescriptionParts(jioSaavnString(item, "description")); artist != "" {
		primary = append(primary, artist)
	}
	if more, ok := item["more_info"].(map[string]any); ok {
		jioSaavnCollectNameList(&primary, more["primary_artists"])
		jioSaavnCollectNameList(&primary, more["primaryArtists"])
		jioSaavnCollectNameList(&extra, more["singers"])
		jioSaavnCollectNameList(&extra, more["featured_artists"])
		jioSaavnCollectNameList(&extra, more["featuredArtists"])
		jioSaavnCollectNestedArtists(&primary, &extra, more["artists"])
		jioSaavnCollectArtistIDMap(&extra, more["artistMap"])
	}
	jioSaavnCollectNestedArtists(&primary, &extra, item["artists"])
	jioSaavnCollectArtistIDMap(&extra, item["artistMap"])
	return primary, extra
}

func jioSaavnCollectNestedArtists(primary, extra *[]string, value any) {
	container, ok := value.(map[string]any)
	if !ok {
		return
	}
	jioSaavnCollectNameList(primary, container["primary"])
	jioSaavnCollectNameList(primary, container["primary_artists"])
	jioSaavnCollectNameList(extra, container["featured"])
	jioSaavnCollectNameList(extra, container["featured_artists"])
	jioSaavnCollectNameList(extra, container["all"])
	jioSaavnCollectNameList(extra, container["artists"])
	jioSaavnCollectNameList(extra, container["singers"])
}

func jioSaavnCollectNameList(dst *[]string, value any) {
	switch typed := value.(type) {
	case string:
		*dst = jioSaavnAppendRaw(*dst, typed)
	case []any:
		for _, entry := range typed {
			switch item := entry.(type) {
			case string:
				*dst = jioSaavnAppendRaw(*dst, item)
			case map[string]any:
				if name := jioSaavnString(item, "name"); name != "" {
					*dst = jioSaavnAppendRaw(*dst, name)
				}
			}
		}
	case map[string]any:
		if name := jioSaavnString(typed, "name"); name != "" {
			*dst = jioSaavnAppendRaw(*dst, name)
		}
	}
}

func jioSaavnCollectArtistIDMap(dst *[]string, value any) {
	artists, ok := value.(map[string]any)
	if !ok {
		return
	}
	jioSaavnCollectNestedArtists(dst, dst, artists)
	for name, rawID := range artists {
		id, ok := rawID.(string)
		if !ok || !jioSaavnNumericID(id) || jioSaavnIgnoredArtistKey(name) {
			continue
		}
		*dst = jioSaavnAppendRaw(*dst, name)
	}
}

func jioSaavnAppendRaw(dst []string, raw string) []string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" {
		return dst
	}
	return append(dst, raw)
}

func jioSaavnDescriptionParts(desc string) (artist, album string) {
	desc = strings.TrimSpace(html.UnescapeString(desc))
	if desc == "" {
		return "", ""
	}
	for _, sep := range []string{" · ", " • ", " ·", "•"} {
		if idx := strings.Index(desc, sep); idx > 0 {
			return strings.TrimSpace(desc[:idx]), strings.TrimSpace(desc[idx+len(sep):])
		}
	}
	if idx := strings.IndexRune(desc, '·'); idx > 0 {
		return strings.TrimSpace(desc[:idx]), strings.TrimSpace(desc[idx+len("·"):])
	}
	return "", ""
}

func jioSaavnIgnoredArtistKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "id", "name", "role", "type", "url", "image", "primary", "featured", "all", "artists", "singers", "primary_artists", "featured_artists", "language", "album", "title", "song":
		return true
	default:
		return false
	}
}

func jioSaavnNumericID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 16 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func jioSaavnArtistKeys(names []string) map[string]struct{} {
	keys := make(map[string]struct{}, len(names))
	for _, name := range names {
		if key := jioSaavnMatchKey(name); key != "" {
			keys[key] = struct{}{}
		}
		for _, credit := range jioSaavnCredits(name) {
			if key := jioSaavnMatchKey(credit); key != "" {
				keys[key] = struct{}{}
			}
		}
	}
	return keys
}

func jioSaavnKeyHit(keys map[string]struct{}, key string) bool {
	if key == "" {
		return false
	}
	_, ok := keys[key]
	return ok
}

var jioSaavnCreditSplitRE = regexp.MustCompile(`(?i)\s*(?:,|&|/|;|·|•|\s-\s|\s+x\s+|\s+feat\.?\s+|\s+ft\.?\s+|\s+featuring\s+)\s*`)
var jioSaavnTrailingWrapRE = regexp.MustCompile(`\s*[(\[]([^)\]]+)[)\]]\s*$`)
var jioSaavnTrailingDashRE = regexp.MustCompile(`(?i)\s+[-–—:]\s+(.+)$`)
var jioSaavnYearRE = regexp.MustCompile(`^(19|20)\d{2}$`)

func jioSaavnCredits(value string) []string {
	value = strings.TrimSpace(html.UnescapeString(value))
	if value == "" {
		return nil
	}
	parts := jioSaavnCreditSplitRE.Split(value, -1)
	credits := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			credits = append(credits, part)
		}
	}
	return credits
}

func jioSaavnExactTitleKey(title string) string {
	title = jioSaavnFeatRE.ReplaceAllString(html.UnescapeString(title), " ")
	return strings.ReplaceAll(jioSaavnFoldSpaces(title), " ", "")
}

func jioSaavnBaseTitleKey(title string) string {
	return strings.ReplaceAll(jioSaavnFoldSpaces(jioSaavnStripVersion(title)), " ", "")
}

func jioSaavnStripVersion(title string) string {
	title = html.UnescapeString(title)
	title = jioSaavnFeatRE.ReplaceAllString(title, " ")
	title = strings.Join(strings.Fields(title), " ")
	for i := 0; i < 4; i++ {
		match := jioSaavnTrailingWrapRE.FindStringSubmatchIndex(title)
		if match == nil || !jioSaavnVersionInner(title[match[2]:match[3]]) {
			break
		}
		title = strings.TrimSpace(title[:match[0]])
	}
	if match := jioSaavnTrailingDashRE.FindStringSubmatchIndex(title); match != nil && jioSaavnVersionInner(title[match[2]:match[3]]) {
		title = strings.TrimSpace(title[:match[0]])
	}
	return strings.Join(strings.Fields(title), " ")
}

func jioSaavnVersionInner(inner string) bool {
	inner = strings.ToLower(strings.TrimSpace(inner))
	if inner == "" {
		return false
	}
	for _, blocked := range []string{"karaoke", "tribute", "cover", "instrumental", "lullaby", "made famous", "style of", "originally performed"} {
		if strings.Contains(inner, blocked) {
			return false
		}
	}
	for _, allowed := range []string{"remaster", "mix", "mono", "stereo", "deluxe", "radio", "edit", "version", "live", "bonus", "explicit", "clean", "anniversary", "expanded", "original"} {
		if strings.Contains(inner, allowed) {
			return true
		}
	}
	return jioSaavnYearRE.MatchString(inner)
}

func jioSaavnMatchKey(value string) string {
	fields := strings.Fields(jioSaavnFoldSpaces(value))
	if len(fields) > 1 && fields[0] == "the" {
		fields = fields[1:]
	}
	return strings.Join(fields, "")
}

func jioSaavnFoldSpaces(value string) string {
	value = norm.NFKD.String(html.UnescapeString(value))
	var folded strings.Builder
	folded.Grow(len(value))
	space := true
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			folded.WriteRune(r)
			space = false
			continue
		}
		if !space {
			folded.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(folded.String())
}

func jioSaavnDisallowedVariant(item map[string]any, want jioSaavnWant) bool {
	primary, extra := jioSaavnArtistNames(item)
	blob := strings.ToLower(jioSaavnItemTitle(item) + " " + strings.Join(primary, " ") + " " + strings.Join(extra, " "))
	requested := strings.ToLower(want.Title + " " + want.Artist)
	for _, phrase := range []string{"karaoke", "tribute", "instrumental", "lullaby", "as made famous", "in the style of", "originally performed"} {
		if strings.Contains(blob, phrase) && !strings.Contains(requested, phrase) {
			return true
		}
	}
	return false
}

func jioSaavnISRCMatches(item map[string]any, want string) bool {
	want = strings.ToUpper(strings.TrimSpace(want))
	got := jioSaavnItemISRC(item)
	return want != "" && got != "" && want == got
}

func jioSaavnISRCScore(item map[string]any, want string) int {
	want = strings.ToUpper(strings.TrimSpace(want))
	if want == "" {
		return 0
	}
	got := jioSaavnItemISRC(item)
	if got == "" {
		return 0
	}
	if got == want {
		return 800
	}
	return -80
}

func jioSaavnItemISRC(item map[string]any) string {
	for _, key := range []string{"isrc", "ISRC"} {
		if value := strings.ToUpper(strings.TrimSpace(jioSaavnString(item, key))); value != "" {
			return value
		}
	}
	more, ok := item["more_info"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"isrc", "ISRC"} {
		if value := strings.ToUpper(strings.TrimSpace(jioSaavnString(more, key))); value != "" {
			return value
		}
	}
	return ""
}

func jioSaavnDurationScore(item map[string]any, want int) int {
	if want <= 0 {
		return 0
	}
	got := jioSaavnDurationSeconds(item)
	if got <= 0 {
		return 0
	}
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	switch {
	case diff <= 3:
		return 220
	case diff <= 10:
		return 80
	case diff >= 20:
		return -160
	default:
		return 0
	}
}

func jioSaavnDurationSeconds(item map[string]any) int {
	for _, key := range []string{"duration", "duration_seconds", "playtime"} {
		if seconds := jioSaavnParseDuration(jioSaavnString(item, key)); seconds > 0 {
			return seconds
		}
	}
	more, ok := item["more_info"].(map[string]any)
	if !ok {
		return 0
	}
	for _, key := range []string{"duration", "duration_seconds", "playtime"} {
		if seconds := jioSaavnParseDuration(jioSaavnString(more, key)); seconds > 0 {
			return seconds
		}
	}
	return 0
}

func jioSaavnParseDuration(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return 0
	}
	if strings.Contains(raw, ":") {
		parts := strings.Split(raw, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return 0
		}
		total := 0
		for _, part := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || n < 0 {
				return 0
			}
			total = total*60 + n
		}
		return total
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	if seconds > 10000 {
		seconds = seconds / 1000
	}
	return int(seconds + 0.5)
}

func jioSaavnAlbumScore(item map[string]any, wantAlbum string) int {
	wantKey := jioSaavnBaseTitleKey(wantAlbum)
	if wantKey == "" {
		return 0
	}
	if jioSaavnBaseTitleKey(jioSaavnItemAlbum(item)) == wantKey {
		return 60
	}
	return 0
}

func jioSaavnItemAlbum(item map[string]any) string {
	if album, ok := item["album"].(map[string]any); ok {
		if name := jioSaavnString(album, "name"); name != "" {
			return name
		}
		if name := jioSaavnString(album, "title"); name != "" {
			return name
		}
	}
	if name := jioSaavnString(item, "album"); name != "" {
		return name
	}
	if more, ok := item["more_info"].(map[string]any); ok {
		if name := jioSaavnString(more, "album"); name != "" {
			return name
		}
	}
	if _, album := jioSaavnDescriptionParts(jioSaavnString(item, "description")); album != "" {
		return album
	}
	return ""
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
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
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
