package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Explicitly opt in: this test uses live providers and downloads one sample per
// route. It never loads the user's settings or saved verification sessions.
func TestLiveDownloadSources(t *testing.T) {
	if os.Getenv("AURALIS_LIVE_SOURCE_CHECK") != "1" {
		t.Skip("set AURALIS_LIVE_SOURCE_CHECK=1 to check real provider downloads")
	}
	root := os.Getenv("AURALIS_LIVE_RESULTS_DIR")
	if root == "" || !filepath.IsAbs(root) {
		t.Fatal("AURALIS_LIVE_RESULTS_DIR must be an absolute isolated directory")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(appDataDirEnv, filepath.Join(root, "app-data"))
	t.Setenv("USERPROFILE", filepath.Join(root, "profile"))
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal("ffprobe is required for live audio validation")
	}
	decoder, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal("ffmpeg is required for live decode validation")
	}
	title, artist := "Come Together", "The Beatles"
	if value := os.Getenv("AURALIS_LIVE_TRACK_TITLE"); value != "" {
		title = value
	}
	if value := os.Getenv("AURALIS_LIVE_TRACK_ARTIST"); value != "" {
		artist = value
	}
	type result struct {
		Service          string  `json:"service"`
		Route            string  `json:"route"`
		Stage            string  `json:"stage"`
		TrackID          string  `json:"track_id,omitempty"`
		Title            string  `json:"title,omitempty"`
		Artist           string  `json:"artist,omitempty"`
		Bytes            int64   `json:"bytes,omitempty"`
		Duration         float64 `json:"duration_seconds,omitempty"`
		Codec            string  `json:"codec,omitempty"`
		SampleRate       string  `json:"sample_rate,omitempty"`
		Error            string  `json:"error,omitempty"`
		Seconds          float64 `json:"elapsed_seconds"`
		IdentityEvidence string  `json:"identity_evidence,omitempty"`
	}
	var results []result
	urlPattern := regexp.MustCompile(`https?://[^\s"<>]+`)
	redact := func(err error) string { return urlPattern.ReplaceAllString(err.Error(), "<redacted-url>") }
	defer func() {
		data, err := json.MarshalIndent(map[string]any{"checked_at_utc": time.Now().UTC().Format(time.RFC3339), "requested_title": title, "requested_artist": artist, "results": results}, "", "  ")
		if err == nil {
			err = os.WriteFile(filepath.Join(root, "results.json"), data, 0600)
		}
		if err != nil {
			t.Errorf("save diagnostic results: %v", err)
		}
	}()
	for _, service := range []string{"tidal", "qobuz", "amazon", "deezer", "apple", "jiosaavn"} {
		t.Run(service, func(t *testing.T) {
			started := time.Now()
			r := result{Service: service, Route: "antra", Stage: "search"}
			if service == "jiosaavn" {
				r.Route = "official/community"
			}
			defer func() {
				r.Seconds = time.Since(started).Seconds()
				results = append(results, r)
				b, _ := json.Marshal(r)
				t.Log(string(b))
			}()
			_, release := BeginDownloadCancellationScope()
			defer release()
			timer := time.AfterFunc(100*time.Second, ForceStopActiveDownloads)
			defer timer.Stop()
			dest := filepath.Join(root, service, "sample.flac")
			p := ExtraDownloadParams{Service: service, TrackName: title, ArtistName: artist}
			expectedMS := 0
			amazonURL := os.Getenv("AURALIS_LIVE_AMAZON_URL")
			if service == "amazon" && amazonURL != "" {
				r.Stage = "resolve"
				parsed, parseErr := url.Parse(amazonURL)
				if parseErr != nil || parsed.Hostname() != "open.spotify.com" || !strings.HasPrefix(parsed.Path, "/track/") {
					t.Fatal("Amazon fixture must be an official Spotify track URL")
				}
				id, resolveErr := antraResolveSpotify("amazon", strings.TrimPrefix(parsed.Path, "/track/"))
				if resolveErr != nil {
					r.Error = redact(resolveErr)
					t.Error(r.Error)
					return
				}
				r.TrackID, r.Title, r.Artist = id, title, artist
				r.IdentityEvidence = "provider resolved the externally verified fixture Spotify URL"
				expectedMS = 258000
			} else if service != "jiosaavn" {
				var hit antraSearchHit
				var searchErr error
				if isrc := os.Getenv("AURALIS_LIVE_ISRC"); isrc != "" {
					r.Stage = "isrc_search"
					hit, searchErr = antraSearchByISRC(service, isrc)
				} else {
					hit, searchErr = antraSearchCatalog(service, title, artist)
				}
				if searchErr != nil {
					r.Error = redact(searchErr)
					t.Error(r.Error)
					return
				}
				r.TrackID, r.Title, r.Artist = hit.TrackID, hit.Title, hit.Artist
				r.IdentityEvidence = "provider search title and artist"
				expectedMS = hit.DurationMS
				p.ISRC = hit.ISRC
				switch service {
				case "deezer":
					p.ServiceURL = "https://www.deezer.com/track/" + hit.TrackID
				case "apple":
					p.ServiceURL = "https://music.apple.com/song/sample/" + hit.TrackID
				}
			} else {
				id, _, searchErr := searchJioSaavn(p)
				if searchErr != nil {
					r.Error = redact(searchErr)
					t.Error(r.Error)
					return
				}
				r.TrackID = id
				params := url.Values{"__call": {"song.getDetails"}, "cc": {"in"}, "_format": {"json"}, "_marker": {"0"}, "pids": {id}}
				req, reqErr := NewRequestWithDefaultHeaders(http.MethodGet, jioSaavnOfficialAPI+"?"+params.Encode(), nil)
				if reqErr != nil {
					t.Fatal(reqErr)
				}
				resp, detailsErr := (&http.Client{Timeout: 12 * time.Second}).Do(WithDownloadContext(req))
				if detailsErr != nil {
					r.Error = redact(detailsErr)
					t.Error(r.Error)
					return
				}
				var details map[string]any
				decodeErr := json.NewDecoder(resp.Body).Decode(&details)
				resp.Body.Close()
				if decodeErr != nil {
					r.Error = "song details were not valid JSON"
					t.Error(r.Error)
					return
				}
				song := jioSaavnExtractSong(details, id)
				r.Title = jioSaavnString(song, "song")
				r.Artist = jioSaavnString(song, "primary_artists")
				r.IdentityEvidence = "official selected-track details"
				fmt.Sscanf(jioSaavnString(song, "duration"), "%d", &expectedMS)
				expectedMS *= 1000
			}
			r.Stage = "identity"
			if !strings.Contains(strings.ToLower(r.Title), strings.ToLower(title)) || !strings.Contains(strings.ToLower(r.Artist), strings.ToLower(artist)) {
				r.Error = "selected track does not match the requested title and artist"
				t.Error(r.Error)
				return
			}
			r.Stage = "download"
			var path string
			var downloadErr error
			switch service {
			case "amazon":
				if amazonURL != "" {
					path, downloadErr = NewAmazonDownloader().downloadFromAntraMirror(amazonURL, filepath.Dir(dest), "16")
				} else {
					path, downloadErr = antraStreamToFile(service, r.TrackID, dest, nil)
				}
			case "deezer":
				path, _, downloadErr = downloadDeezerTrack(p, dest)
			case "apple":
				path, _, downloadErr = downloadAppleTrack(p, dest)
			case "jiosaavn":
				path, _, downloadErr = downloadJioSaavnTrack(p, dest)
			default:
				path, downloadErr = antraStreamToFile(service, r.TrackID, dest, nil)
			}
			if downloadErr != nil {
				r.Error = redact(downloadErr)
				t.Error(r.Error)
				return
			}
			r.Stage = "probe"
			info, statErr := os.Stat(path)
			if statErr != nil {
				r.Error = redact(statErr)
				t.Error(r.Error)
				return
			}
			r.Bytes = info.Size()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			body, probeErr := exec.CommandContext(ctx, probe, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,sample_rate:format=duration", "-of", "json", path).Output()
			var media struct {
				Streams []struct {
					Codec string `json:"codec_name"`
					Rate  string `json:"sample_rate"`
				} `json:"streams"`
				Format struct {
					Duration string `json:"duration"`
				} `json:"format"`
			}
			if probeErr != nil || json.Unmarshal(body, &media) != nil || len(media.Streams) == 0 {
				r.Error = "download is not readable audio"
				t.Error(r.Error)
				return
			}
			fmt.Sscanf(media.Format.Duration, "%f", &r.Duration)
			r.Codec, r.SampleRate = media.Streams[0].Codec, media.Streams[0].Rate
			if r.Duration <= 35 || (expectedMS > 0 && math.Abs(r.Duration-float64(expectedMS)/1000) > math.Max(15, float64(expectedMS)/4000)) {
				r.Error = "preview or duration mismatch"
				t.Error(r.Error)
				return
			}
			r.Stage = "decode"
			if output, decodeErr := exec.CommandContext(ctx, decoder, "-v", "error", "-xerror", "-i", path, "-map", "0:a:0", "-f", "null", "-").CombinedOutput(); decodeErr != nil {
				r.Error = "audio decode failed: " + strings.TrimSpace(string(output))
				if len(r.Error) > 250 {
					r.Error = r.Error[:250]
				}
				t.Error(r.Error)
				return
			}
			r.Stage = "passed"
		})
	}
}

// Bootstrap checks deliberately stop before a human challenge. A challenge is
// recorded as an authentication boundary, never counted as a download success.
func TestLiveVerificationGatewayAvailability(t *testing.T) {
	if os.Getenv("AURALIS_LIVE_SOURCE_CHECK") != "1" {
		t.Skip("live provider diagnostic is opt-in")
	}
	root := os.Getenv("AURALIS_LIVE_RESULTS_DIR")
	if root == "" || !filepath.IsAbs(root) {
		t.Fatal("isolated results directory required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	type gatewayResult struct {
		Gateway     string `json:"gateway"`
		Provider    string `json:"provider,omitempty"`
		Status      int    `json:"http_status,omitempty"`
		Challenge   bool   `json:"challenge_returned"`
		Session     bool   `json:"session_returned"`
		CFMitigated string `json:"cf_mitigated,omitempty"`
		Error       string `json:"error,omitempty"`
	}
	var results []gatewayResult
	checks := []struct{ gateway, provider, base, version, platform string }{
		{"spotbye", "shared", GetCommunityVerifyURL(), "1.0.0", "desktop"},
		{"zarz", "tidal", zarzBaseURL, zarzAppVersionForProvider("tidal"), zarzPlatform},
		{"zarz", "qobuz", zarzBaseURL, zarzAppVersionForProvider("qbz"), zarzPlatform},
		{"zarz", "amazon", zarzBaseURL, zarzAppVersionForProvider("amazeamazeamaze"), zarzPlatform},
	}
	for _, check := range checks {
		r := gatewayResult{Gateway: check.gateway, Provider: check.provider}
		params := url.Values{"install_id": {zarzRandomHex(16)}, "app_version": {check.version}, "platform": {check.platform}}
		req, err := http.NewRequest(http.MethodGet, check.base+"/bootstrap?"+params.Encode(), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Auralis/"+check.version)
		resp, err := NewSignedHTTPClient(20 * time.Second).Do(req)
		if err != nil {
			r.Error = "bootstrap network request failed"
		} else {
			r.Status, r.CFMitigated = resp.StatusCode, resp.Header.Get("cf-mitigated")
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			var payload map[string]any
			if readErr != nil || json.Unmarshal(body, &payload) != nil {
				r.Error = "bootstrap did not return JSON"
			} else {
				r.Challenge = payload["challenge_url"] != nil || payload["auth_url"] != nil || payload["challenge_id"] != nil
				r.Session = payload["session_id"] != nil && payload["session_secret"] != nil
			}
		}
		results = append(results, r)
		data, _ := json.Marshal(r)
		t.Log(string(data))
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(root, "gateways.json"), data, 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
}
