package taste

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/vekhyat/Auralis/backend/credentials"
)

// SyncProgress is published during a sync run.
type SyncProgress struct {
	Phase   string `json:"phase"` // "spotify" | "lastfm" | "profile" | "gaps" | "done" | "error"
	Message string `json:"message"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Done    bool   `json:"done"`
	Error   string `json:"error,omitempty"`
}

// Settings is what the UI shows about connections (never tokens).
type Settings struct {
	ForYouEnabled     bool   `json:"for_you_enabled"`
	SpotifyClientID   string `json:"spotify_client_id"`
	// SpotifyRedirectURI is shown in the setup guide.
	SpotifyRedirectURI string `json:"spotify_redirect_uri"`
	SpotifyConnected  bool   `json:"spotify_connected"`
	LastFMUsername    string `json:"lastfm_username"`
	LastFMConfigured  bool   `json:"lastfm_configured"`
	LastSync          string `json:"last_sync"`
	EventCount        int    `json:"event_count"`
}

// Service owns the taste store and fans out over the configured sources.
type Service struct {
	Store  *Store
	Creds  *credentials.Store
	Spotify *SpotifyAPISource
	LastFM  *LastFMSource

	// Injectable seams (tests substitute fakes).
	LibraryFilter LibraryFilter
	// AlbumOwned reports albums already in the library; nil skips the check.
	AlbumOwned AlbumFilter
	// FetchArtistDiscography returns an artist's albums for the discography
	// gaps shelf. Production wiring uses the anonymous Spotify metadata
	// client; nil disables that shelf.
	FetchArtistDiscography func(ctx context.Context, artistID string) ([]GapAlbum, error)

	GapsTTL         time.Duration // default 24h
	GapsMinInterval time.Duration // default 150ms between live fetches

	mu          sync.Mutex
	syncCancel  context.CancelFunc
	syncRunning bool
	lastGaps    time.Time
}

func NewService(store *Store, creds *credentials.Store) *Service {
	s := &Service{Store: store, Creds: creds, GapsTTL: 24 * time.Hour, GapsMinInterval: 150 * time.Millisecond}
	s.Spotify = &SpotifyAPISource{Store: creds}
	s.LastFM = &LastFMSource{Store: creds}
	return s
}

// Close releases the bbolt file handle.
func (s *Service) Close() error { return s.Store.Close() }

func (s *Service) GetSettings() Settings {
	cfg := Settings{SpotifyRedirectURI: SpotifyRedirectURI}
	if v, ok := s.Store.GetMeta("for_you_enabled"); ok && v == "1" {
		cfg.ForYouEnabled = true
	}
	if v, _ := s.Creds.Get(spotifyClientIDKey); v != "" {
		cfg.SpotifyClientID = v
	}
	cfg.SpotifyConnected = s.SpotifyConnected()
	if v, _ := s.Creds.Get(lastfmUserKey); v != "" {
		cfg.LastFMUsername = v
	}
	cfg.LastFMConfigured = s.LastFMConfigured()
	if v, ok := s.Store.GetMeta("last_sync"); ok {
		cfg.LastSync = v
	}
	if n, err := s.Store.EventCount(); err == nil {
		cfg.EventCount = n
	}
	return cfg
}

func (s *Service) SpotifyConnected() bool { return s.Spotify.Connected() }
func (s *Service) LastFMConfigured() bool { return s.LastFM.Connected() }

func (s *Service) SetForYouEnabled(enabled bool) error {
	v := "0"
	if enabled {
		v = "1"
	}
	return s.Store.SetMeta("for_you_enabled", v)
}

func (s *Service) SetSpotifyClientID(id string) error { return s.Spotify.SetClientID(id) }
func (s *Service) DisconnectSpotify() error          { return s.Spotify.Disconnect() }
func (s *Service) SetLastFMCredentials(apiKey, username string) error {
	return s.LastFM.SetCredentials(apiKey, username)
}
func (s *Service) DisconnectLastFM() error { return s.LastFM.Disconnect() }

// ImportSpotifyExport parses a folder/zip/file of export JSON and ingests the
// events, then rebuilds the derived profile.
func (s *Service) ImportSpotifyExport(ctx context.Context, path string, progress func(ImportProgress)) (int, error) {
	if progress != nil {
		progress(ImportProgress{Phase: "scan", Message: path})
	}
	importer := &SpotifyExport{Progress: progress}
	events, err := importer.ImportPath(path)
	if err != nil {
		return 0, err
	}
	added, err := s.Store.AddEvents(events)
	if err != nil {
		return 0, err
	}
	if err := s.rebuildProfile(); err != nil {
		return added, err
	}
	return added, nil
}

// SyncNow starts an async sync. Only one sync can run at a time; calling this
// while one is running returns an error. Progress goes to the callback.
func (s *Service) SyncNow(ctx context.Context, progress func(SyncProgress)) error {
	s.mu.Lock()
	if s.syncRunning {
		s.mu.Unlock()
		return errors.New("a taste sync is already running")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.syncCancel = cancel
	s.syncRunning = true
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			s.syncRunning = false
			s.syncCancel = nil
			s.mu.Unlock()
		}()
		s.runSync(runCtx, progress)
	}()
	return nil
}

// CancelSync cancels the running sync, if any.
func (s *Service) CancelSync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncCancel != nil {
		s.syncCancel()
	}
}

func (s *Service) runSync(ctx context.Context, progress func(SyncProgress)) {
	report := func(p SyncProgress) {
		if progress != nil {
			progress(p)
		}
	}
	var sources []TasteSource
	if s.SpotifyConnected() {
		sources = append(sources, s.Spotify)
	}
	if s.LastFMConfigured() {
		sources = append(sources, s.LastFM)
	}
	total := len(sources) + 2
	current := 0
	for _, src := range sources {
		if ctx.Err() != nil {
			report(SyncProgress{Phase: "error", Error: ctx.Err().Error(), Done: true})
			return
		}
		report(SyncProgress{Phase: src.ID(), Message: "Pulling " + src.ID(), Current: current, Total: total})
		events, err := src.Pull(ctx, time.Time{})
		if err != nil {
			if ctx.Err() != nil {
				report(SyncProgress{Phase: "error", Error: ctx.Err().Error(), Done: true})
				return
			}
			// A failing source drops out but does not break the profile.
			report(SyncProgress{Phase: src.ID(), Message: "Skipped " + src.ID() + ": " + err.Error(), Current: current, Total: total})
			current++
			continue
		}
		if _, err := s.Store.AddEvents(events); err != nil {
			report(SyncProgress{Phase: "error", Error: err.Error(), Done: true})
			return
		}
		current++
	}
	report(SyncProgress{Phase: "profile", Message: "Rebuilding taste profile", Current: current, Total: total})
	if err := s.rebuildProfile(); err != nil {
		report(SyncProgress{Phase: "error", Error: err.Error(), Done: true})
		return
	}
	current++
	report(SyncProgress{Phase: "gaps", Message: "Updating discography gaps", Current: current, Total: total})
	if err := s.refreshGaps(ctx); err != nil && ctx.Err() == nil {
		report(SyncProgress{Phase: "gaps", Message: err.Error(), Current: current, Total: total})
	}
	current++
	_ = s.Store.SetMeta("last_sync", time.Now().Format(time.RFC3339))
	report(SyncProgress{Phase: "done", Message: "Sync complete", Current: current, Total: total, Done: true})
}

func (s *Service) rebuildProfile() error {
	events, err := s.Store.AllEvents()
	if err != nil {
		return err
	}
	fb, err := s.Store.GetFeedback()
	if err != nil {
		return err
	}
	profile := BuildProfile(events, fb, time.Now())
	return s.Store.SetCachedProfile(profile)
}

// Profile recomputes (and caches) the profile on demand.
func (s *Service) Profile() (Profile, error) {
	if err := s.rebuildProfile(); err != nil {
		return Profile{}, err
	}
	p, err := s.Store.GetCachedProfile()
	if err != nil {
		return Profile{}, err
	}
	return *p, nil
}

// refreshGaps updates the discography-gap cache for top artists with Spotify
// IDs, rate-limited and reused when fresh.
func (s *Service) refreshGaps(ctx context.Context) error {
	if s.FetchArtistDiscography == nil {
		return nil
	}
	profile, err := s.Store.GetCachedProfile()
	if err != nil {
		return nil
	}
	// Map artists to Spotify IDs from events.
	idByArtist := map[string]string{}
	events, err := s.Store.AllEvents()
	if err != nil {
		return err
	}
	for _, e := range events {
		if e.ArtistID != "" && e.Artist != "" {
			if _, ok := idByArtist[Normalise(e.Artist)]; !ok {
				idByArtist[Normalise(e.Artist)] = e.ArtistID
			}
		}
	}
	ranked := s.topArtistKeys(*profile, 5)
	var firstErr error
	for _, key := range ranked {
		artistID, ok := idByArtist[key]
		if !ok {
			continue
		}
		if albums, fetchedAt, ok := s.Store.GetGaps(artistID); ok && time.Since(fetchedAt) < s.ttl() {
			_ = albums
			continue
		}
		if s.gapsMinInterval() > 0 {
			since := time.Since(s.lastGaps)
			if since < s.gapsMinInterval() {
				wait := s.gapsMinInterval() - since
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
				}
			}
		}
		albums, err := s.FetchArtistDiscography(ctx, artistID)
		s.lastGaps = time.Now()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_ = s.Store.SetGaps(artistID, albums, time.Now())
	}
	return firstErr
}

func (s *Service) ttl() time.Duration {
	if s.GapsTTL > 0 {
		return s.GapsTTL
	}
	return 24 * time.Hour
}

func (s *Service) gapsMinInterval() time.Duration {
	if s.GapsMinInterval > 0 {
		return s.GapsMinInterval
	}
	return 150 * time.Millisecond
}

func (s *Service) topArtistKeys(p Profile, n int) []string {
	return p.CoreArtists.TopN(n)
}

// GetShelves builds all recommendation shelves for the current profile.
func (s *Service) GetShelves(ctx context.Context) ([]Shelf, error) {
	profile, err := s.Profile()
	if err != nil {
		return nil, err
	}
	fb, err := s.Store.GetFeedback()
	if err != nil {
		return nil, err
	}
	events, err := s.Store.AllEvents()
	if err != nil {
		return nil, err
	}

	owned := ownership{tracks: s.LibraryFilter, albums: s.AlbumOwned}
	shelf1 := finalise(likedNotDownloaded(events, profile), fb, owned)
	// Finish-albums candidates are partly owned by definition; only the
	// liked tracks are checked (inside finishAlbums).
	shelf2 := finalise(finishAlbums(events, profile, s.LibraryFilter), fb, ownership{})

	var similar []similarArtistKey
	if s.LastFMConfigured() {
		for _, seed := range s.topArtistDisplayNames(profile, 2) {
			candidates, err := s.LastFM.GetSimilar(ctx, seed, 15)
			if err != nil {
				continue
			}
			similar = append(similar, similarArtistKey{seed: seed, candidates: candidates})
		}
	}
	var similarItems []Item
	for _, s := range similar {
		similarItems = append(similarItems, similarArtistCandidates(s.candidates, s.seed, events, fb)...)
	}
	shelf3 := finalise(similarItems, fb, ownership{})

	var gapItems []Item
	if s.FetchArtistDiscography != nil {
		seen := map[string]bool{}
		for artistKey, albums := range s.gapAlbumsByArtist(profile, events) {
			key := Normalise(artistKey)
			if seen[key] {
				continue
			}
			seen[key] = true
			gapItems = append(gapItems, discographyGapItems(s.displayNameOf(profile, key), albums, profile)...)
		}
	}
	shelf4 := finalise(gapItems, fb, ownership{albums: s.AlbumOwned})

	title3 := "Because you listen to X"
	if len(similar) > 0 && similar[0].seed != "" {
		title3 = "Because you listen to " + similar[0].seed
	}
	return []Shelf{
		{ID: "liked-not-downloaded", Title: "Liked, not downloaded", Items: shelf1},
		{ID: "finish-albums", Title: "Finish these albums", Items: shelf2},
		{ID: "similar-artists", Title: title3, Items: shelf3},
		{ID: "discography-gaps", Title: "Discography gaps", Items: shelf4},
	}, nil
}

type similarArtistKey struct {
	seed       string
	candidates []SimilarArtist
}

// gapAlbumsByArtist maps artist display name -> cached gap albums, using the
// persisted fetcher cache (only refreshed during sync runs).
func (s *Service) gapAlbumsByArtist(profile Profile, events []TasteEvent) map[string][]GapAlbum {
	idByArtist := map[string]string{}
	for _, e := range events {
		if e.ArtistID != "" && e.Artist != "" {
			if _, ok := idByArtist[Normalise(e.Artist)]; !ok {
				idByArtist[Normalise(e.Artist)] = e.ArtistID
			}
		}
	}
	out := map[string][]GapAlbum{}
	for _, key := range s.topArtistKeys(profile, 5) {
		artistID, ok := idByArtist[key]
		if !ok {
			continue
		}
		albums, _, ok := s.Store.GetGaps(artistID)
		if !ok {
			continue
		}
		// Keep only albums not already represented among saved/played events
		// (owned locally or known from the library index check).
		known := map[string]bool{}
		for _, e := range events {
			if Normalise(e.Artist) == key && e.Album != "" {
				known[Normalise(e.Album)] = true
			}
		}
		filtered := albums[:0]
		for _, a := range albums {
			if known[Normalise(a.Name)] {
				continue
			}
			filtered = append(filtered, a)
		}
		if len(filtered) > 0 {
			out[key] = filtered
		}
	}
	return out
}

func (s *Service) displayNameOf(p Profile, key string) string {
	if name, ok := p.ArtistNames[key]; ok {
		return name
	}
	return key
}

func (s *Service) topArtistDisplayNames(p Profile, n int) []string {
	return TopAffinityArtists(p, n)
}

// DismissItem persists a dismissal so the item never reappears.
func (s *Service) DismissItem(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("empty item id")
	}
	return s.Store.UpdateFeedback(func(fb *Feedback) {
		if fb.DismissedItems == nil {
			fb.DismissedItems = map[string]bool{}
		}
		fb.DismissedItems[id] = true
	})
}

// NotInterestedInArtist dismisses every current and future item of an artist.
func (s *Service) NotInterestedInArtist(artist string) error {
	return s.BanArtist(artist)
}

func (s *Service) BanArtist(artist string) error {
	artist = strings.TrimSpace(artist)
	if artist == "" {
		return errors.New("empty artist")
	}
	return s.Store.UpdateFeedback(func(fb *Feedback) {
		for _, b := range fb.BannedArtists {
			if Normalise(b) == Normalise(artist) {
				return
			}
		}
		fb.BannedArtists = append(fb.BannedArtists, artist)
	})
}

func (s *Service) UnbanArtist(artist string) error {
	return s.Store.UpdateFeedback(func(fb *Feedback) {
		kept := fb.BannedArtists[:0]
		for _, b := range fb.BannedArtists {
			if Normalise(b) != Normalise(artist) {
				kept = append(kept, b)
			}
		}
		fb.BannedArtists = kept
	})
}

func (s *Service) PinArtist(artist string) error {
	artist = strings.TrimSpace(artist)
	if artist == "" {
		return errors.New("empty artist")
	}
	return s.Store.UpdateFeedback(func(fb *Feedback) {
		for _, p := range fb.PinnedArtists {
			if Normalise(p) == Normalise(artist) {
				return
			}
		}
		fb.PinnedArtists = append(fb.PinnedArtists, artist)
	})
}

func (s *Service) UnpinArtist(artist string) error {
	return s.Store.UpdateFeedback(func(fb *Feedback) {
		kept := fb.PinnedArtists[:0]
		for _, p := range fb.PinnedArtists {
			if Normalise(p) != Normalise(artist) {
				kept = append(kept, p)
			}
		}
		fb.PinnedArtists = kept
	})
}

// Summary powers the "Your taste" panel.
func (s *Service) Summary() (Summary, error) {
	profile, err := s.Profile()
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{}
	for _, name := range TopAffinityArtists(profile, 8) {
		sum.TopArtists = append(sum.TopArtists, name)
	}
	for _, g := range profile.CoreGenres.TopN(8) {
		sum.TopGenres = append(sum.TopGenres, g)
	}
	if n, err := s.Store.EventCount(); err == nil {
		sum.EventCount = n
	}
	if v, ok := s.Store.GetMeta("last_sync"); ok {
		sum.LastSync = v
	}
	return sum, nil
}

