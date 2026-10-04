package taste

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vekhyat/Auralis/backend/credentials"
)

const (
	spotifyRefreshKey   = "spotify.refresh"
	spotifyClientIDKey  = "spotify.client_id"
	spotifyScopeRequest = "user-top-read user-library-read user-read-recently-played user-follow-read playlist-read-private"

	// SpotifyRedirectPort is fixed because Spotify only accepts redirect
	// URIs registered exactly in the user's developer app.
	SpotifyRedirectPort = 43821
	// SpotifyRedirectURI is what users register in their Spotify app.
	SpotifyRedirectURI = "http://127.0.0.1:43821/callback"
)

// SpotifyAPISource is a TasteSource backed by the official Spotify Web API
// with a bring-your-own client ID. It only ever reads.
type SpotifyAPISource struct {
	Store        *credentials.Store
	ClientID     string
	APIBase      string // default https://api.spotify.com
	AccountsBase string // default https://accounts.spotify.com
	HTTPClient   *http.Client
	// OpenURL launches the system browser. When nil, the caller must
	// authorize via AuthorizeURL instead (used by tests).
	OpenURL func(string) error
	// Timeout bounds the loopback OAuth wait. Default 3 minutes.
	Timeout time.Duration
	// ListenAddr overrides the loopback listener (tests use port 0).
	ListenAddr string

	mu    sync.Mutex
	token *tokenState
}

type tokenState struct {
	AccessToken string
	ExpiresAt   time.Time
}

func (s *SpotifyAPISource) apiBase() string {
	if s.APIBase != "" {
		return s.APIBase
	}
	return "https://api.spotify.com"
}

func (s *SpotifyAPISource) accountsBase() string {
	if s.AccountsBase != "" {
		return s.AccountsBase
	}
	return "https://accounts.spotify.com"
}

func (s *SpotifyAPISource) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s *SpotifyAPISource) ID() string { return "spotify_api" }

// ListenAddr overrides the loopback address in tests.
func (s *SpotifyAPISource) listenAddr() string {
	if s.ListenAddr != "" {
		return s.ListenAddr
	}
	return fmt.Sprintf("127.0.0.1:%d", SpotifyRedirectPort)
}

// sameHost reports whether a pagination link points at the API host, so the
// bearer token is never sent anywhere else.
func (s *SpotifyAPISource) sameHost(link string) bool {
	next, err := url.Parse(link)
	if err != nil {
		return false
	}
	base, err := url.Parse(s.apiBase())
	if err != nil {
		return false
	}
	return next.Scheme == base.Scheme && next.Host == base.Host
}

// Connected reports whether a refresh token is stored.
func (s *SpotifyAPISource) Connected() bool {
	_, err := s.Store.Get(spotifyRefreshKey)
	return err == nil
}

func (s *SpotifyAPISource) clientID() (string, error) {
	if strings.TrimSpace(s.ClientID) != "" {
		return s.ClientID, nil
	}
	id, err := s.Store.Get(spotifyClientIDKey)
	if err != nil {
		return "", errors.New("spotify client ID is not configured")
	}
	return id, nil
}

// SetClientID persists the BYO client id in the credential store.
func (s *SpotifyAPISource) SetClientID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("spotify client ID is empty")
	}
	if err := s.Store.Put(spotifyClientIDKey, id); err != nil {
		return err
	}
	s.mu.Lock()
	s.ClientID = id
	s.mu.Unlock()
	return nil
}

// Disconnect removes stored tokens. It does NOT revoke access on Spotify's side.
func (s *SpotifyAPISource) Disconnect() error {
	_ = s.Store.Delete(spotifyRefreshKey)
	s.mu.Lock()
	s.token = nil
	s.mu.Unlock()
	return nil
}

// AuthorizeURL builds the authorize URL for a manual (non-browser) flow.
func (s *SpotifyAPISource) AuthorizeURL(redirectURI, state, challenge string) (string, error) {
	id, err := s.clientID()
	if err != nil {
		return "", err
	}
	v := url.Values{}
	v.Set("client_id", id)
	v.Set("response_type", "code")
	v.Set("redirect_uri", redirectURI)
	v.Set("code_challenge_method", "S256")
	v.Set("code_challenge", challenge)
	v.Set("state", state)
	v.Set("scope", spotifyScopeRequest)
	return s.accountsBase() + "/authorize?" + v.Encode(), nil
}

// Connect performs OAuth Authorization Code + PKCE over a local loopback listener.
// It opens the system browser and blocks until the redirect completes or ctx/timeout fires.
func (s *SpotifyAPISource) Connect(ctx context.Context) error {
	verifier, err := pkceVerifier()
	if err != nil {
		return fmt.Errorf("pkce verifier: %w", err)
	}
	state, err := randomState()
	if err != nil {
		return fmt.Errorf("oauth state: %w", err)
	}

	listener, err := net.Listen("tcp", s.listenAddr())
	if err != nil {
		return fmt.Errorf("spotify connect: port %d is in use by another program: %w", SpotifyRedirectPort, err)
	}
	defer listener.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)

	callbackCh := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// Anything without our state did not come from this sign-in, so it
		// is ignored rather than allowed to end the flow.
		if !validState(state, q.Get("state")) {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if errStr := q.Get("error"); errStr != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "<html><body style=\"font-family:sans-serif;padding:32px;\"><h2>Auralis Spotify Connection Failed</h2><p>%s</p></body></html>", html.EscapeString(errStr))
			select {
			case callbackCh <- callbackResult{err: fmt.Errorf("spotify authorization denied: %s", errStr)}:
			default:
			}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintln(w, "<html><body style=\"font-family:sans-serif;padding:32px;text-align:center;\"><h2>Auralis</h2><p>Connected to Spotify! You can close this tab and return to the app.</p></body></html>")
		select {
		case callbackCh <- callbackResult{code: q.Get("code"), state: q.Get("state")}:
		default:
		}
	})

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Close()

	authURL, err := s.AuthorizeURL(redirectURI, state, pkceChallenge(verifier))
	if err != nil {
		return err
	}
	if s.OpenURL != nil {
		if err := s.OpenURL(authURL); err != nil {
			return fmt.Errorf("open browser: %w", err)
		}
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return errors.New("spotify connect timed out waiting for the browser")
	case cb := <-callbackCh:
		if cb.err != nil {
			return cb.err
		}
		if !validState(state, cb.state) {
			return errors.New("spotify connect: state mismatch")
		}
		if cb.code == "" {
			return errors.New("spotify connect: empty code")
		}
		return s.exchangeCode(ctx, cb.code, verifier, redirectURI)
	}
}

type callbackResult struct {
	code  string
	state string
	err   error
}

func (s *SpotifyAPISource) exchangeCode(ctx context.Context, code, verifier, redirectURI string) error {
	id, err := s.clientID()
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("client_id", id)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)
	resp, err := s.doWithRetry(func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.accountsBase()+"/api/token", bytes.NewBufferString(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return err
	}
	if out.AccessToken == "" {
		return errors.New("spotify token exchange failed")
	}
	if out.RefreshToken != "" {
		if err := s.Store.Put(spotifyRefreshKey, out.RefreshToken); err != nil {
			return err
		}
	}
	expiresIn := out.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	s.mu.Lock()
	s.token = &tokenState{AccessToken: out.AccessToken, ExpiresAt: time.Now().Add(time.Duration(expiresIn) * time.Second)}
	s.mu.Unlock()
	return nil
}

// refresh rotates the refresh token if Spotify returns a new one.
func (s *SpotifyAPISource) refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshLocked(ctx)
}

func (s *SpotifyAPISource) refreshLocked(ctx context.Context) error {
	id, err := s.clientID()
	if err != nil {
		return err
	}
	refreshToken, err := s.Store.Get(spotifyRefreshKey)
	if err != nil {
		return errors.New("spotify is not connected")
	}
	form := url.Values{}
	form.Set("client_id", id)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	resp, err := s.doWithRetry(func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.accountsBase()+"/api/token", bytes.NewBufferString(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return err
	}
	if out.AccessToken == "" {
		return errors.New("spotify token refresh failed")
	}
	if out.RefreshToken != "" && out.RefreshToken != refreshToken {
		if err := s.Store.Put(spotifyRefreshKey, out.RefreshToken); err != nil {
			return err
		}
	}
	expiresIn := out.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	s.token = &tokenState{AccessToken: out.AccessToken, ExpiresAt: time.Now().Add(time.Duration(expiresIn) * time.Second)}
	return nil
}

func (s *SpotifyAPISource) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != nil && time.Now().Before(s.token.ExpiresAt.Add(-30*time.Second)) {
		return s.token.AccessToken, nil
	}
	if err := s.refreshLocked(ctx); err != nil {
		return "", err
	}
	return s.token.AccessToken, nil
}

// doWithRetry retries on 429 honoring Retry-After, and on 5xx up to 3 tries.
// newReq must return a fresh request each time (bodies are consumed on Do).
func (s *SpotifyAPISource) doWithRetry(newReq func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := newReq()
		if err != nil {
			return nil, err
		}
		resp, err := s.httpClient().Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := 1 * time.Second
			if h := resp.Header.Get("Retry-After"); h != "" {
				if n, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && n >= 0 {
					wait = time.Duration(n) * time.Second
				}
			}
			resp.Body.Close()
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(wait):
			}
			continue
		}
		if resp.StatusCode >= 500 && attempt < 2 {
			resp.Body.Close()
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
			continue
		}
		if resp.StatusCode >= 400 {
			defer resp.Body.Close()
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
			return nil, fmt.Errorf("spotify api %s: HTTP %d: %s", req.URL.Path, resp.StatusCode, strings.TrimSpace(string(raw)))
		}
		return resp, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("spotify api: request failed after retries")
}

func (s *SpotifyAPISource) getJSON(ctx context.Context, u string, out interface{}) error {
	token, err := s.accessToken(ctx)
	if err != nil {
		return err
	}
	resp, err := s.doWithRetry(func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return req, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// getPagedList follows Spotify's offset pagination (`next`) and returns every
// item as raw JSON, capped to keep memory bounded.
func (s *SpotifyAPISource) getPagedList(ctx context.Context, url string) ([]json.RawMessage, error) {
	var items []json.RawMessage
	for url != "" && len(items) < 2000 {
		var page struct {
			Items json.RawMessage `json:"items"`
			Next  string          `json:"next"`
		}
		if err := s.getJSON(ctx, url, &page); err != nil {
			return items, err
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(page.Items, &rows); err != nil {
			return items, err
		}
		items = append(items, rows...)
		url = page.Next
		if url != "" && !s.sameHost(url) {
			return items, fmt.Errorf("spotify api: refusing pagination link to another host")
		}
	}
	return items, nil
}

// Pull fetches top artists/tracks (all three ranges), saved tracks/albums,
// followed artists and recently played, mapped to taste events.
func (s *SpotifyAPISource) Pull(ctx context.Context, since time.Time) ([]TasteEvent, error) {
	var events []TasteEvent
	ranges := []string{"long_term", "medium_term", "short_term"}
	for _, r := range ranges {
		artists, err := s.getPagedList(ctx, s.apiBase()+"/v1/me/top/artists?time_range="+r+"&limit=50")
		if err != nil {
			return events, err
		}
		for _, raw := range artists {
			var a struct {
				ID     string   `json:"id"`
				Name   string   `json:"name"`
				Genres []string `json:"genres"`
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			}
			if json.Unmarshal(raw, &a) == nil && a.ID != "" {
				e := TasteEvent{Kind: KindTopArtist, Artist: a.Name, ArtistID: a.ID, Genres: a.Genres, Timestamp: time.Now(), Source: "spotify_api"}
				if len(a.Images) > 0 {
					e.Image = a.Images[0].URL
				}
				e.ID = eventID("spotify_api", string(KindTopArtist), r, a.ID)
				events = append(events, e)
			}
		}
		tracks, err := s.getPagedList(ctx, s.apiBase()+"/v1/me/top/tracks?time_range="+r+"&limit=50")
		if err != nil {
			return events, err
		}
		for _, raw := range tracks {
			var t struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Name   string `json:"name"`
					Images []struct {
						URL string `json:"url"`
					} `json:"images"`
				} `json:"album"`
				ExternalIDs struct {
					ISRC string `json:"isrc"`
				} `json:"external_ids"`
			}
			if json.Unmarshal(raw, &t) == nil && t.ID != "" {
				e := TasteEvent{Kind: KindTopTrack, SpotifyID: t.ID, ISRC: t.ExternalIDs.ISRC, Title: t.Name, Album: t.Album.Name, Timestamp: time.Now(), Source: "spotify_api"}
				if len(t.Artists) > 0 {
					e.Artist = t.Artists[0].Name
				}
				if len(t.Album.Images) > 0 {
					e.Image = t.Album.Images[0].URL
				}
				e.ID = eventID("spotify_api", string(KindTopTrack), r, t.ID)
				events = append(events, e)
			}
		}
	}

	savedTracks, err := s.getPagedList(ctx, s.apiBase()+"/v1/me/tracks?limit=50")
	if err != nil {
		return events, err
	}
	for _, raw := range savedTracks {
		var row struct {
			AddedAt string `json:"added_at"`
			Track   struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Name   string `json:"name"`
					Images []struct {
						URL string `json:"url"`
					} `json:"images"`
				} `json:"album"`
				ExternalIDs struct {
					ISRC string `json:"isrc"`
				} `json:"external_ids"`
			} `json:"track"`
		}
		if json.Unmarshal(raw, &row) == nil && row.Track.ID != "" {
			t := row.Track
			e := TasteEvent{Kind: KindSaveTrack, SpotifyID: t.ID, ISRC: t.ExternalIDs.ISRC, Title: t.Name, Album: t.Album.Name, Timestamp: parseSpotifyTime(row.AddedAt), Source: "spotify_api"}
			if len(t.Artists) > 0 {
				e.Artist = t.Artists[0].Name
			}
			if len(t.Album.Images) > 0 {
				e.Image = t.Album.Images[0].URL
			}
			e.ID = eventID("spotify_api", string(KindSaveTrack), t.ID)
			events = append(events, e)
		}
	}

	savedAlbums, err := s.getPagedList(ctx, s.apiBase()+"/v1/me/albums?limit=50")
	if err != nil {
		return events, err
	}
	for _, raw := range savedAlbums {
		var row struct {
			AddedAt string `json:"added_at"`
			Album   struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"album"`
		}
		if json.Unmarshal(raw, &row) == nil && row.Album.ID != "" {
			a := row.Album
			e := TasteEvent{Kind: KindSaveAlbum, AlbumID: a.ID, Album: a.Name, Timestamp: parseSpotifyTime(row.AddedAt), Source: "spotify_api"}
			if len(a.Artists) > 0 {
				e.Artist = a.Artists[0].Name
			}
			if len(a.Images) > 0 {
				e.Image = a.Images[0].URL
			}
			e.ID = eventID("spotify_api", string(KindSaveAlbum), a.ID)
			events = append(events, e)
		}
	}

	// Followed artists use cursor pagination (`after`).
	followURL := s.apiBase() + "/v1/me/following?type=artist&limit=50"
	for followURL != "" {
		var payload struct {
			Artists struct {
				Items []struct {
					ID     string   `json:"id"`
					Name   string   `json:"name"`
					Genres []string `json:"genres"`
					Images []struct {
						URL string `json:"url"`
					} `json:"images"`
				} `json:"items"`
				Cursors struct {
					After string `json:"after"`
				} `json:"cursors"`
				Next string `json:"next"`
			} `json:"artists"`
		}
		if err := s.getJSON(ctx, followURL, &payload); err != nil {
			return events, err
		}
		for _, a := range payload.Artists.Items {
			e := TasteEvent{Kind: KindFollow, Artist: a.Name, ArtistID: a.ID, Genres: a.Genres, Timestamp: time.Now(), Source: "spotify_api"}
			if len(a.Images) > 0 {
				e.Image = a.Images[0].URL
			}
			e.ID = eventID("spotify_api", string(KindFollow), a.ID)
			events = append(events, e)
		}
		followURL = payload.Artists.Next
		if followURL != "" && !s.sameHost(followURL) {
			return events, fmt.Errorf("spotify api: refusing pagination link to another host")
		}
	}

	recent, err := s.getRecentlyPlayed(ctx)
	if err != nil {
		return events, err
	}
	events = append(events, recent...)

	if !since.IsZero() {
		filtered := events[:0]
		for _, e := range events {
			if e.Kind == KindPlay && e.Timestamp.Before(since) {
				continue
			}
			filtered = append(filtered, e)
		}
		events = filtered
	}
	return events, nil
}

func (s *SpotifyAPISource) getRecentlyPlayed(ctx context.Context) ([]TasteEvent, error) {
	var out []TasteEvent
	var payload struct {
		Items []struct {
			PlayedAt string `json:"played_at"`
			Track    struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Name   string `json:"name"`
					Images []struct {
						URL string `json:"url"`
					} `json:"images"`
				} `json:"album"`
				ExternalIDs struct {
					ISRC string `json:"isrc"`
				} `json:"external_ids"`
			} `json:"track"`
		} `json:"items"`
	}
	if err := s.getJSON(ctx, s.apiBase()+"/v1/me/player/recently-played?limit=50", &payload); err != nil {
		return nil, err
	}
	for _, row := range payload.Items {
		e := TasteEvent{Kind: KindPlay, SpotifyID: row.Track.ID, ISRC: row.Track.ExternalIDs.ISRC, Title: row.Track.Name, Album: row.Track.Album.Name, Timestamp: parseSpotifyTime(row.PlayedAt), Source: "spotify_api"}
		if len(row.Track.Artists) > 0 {
			e.Artist = row.Track.Artists[0].Name
		}
		if len(row.Track.Album.Images) > 0 {
			e.Image = row.Track.Album.Images[0].URL
		}
		e.ID = eventID("spotify_api", string(KindPlay), row.Track.ID, row.PlayedAt)
		out = append(out, e)
	}
	return out, nil
}

func parseSpotifyTime(value string) time.Time {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	return time.Now()
}
