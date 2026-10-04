package taste

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend/credentials"
)

func TestLastFMSource(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "auralis-taste-lastfm-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	credStore, err := credentials.OpenAt(tempDir)
	if err != nil {
		t.Fatalf("failed to open cred store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Query().Get("method")
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "user.getTopArtists":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"topartists": map[string]interface{}{
					"artist": []map[string]interface{}{
						{
							"name": "Aphex Twin",
							"mbid": "mbid-1",
							"image": []map[string]string{
								{"#text": "https://example.com/aphex.png"},
							},
						},
					},
					"@attr": map[string]string{"page": "1", "totalPages": "1"},
				},
			})
		case "user.getTopAlbums":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"topalbums": map[string]interface{}{
					"album": []map[string]interface{}{
						{
							"name": "Selected Ambient Works 85-92",
							"artist": map[string]string{"name": "Aphex Twin"},
							"image": []map[string]string{
								{"#text": "https://example.com/saw.png"},
							},
						},
					},
					"@attr": map[string]string{"page": "1", "totalPages": "1"},
				},
			})
		case "user.getLovedTracks":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"lovedtracks": map[string]interface{}{
					"track": []map[string]interface{}{
						{
							"name":   "Xtal",
							"artist": map[string]string{"name": "Aphex Twin"},
							"date":   map[string]string{"uts": "1672531199"},
						},
					},
					"@attr": map[string]string{"page": "1", "totalPages": "1"},
				},
			})
		case "user.getRecentTracks":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"recenttracks": map[string]interface{}{
					"track": []map[string]interface{}{
						{
							"name":   "Pulsewidth",
							"artist": map[string]string{"name": "Aphex Twin"},
							"album":  map[string]string{"#text": "Selected Ambient Works 85-92"},
							"date":   map[string]string{"uts": "1672531200"},
						},
					},
					"@attr": map[string]string{"page": "1", "totalPages": "1"},
				},
			})
		case "artist.getSimilar":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"similarartists": map[string]interface{}{
					"artist": []map[string]interface{}{
						{
							"name":  "Boards of Canada",
							"match": "0.95",
							"image": []map[string]string{
								{"#text": "https://example.com/boc.png"},
							},
						},
					},
				},
			})
		default:
			http.Error(w, "unknown method", http.StatusNotFound)
		}
	}))
	defer server.Close()

	source := &LastFMSource{
		Store:      credStore,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	if source.Connected() {
		t.Fatalf("expected source not to be connected initially")
	}

	if err := source.SetCredentials("test-api-key", "test-user"); err != nil {
		t.Fatalf("SetCredentials failed: %v", err)
	}

	if !source.Connected() {
		t.Fatalf("expected source to be connected after setting credentials")
	}

	ctx := context.Background()

	// Test Pull (top artists, loved tracks, recent tracks)
	events, err := source.Pull(ctx, time.Time{})
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events from pull, got %d", len(events))
	}

	// Test GetSimilar
	sim, err := source.GetSimilar(ctx, "Aphex Twin", 5)
	if err != nil {
		t.Fatalf("GetSimilar failed: %v", err)
	}
	if len(sim) != 1 || sim[0].Name != "Boards of Canada" {
		t.Fatalf("unexpected similar artist result: %+v", sim)
	}

	// Test Disconnect
	if err := source.Disconnect(); err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}
	if source.Connected() {
		t.Fatalf("expected disconnected after Disconnect()")
	}
}
