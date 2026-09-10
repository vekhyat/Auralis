package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const zarzResolveURL = "https://api.zarz.moe/v1/resolve"

func parseZarzResolveBody(body []byte) (*resolvedTrackLinks, error) {
	var parsed struct {
		Success  bool                       `json:"success"`
		ISRC     string                     `json:"isrc"`
		SongUrls map[string]json.RawMessage `json:"songUrls"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("invalid zarz resolve response: %w", err)
	}
	if !parsed.Success {
		return nil, fmt.Errorf("zarz resolve returned success=false")
	}
	links := &resolvedTrackLinks{ISRC: strings.ToUpper(strings.TrimSpace(parsed.ISRC))}
	for key, raw := range parsed.SongUrls {
		value := extractZarzResolveURLValue(raw)
		if value == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "tidal":
			if links.TidalURL == "" {
				links.TidalURL = value
			}
		case "amazonmusic", "amazon":
			if links.AmazonURL == "" {
				if normalized := normalizeAmazonMusicURL(value); normalized != "" {
					links.AmazonURL = normalized
				} else {
					links.AmazonURL = value
				}
			}
		case "qobuz":
			if links.QobuzURL == "" {
				links.QobuzURL = value
			}
		case "deezer":
			if links.DeezerURL == "" {
				links.DeezerURL = normalizeDeezerTrackURL(value)
			}
		}
	}
	if links.TidalURL == "" && links.AmazonURL == "" && links.QobuzURL == "" && links.DeezerURL == "" {
		return nil, fmt.Errorf("zarz resolve returned no platform links")
	}
	return links, nil
}

func extractZarzResolveURLValue(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var direct string
	if err := json.Unmarshal(trimmed, &direct); err == nil {
		return strings.TrimSpace(direct)
	}
	var list []string
	if err := json.Unmarshal(trimmed, &list); err == nil {
		for _, candidate := range list {
			if cleaned := strings.TrimSpace(candidate); cleaned != "" {
				return cleaned
			}
		}
	}
	return ""
}

func mergeResolvedTrackLinks(dst, src *resolvedTrackLinks) {
	if dst == nil || src == nil {
		return
	}
	if dst.TidalURL == "" {
		dst.TidalURL = src.TidalURL
	}
	if dst.AmazonURL == "" {
		dst.AmazonURL = src.AmazonURL
	}
	if dst.QobuzURL == "" {
		dst.QobuzURL = src.QobuzURL
	}
	if dst.DeezerURL == "" {
		dst.DeezerURL = src.DeezerURL
	}
	if dst.ISRC == "" {
		dst.ISRC = src.ISRC
	}
}

func lookupZarzResolveLinks(spotifyTrackID string) (*resolvedTrackLinks, error) {
	trackID, err := extractSpotifyTrackID(spotifyTrackID)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]string{
		"platform": "spotify",
		"type":     "track",
		"id":       trackID,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, zarzResolveURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", zarzUserAgentFor("tidal-web@1.1.0"))

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("zarz resolve failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("zarz resolve returned HTTP %d", resp.StatusCode)
	}
	return parseZarzResolveBody(body)
}

func (s *SongLinkClient) resolveLinksViaZarz(links *resolvedTrackLinks, spotifyTrackID string) (bool, error) {
	if links == nil {
		return false, fmt.Errorf("links is required for zarz resolve")
	}
	before := *links
	resolved, err := lookupZarzResolveLinks(spotifyTrackID)
	if err != nil {
		return false, err
	}
	// Tidal IDs from resolve can rematch a different recording. Leave Tidal
	// to song.link / the public US catalog search, matching tidal-web.
	resolved.TidalURL = ""
	mergeResolvedTrackLinks(links, resolved)
	return *links != before, nil
}
