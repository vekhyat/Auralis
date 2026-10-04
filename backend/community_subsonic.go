package backend

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Subsonic's standard authentication uses MD5(password+salt); passwords are
// read from the configured environment variable and never persisted here.
func subsonicParameters(source CommunitySource) (url.Values, error) {
	parts := strings.SplitN(os.Getenv(source.CredentialEnv), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, &communityAccessError{Status: 401}
	}
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return nil, fmt.Errorf("could not initialize server authentication")
	}
	salt := hex.EncodeToString(saltBytes)
	digest := md5.Sum([]byte(parts[1] + salt))
	return url.Values{"u": {parts[0]}, "t": {hex.EncodeToString(digest[:])}, "s": {salt}, "v": {"1.16.1"}, "c": {"Auralis"}, "f": {"json"}}, nil
}

func resolveSubsonicSource(ctx context.Context, source CommunitySource, track SourceTrack, quality string) (string, error) {
	if sourceQuality(quality) == "atmos" {
		return "", fmt.Errorf("Subsonic does not expose an Atmos selection")
	}
	if track.Title == "" || track.Artist == "" {
		return "", fmt.Errorf("Subsonic requires title and artist for recording matching")
	}
	params, err := subsonicParameters(source)
	if err != nil {
		return "", err
	}
	params.Set("query", track.Title+" "+track.Artist)
	params.Set("songCount", "20")
	params.Set("artistCount", "0")
	params.Set("albumCount", "0")
	body, _, err := sourceResponse(source, http.MethodGet, "/rest/search3", params, nil, ctx)
	if err != nil {
		return "", err
	}
	if err := expectSourceJSON(body); err != nil {
		return "", err
	}
	var result struct {
		Response struct {
			Status string `json:"status"`
			Search struct {
				Songs []struct {
					ID       string `json:"id"`
					Title    string `json:"title"`
					Artist   string `json:"artist"`
					ISRC     string `json:"isrc"`
					Duration int    `json:"duration"`
				} `json:"song"`
			} `json:"searchResult3"`
		} `json:"subsonic-response"`
	}
	if json.Unmarshal(body, &result) != nil || result.Response.Status != "ok" {
		return "", fmt.Errorf("Subsonic search was rejected")
	}
	match := ""
	for _, song := range result.Response.Search.Songs {
		if normalizedSourceTitle(song.Title) != normalizedSourceTitle(track.Title) || !strings.EqualFold(strings.TrimSpace(song.Artist), strings.TrimSpace(track.Artist)) {
			continue
		}
		if track.ISRC != "" && song.ISRC != "" && !strings.EqualFold(track.ISRC, song.ISRC) {
			continue
		}
		if track.Duration > 0 && song.Duration > 0 && math.Abs(float64(track.Duration-song.Duration)) > 15 {
			continue
		}
		match = song.ID
		break
	}
	if match == "" {
		return "", fmt.Errorf("Subsonic did not match the requested recording")
	}
	params, err = subsonicParameters(source)
	if err != nil {
		return "", err
	}
	params.Set("id", match)
	params.Set("maxBitRate", "0")
	params.Set("format", "raw")
	return sourceURL(source.BaseURL + "/rest/stream?" + params.Encode())
}

func downloadExtraCommunitySources(p ExtraDownloadParams, dest, service string, native func(ExtraDownloadParams, string) (string, string, error)) (string, string, error) {
	sourceURL := p.ServiceURL
	attempts := []sourceDownloadAttempt{{id: "antra-" + service, download: func() (string, error) {
		path, raw, err := native(p, dest)
		if raw != "" {
			sourceURL = raw
		}
		return path, err
	}}}
	track := SourceTrack{Title: p.TrackName, Artist: p.ArtistName, ISRC: p.ISRC, Duration: p.DurationSeconds, ServiceURL: p.ServiceURL}
	if service == "apple" {
		track.ID = extractAppleTrackID(p.ServiceURL)
	} else if id, err := extractDeezerTrackID(p.ServiceURL); err == nil {
		track.ID = id
	}
	attempts = append(attempts, communitySourceAttempts(service, "16", dest, track)...)
	path, err := runDownloadSources(service, "16", p.DurationSeconds, attempts)
	return path, sourceURL, err
}
