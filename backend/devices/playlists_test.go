package devices

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend/syncengine"
)

func TestPlaylistSelectionResolvesRelativePaths(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "chosen.m3u8")
	if err := os.WriteFile(file, []byte("\ufeff#EXTM3U\n#EXTINF:12,A - Song\nMusic/song.flac\nhttps://example.invalid/song\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selection := syncengine.Selection{Playlists: []string{file}}
	paths, err := LoadLibraryPlaylists(context.Background(), root, selection)
	if err != nil {
		t.Fatal(err)
	}
	selected := selection.Select([]syncengine.SourceTrack{{Path: filepath.Join(root, "Music", "song.flac")}, {Path: filepath.Join(root, "other.flac")}}, paths, time.Now())
	if len(selected) != 1 || selected[0].Path != filepath.Join(root, "Music", "song.flac") {
		t.Fatalf("selection = %+v", selected)
	}
}

func TestPlaylistAndScanErrorsStopPlanning(t *testing.T) {
	root := t.TempDir()
	_, err := LoadLibraryPlaylists(context.Background(), root, syncengine.Selection{Playlists: []string{"missing.m3u8"}})
	if err == nil {
		t.Fatal("missing selected playlist must fail")
	}
	if _, err := ScanLibraryTracks(context.Background(), filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing library must fail instead of returning an empty selection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ScanLibraryTracks(ctx, root); err == nil {
		t.Fatal("cancelled scan must fail")
	}
}
