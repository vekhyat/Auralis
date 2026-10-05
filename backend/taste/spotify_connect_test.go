package taste

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend/credentials"
)

func TestSpotifyConnectLoopbackFlow(t *testing.T) {
	store, err := credentials.OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var gotVerifier, gotRedirect string
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/token" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		gotVerifier = r.Form.Get("code_verifier")
		gotRedirect = r.Form.Get("redirect_uri")
		if r.Form.Get("code") != "good-code" {
			http.Error(w, "bad code", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "at", "refresh_token": "rt", "expires_in": 3600})
	}))
	defer accounts.Close()

	src := &SpotifyAPISource{Store: store, ClientID: "cid", AccountsBase: accounts.URL, ListenAddr: "127.0.0.1:0", Timeout: 5 * time.Second}
	src.OpenURL = func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
			t.Errorf("authorize URL lacks PKCE: %s", authURL)
		}
		redirect := q.Get("redirect_uri")
		callback, err := url.Parse(redirect)
		if err != nil || callback.Hostname() != "127.0.0.1" || callback.Port() == "" || callback.Port() == "0" || callback.Path != "" {
			t.Fatalf("callback must use the registered loopback root with a dynamically assigned port: %q", redirect)
		}
		go func() {
			// A forged callback without our state must be ignored...
			if resp, err := http.Get(redirect + "?code=evil&state=wrong"); err == nil {
				if resp.StatusCode != http.StatusBadRequest {
					t.Errorf("forged callback status = %d", resp.StatusCode)
				}
				resp.Body.Close()
			}
			// ...and the real one completes the flow.
			if resp, err := http.Get(redirect + "?code=good-code&state=" + url.QueryEscape(q.Get("state"))); err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	if err := src.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotVerifier == "" || gotRedirect == "" {
		t.Fatalf("token exchange missing verifier/redirect: %q %q", gotVerifier, gotRedirect)
	}
	if refresh, err := store.Get(spotifyRefreshKey); err != nil || refresh != "rt" {
		t.Fatalf("refresh token not stored: %q %v", refresh, err)
	}
}

func TestSpotifyRegisteredRedirectUsesPortlessLoopback(t *testing.T) {
	src := &SpotifyAPISource{}
	if SpotifyRedirectURI != "http://127.0.0.1" || src.listenAddr() != "127.0.0.1:0" {
		t.Fatal("Spotify registration and listener must support dynamic loopback ports")
	}
}

func TestSpotifyPaginationStaysOnAPIHost(t *testing.T) {
	src := &SpotifyAPISource{APIBase: "https://api.spotify.com"}
	if !src.sameHost("https://api.spotify.com/v1/me/tracks?offset=50") {
		t.Fatal("same host rejected")
	}
	for _, link := range []string{"https://evil.example/v1/me/tracks", "http://api.spotify.com/v1/me/tracks"} {
		if src.sameHost(link) {
			t.Fatalf("token would be sent to %s", link)
		}
	}
}
