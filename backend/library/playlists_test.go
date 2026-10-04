package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleTracks() []PlaylistTrack {
	return []PlaylistTrack{
		{Path: `C:\music\Artist\Album\01 Song.flac`, Title: "Song", Artist: "Artist", DurationSeconds: 62},
		{Path: `D:\other\track.mp3`, Title: "Other", Artist: "Else", DurationSeconds: 10},
	}
}

func TestPlaylistRelativeMode(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "Playlists", "mix.m3u8")
	track := PlaylistTrack{Path: filepath.Join(dir, "Artist", "Al", "01.flac"), Title: "S", Artist: "A", DurationSeconds: 62}
	res, err := WritePlaylist(out, []PlaylistTrack{track}, PlaylistRelative, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 1 || len(res.Skipped) != 0 {
		t.Fatalf("%+v", res)
	}
	data, _ := os.ReadFile(out)
	text := string(data)
	if !strings.HasPrefix(text, "#EXTM3U\n") {
		t.Fatalf("header: %q", text)
	}
	if !strings.Contains(text, "#EXTINF:62,A - S\n") {
		t.Fatalf("extinf: %q", text)
	}
	if !strings.Contains(text, "../Artist/Al/01.flac\n") {
		t.Fatalf("relative path: %q", text)
	}
}

func TestPlaylistRootModeSkipsOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "mix.m3u8")
	inside := PlaylistTrack{Path: filepath.Join(dir, "A", "x.flac"), Title: "X"}
	outside := PlaylistTrack{Path: filepath.Join(t.TempDir(), "y.flac"), Title: "Y"}
	res, err := WritePlaylist(out, []PlaylistTrack{inside, outside}, PlaylistRoot, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 1 || len(res.Skipped) != 1 {
		t.Fatalf("%+v", res)
	}
	if res.Skipped[0].Reason == "" {
		t.Fatal("expected skip reason")
	}
	data, _ := os.ReadFile(out)
	if strings.Contains(string(data), "y.flac") {
		t.Fatalf("outside track written: %s", data)
	}
}

func TestPlaylistDeviceModePrefixesAndSkipsForeignVolume(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "mix.m3u8")
	inside := PlaylistTrack{Path: filepath.Join(dir, "A", "x.flac"), Title: "X"}
	foreign := PlaylistTrack{Path: `D:\elsewhere\y.flac`, Title: "Y"}
	res, err := WritePlaylist(out, []PlaylistTrack{inside, foreign}, PlaylistDevice, dir, "/storage/emulated/0/Music")
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 1 || len(res.Skipped) != 1 {
		t.Fatalf("%+v", res)
	}
	data, _ := os.ReadFile(out)
	text := filepath.ToSlash(string(data))
	if !strings.Contains(text, "/storage/emulated/0/Music/A/x.flac") {
		t.Fatalf("device path: %q", text)
	}
	if strings.Contains(text, "D:") || strings.Contains(text, `D:\`) {
		t.Fatalf("absolute windows path leaked: %q", text)
	}
}

func TestPlaylistRelativeDifferentVolumeSkipped(t *testing.T) {
	// C: and D: are always different volumes on Windows, so a C: playlist
	// cannot reference a D: file and must skip it rather than write C:\...
	line, reason := playlistLine(`C:\music\Playlists\mix.m3u8`, `D:\songs\x.flac`, PlaylistRelative, "", "")
	if line != "" || reason == "" {
		t.Fatalf("line=%q reason=%q", line, reason)
	}
}

func TestPlaylistUTF8AndForwardSlashes(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "mix.m3u8")
	track := PlaylistTrack{Path: filepath.Join(dir, "Björk", "Debut", "02. ræ».flac"), Title: "Ræ»", Artist: "Björk", DurationSeconds: 5}
	if _, err := WritePlaylist(out, []PlaylistTrack{track}, PlaylistRelative, "", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	text := string(data)
	if strings.Contains(text, "\\") {
		t.Fatalf("backslash in playlist: %q", text)
	}
	if !strings.Contains(text, "Björk/Debut/02. ræ».flac") {
		t.Fatalf("utf-8 path: %q", text)
	}
}
