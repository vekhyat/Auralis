package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.senan.xyz/taglib"
)

// PlaylistMode selects how track paths are written into the .m3u8.
type PlaylistMode string

const (
	// PlaylistRelative makes paths relative to the playlist file (default).
	PlaylistRelative PlaylistMode = "relative"
	// PlaylistRoot makes paths relative to the music root.
	PlaylistRoot PlaylistMode = "root"
	// PlaylistDevice prefixes paths made relative to the music root with a
	// device-absolute prefix such as /storage/emulated/0/Music.
	PlaylistDevice PlaylistMode = "device"
	// PlaylistLocal is relative to the playlist like PlaylistRelative, but
	// falls back to the absolute path for files on another drive. It is only
	// for playlists played on this PC, which is what CreateM3U8File has
	// always written; never use it for device exports.
	PlaylistLocal PlaylistMode = "local"
)

// PlaylistTrack is one entry to write.
type PlaylistTrack struct {
	Path            string  `json:"path"`
	Title           string  `json:"title"`
	Artist          string  `json:"artist"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// PlaylistSkip is a track that could not be placed and was left out.
type PlaylistSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// PlaylistResult reports what WritePlaylist did.
type PlaylistResult struct {
	Path    string         `json:"path"`
	Written int            `json:"written"`
	Skipped []PlaylistSkip `json:"skipped"`
}

// WritePlaylist writes tracks to m3u8Path as UTF-8 .m3u8 with #EXTINF
// lines (when tags can be read) and mode-appropriate paths. Tracks whose
// path cannot be expressed in the mode are skipped and reported.
func WritePlaylist(m3u8Path string, tracks []PlaylistTrack, mode PlaylistMode, musicRoot, devicePrefix string) (*PlaylistResult, error) {
	result := &PlaylistResult{Path: m3u8Path, Skipped: []PlaylistSkip{}}
	if err := os.MkdirAll(filepath.Dir(m3u8Path), 0o755); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, track := range tracks {
		if strings.TrimSpace(track.Path) == "" {
			continue
		}
		line, skip := playlistLine(m3u8Path, track.Path, mode, musicRoot, devicePrefix)
		if skip != "" {
			result.Skipped = append(result.Skipped, PlaylistSkip{Path: track.Path, Reason: skip})
			continue
		}
		if track.Title != "" || track.Artist != "" {
			seconds := int(track.DurationSeconds)
			if seconds <= 0 {
				seconds = -1
			}
			artistTitle := ""
			switch {
			case track.Artist != "" && track.Title != "":
				artistTitle = track.Artist + " - " + track.Title
			case track.Title != "":
				artistTitle = track.Title
			default:
				artistTitle = track.Artist
			}
			b.WriteString(fmt.Sprintf("#EXTINF:%d,%s\n", seconds, artistTitle))
		}
		b.WriteString(line + "\n")
		result.Written++
	}
	if err := os.WriteFile(m3u8Path, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	return result, nil
}

// playlistLine returns the path line for one track, or a skip reason.
func playlistLine(m3u8Path, trackPath string, mode PlaylistMode, musicRoot, devicePrefix string) (string, string) {
	switch mode {
	case PlaylistDevice:
		rel, reason := relToRoot(trackPath, musicRoot)
		if reason != "" {
			return "", reason
		}
		prefix := strings.TrimRight(devicePrefix, "/")
		if prefix == "" {
			return "", "device prefix is empty"
		}
		return prefix + "/" + filepath.ToSlash(rel), ""
	case PlaylistRoot:
		rel, reason := relToRoot(trackPath, musicRoot)
		if reason != "" {
			return "", reason
		}
		return filepath.ToSlash(rel), ""
	case PlaylistLocal:
		if rel, err := filepath.Rel(filepath.Dir(m3u8Path), trackPath); err == nil {
			return filepath.ToSlash(rel), ""
		}
		return trackPath, ""
	default: // PlaylistRelative
		rel, err := filepath.Rel(filepath.Dir(m3u8Path), trackPath)
		if err != nil {
			return "", "path is on a different volume"
		}
		return filepath.ToSlash(rel), ""
	}
}

func relToRoot(trackPath, musicRoot string) (string, string) {
	if strings.TrimSpace(musicRoot) == "" {
		return "", "music root is empty"
	}
	rel, err := filepath.Rel(musicRoot, trackPath)
	if err != nil {
		return "", "path is on a different volume"
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "path is outside the music root"
	}
	return rel, ""
}

// TracksFromPaths builds PlaylistTrack entries, reading tags and duration
// for the EXTINF lines. Unreadable files keep their path with empty tags.
func TracksFromPaths(paths []string) []PlaylistTrack {
	tracks := make([]PlaylistTrack, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		track := PlaylistTrack{Path: p}
		if tags, err := taglib.ReadTags(p); err == nil {
			track.Title = firstNonEmptyPlaylist(tags, "TITLE", taglib.Title)
			track.Artist = firstNonEmptyPlaylist(tags, "ARTIST", taglib.Artist)
		}
		if props, err := taglib.ReadProperties(p); err == nil {
			track.DurationSeconds = props.Length.Seconds()
		}
		tracks = append(tracks, track)
	}
	return tracks
}

func firstNonEmptyPlaylist(tags map[string][]string, keys ...string) string {
	byUpper := map[string]string{}
	for key := range tags {
		byUpper[strings.ToUpper(key)] = key
	}
	for _, key := range keys {
		actual, ok := byUpper[strings.ToUpper(key)]
		if !ok {
			continue
		}
		for _, v := range tags[actual] {
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}
