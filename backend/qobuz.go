package backend

import (
	"context"
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

type QobuzDownloader struct {
	client      *http.Client
	customURL   string
	SourceURL   string
	SourceLabel string
}

func (q *QobuzDownloader) SetCustomAPIURL(apiURL string) {
	apiURL = strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if !strings.HasPrefix(apiURL, "https://") {
		apiURL = ""
	}
	q.customURL = apiURL
}

type QobuzTrack struct {
	ID                  int64   `json:"id"`
	Title               string  `json:"title"`
	Version             string  `json:"version"`
	Duration            int     `json:"duration"`
	TrackNumber         int     `json:"track_number"`
	MediaNumber         int     `json:"media_number"`
	ISRC                string  `json:"isrc"`
	Copyright           string  `json:"copyright"`
	MaximumBitDepth     int     `json:"maximum_bit_depth"`
	MaximumSamplingRate float64 `json:"maximum_sampling_rate"`
	Hires               bool    `json:"hires"`
	HiresStreamable     bool    `json:"hires_streamable"`
	ReleaseDateOriginal string  `json:"release_date_original"`
	Performer           struct {
		Name string `json:"name"`
		ID   int64  `json:"id"`
	} `json:"performer"`
	Album struct {
		Title string `json:"title"`
		ID    string `json:"id"`
		Image struct {
			Small     string `json:"small"`
			Thumbnail string `json:"thumbnail"`
			Large     string `json:"large"`
		} `json:"image"`
		Artist struct {
			Name string `json:"name"`
			ID   int64  `json:"id"`
		} `json:"artist"`
		Label struct {
			Name string `json:"name"`
		} `json:"label"`
	} `json:"album"`
}

type qobuzPublicSearchResponse struct {
	Tracks struct {
		Total int          `json:"total"`
		Items []QobuzTrack `json:"items"`
	} `json:"tracks"`
}

var (
	qobuzStreamingURLPattern = regexp.MustCompile(`https?://[^\s"'<>\\)]+`)
	qobuzTrackURLPattern     = regexp.MustCompile(`(?i)/(?:track|tracks)/(\d+)(?:[/?#]|$)`)
)

const qobuzZarzCatalogAppID = "798273057"

func qobuzZarzSearchPath(objectPath, query string, limit int) string {
	if limit <= 0 {
		limit = 10
	}
	values := url.Values{}
	values.Set("query", strings.TrimSpace(query))
	values.Set("limit", strconv.Itoa(limit))
	values.Set("app_id", qobuzZarzCatalogAppID)
	return "/qbz/" + strings.TrimLeft(objectPath, "/") + "?" + values.Encode()
}

func doQobuzCatalogSearch(query string, limit int) (*qobuzPublicSearchResponse, error) {
	var parsed qobuzPublicSearchResponse
	body, zarzErr := zarzSignedJSON(zarzAppVersionForProvider("qbz"), http.MethodGet, qobuzZarzSearchPath("track/search", query, limit), nil, nil)
	if zarzErr == nil {
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("invalid zarz qobuz search response: %w", err)
		}
		return &parsed, nil
	}

	if err := doQobuzSignedJSONRequest("track/search", url.Values{
		"query": {strings.TrimSpace(query)},
		"limit": {strconv.Itoa(limit)},
	}, &parsed); err != nil {
		return nil, fmt.Errorf("zarz qobuz search: %v; official: %w", zarzErr, err)
	}
	return &parsed, nil
}

func NewQobuzDownloader() *QobuzDownloader {
	return &QobuzDownloader{
		client: NewSignedHTTPClient(60 * time.Second),
	}
}

func previewQobuzResponseBody(body []byte, maxLen int) string {
	preview := strings.TrimSpace(string(body))
	if len(preview) > maxLen {
		return preview[:maxLen] + "..."
	}
	return preview
}

func firstNonEmptyQobuzValue(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeQobuzSearchValue(value string) string {
	replacer := strings.NewReplacer(
		"&", " and ",
		"feat.", " ",
		"ft.", " ",
		"/", " ",
		"-", " ",
		"_", " ",
	)
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = replacer.Replace(normalized)
	return strings.Join(strings.Fields(normalized), " ")
}

func looksLikeQobuzReleaseVersion(value string) bool {
	normalized := normalizeQobuzSearchValue(value)
	if normalized == "" {
		return false
	}
	keywords := []string{
		"mix", "remix", "remaster", "remastered", "re master", "edit", "version",
		"mono", "stereo", "deluxe", "super deluxe", "anniversary", "radio edit",
		"extended", "remix",
	}
	hasYear := false
	for _, token := range strings.Fields(normalized) {
		if len(token) == 4 && (strings.HasPrefix(token, "19") || strings.HasPrefix(token, "20")) {
			allDigits := true
			for _, r := range token {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				hasYear = true
			}
		}
	}
	if hasYear && len(strings.Fields(normalized)) <= 4 {
		for _, kw := range keywords {
			if normalized == kw || strings.Contains(normalized, kw) {
				return true
			}
		}
		if len(strings.Fields(normalized)) == 1 {
			return true
		}
	}
	for _, kw := range keywords {
		if normalized == kw || strings.HasPrefix(normalized, kw+" ") || strings.HasSuffix(normalized, " "+kw) || strings.Contains(normalized, " "+kw+" ") {
			return true
		}
	}
	return false
}

func qobuzTitleSearchVariants(title string) []string {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	seen := map[string]struct{}{strings.ToLower(title): {}}
	variants := []string{title}
	add := func(value string) {
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "-–—/: ")
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		variants = append(variants, value)
	}

	dashed := strings.NewReplacer("–", "-", "—", "-").Replace(title)
	if parts := strings.Split(dashed, " - "); len(parts) > 1 {
		if looksLikeQobuzReleaseVersion(parts[len(parts)-1]) {
			add(strings.Join(parts[:len(parts)-1], " - "))
		}
		add(parts[0])
	}

	trimmed := strings.TrimSpace(title)
	if strings.HasSuffix(trimmed, ")") {
		if open := strings.LastIndex(trimmed, "("); open > 0 {
			inner := strings.TrimSpace(trimmed[open+1 : len(trimmed)-1])
			if looksLikeQobuzReleaseVersion(inner) {
				add(trimmed[:open])
			}
		}
	}
	return variants
}

func qobuzSearchQueries(isrc, spotifyTrackName, spotifyArtistName, spotifyAlbumName string) []string {
	queries := make([]string, 0, 8)
	seen := map[string]struct{}{}
	add := func(parts ...string) {
		query := strings.TrimSpace(strings.Join(parts, " "))
		if query == "" {
			return
		}
		key := strings.ToLower(query)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		queries = append(queries, query)
	}

	if trimmedISRC := strings.TrimSpace(isrc); trimmedISRC != "" && !strings.HasPrefix(trimmedISRC, "qobuz_") {
		add(trimmedISRC)
	}
	artist := strings.TrimSpace(spotifyArtistName)
	album := strings.TrimSpace(spotifyAlbumName)
	titles := qobuzTitleSearchVariants(spotifyTrackName)
	for _, title := range titles {
		add(title, artist)
	}
	if album != "" && len(titles) > 0 {
		add(titles[len(titles)-1], artist, album)
	}
	return queries
}

func parseQobuzTrackID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("qobuz url is empty")
	}
	match := qobuzTrackURLPattern.FindStringSubmatch(raw)
	if len(match) < 2 {
		return 0, fmt.Errorf("no qobuz track id in url")
	}
	id, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid qobuz track id")
	}
	return id, nil
}

func qobuzTrackFromAntraISRC(isrc, title, artist, album string) (*QobuzTrack, error) {
	hit, err := antraSearchByISRC("qobuz", isrc)
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(strings.TrimSpace(hit.TrackID), 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("qobuz antra returned no track id")
	}
	resolvedTitle := strings.TrimSpace(hit.Title)
	if resolvedTitle == "" {
		resolvedTitle = title
	}
	resolvedArtist := strings.TrimSpace(hit.Artist)
	if resolvedArtist == "" {
		resolvedArtist = artist
	}
	resolvedAlbum := strings.TrimSpace(hit.Album)
	if resolvedAlbum == "" {
		resolvedAlbum = album
	}
	resolvedISRC := strings.TrimSpace(hit.ISRC)
	if resolvedISRC == "" {
		resolvedISRC = isrc
	}
	return qobuzTrackFromID(id, resolvedTitle, resolvedArtist, resolvedAlbum, resolvedISRC), nil
}

func qobuzTrackFromID(id int64, title, artist, album, isrc string) *QobuzTrack {
	track := &QobuzTrack{
		ID:    id,
		Title: strings.TrimSpace(title),
		ISRC:  strings.TrimSpace(isrc),
	}
	track.Performer.Name = strings.TrimSpace(artist)
	track.Album.Title = strings.TrimSpace(album)
	track.Album.Artist.Name = strings.TrimSpace(artist)
	return track
}

func lookupQobuzTrackFromExternalLinks(isrc, spotifyURL string) (int64, error) {
	return lookupQobuzTrackFromExternalLinksWithContext(context.Background(), isrc, spotifyURL)
}

func lookupQobuzTrackFromExternalLinksWithContext(ctx context.Context, isrc, spotifyURL string) (int64, error) {
	if id, err := parseQobuzTrackID(spotifyURL); err == nil {
		return id, nil
	}
	if spotifyID, err := extractSpotifyTrackID(spotifyURL); err == nil && spotifyID != "" {
		if resolved, resolveErr := lookupZarzResolveLinksWithContext(ctx, spotifyID); resolveErr == nil {
			if id, parseErr := parseQobuzTrackID(resolved.QobuzURL); parseErr == nil {
				fmt.Printf("Found Qobuz track via Zarz resolve: %d\n", id)
				return id, nil
			}
		}
	}

	client := NewSongLinkClientWithContext(ctx)
	pages := make([]string, 0, 2)
	if trimmedISRC := strings.TrimSpace(isrc); trimmedISRC != "" && !strings.HasPrefix(trimmedISRC, "qobuz_") {
		pages = append(pages, "https://song.link/isrc/"+url.PathEscape(strings.ToUpper(trimmedISRC)))
	}
	if spotifyID, err := extractSpotifyTrackID(spotifyURL); err == nil && spotifyID != "" {
		pages = append(pages, "https://song.link/s/"+url.PathEscape(spotifyID))
	}

	var lastErr error
	for _, page := range pages {
		result, err := client.scrapeSongLinkPage(page, "")
		if err != nil {
			lastErr = err
			continue
		}
		if result == nil {
			continue
		}
		id, err := parseQobuzTrackID(result.QobuzURL)
		if err != nil {
			lastErr = err
			continue
		}
		fmt.Printf("Found Qobuz track via song.link: %d\n", id)
		return id, nil
	}
	if lastErr != nil {
		return 0, lastErr
	}
	return 0, fmt.Errorf("qobuz track id not found via external links")
}

func qobuzTrackDisplayArtist(track QobuzTrack) string {
	return firstNonEmptyQobuzValue(track.Performer.Name, track.Album.Artist.Name)
}

func qobuzTrackSupportsHiRes(track QobuzTrack) bool {
	if track.Hires || track.HiresStreamable {
		return true
	}
	return track.MaximumBitDepth >= 24 || track.MaximumSamplingRate > 48
}

func scoreQobuzSearchCandidate(track QobuzTrack, spotifyTrackName string, spotifyArtistName string, spotifyAlbumName string) int {
	score := 0

	titleNeedle := normalizeQobuzSearchValue(spotifyTrackName)
	titleHaystack := normalizeQobuzSearchValue(track.Title)
	if version := strings.TrimSpace(track.Version); version != "" {
		titleHaystack = normalizeQobuzSearchValue(strings.TrimSpace(track.Title + " " + version))
	}
	switch {
	case titleNeedle != "" && titleHaystack == titleNeedle:
		score += 1000
	case titleNeedle != "" && (strings.Contains(titleHaystack, titleNeedle) || strings.Contains(titleNeedle, titleHaystack)):
		score += 500
	}

	artistNeedle := normalizeQobuzSearchValue(spotifyArtistName)
	artistHaystack := normalizeQobuzSearchValue(qobuzTrackDisplayArtist(track))
	artistMatched := false
	switch {
	case artistNeedle != "" && artistHaystack == artistNeedle:
		score += 300
		artistMatched = true
	case artistNeedle != "" && artistHaystack != "" && (strings.Contains(artistHaystack, artistNeedle) || strings.Contains(artistNeedle, artistHaystack)):
		score += 180
		artistMatched = true
	}

	if artistNeedle != "" && !artistMatched {
		needleTokens := strings.Fields(artistNeedle)
		haystackTokens := strings.Fields(artistHaystack)
		matchCount := 0
		for _, nt := range needleTokens {
			for _, ht := range haystackTokens {
				if nt == ht {
					matchCount++
					break
				}
			}
		}
		if matchCount > 0 {
			score += 50
			artistMatched = true
		} else {
			score -= 2000
		}
	}

	albumNeedle := normalizeQobuzSearchValue(spotifyAlbumName)
	albumHaystack := normalizeQobuzSearchValue(track.Album.Title)
	switch {
	case albumNeedle != "" && albumHaystack == albumNeedle:
		score += 150
	case albumNeedle != "" && albumHaystack != "" && (strings.Contains(albumHaystack, albumNeedle) || strings.Contains(albumNeedle, albumHaystack)):
		score += 90
	}

	if qobuzTrackSupportsHiRes(track) {
		score += 40
	} else if track.MaximumBitDepth >= 16 {
		score += 20
	}

	badKeywords := []string{"karaoke", "instrumental", "cover", "tribute", "as made famous by", "in the style of", "lullaby", "8 bit", "8-bit", "16 bit", "16-bit", "chill"}
	for _, kw := range badKeywords {
		if strings.Contains(titleHaystack, kw) && !strings.Contains(titleNeedle, kw) {
			score -= 2000
		}
		if strings.Contains(artistHaystack, kw) && !strings.Contains(artistNeedle, kw) {
			score -= 2000
		}
	}

	return score
}

func qobuzURLLooksStreamable(raw string) bool {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return false
	}

	parsed, err := url.Parse(candidate)
	if err != nil {
		return false
	}

	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func findQobuzStreamingURLInPayload(payload interface{}) string {
	switch value := payload.(type) {
	case string:
		candidate := strings.ReplaceAll(strings.TrimSpace(value), `\/`, `/`)
		if qobuzURLLooksStreamable(candidate) {
			return candidate
		}
	case []interface{}:
		for _, item := range value {
			if url := findQobuzStreamingURLInPayload(item); url != "" {
				return url
			}
		}
	case map[string]interface{}:
		for _, key := range []string{"download_url", "url", "play_url", "stream_url", "link", "file"} {
			if nested, ok := value[key]; ok {
				if url := findQobuzStreamingURLInPayload(nested); url != "" {
					return url
				}
			}
		}
		for _, nested := range value {
			if url := findQobuzStreamingURLInPayload(nested); url != "" {
				return url
			}
		}
	}

	return ""
}

func extractQobuzStreamingURL(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	var directResp struct {
		URL         string `json:"url"`
		DownloadURL string `json:"download_url"`
		Data        struct {
			URL         string `json:"url"`
			DownloadURL string `json:"download_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &directResp); err == nil {
		for _, candidate := range []string{
			directResp.DownloadURL,
			directResp.URL,
			directResp.Data.DownloadURL,
			directResp.Data.URL,
		} {
			if qobuzURLLooksStreamable(candidate) {
				return candidate
			}
		}
	}

	var genericPayload interface{}
	if err := json.Unmarshal(body, &genericPayload); err == nil {
		if streamURL := findQobuzStreamingURLInPayload(genericPayload); streamURL != "" {
			return streamURL
		}
	}

	if openIdx := strings.Index(trimmed, "("); openIdx >= 0 {
		if closeIdx := strings.LastIndex(trimmed, ")"); closeIdx > openIdx+1 {
			callbackBody := strings.TrimSpace(trimmed[openIdx+1 : closeIdx])
			if streamURL := extractQobuzStreamingURL([]byte(callbackBody)); streamURL != "" {
				return streamURL
			}
		}
	}

	for _, match := range qobuzStreamingURLPattern.FindAllString(trimmed, -1) {
		candidate := strings.ReplaceAll(match, `\/`, `/`)
		if qobuzURLLooksStreamable(candidate) {
			return candidate
		}
	}

	return ""
}

func (q *QobuzDownloader) searchByISRC(isrc string, spotifyTrackName string, spotifyArtistName string, spotifyAlbumName string, spotifyURL string) (*QobuzTrack, error) {
	if strings.HasPrefix(isrc, "qobuz_") {
		trackID := strings.TrimSpace(strings.TrimPrefix(isrc, "qobuz_"))
		id, err := strconv.ParseInt(trackID, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid qobuz track id")
		}
		return qobuzTrackFromID(id, spotifyTrackName, spotifyArtistName, spotifyAlbumName, ""), nil
	}

	if track, err := qobuzTrackFromAntraISRC(isrc, spotifyTrackName, spotifyArtistName, spotifyAlbumName); err == nil {
		fmt.Printf("Found Qobuz track via Antra ISRC: %d\n", track.ID)
		return track, nil
	}

	queries := qobuzSearchQueries(isrc, spotifyTrackName, spotifyArtistName, spotifyAlbumName)

	var lastErr error
	for _, query := range queries {
		if strings.TrimSpace(query) == "" {
			continue
		}

		searchResp, err := doQobuzCatalogSearch(query, 10)
		if err != nil {
			lastErr = fmt.Errorf("failed to search Qobuz catalog: %w", err)
			continue
		}

		if searchResp == nil || searchResp.Tracks.Total == 0 || len(searchResp.Tracks.Items) == 0 {
			lastErr = fmt.Errorf("track not found for query: %s", query)
			continue
		}

		bestIndex := 0
		bestScore := -1
		for idx, candidate := range searchResp.Tracks.Items {
			score := scoreQobuzSearchCandidate(candidate, spotifyTrackName, spotifyArtistName, spotifyAlbumName)
			if idx == 0 || score > bestScore {
				bestIndex = idx
				bestScore = score
			}
		}

		selected := searchResp.Tracks.Items[bestIndex]
		return &selected, nil
	}

	if id, lookupErr := lookupQobuzTrackFromExternalLinksWithContext(ActiveDownloadContext(), isrc, spotifyURL); lookupErr == nil {
		fmt.Println("Official Qobuz catalog search returned no matches; using song.link Qobuz track id")
		return qobuzTrackFromID(id, spotifyTrackName, spotifyArtistName, spotifyAlbumName, isrc), nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("track not found for ISRC: %s", isrc)
	}
	return nil, lastErr
}

func (q *QobuzDownloader) GetDownloadURL(trackID int64, quality string, allowFallback bool) (string, error) {
	qualityCode := quality
	if qualityCode == "" || qualityCode == "5" {
		qualityCode = "6"
	}

	fmt.Printf("Getting download URL for track ID: %d with requested quality: %s\n", trackID, qualityCode)

	if strings.TrimSpace(q.customURL) != "" {
		fmt.Printf("Trying custom Qobuz instance...\n")
		url, err := q.getQobuzCustomDownloadURL(trackID, qualityCode)
		if err == nil {
			fmt.Printf("Success (custom Qobuz instance)\n")
			return url, nil
		}
		if IsDownloadCancelledError(err) {
			return "", err
		}
		fmt.Printf("Custom Qobuz instance failed: %v\n", err)
		if !allowFallback {
			return "", err
		}

	}

	return q.downloadQobuzViaGateways(trackID, qualityCode, allowFallback)
}

var fetchQobuzGatewayURL = func(q *QobuzDownloader, trackID int64, qualityCode string, allowFallback bool) (string, error) {
	return q.downloadQobuzViaGateways(trackID, qualityCode, allowFallback)
}

func (q *QobuzDownloader) downloadQobuzViaGateways(trackID int64, qualityCode string, allowFallback bool) (string, error) {
	downloadFunc := func(qual string) (string, error) {
		url, err := q.getQobuzCommunityDownloadURL(trackID, qual)
		if err == nil {
			fmt.Printf("Success (community qbz-a)\n")
			return url, nil
		}
		if IsDownloadCancelledError(err) {
			return "", err
		}
		if !IsCommunityCooldownError(err) {
			fmt.Printf("Community qbz-a failed: %v\n", err)
		}
		url, zarzErr := q.getQobuzZarzDownloadURL(trackID, qual)
		if zarzErr == nil {
			fmt.Printf("Success (zarz qbz)\n")
			return url, nil
		}
		if IsDownloadCancelledError(zarzErr) {
			return "", zarzErr
		}
		fmt.Printf("Zarz Qobuz API failed: %v\n", zarzErr)
		if err != nil {
			return "", err
		}
		return "", zarzErr
	}

	url, err := downloadFunc(qualityCode)
	if err == nil {
		return url, nil
	}
	if IsDownloadCancelledError(err) {
		return "", err
	}

	currentQuality := qualityCode

	if currentQuality == "27" && allowFallback {
		fmt.Printf("Download with quality 27 failed, trying fallback to 7 (24-bit Standard)...\n")
		url, err := downloadFunc("7")
		if err == nil {
			fmt.Println("Success with fallback quality 7")
			return url, nil
		}
		if IsDownloadCancelledError(err) {
			return "", err
		}

		currentQuality = "7"
	}

	if currentQuality == "7" && allowFallback {
		fmt.Printf("Download with quality 7 failed, trying fallback to 6 (16-bit Lossless)...\n")
		url, err := downloadFunc("6")
		if err == nil {
			fmt.Println("Success with fallback quality 6")
			return url, nil
		}
		if IsDownloadCancelledError(err) {
			return "", err
		}
	}

	return "", fmt.Errorf("all APIs and fallbacks failed. Last error: %v", err)
}

func (q *QobuzDownloader) downloadQobuzTrackFile(trackID int64, quality, dest string, allowFallback bool) (string, error) {
	qualityCode := strings.TrimSpace(quality)
	if qualityCode == "" || qualityCode == "5" {
		qualityCode = "6"
	}
	customConfigured := strings.TrimSpace(q.customURL) != ""
	if customConfigured {
		downloadURL, customErr := q.getQobuzCustomDownloadURL(trackID, qualityCode)
		if IsDownloadCancelledError(customErr) {
			return "", customErr
		}
		if customErr == nil {
			if err := q.DownloadFile(downloadURL, dest); err == nil {
				return dest, nil
			} else if IsDownloadCancelledError(err) {
				return "", err
			} else {
				fmt.Printf("Custom Qobuz download failed, trying Antra mirror: %v\n", err)
			}
		} else {
			fmt.Printf("Custom Qobuz instance failed, trying Antra mirror: %v\n", customErr)
		}
	}

	fmt.Println("Trying Antra Qobuz mirror...")
	antraPath, antraErr := antraStreamToFile("qobuz", strconv.FormatInt(trackID, 10), dest, antraQualityQuery("qobuz", qualityCode))
	if antraErr == nil {
		return antraPath, nil
	}
	if IsDownloadCancelledError(antraErr) {
		return "", antraErr
	}
	if customConfigured && !allowFallback {
		return "", antraErr
	}
	fmt.Printf("Antra Qobuz mirror failed, trying community/Zarz: %v\n", antraErr)
	downloadURL, err := fetchQobuzGatewayURL(q, trackID, qualityCode, allowFallback)
	if err != nil {
		return "", err
	}
	if err := q.DownloadFile(downloadURL, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func (q *QobuzDownloader) DownloadFile(url, filepath string) (err error) {
	fmt.Println("Starting file download...")

	fmt.Printf("Creating file: %s\n", filepath)
	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(filepath)
		}
	}()

	fmt.Println("Downloading...")

	pw := NewProgressWriter(out)
	client := newMediaHTTPClient()
	err = copyResponseWithRetry(pw, func() (*http.Response, error) {
		req, reqErr := NewRequestWithDefaultHeaders(http.MethodGet, url, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		req = WithDownloadContext(req)
		return client.Do(req)
	})
	if err != nil {
		return fmt.Errorf("failed to download file: %w", WrapDownloadCancelled(err))
	}

	fmt.Printf("\rDownloaded: %.2f MB (Complete)\n", float64(pw.GetTotal())/(1024*1024))
	return nil
}

func (q *QobuzDownloader) DownloadCoverArt(coverURL, filepath string) error {
	if coverURL == "" {
		return fmt.Errorf("no cover URL provided")
	}

	req, err := NewRequestWithDefaultHeaders(http.MethodGet, coverURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create cover request: %w", err)
	}

	resp, err := q.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download cover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("cover download failed with status %d", resp.StatusCode)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create cover file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func buildQobuzFilename(title, artist, album, albumArtist, releaseDate string, trackNumber, discNumber int, format string, includeTrackNumber bool, position int, useAlbumTrackNumber bool, extra ...string) string {
	var filename string
	isrc := ""
	if len(extra) > 0 {
		isrc = SanitizeOptionalFilename(extra[0])
	}

	numberToUse := position
	if useAlbumTrackNumber && trackNumber > 0 {
		numberToUse = trackNumber
	}

	year := ""
	if len(releaseDate) >= 4 {
		year = releaseDate[:4]
	}

	if strings.Contains(format, "{") {
		filename = format
		filename = strings.ReplaceAll(filename, "{title}", title)
		filename = strings.ReplaceAll(filename, "{artist}", artist)
		filename = strings.ReplaceAll(filename, "{album}", album)
		filename = strings.ReplaceAll(filename, "{album_artist}", albumArtist)
		filename = strings.ReplaceAll(filename, "{year}", year)
		filename = strings.ReplaceAll(filename, "{date}", SanitizeFilename(releaseDate))
		filename = strings.ReplaceAll(filename, "{isrc}", isrc)

		if discNumber > 0 {
			filename = strings.ReplaceAll(filename, "{disc}", fmt.Sprintf("%d", discNumber))
		} else {
			filename = strings.ReplaceAll(filename, "{disc}", "")
		}

		if numberToUse > 0 {
			filename = strings.ReplaceAll(filename, "{track}", fmt.Sprintf("%02d", numberToUse))
		} else {

			filename = regexp.MustCompile(`\{track\}\.\s*`).ReplaceAllString(filename, "")
			filename = regexp.MustCompile(`\{track\}\s*-\s*`).ReplaceAllString(filename, "")
			filename = regexp.MustCompile(`\{track\}\s*`).ReplaceAllString(filename, "")
		}
	} else {

		switch format {
		case "artist-title":
			filename = fmt.Sprintf("%s - %s", artist, title)
		case "title":
			filename = title
		default:
			filename = fmt.Sprintf("%s - %s", title, artist)
		}

		if includeTrackNumber && position > 0 {
			filename = fmt.Sprintf("%02d. %s", numberToUse, filename)
		}
	}

	return filename + ".flac"
}

func (q *QobuzDownloader) DownloadTrack(spotifyID, outputDir, quality, filenameFormat string, includeTrackNumber bool, position int, spotifyTrackName, spotifyArtistName, spotifyAlbumName, spotifyAlbumArtist, spotifyReleaseDate string, useAlbumTrackNumber bool, spotifyCoverURL string, embedMaxQualityCover bool, spotifyTrackNumber, spotifyDiscNumber, spotifyTotalTracks int, spotifyTotalDiscs int, spotifyCopyright, spotifyPublisher, spotifyComposer, metadataSeparator, spotifyURL string, allowFallback bool, useFirstArtistOnly bool, useSingleGenre bool, embedGenre bool) (string, error) {
	var isrc string
	if spotifyID != "" {
		linkClient := NewSongLinkClientWithContext(ActiveDownloadContext())
		resolvedISRC, err := linkClient.GetISRCDirect(spotifyID)
		if err != nil {
			return "", fmt.Errorf("failed to get ISRC: %v", err)
		}
		isrc = resolvedISRC
	} else {
		return "", fmt.Errorf("spotify ID is required for Qobuz download")
	}

	return q.DownloadTrackWithISRC(isrc, outputDir, quality, filenameFormat, includeTrackNumber, position, spotifyTrackName, spotifyArtistName, spotifyAlbumName, spotifyAlbumArtist, spotifyReleaseDate, useAlbumTrackNumber, spotifyCoverURL, embedMaxQualityCover, spotifyTrackNumber, spotifyDiscNumber, spotifyTotalTracks, spotifyTotalDiscs, spotifyCopyright, spotifyPublisher, spotifyComposer, metadataSeparator, spotifyURL, allowFallback, useFirstArtistOnly, useSingleGenre, embedGenre)
}

func (q *QobuzDownloader) DownloadTrackWithISRC(isrc, outputDir, quality, filenameFormat string, includeTrackNumber bool, position int, spotifyTrackName, spotifyArtistName, spotifyAlbumName, spotifyAlbumArtist, spotifyReleaseDate string, useAlbumTrackNumber bool, spotifyCoverURL string, embedMaxQualityCover bool, spotifyTrackNumber, spotifyDiscNumber, spotifyTotalTracks int, spotifyTotalDiscs int, spotifyCopyright, spotifyPublisher, spotifyComposer, metadataSeparator, spotifyURL string, allowFallback bool, useFirstArtistOnly bool, useSingleGenre bool, embedGenre bool) (string, error) {
	fmt.Printf("Fetching track info for ISRC: %s\n", isrc)

	metaChan := make(chan Metadata, 1)
	if embedGenre && isrc != "" {
		go func() {
			if ShouldSkipMusicBrainzMetadataFetch() {
				fmt.Println("Skipping MusicBrainz metadata fetch because status check is offline.")
				metaChan <- Metadata{}
			} else {
				fmt.Println("Fetching MusicBrainz metadata...")
				if fetchedMeta, err := FetchMusicBrainzMetadata(isrc, spotifyTrackName, spotifyArtistName, spotifyAlbumName, useSingleGenre, embedGenre); err == nil {
					fmt.Println("MusicBrainz metadata fetched")
					metaChan <- fetchedMeta
				} else {
					fmt.Printf("Warning: Failed to fetch MusicBrainz metadata: %v\n", err)
					metaChan <- Metadata{}
				}
			}
		}()
	} else {
		close(metaChan)
	}

	if outputDir != "." {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	track, err := q.searchByISRC(isrc, spotifyTrackName, spotifyArtistName, spotifyAlbumName, spotifyURL)
	if err != nil {
		return "", err
	}

	matchedTitle := strings.TrimSpace(track.Title)
	if v := strings.TrimSpace(track.Version); v != "" {
		matchedTitle = matchedTitle + " (" + v + ")"
	}
	q.SourceURL = fmt.Sprintf("https://open.qobuz.com/track/%d", track.ID)
	q.SourceLabel = strings.TrimSpace(fmt.Sprintf("%s - %s", qobuzTrackDisplayArtist(*track), matchedTitle))

	artists := spotifyArtistName
	trackTitle := spotifyTrackName
	albumTitle := spotifyAlbumName

	fmt.Printf("Found track: %s - %s\n", artists, trackTitle)
	fmt.Printf("Album: %s\n", albumTitle)

	qualityInfo := "Standard"
	if track.Hires {
		if track.MaximumBitDepth > 0 && track.MaximumSamplingRate > 0 {
			qualityInfo = fmt.Sprintf("Hi-Res (%d-bit / %.1f kHz)", track.MaximumBitDepth, track.MaximumSamplingRate)
		} else if track.MaximumBitDepth > 0 {
			qualityInfo = fmt.Sprintf("Hi-Res available (%d-bit)", track.MaximumBitDepth)
		} else {
			qualityInfo = "Hi-Res available"
		}
	}
	fmt.Printf("Quality: %s\n", qualityInfo)

	safeArtist := sanitizeFilename(artists)
	safeAlbumArtist := sanitizeFilename(spotifyAlbumArtist)

	if useFirstArtistOnly {
		safeArtist = sanitizeFilename(GetFirstArtist(artists))
		safeAlbumArtist = sanitizeFilename(GetFirstArtist(spotifyAlbumArtist))
	}

	safeTitle := sanitizeFilename(trackTitle)
	safeAlbum := sanitizeFilename(albumTitle)

	filename := buildQobuzFilename(safeTitle, safeArtist, safeAlbum, safeAlbumArtist, spotifyReleaseDate, spotifyTrackNumber, spotifyDiscNumber, filenameFormat, includeTrackNumber, position, useAlbumTrackNumber, isrc)
	filepath := filepath.Join(outputDir, filename)
	filepath, alreadyExists := ResolveOutputPathForDownload(filepath, GetRedownloadWithSuffixSetting())
	if alreadyExists {
		fmt.Printf("File already exists: %s (%.2f MB)\n", filepath, float64(mustFileSize(filepath))/(1024*1024))
		return "EXISTS:" + filepath, nil
	}

	fmt.Printf("Downloading FLAC file to: %s\n", filepath)
	downloadedPath, err := q.downloadRankedQobuz(track.ID, quality, filepath, track.Duration, allowFallback, SourceTrack{Title: track.Title, Artist: qobuzTrackDisplayArtist(*track), ISRC: track.ISRC})
	if err != nil {
		return "", err
	}
	filepath = downloadedPath

	fmt.Printf("Downloaded: %s\n", filepath)

	coverPath := ""

	if spotifyCoverURL != "" {
		coverPath = filepath + ".cover.jpg"
		coverClient := NewCoverClient()
		if err := coverClient.DownloadCoverToPath(spotifyCoverURL, coverPath, embedMaxQualityCover); err != nil {
			fmt.Printf("Warning: Failed to download Spotify cover: %v\n", err)
			coverPath = ""
		} else {
			defer os.Remove(coverPath)
			fmt.Println("Spotify cover downloaded")
		}
	}

	var mbMeta Metadata
	if isrc != "" {
		mbMeta = <-metaChan
	}

	fmt.Println("Embedding metadata and cover art...")

	trackNumberToEmbed := spotifyTrackNumber
	if trackNumberToEmbed == 0 {
		trackNumberToEmbed = 1
	}

	upc := ""
	if identifiers, err := GetSpotifyTrackIdentifiersWithContext(ActiveDownloadContext(), spotifyURL); err == nil || identifiers.ISRC != "" || identifiers.UPC != "" {
		if strings.TrimSpace(isrc) == "" && strings.TrimSpace(identifiers.ISRC) != "" {
			isrc = strings.TrimSpace(identifiers.ISRC)
		}
		upc = strings.TrimSpace(identifiers.UPC)
	}

	metadata := Metadata{
		Title:       trackTitle,
		Artist:      artists,
		Album:       albumTitle,
		AlbumArtist: spotifyAlbumArtist,
		Date:        spotifyReleaseDate,
		TrackNumber: trackNumberToEmbed,
		TotalTracks: spotifyTotalTracks,
		DiscNumber:  spotifyDiscNumber,
		TotalDiscs:  spotifyTotalDiscs,
		URL:         spotifyURL,
		Comment:     spotifyURL,
		Copyright:   spotifyCopyright,
		Publisher:   spotifyPublisher,
		Composer:    spotifyComposer,
		Separator:   metadataSeparator,
		Description: "https://github.com/vekhyat/Auralis",
		ISRC:        isrc,
		UPC:         upc,
		Genre:       mbMeta.Genre,
	}

	if err := EmbedMetadata(filepath, metadata, coverPath); err != nil {
		return "", fmt.Errorf("failed to embed metadata: %w", err)
	}

	fmt.Println("Metadata embedded successfully!")
	return filepath, nil
}
