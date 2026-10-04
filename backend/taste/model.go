// Package taste builds a local, time-decayed profile from streaming accounts
// and exported listening history, and turns it into ranked, explainable
// download suggestions ("For You"). Nothing leaves the machine except the
// explicit read-only API calls to the connected providers.
package taste

import (
	"context"
	"strings"
	"time"
)

// TasteKind enumerates the signals written into the raw event store.
type TasteKind string

const (
	KindPlay      TasteKind = "play"
	KindSaveTrack  TasteKind = "save-track"
	KindSaveAlbum  TasteKind = "save-album"
	KindFollow     TasteKind = "follow"
	KindTopArtist  TasteKind = "top-artist"
	KindTopTrack   TasteKind = "top-track"
	KindLoved      TasteKind = "loved"
)

// TasteEvent is one raw listening/library signal from a source.
type TasteEvent struct {
	ID         string    `json:"id"` // dedup fingerprint, stable across re-imports
	Kind       TasteKind `json:"kind"`
	SpotifyID  string    `json:"spotify_id,omitempty"`
	ISRC       string    `json:"isrc,omitempty"`
	Artist     string    `json:"artist,omitempty"`
	Album      string    `json:"album,omitempty"`
	Title      string    `json:"title,omitempty"`
	ArtistID   string    `json:"artist_id,omitempty"` // Spotify artist id when the source provides one
	AlbumID    string    `json:"album_id,omitempty"`
	Genres     []string  `json:"genres,omitempty"`
	Image      string    `json:"image,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	MsPlayed   int       `json:"ms_played,omitempty"`
	Source     string    `json:"source"` // spotify_api | spotify_export | lastfm
}

// Weight maps each kind to its affinity weight. Plays under 30s count as
// skips and produce a small negative signal.
func (e TasteEvent) Weight() float64 {
	switch e.Kind {
	case KindTopArtist:
		return 5
	case KindTopTrack:
		return 4
	case KindLoved:
		return 4
	case KindSaveTrack:
		return 3
	case KindSaveAlbum:
		return 2
	case KindFollow:
		return 1.5
	case KindPlay:
		if e.MsPlayed > 0 && e.MsPlayed < 30000 {
			return -0.5
		}
		return 1
	}
	return 0
}

// Normalise lower-cases and trims an entity name so affinities merge
// spelling variants of the same artist/album/track.
func Normalise(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// TasteSource is a place music comes from for taste modelling. All sources
// degrade independently: a failing source drops out, it does not break the
// profile.
type TasteSource interface {
	ID() string
	Connected() bool
	Pull(ctx context.Context, since time.Time) ([]TasteEvent, error)
}
