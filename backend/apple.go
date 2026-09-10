package backend

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var appleSongIDPattern = regexp.MustCompile(`(?:/song/[^/]+/|/songs/)(\d+)`)
var appleIQueryPattern = regexp.MustCompile(`[?&]i=(\d+)`)

func extractAppleTrackID(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if match := appleSongIDPattern.FindStringSubmatch(rawURL); len(match) > 1 {
		return match[1]
	}
	if match := appleIQueryPattern.FindStringSubmatch(rawURL); len(match) > 1 {
		return match[1]
	}
	return ""
}

func downloadAppleTrack(p ExtraDownloadParams, destPath string) (string, string, error) {
	trackID := extractAppleTrackID(p.ServiceURL)
	sourceURL := p.ServiceURL
	if trackID == "" && p.SpotifyID != "" {
		if id, err := antraResolveSpotify("apple", p.SpotifyID); err == nil {
			trackID = id
		}
	}
	if trackID == "" {
		hit, err := resolveISRCOrSearch(p, "apple")
		if err != nil {
			return "", "", fmt.Errorf("apple: %w", err)
		}
		trackID = hit.TrackID
	}
	if trackID == "" {
		return "", "", fmt.Errorf("apple: could not resolve track")
	}
	if sourceURL == "" {
		sourceURL = fmt.Sprintf("https://music.apple.com/song/%s", trackID)
	}
	fmt.Printf("Downloading Apple Music track %s via Antra mirror...\n", trackID)
	path, err := antraStreamToFile("apple", trackID, destPath, url.Values{})
	if err != nil {
		info, infoErr := antraGetTrackInfo("apple", trackID)
		if infoErr != nil || info.StreamURL == "" {
			return "", sourceURL, err
		}
		path, err = antraDownloadURLToFile(info.StreamURL, destPath)
	}
	return path, sourceURL, err
}
