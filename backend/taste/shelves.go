package taste

import (
	"sort"
	"strings"
)

// LibraryLookup describes one candidate item for the "already owned?" check.
type LibraryLookup struct {
	SpotifyID string
	ISRC      string
	Artist    string
	Title     string
}

// LibraryFilter reports whether the lookup is already in the library. It is
// injected (using backend.FindExistingLibraryFile in production) so the taste
// package stays testable with a fake.
type LibraryFilter func(LibraryLookup) bool

// Item is one suggestion card on a shelf.
type Item struct {
	ID        string  `json:"id"` // fingerprint used for dismissals
	Kind      string  `json:"kind"` // "track" | "album" | "artist"
	Title     string  `json:"title"`
	Artist    string  `json:"artist"`
	Album     string  `json:"album,omitempty"`
	Image     string  `json:"image,omitempty"`
	SpotifyID string  `json:"spotify_id,omitempty"`
	AlbumID   string  `json:"album_id,omitempty"`
	ArtistID  string  `json:"artist_id,omitempty"`
	ISRC      string  `json:"isrc,omitempty"`
	Reason    string  `json:"reason"`
	Score     float64 `json:"score"`
}

// Shelf is a named row of items.
type Shelf struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Items []Item `json:"items"`
}

const (
	maxItemsPerArtistPerShelf = 3
	maxItemsPerShelf          = 24
)

// fingerprint builds the stable dismissal ID for an item.
func fingerprint(kind, spotifyID, artist, album, title string) string {
	if spotifyID != "" {
		return kind + ":" + spotifyID
	}
	return kind + ":" + Normalise(artist) + ":" + Normalise(album) + ":" + Normalise(title)
}

func isBanned(fb Feedback, artist string) bool {
	for _, b := range fb.BannedArtists {
		if Normalise(b) == Normalise(artist) {
			return true
		}
	}
	return false
}

// artistKey is the normalized artist used for per-artist capping.
func artistKey(item Item) string { return Normalise(item.Artist) }

// diversified sorts items by score desc, then interleaves artists so no one
// artist dominates a row (maximal-marginal-relevance style).
func diversified(items []Item) []Item {
	byArtist := map[string][]Item{}
	var keys []string
	for _, it := range items {
		k := artistKey(it)
		if _, ok := byArtist[k]; !ok {
			keys = append(keys, k)
		}
		byArtist[k] = append(byArtist[k], it)
	}
	for _, k := range keys {
		sort.Slice(byArtist[k], func(i, j int) bool { return byArtist[k][i].Score > byArtist[k][j].Score })
	}
	sort.Slice(keys, func(i, j int) bool {
		return byArtist[keys[i]][0].Score > byArtist[keys[j]][0].Score
	})
	out := []Item{}
	for round := 0; len(out) < len(items); round++ {
		progress := false
		for _, k := range keys {
			if round < len(byArtist[k]) {
				out = append(out, byArtist[k][round])
				progress = true
			}
		}
		if !progress {
			break
		}
	}
	return out
}

// capPerArtist keeps at most maxItemsPerArtistPerShelf items per artist.
func capPerArtist(items []Item) []Item {
	counts := map[string]int{}
	out := items[:0]
	for _, it := range items {
		k := artistKey(it)
		if counts[k] >= maxItemsPerArtistPerShelf {
			continue
		}
		counts[k]++
		out = append(out, it)
	}
	return out
}

// ownership answers "already downloaded?" for tracks and albums.
type ownership struct {
	tracks LibraryFilter
	albums AlbumFilter
}

func (o ownership) owns(it Item) bool {
	if it.Artist == "" {
		return false
	}
	switch it.Kind {
	case "track":
		return o.tracks != nil && o.tracks(LibraryLookup{SpotifyID: it.SpotifyID, ISRC: it.ISRC, Artist: it.Artist, Title: it.Title})
	case "album":
		return o.albums != nil && o.albums(it.Artist, it.Title)
	}
	return false
}

// finalise applies owned/dismissed/banned filtering, caps and diversity.
func finalise(items []Item, fb Feedback, owned ownership) []Item {
	kept := items[:0]
	for _, it := range items {
		if fb.DismissedItems != nil && fb.DismissedItems[it.ID] {
			continue
		}
		if isBanned(fb, it.Artist) {
			continue
		}
		if owned.owns(it) {
			continue
		}
		kept = append(kept, it)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Score > kept[j].Score })
	kept = capPerArtist(kept)
	kept = diversified(kept)
	if len(kept) > maxItemsPerShelf {
		kept = kept[:maxItemsPerShelf]
	}
	return kept
}

// likedNotDownloaded gathers saved/loved tracks and albums from all sources.
func likedNotDownloaded(events []TasteEvent, profile Profile) []Item {
	var items []Item
	seen := map[string]bool{}
	for _, e := range events {
		switch e.Kind {
		case KindSaveTrack, KindTopTrack, KindLoved:
			if e.Title == "" || e.Artist == "" {
				continue
			}
			id := fingerprint("track", e.SpotifyID, e.Artist, "", e.Title)
			if seen[id] {
				continue
			}
			seen[id] = true
			reason := "You saved this on Spotify"
			if e.Kind == KindLoved {
				reason = "You loved this on Last.fm"
			} else if e.Kind == KindTopTrack {
				reason = "You play this a lot"
			}
			items = append(items, Item{
				ID: id, Kind: "track", Title: e.Title, Artist: e.Artist, Album: e.Album,
				Image: e.Image, SpotifyID: e.SpotifyID, ISRC: e.ISRC, Reason: reason,
				Score: profile.CoreArtists[Normalise(e.Artist)] + 1,
			})
		case KindSaveAlbum:
			if e.Album == "" || e.Artist == "" {
				continue
			}
			id := fingerprint("album", "", e.Artist, e.Album, "")
			if e.AlbumID != "" {
				id = "album:" + e.AlbumID
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			items = append(items, Item{
				ID: id, Kind: "album", Title: e.Album, Artist: e.Artist, Album: e.Album,
				Image: e.Image, AlbumID: e.AlbumID, Reason: "You saved this album",
				Score: profile.CoreArtists[Normalise(e.Artist)] + 1,
			})
		}
	}
	return items
}

// finishAlbums groups saved/played tracks by album and suggests albums the
// user has clearly invested in (>= 3 liked/played tracks) but may be missing.
func finishAlbums(events []TasteEvent, profile Profile, belongsToLibrary LibraryFilter) []Item {
	type albumAgg struct {
		artist, album string
		tracks        map[string]TasteEvent
		image         string
		albumID       string
	}
	groups := map[string]*albumAgg{}
	for _, e := range events {
		switch e.Kind {
		case KindSaveTrack, KindPlay, KindLoved, KindTopTrack:
		default:
			continue
		}
		if e.Album == "" || e.Artist == "" || e.Title == "" {
			continue
		}
		key := Normalise(e.Artist) + "|" + Normalise(e.Album)
		agg, ok := groups[key]
		if !ok {
			agg = &albumAgg{artist: e.Artist, album: e.Album, tracks: map[string]TasteEvent{}, image: e.Image}
			groups[key] = agg
		}
		if agg.image == "" && e.Image != "" {
			agg.image = e.Image
		}
		if agg.albumID == "" && e.AlbumID != "" {
			agg.albumID = e.AlbumID
		}
		agg.tracks[Normalise(e.Title)] = e
	}
	var items []Item
	for _, agg := range groups {
		if len(agg.tracks) < 3 {
			continue
		}
		owned := 0
		if belongsToLibrary != nil {
			for _, t := range agg.tracks {
				if belongsToLibrary(LibraryLookup{SpotifyID: t.SpotifyID, ISRC: t.ISRC, Artist: t.Artist, Title: t.Title}) {
					owned++
				}
			}
		}
		if owned >= len(agg.tracks) {
			continue // everything the user liked is already owned
		}
		score := profile.CoreAlbums[Normalise(agg.artist)+"|"+Normalise(agg.album)] + float64(len(agg.tracks))
		items = append(items, Item{
			ID:      fingerprint("album", "", agg.artist, agg.album, ""),
			Kind:    "album",
			Title:   agg.album,
			Artist:  agg.artist,
			Image:   agg.image,
			AlbumID: agg.albumID,
			Reason:  reasonAlbum(len(agg.tracks)),
			Score:   score,
		})
	}
	return items
}

func reasonAlbum(n int) string {
	return "You liked " + itoa(n) + " tracks from this album"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// similarArtistCandidates turns Last.fm similar-artist results into items.
func similarArtistCandidates(candidates []SimilarArtist, seedArtist string, events []TasteEvent, fb Feedback) []Item {
	known := map[string]bool{}
	for _, e := range events {
		if e.Artist != "" {
			known[Normalise(e.Artist)] = true
		}
	}
	var items []Item
	for i, c := range candidates {
		if c.Name == "" || known[Normalise(c.Name)] || isBanned(fb, c.Name) {
			continue
		}
		id := "artist:" + Normalise(c.Name)
		if fb.DismissedItems != nil && fb.DismissedItems[id] {
			continue
		}
		items = append(items, Item{
			ID:     id,
			Kind:   "artist",
			Title:  c.Name,
			Artist: c.Name,
			Reason: "Similar to " + strings.TrimSpace(seedArtist),
			Score:  c.Match * 10 - float64(i)*0.01,
		})
	}
	return items
}

// discographyGapItems converts cached gap albums into items.
func discographyGapItems(artist string, albums []GapAlbum, profile Profile) []Item {
	var items []Item
	for _, a := range albums {
		if a.ID == "" || a.Name == "" || !isFullRelease(a) {
			continue
		}
		items = append(items, Item{
			ID:      "album:" + a.ID,
			Kind:    "album",
			Title:   a.Name,
			Artist:  artist,
			Image:   a.Image,
			AlbumID: a.ID,
			Reason:  "Missing from your library: album by " + artist,
			Score:   profile.CoreArtists[Normalise(artist)] + 0.5,
		})
	}
	return items
}

// isFullRelease keeps albums and EPs. Spotify labels EPs "single", so a
// "single" with four or more tracks counts; compilations and true singles
// would crowd out the albums a fan is actually missing.
func isFullRelease(a GapAlbum) bool {
	switch strings.ToLower(a.AlbumType) {
	case "album", "":
		return true
	case "single":
		return a.TotalTracks >= 4
	}
	return false
}

// TopAffinityArtists returns up to n display names ranked by Core affinity.
func TopAffinityArtists(profile Profile, n int) []string {
	keys := profile.CoreArtists.TopN(n)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if name, ok := profile.ArtistNames[k]; ok {
			out = append(out, name)
		} else {
			out = append(out, k)
		}
	}
	return out
}
