package syncengine

import (
	"strings"
	"time"
)

// SourceTrack is one library track considered for sync. Paths are local PC paths.
type SourceTrack struct {
	Path        string
	Size        int64
	ModTime     time.Time
	Hash        string // source content hash (HashSource); required
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	Year        string
	TrackNumber int
	DiscNumber  int
	DiscTotal   int
	Ext         string // lower-case extension with dot, e.g. ".flac"
	DurationSec int
	AddedAt     time.Time // when the track landed in the library; ModTime if zero
}

// Selection narrows which library tracks a device sync covers.
type Selection struct {
	WholeLibrary bool `json:"whole_library"`
	// Artists matches Artist or AlbumArtist, case-insensitive exact.
	Artists []string `json:"artists"`
	// Albums matches Album, case-insensitive exact.
	Albums []string `json:"albums"`
	// Playlists holds PC-side .m3u8 paths; their entries resolve to tracks.
	Playlists []string `json:"playlists"`
	// RecentDays > 0 keeps tracks whose AddedAt is within the window.
	RecentDays int `json:"recent_days"`
}

// Select applies the selection rules to tracks and returns the kept ones.
// An all-empty selection with WholeLibrary=false is treated as the whole
// library so a fresh device still syncs something; explicit filters always win.
func (s Selection) Select(tracks []SourceTrack, playlistPaths map[string][]string, now time.Time) []SourceTrack {
	empty := !s.WholeLibrary && len(s.Artists) == 0 && len(s.Albums) == 0 && len(s.Playlists) == 0 && s.RecentDays <= 0
	artists := lowerSet(s.Artists)
	albums := lowerSet(s.Albums)
	playlistTracks := map[string]bool{}
	for _, paths := range playlistPaths {
		for _, p := range paths {
			playlistTracks[p] = true
		}
	}
	var out []SourceTrack
	for _, tr := range tracks {
		switch {
		case empty || s.WholeLibrary:
			out = append(out, tr)
		case artists[strings.ToLower(tr.Artist)] || artists[strings.ToLower(tr.AlbumArtist)]:
			out = append(out, tr)
		case albums[strings.ToLower(tr.Album)]:
			out = append(out, tr)
		case playlistTracks[tr.Path]:
			out = append(out, tr)
		case s.RecentDays > 0:
			added := tr.AddedAt
			if added.IsZero() {
				added = tr.ModTime
			}
			if now.Sub(added) <= time.Duration(s.RecentDays)*24*time.Hour {
				out = append(out, tr)
			}
		}
	}
	return out
}

func lowerSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			out[strings.ToLower(v)] = true
		}
	}
	return out
}
