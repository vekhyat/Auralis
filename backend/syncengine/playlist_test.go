package syncengine

import (
	"strings"
	"testing"
)

func TestBuildM3U8(t *testing.T) {
	tracks := []PlaylistTrack{
		{Rel: "Artist/Album/01. One.flac", Title: "One", Artist: "Artist", DurationSec: 183},
		{Rel: "Artist/Album/02. Two.flac", Title: "Two", Artist: "Artist"},
	}
	out := BuildM3U8(tracks, "Playlists")
	if !strings.Contains(out, "#EXTM3U") {
		t.Fatal("missing header")
	}
	if !strings.Contains(out, "#EXTINF:183,Artist - One\n../Artist/Album/01. One.flac") {
		t.Fatalf("missing EXTINF for first track:\n%s", out)
	}
	if !strings.Contains(out, "#EXTINF:-1,Artist - Two\n../Artist/Album/02. Two.flac") {
		t.Fatalf("missing EXTINF for second track:\n%s", out)
	}
}

func TestSafePlaylistName(t *testing.T) {
	if got := safePlaylistName(`../../evil`); strings.Contains(got, "/") || strings.Contains(got, "..") {
		t.Fatalf("unsafe name %q", got)
	}
}
