package taste

import (
	"math"
	"sort"
	"strings"
	"time"
)

// Decay constants. "Core" is the long-horizon view; "Current" is roughly the
// last four weeks of listening.
const (
	CoreTau    = 365 * 24 * time.Hour
	CurrentTau = 28 * 24 * time.Hour
)

// Affinity maps a normalised entity key to its decayed affinity.
type Affinity map[string]float64

// Profile is the derived view over raw events. Artists/albums/tracks/genres
// each carry a Core (long τ) and Current (short τ) affinity map.
type Profile struct {
	ComputedAt     time.Time `json:"computed_at"`
	CoreArtists    Affinity  `json:"core_artists"`
	CoreAlbums     Affinity  `json:"core_albums"`
	CoreTracks     Affinity  `json:"core_tracks"`
	CurrentArtists Affinity  `json:"current_artists"`
	CurrentAlbums  Affinity  `json:"current_albums"`
	CurrentTracks  Affinity  `json:"current_tracks"`
	CoreGenres      Affinity  `json:"core_genres"`
	CurrentGenres   Affinity  `json:"current_genres"`
	PinnedArtists   []string  `json:"pinned_artists,omitempty"`
	BannedArtists   []string  `json:"banned_artists,omitempty"`
	ArtistNames     map[string]string `json:"artist_names,omitempty"` // normalised key -> display name
}

const pinnedBoost = 8.0

// BuildProfile folds events into a profile at time now. It is pure: replay
// over the same events yields the same result, so the cached profile can be
// recomputed whenever the algorithm changes.
func BuildProfile(events []TasteEvent, fb Feedback, now time.Time) Profile {
	p := Profile{
		ComputedAt:     now,
		CoreArtists:    Affinity{},
		CoreAlbums:     Affinity{},
		CoreTracks:     Affinity{},
		CurrentArtists: Affinity{},
		CurrentAlbums:  Affinity{},
		CurrentTracks:  Affinity{},
		CoreGenres:     Affinity{},
		CurrentGenres:  Affinity{},
		PinnedArtists:  append([]string{}, fb.PinnedArtists...),
		BannedArtists:  append([]string{}, fb.BannedArtists...),
		ArtistNames:    map[string]string{},
	}
	banned := map[string]bool{}
	for _, name := range fb.BannedArtists {
		banned[Normalise(name)] = true
	}
	for _, e := range events {
		w := e.Weight()
		if w == 0 {
			continue
		}
		age := now.Sub(e.Timestamp)
		if age < 0 {
			age = 0
		}
		core := w * math.Exp(-age.Hours()/CoreTau.Hours())
		current := w * math.Exp(-age.Hours()/CurrentTau.Hours())
		if artist := Normalise(e.Artist); artist != "" && !banned[artist] {
			p.CoreArtists[artist] += core
			p.CurrentArtists[artist] += current
			if _, ok := p.ArtistNames[artist]; !ok {
				p.ArtistNames[artist] = strings.TrimSpace(e.Artist)
			}
		}
		if album := albumKey(e.Artist, e.Album); album != "" {
			if artist := Normalise(e.Artist); !banned[artist] {
				p.CoreAlbums[album] += core
				p.CurrentAlbums[album] += current
			}
		}
		if artist := Normalise(e.Artist); artist != "" && e.Title != "" && !banned[artist] {
			key := artist + "|" + Normalise(e.Title)
			p.CoreTracks[key] += core
			p.CurrentTracks[key] += current
		}
		for _, g := range e.Genres {
			g = Normalise(g)
			if g == "" {
				continue
			}
			p.CoreGenres[g] += core
			p.CurrentGenres[g] += current
		}
	}
	for _, name := range fb.PinnedArtists {
		key := Normalise(name)
		if key == "" || banned[key] {
			continue
		}
		p.CoreArtists[key] += pinnedBoost
		p.CurrentArtists[key] += pinnedBoost
		if _, ok := p.ArtistNames[key]; !ok {
			p.ArtistNames[key] = strings.TrimSpace(name)
		}
	}
	// Banned artists fully leave the profile, no matter how much they were played.
	for _, name := range fb.BannedArtists {
		key := Normalise(name)
		delete(p.CoreArtists, key)
		delete(p.CurrentArtists, key)
		delete(p.ArtistNames, key)
	}
	return p
}

func albumKey(artist, album string) string {
	artist = Normalise(artist)
	album = Normalise(album)
	if artist == "" || album == "" {
		return ""
	}
	return artist + "|" + album
}

// TopN returns the n highest-scoring keys, descending. Callers merge the key
// map back to display names where needed.
func (a Affinity) TopN(n int) []string {
	type kv struct {
		key   string
		score float64
	}
	items := make([]kv, 0, len(a))
	for key, score := range a {
		items = append(items, kv{key, score})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].key < items[j].key
		}
		return items[i].score > items[j].score
	})
	if n > len(items) {
		n = len(items)
	}
	out := make([]string, 0, n)
	for _, item := range items[:n] {
		out = append(out, item.key)
	}
	return out
}

// Summary is the small "Your taste" panel payload.
type Summary struct {
	TopArtists []string `json:"top_artists"`
	TopGenres  []string `json:"top_genres"`
	EventCount int      `json:"event_count"`
	LastSync   string   `json:"last_sync"`
}
