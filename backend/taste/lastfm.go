package taste

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vekhyat/Auralis/backend/credentials"
)

const (
	lastfmKeyKey    = "lastfm.api_key"
	lastfmUserKey   = "lastfm.username"
	lastfmDefaultBase = "https://ws.audioscrobbler.com/2.0/"
)

// LastFMSource is a TasteSource over the public Last.fm read API. The API
// key and username are user-supplied and stored in the credential store.
type LastFMSource struct {
	Store      *credentials.Store
	APIKey     string
	Username   string
	BaseURL    string // injectable for tests
	HTTPClient *http.Client
}

func (s *LastFMSource) baseURL() string {
	if s.BaseURL != "" {
		return s.BaseURL
	}
	return lastfmDefaultBase
}

func (s *LastFMSource) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s *LastFMSource) ID() string { return "lastfm" }

func (s *LastFMSource) Connected() bool {
	_, errKey := s.Store.Get(lastfmKeyKey)
	_, errUser := s.Store.Get(lastfmUserKey)
	return errKey == nil && errUser == nil
}

func (s *LastFMSource) credentials2() (apiKey, username string, err error) {
	apiKey = strings.TrimSpace(s.APIKey)
	username = strings.TrimSpace(s.Username)
	if apiKey == "" {
		apiKey, _ = s.Store.Get(lastfmKeyKey)
	}
	if username == "" {
		username, _ = s.Store.Get(lastfmUserKey)
	}
	if apiKey == "" || username == "" {
		return "", "", errors.New("last.fm is not configured")
	}
	return apiKey, username, nil
}

// SetCredentials persists the key/username (or clears them with empties).
func (s *LastFMSource) SetCredentials(apiKey, username string) error {
	apiKey = strings.TrimSpace(apiKey)
	username = strings.TrimSpace(username)
	if apiKey == "" || username == "" {
		return errors.New("last.fm API key and username are both required")
	}
	if err := s.Store.Put(lastfmKeyKey, apiKey); err != nil {
		return err
	}
	if err := s.Store.Put(lastfmUserKey, username); err != nil {
		return err
	}
	s.APIKey = apiKey
	s.Username = username
	return nil
}

func (s *LastFMSource) Disconnect() error {
	_ = s.Store.Delete(lastfmKeyKey)
	_ = s.Store.Delete(lastfmUserKey)
	s.APIKey = ""
	s.Username = ""
	return nil
}

func (s *LastFMSource) call(ctx context.Context, params url.Values, out interface{}) error {
	apiKey, _, err := s.credentials2()
	if err != nil {
		return err
	}
	params.Set("api_key", apiKey)
	params.Set("format", "json")
	url := s.baseURL() + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	var resp *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = s.httpClient().Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == 429 {
			wait := time.Duration(attempt+1) * time.Second
			resp.Body.Close()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		break
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("last.fm %s: HTTP %d: %s", params.Get("method"), resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

type lastfmArtistPage struct {
	TopArtists struct {
		Artist []struct {
			Name string `json:"name"`
			Mbid string `json:"mbid"`
			Image []struct {
				URL string `json:"#text"`
			} `json:"image"`
		} `json:"artist"`
		Attr struct {
			Page       string `json:"page"`
			TotalPages string `json:"totalPages"`
		} `json:"@attr"`
	} `json:"topartists"`
}

type lastfmAlbumPage struct {
	TopAlbums struct {
		Album []struct {
			Name   string `json:"name"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
			Image []struct {
				URL string `json:"#text"`
			} `json:"image"`
		} `json:"album"`
		Attr struct {
			Page       string `json:"page"`
			TotalPages string `json:"totalPages"`
		} `json:"@attr"`
	} `json:"topalbums"`
}

type lastfmLovedPage struct {
	Lovedtracks struct {
		Track []struct {
			Name   string `json:"name"`
			Mbid   string `json:"mbid"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
			Date struct {
				UTS string `json:"uts"`
			} `json:"date"`
		} `json:"track"`
		Attr struct {
			Page       string `json:"page"`
			TotalPages string `json:"totalPages"`
		} `json:"@attr"`
	} `json:"lovedtracks"`
}

type lastfmRecentPage struct {
	Recenttracks struct {
		Track []struct {
			Name   string `json:"name"`
			Mbid   string `json:"mbid"`
			Attr   struct {
				NowPlaying string `json:"nowplaying"`
			} `json:"@attr"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
			Album struct {
				Namestring string `json:"#text"`
			} `json:"album"`
			Date struct {
				UTS string `json:"uts"`
			} `json:"date"`
		} `json:"track"`
		Attr struct {
			Page       string `json:"page"`
			TotalPages string `json:"totalPages"`
		} `json:"@attr"`
	} `json:"recenttracks"`
}

// TopArtists fetches the user's top artists for the given period ("overall",
// "7day", "1month"...). The result is capped at maxItems.
func (s *LastFMSource) TopArtists(ctx context.Context, period string, maxItems int) ([]TasteEvent, error) {
	_, username, err := s.credentials2()
	if err != nil {
		return nil, err
	}
	var events []TasteEvent
	page, totalPages := 1, 1
	for page <= totalPages && len(events) < maxItems {
		v := url.Values{}
		v.Set("method", "user.getTopArtists")
		v.Set("user", username)
		v.Set("period", period)
		v.Set("limit", "50")
		v.Set("page", strconv.Itoa(page))
		var out lastfmArtistPage
		if err := s.call(ctx, v, &out); err != nil {
			return events, err
		}
		for _, a := range out.TopArtists.Artist {
			e := TasteEvent{Kind: KindTopArtist, Artist: a.Name, Timestamp: time.Now(), Source: "lastfm"}
			e.ID = eventID("lastfm", string(KindTopArtist), period, a.Name)
			events = append(events, e)
		}
		if n, _ := strconv.Atoi(out.TopArtists.Attr.TotalPages); n > 0 {
			totalPages = n
		}
		page++
	}
	if len(events) > maxItems {
		events = events[:maxItems]
	}
	return events, nil
}

// TopAlbums fetches the user's top albums.
func (s *LastFMSource) TopAlbums(ctx context.Context, period string, maxItems int) ([]TasteEvent, error) {
	_, username, err := s.credentials2()
	if err != nil {
		return nil, err
	}
	var events []TasteEvent
	page, totalPages := 1, 1
	for page <= totalPages && len(events) < maxItems {
		v := url.Values{}
		v.Set("method", "user.getTopAlbums")
		v.Set("user", username)
		v.Set("period", period)
		v.Set("limit", "50")
		v.Set("page", strconv.Itoa(page))
		var out lastfmAlbumPage
		if err := s.call(ctx, v, &out); err != nil {
			return events, err
		}
		for _, a := range out.TopAlbums.Album {
			e := TasteEvent{Kind: KindSaveAlbum, Artist: a.Artist.Name, Album: a.Name, Timestamp: time.Now(), Source: "lastfm"}
			e.ID = eventID("lastfm", string(e.Kind), period, a.Artist.Name+a.Name)
			events = append(events, e)
		}
		if n, _ := strconv.Atoi(out.TopAlbums.Attr.TotalPages); n > 0 {
			totalPages = n
		}
		page++
	}
	if len(events) > maxItems {
		events = events[:maxItems]
	}
	return events, nil
}

// LovedTracks fetches the user's loved tracks.
func (s *LastFMSource) LovedTracks(ctx context.Context, maxItems int) ([]TasteEvent, error) {
	_, username, err := s.credentials2()
	if err != nil {
		return nil, err
	}
	var events []TasteEvent
	page, totalPages := 1, 1
	for page <= totalPages && len(events) < maxItems {
		v := url.Values{}
		v.Set("method", "user.getLovedTracks")
		v.Set("user", username)
		v.Set("limit", "50")
		v.Set("page", strconv.Itoa(page))
		var out lastfmLovedPage
		if err := s.call(ctx, v, &out); err != nil {
			return events, err
		}
		for _, t := range out.Lovedtracks.Track {
			ts := time.Now()
			if n, err := strconv.ParseInt(t.Date.UTS, 10, 64); err == nil && n > 0 {
				ts = time.Unix(n, 0)
			}
			e := TasteEvent{Kind: KindLoved, Artist: t.Artist.Name, Title: t.Name, Timestamp: ts, Source: "lastfm"}
			e.ID = eventID("lastfm", string(KindLoved), t.Artist.Name+t.Name)
			events = append(events, e)
		}
		if n, _ := strconv.Atoi(out.Lovedtracks.Attr.TotalPages); n > 0 {
			totalPages = n
		}
		page++
	}
	if len(events) > maxItems {
		events = events[:maxItems]
	}
	return events, nil
}

// RecentTracks fetches scrobbles. Now-playing rows (no timestamp) are skipped.
func (s *LastFMSource) RecentTracks(ctx context.Context, maxItems int) ([]TasteEvent, error) {
	_, username, err := s.credentials2()
	if err != nil {
		return nil, err
	}
	var events []TasteEvent
	page, totalPages := 1, 1
	for page <= totalPages && len(events) < maxItems && page <= 5 {
		v := url.Values{}
		v.Set("method", "user.getRecentTracks")
		v.Set("user", username)
		v.Set("limit", "50")
		v.Set("page", strconv.Itoa(page))
		var out lastfmRecentPage
		if err := s.call(ctx, v, &out); err != nil {
			return events, err
		}
		for _, t := range out.Recenttracks.Track {
			if t.Attr.NowPlaying == "true" {
				continue
			}
			ts := time.Now()
			if n, err := strconv.ParseInt(t.Date.UTS, 10, 64); err == nil && n > 0 {
				ts = time.Unix(n, 0)
			}
			e := TasteEvent{Kind: KindPlay, Artist: t.Artist.Name, Title: t.Name, Album: t.Album.Namestring, Timestamp: ts, Source: "lastfm"}
			e.ID = eventID("lastfm", string(KindPlay), t.Artist.Name+t.Name, t.Date.UTS)
			events = append(events, e)
		}
		if n, _ := strconv.Atoi(out.Recenttracks.Attr.TotalPages); n > 0 {
			totalPages = n
		}
		page++
	}
	if len(events) > maxItems {
		events = events[:maxItems]
	}
	return events, nil
}

// SimilarArtist is a candidate from artist.getSimilar.
type SimilarArtist struct {
	Name  string
	Match float64
}

// GetSimilar returns similar artists for an artist name.
func (s *LastFMSource) GetSimilar(ctx context.Context, artist string, limit int) ([]SimilarArtist, error) {
	if limit <= 0 {
		limit = 20
	}
	if _, _, err := s.credentials2(); err != nil {
		return nil, err
	}
	v := url.Values{}
	v.Set("method", "artist.getSimilar")
	v.Set("artist", artist)
	v.Set("limit", strconv.Itoa(limit))
	var out struct {
		Similarartists struct {
			Artist []struct {
				Name  string      `json:"name"`
				Match json.Number `json:"match"`
			} `json:"artist"`
		} `json:"similarartists"`
	}
	if err := s.call(ctx, v, &out); err != nil {
		return nil, err
	}
	result := make([]SimilarArtist, 0, len(out.Similarartists.Artist))
	for _, a := range out.Similarartists.Artist {
		m, _ := a.Match.Float64()
		result = append(result, SimilarArtist{Name: a.Name, Match: m})
	}
	return result, nil
}

// Pull pulls everything Last.fm can offer, newest signals first where the API
// provides timestamps.
func (s *LastFMSource) Pull(ctx context.Context, since time.Time) ([]TasteEvent, error) {
	var events []TasteEvent
	top, err := s.TopArtists(ctx, "overall", 100)
	if err != nil {
		return events, err
	}
	events = append(events, top...)
	albums, err := s.TopAlbums(ctx, "overall", 100)
	if err != nil {
		return events, err
	}
	events = append(events, albums...)
	loved, err := s.LovedTracks(ctx, 200)
	if err != nil {
		return events, err
	}
	events = append(events, loved...)
	recent, err := s.RecentTracks(ctx, 200)
	if err != nil {
		return events, err
	}
	events = append(events, recent...)
	if !since.IsZero() {
		keep := events[:0]
		for _, e := range events {
			if e.Kind == KindPlay && e.Timestamp.Before(since) {
				continue
			}
			keep = append(keep, e)
		}
		events = keep
	}
	return events, nil
}
