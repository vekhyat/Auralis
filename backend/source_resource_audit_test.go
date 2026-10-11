package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type sourceAuditRow struct {
	ID         string    `json:"id"`
	Role       string    `json:"role"`
	State      string    `json:"state"`
	HTTPStatus int       `json:"http_status,omitempty"`
	MedianMS   float64   `json:"median_ms"`
	Attempts   int       `json:"attempts"`
	Successes  int       `json:"successes"`
	CheckedAt  time.Time `json:"checked_at"`
}

// Opt-in network audit. Root/metadata success is never an audio success.
// No saved sessions are loaded and verification challenges are not automated.
func TestLiveSourceResourceAudit(t *testing.T) {
	if os.Getenv("AURALIS_LIVE_SOURCE_CHECK") != "1" {
		t.Skip("live source audit is opt-in")
	}
	root := os.Getenv("AURALIS_LIVE_RESULTS_DIR")
	if !filepath.IsAbs(root) {
		t.Fatal("isolated absolute results directory required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	type target struct {
		id, role, method, raw, payload string
		validate                       func([]byte) bool
		headers                        map[string]string
	}
	jsonObject := func(body []byte) bool {
		var object map[string]any
		return json.Unmarshal(body, &object) == nil && len(object) > 0
	}
	jsonArray := func(body []byte) bool {
		var objects []map[string]any
		return json.Unmarshal(body, &objects) == nil && len(objects) > 0
	}
	checks := []target{
		{id: "resource-lrclib", role: "lyrics", raw: "https://lrclib.net/api/search?artist_name=The%20Beatles&track_name=Come%20Together", validate: jsonArray},
		{id: "resource-musicbrainz", role: "metadata", raw: musicBrainzAPIBase + "/recording/?query=isrc:GBAYE0601690&fmt=json&limit=1", validate: func(b []byte) bool {
			var p struct {
				Recordings []any `json:"recordings"`
			}
			return json.Unmarshal(b, &p) == nil && len(p.Recordings) > 0
		}},
		{id: "resource-deezer", role: "catalog", raw: "https://api.deezer.com/track/116348128", validate: func(b []byte) bool {
			var p struct {
				ID int64 `json:"id"`
			}
			return json.Unmarshal(b, &p) == nil && p.ID == 116348128
		}},
		{id: "resource-qobuz", role: "catalog", raw: qobuzAPIBaseURL + "/track/get?track_id=30369895&app_id=" + qobuzZarzCatalogAppID, validate: func(b []byte) bool {
			var p struct {
				ID int64 `json:"id"`
			}
			return json.Unmarshal(b, &p) == nil && p.ID == 30369895
		}},
		{id: "resource-tidal", role: "catalog", raw: tidalPublicSearchPath("GBAYE0601690", 1), headers: map[string]string{"X-Tidal-Token": tidalPublicToken}, validate: func(b []byte) bool {
			var p struct {
				Items []any `json:"items"`
			}
			return json.Unmarshal(b, &p) == nil && len(p.Items) > 0
		}},
		{id: "samidy-catalog", role: "catalog", raw: "https://monochrome-api.samidy.com/info/?id=55130631", validate: func(b []byte) bool {
			var p struct {
				Data struct {
					ID int64 `json:"id"`
				} `json:"data"`
			}
			return json.Unmarshal(b, &p) == nil && p.Data.ID == 55130631
		}},
		{id: "dab-xyz", role: "candidate-playback", raw: "https://dabmusic.xyz/api/stream?trackId=30369895&quality=27", validate: jsonObject},
		{id: "dab-yeet", role: "candidate-playback", raw: "https://dab.yeet.su/api/stream?trackId=30369895&quality=27", validate: jsonObject},
		{id: "lucida", role: "candidate", raw: "https://lucida.to/", validate: func(b []byte) bool { return bytes.Contains(b, []byte("Lucida")) }},
		{id: "jiosaavn-community", role: "download-url", raw: "https://saavn.dev/api/songs/buPhYncP", validate: func(b []byte) bool {
			var p struct {
				Data []struct {
					Download []any `json:"downloadUrl"`
				} `json:"data"`
			}
			return json.Unmarshal(b, &p) == nil && len(p.Data) > 0 && len(p.Data[0].Download) > 0
		}},
	}
	for _, host := range []string{"eu-central.monochrome.tf", "us-west.monochrome.tf", "arran.monochrome.tf", "api.monochrome.tf", "monochrome-api.samidy.com", "triton.squid.wtf", "wolf.qqdl.site", "maus.qqdl.site", "vogel.qqdl.site", "katze.qqdl.site", "hund.qqdl.site", "tidal.kinoplus.online", "hifi.p1nkhamster.xyz"} {
		checks = append(checks, target{id: "candidate-" + host, role: "candidate-playback", raw: "https://" + host + "/track/?id=55130631&quality=LOSSLESS", validate: func(b []byte) bool {
			var p TidalAPIResponseV2
			return json.Unmarshal(b, &p) == nil && p.Data.AssetPresentation == "FULL" && p.Data.Manifest != ""
		}})
	}
	checks = append(checks, target{id: "candidate-qobuz-squid", role: "candidate-playback", raw: "https://qobuz.squid.wtf/api/download-music?track_id=30369895&quality=6", validate: jsonObject})
	for _, service := range []string{"tidal", "qobuz", "amazon"} {
		endpoint := GetTidalCommunityDownloadURL()
		id := "55130631"
		if service == "qobuz" {
			endpoint = GetQobuzCommunityDownloadURL()
			id = "30369895"
		}
		if service == "amazon" {
			endpoint = GetAmazonCommunityDownloadURL()
			id = "B07FSSGBJV"
		}
		checks = append(checks, target{id: "community-" + service, role: "download-url", method: http.MethodPost, raw: endpoint, payload: fmt.Sprintf(`{"id":%q,"quality":"16"}`, id), validate: jsonObject})
		params := url.Values{"install_id": {zarzRandomHex(16)}, "app_version": {zarzAppVersionForProvider(map[string]string{"tidal": "tidal", "qobuz": "qbz", "amazon": "amazeamazeamaze"}[service])}, "platform": {zarzPlatform}}
		checks = append(checks, target{id: "zarz-" + service, role: "verification", raw: zarzBaseURL + "/bootstrap?" + params.Encode(), validate: func(b []byte) bool {
			var p map[string]any
			if json.Unmarshal(b, &p) != nil {
				return false
			}
			return p["session_id"] != nil && p["session_secret"] != nil
		}})
	}
	var rows []sourceAuditRow
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, check := range checks {
		wg.Add(1)
		go func(c target) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			row := sourceAuditRow{ID: c.id, Role: c.role, State: "failed", CheckedAt: time.Now().UTC()}
			var times []float64
			for i := 0; i < 3; i++ {
				row.Attempts++
				start := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				method := c.method
				if method == "" {
					method = http.MethodGet
				}
				req, err := http.NewRequestWithContext(ctx, method, c.raw, strings.NewReader(c.payload))
				if err != nil {
					cancel()
					break
				}
				req.Header.Set("User-Agent", "Auralis/source-audit (https://github.com/vekhyat/Auralis)")
				req.Header.Set("Accept", "application/json")
				if c.payload != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				for k, v := range c.headers {
					req.Header.Set(k, v)
				}
				resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
				if err == nil {
					row.HTTPStatus = resp.StatusCode
					body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
					resp.Body.Close()
					if readErr == nil && resp.StatusCode == 200 && c.validate(body) {
						row.Successes++
						row.State = "available"
					} else if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 428 {
						row.State = "authentication_required"
					} else if resp.StatusCode == 429 {
						row.State = "rate_limited"
					}
					if c.role == "verification" && resp.StatusCode == 200 && row.Successes == 0 {
						row.State = "authentication_required"
					}
				}
				times = append(times, float64(time.Since(start))/float64(time.Millisecond))
				cancel()
				if row.State == "authentication_required" || row.State == "rate_limited" {
					break
				}
			}
			sort.Float64s(times)
			if len(times) > 0 {
				row.MedianMS = times[len(times)/2]
			}
			mu.Lock()
			rows = append(rows, row)
			mu.Unlock()
		}(check)
	}
	wg.Wait()
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	body, err := json.MarshalIndent(rows, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(root, "resource-audit.json"), body, 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		b, _ := json.Marshal(row)
		t.Log(string(b))
	}
}
