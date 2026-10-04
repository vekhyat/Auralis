package backend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestQobuzSearchUsesAntraBeforeCatalog(t *testing.T) {
	isolatedDownloadDir(t)
	var sawISRC atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search/isrc/GBAYE0601690" {
			http.NotFound(w, r)
			return
		}
		sawISRC.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"track_id": "30369895",
			"title":    "Come Together",
			"artist":   "The Beatles",
			"isrc":     "GBAYE0601690",
		})
	}))
	defer srv.Close()
	manifest := antraEndpointManifest{APIKey: "contract-test-key"}
	manifest.Mirrors.Qobuz = srv.URL
	defer seedAntraManifest(manifest)()

	track, err := NewQobuzDownloader().searchByISRC("GBAYE0601690", "Other Title", "Other Artist", "Other Album", "")
	if err != nil {
		t.Fatal(err)
	}
	if track.ID != 30369895 {
		t.Fatalf("id %d", track.ID)
	}
	if track.Title != "Come Together" || track.ISRC != "GBAYE0601690" {
		t.Fatalf("metadata %+v", track)
	}
	if !sawISRC.Load() {
		t.Fatal("antra ISRC search was not used")
	}
}

func TestQobuzDirectIDSkipsCatalog(t *testing.T) {
	track, err := NewQobuzDownloader().searchByISRC("qobuz_30369895", "Come Together", "The Beatles", "Abbey Road", "")
	if err != nil {
		t.Fatal(err)
	}
	if track.ID != 30369895 || track.Title != "Come Together" {
		t.Fatalf("track %+v", track)
	}
}

func TestQobuzDownloadUsesAntraBeforeGateways(t *testing.T) {
	dir := isolatedDownloadDir(t)
	var sawStream atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/stream/30369895" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("quality") != "" {
			http.Error(w, "unexpected quality", http.StatusBadRequest)
			return
		}
		sawStream.Store(true)
		w.Header().Set("Content-Type", "audio/flac")
		_, _ = w.Write([]byte("fLaC-test-audio"))
	}))
	defer srv.Close()
	manifest := antraEndpointManifest{APIKey: "contract-test-key"}
	manifest.Mirrors.Qobuz = srv.URL
	defer seedAntraManifest(manifest)()

	var gatewayCalls atomic.Int32
	prev := fetchQobuzGatewayURL
	fetchQobuzGatewayURL = func(q *QobuzDownloader, trackID int64, qualityCode string, allowFallback bool) (string, error) {
		gatewayCalls.Add(1)
		return "", fmt.Errorf("gateway should not run")
	}
	defer func() { fetchQobuzGatewayURL = prev }()

	path, err := NewQobuzDownloader().downloadQobuzTrackFile(30369895, "6", filepath.Join(dir, "track.flac"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !sawStream.Load() {
		t.Fatal("antra stream was not used")
	}
	if gatewayCalls.Load() != 0 {
		t.Fatal("gateway ran before antra succeeded")
	}
	if filepath.Base(path) == "" {
		t.Fatal("empty path")
	}
}

func TestQobuzCustomInstancePrecedesAntra(t *testing.T) {
	dir := isolatedDownloadDir(t)
	var sawAntra atomic.Bool
	var sawCustom atomic.Bool
	var custom *httptest.Server
	custom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/download-music":
			sawCustom.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data":    map[string]string{"url": custom.URL + "/file"},
			})
		case "/file":
			w.Header().Set("Content-Type", "audio/flac")
			_, _ = w.Write([]byte("fLaC-custom"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer custom.Close()
	antra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAntra.Store(true)
		http.NotFound(w, r)
	}))
	defer antra.Close()
	manifest := antraEndpointManifest{APIKey: "contract-test-key"}
	manifest.Mirrors.Qobuz = antra.URL
	defer seedAntraManifest(manifest)()

	q := NewQobuzDownloader()
	q.customURL = custom.URL
	if _, err := q.downloadQobuzTrackFile(30369895, "6", filepath.Join(dir, "track.flac"), true); err != nil {
		t.Fatal(err)
	}
	if !sawCustom.Load() {
		t.Fatal("custom instance was not used")
	}
	if sawAntra.Load() {
		t.Fatal("antra ran before the custom instance")
	}
}
