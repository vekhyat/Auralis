package syncengine

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// PlaylistTrack is one entry in a device playlist. Rel is the path of the
// track relative to the music root.
type PlaylistTrack struct {
	Rel         string
	Title       string
	Artist      string
	DurationSec int
}

// DevicePlaylist is one .m3u8 to regenerate on the device.
type DevicePlaylist struct {
	// Name is the file base, e.g. "Workout".
	Name   string
	Tracks []PlaylistTrack
}

// BuildM3U8 renders an extended M3U8. Entry paths are relative to the
// playlist file's directory, forward-slashed, and each entry carries an
// #EXTINF line so players can show it before scanning.
func BuildM3U8(tracks []PlaylistTrack, playlistRelDir string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, tr := range tracks {
		entry := relFromDir(playlistRelDir, tr.Rel)
		header := "#EXTINF:-1"
		if tr.DurationSec > 0 {
			header = fmt.Sprintf("#EXTINF:%d", tr.DurationSec)
		}
		label := strings.TrimSpace(strings.Trim(tr.Artist+" - "+tr.Title, " -"))
		if label == "" {
			label = path.Base(tr.Rel)
		}
		fmt.Fprintf(&b, "%s,%s\n%s\n", header, label, entry)
	}
	return b.String()
}

// relFromDir returns target relative to a slash-separated directory.
func relFromDir(dir, target string) string {
	dir = path.Clean("/" + dir)
	target = path.Clean("/" + target)
	rel, err := filepathRel(dir, target)
	if err != nil || rel == "" {
		return strings.TrimPrefix(target, "/")
	}
	return rel
}

// filepathRel is filepath.Rel but guaranteed to return "./" instead of "."
// when dir == target (POSIX semantics for our slash paths on Windows too).
func filepathRel(basepath, targpath string) (string, error) {
	base := strings.Split(path.Clean(basepath), "/")
	target := strings.Split(path.Clean(targpath), "/")
	for len(base) > 0 && len(target) > 0 && base[0] == target[0] {
		base = base[1:]
		target = target[1:]
	}
	return strings.Join(append(repeat("..", len(base)), target...), "/"), nil
}

func repeat(s string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s)
	}
	return out
}

// WriteDevicePlaylists regenerates every playlist through the target.
// Only tracks whose paths exist in the manifest are included; `relFor`
// maps a source path to its device-relative path.
func WriteDevicePlaylists(ctx context.Context, t SyncTarget, root, playlistDir string, playlists []DevicePlaylist, manifest *Manifest, staging string) error {
	for _, pl := range playlists {
		var tracks []PlaylistTrack
		for _, tr := range pl.Tracks {
			if manifest.Entry(tr.Rel) != nil {
				tracks = append(tracks, tr)
			}
		}
		name := safePlaylistName(pl.Name)
		if name == "" {
			continue
		}
		rel := path.Join(playlistDir, name+".m3u8")
		content := BuildM3U8(tracks, playlistDir)
		dir := staging
		if dir == "" {
			var err error
			dir, err = os.MkdirTemp("", "auralis-pl-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)
		}
		tmp := filepath.Join(dir, name+".m3u8")
		if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
			return err
		}
		if err := t.Put(ctx, tmp, JoinRemote(root, rel), nil); err != nil {
			return err
		}
	}
	return nil
}

// safePlaylistName strips separators and dots so a playlist name can never
// escape its directory.
func safePlaylistName(name string) string {
	name = strings.ReplaceAll(name, "\\", " ")
	name = strings.ReplaceAll(name, "/", " ")
	name = strings.Trim(name, " .")
	return name
}
