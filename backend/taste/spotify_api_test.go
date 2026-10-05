package taste

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend/credentials"
)

func TestSpotifyAPIRefreshAndRetry(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "auralis-taste-spotify-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	credStore, err := credentials.OpenAt(tempDir)
	if err != nil {
		t.Fatalf("failed to open cred store: %v", err)
	}

	if err := credStore.Put(spotifyClientIDKey, "test-client-id"); err != nil {
		t.Fatalf("failed to put client id: %v", err)
	}
	if err := credStore.Put(spotifyRefreshKey, "test-refresh-token"); err != nil {
		t.Fatalf("failed to put refresh token: %v", err)
	}

	var tokenRequests int32
	var apiAttempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token":
			atomic.AddInt32(&tokenRequests, 1)
			grantType := r.FormValue("grant_type")
			if grantType != "refresh_token" && grantType != "authorization_code" {
				http.Error(w, "bad grant type", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "mock-access-token-123",
				"refresh_token": "updated-refresh-token",
				"expires_in":    3600,
			})
		case "/v1/me/top/artists":
			count := atomic.AddInt32(&apiAttempts, 1)
			if count == 1 {
				// Test 429 Retry-After handling
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			auth := r.Header.Get("Authorization")
			if auth != "Bearer mock-access-token-123" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"id":     "artist-1",
						"name":   "Daft Punk",
						"genres": []string{"french house", "electronic"},
						"images": []map[string]string{{"url": "https://example.com/daft.jpg"}},
					},
				},
				"next": nil,
			})
		default:
			// Default empty responses for other endpoints
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/v1/me/following" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"artists": map[string]interface{}{
						"items":   []interface{}{},
						"cursors": map[string]string{"after": ""},
						"next":    nil,
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"items": []interface{}{},
					"next":  nil,
				})
			}
		}
	}))
	defer server.Close()

	source := &SpotifyAPISource{
		Store:        credStore,
		APIBase:      server.URL,
		AccountsBase: server.URL,
		HTTPClient:   server.Client(),
	}

	ctx := context.Background()

	// 1. Pull events and verify token exchange + retry logic
	events, err := source.Pull(ctx, time.Time{})
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}

	if atomic.LoadInt32(&tokenRequests) == 0 {
		t.Fatalf("expected token request during Pull")
	}
	if atomic.LoadInt32(&apiAttempts) < 2 {
		t.Fatalf("expected retry after 429")
	}

	// Verify updated refresh token in store
	savedRef, err := credStore.Get(spotifyRefreshKey)
	if err != nil || savedRef != "updated-refresh-token" {
		t.Fatalf("expected updated refresh token, got %s (err: %v)", savedRef, err)
	}

	// Verify parsed top artist with genres
	var foundArtist bool
	for _, ev := range events {
		if ev.Artist == "Daft Punk" && ev.Kind == KindTopArtist {
			foundArtist = true
			if len(ev.Genres) != 2 || ev.Genres[0] != "french house" {
				t.Fatalf("unexpected genres on event: %+v", ev.Genres)
			}
			if ev.Image != "https://example.com/daft.jpg" {
				t.Fatalf("unexpected image: %s", ev.Image)
			}
		}
	}
	if !foundArtist {
		t.Fatalf("expected Daft Punk top artist in pulled events")
	}
}

func TestSpotifyPagination(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "auralis-taste-spotify-paging-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	credStore, err := credentials.OpenAt(tempDir)
	if err != nil {
		t.Fatalf("failed to open cred store: %v", err)
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/token":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "token-xyz",
				"expires_in":   3600,
			})
		case "/paged":
			page := r.URL.Query().Get("page")
			if page == "" || page == "1" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"items": []map[string]string{{"id": "item1"}},
					"next":  serverURL + "/paged?page=2",
				})
			} else {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"items": []map[string]string{{"id": "item2"}},
					"next":  nil,
				})
			}
		}
	}))
	defer server.Close()
	serverURL = server.URL

	credStore.Put(spotifyClientIDKey, "client")
	credStore.Put(spotifyRefreshKey, "refresh")

	source := &SpotifyAPISource{
		Store:        credStore,
		APIBase:      serverURL,
		AccountsBase: serverURL,
		HTTPClient:   server.Client(),
	}

	ctx := context.Background()
	items, err := source.getPagedList(ctx, serverURL+"/paged")
	if err != nil {
		t.Fatalf("getPagedList failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items across pages, got %d", len(items))
	}
}
