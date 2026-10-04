package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScanLibraryReadsTagsAndFolders(t *testing.T) {
	root := t.TempDir()
	album := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	flac := filepath.Join(album, "01. Song.flac")
	makeAudioFile(t, flac, map[string]string{"title": "Song", "artist": "Artist", "album": "Album", "track": "1/2"})
	cover := filepath.Join(album, "cover.jpg")
	if err := os.WriteFile(cover, []byte{0xFF, 0xD8, 0xFF, 0xD9}, 0o644); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(root, "stray.jpg")
	if err := os.WriteFile(stray, []byte{1}, 0o644); err != nil {
		t.Fatal(err)
	}

	var progress []ScanProgress
	scan, err := ScanLibrary(context.Background(), root, func(p ScanProgress) { progress = append(progress, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Tracks) != 1 {
		t.Fatalf("tracks: %+v", scan.Tracks)
	}
	track := scan.Tracks[0]
	if track.Title != "Song" || track.Artist != "Artist" || track.Album != "Album" {
		t.Fatalf("tags: %+v", track)
	}
	if track.TrackNumber != 1 || track.TrackTotal != 2 {
		t.Fatalf("track number: %+v", track)
	}
	if !track.FolderCover {
		t.Fatal("expected folder cover detected")
	}
	if track.DurationSeconds <= 0 {
		t.Fatalf("duration: %+v", track)
	}
	if track.Format != "flac" || track.BitDepth == 0 {
		t.Fatalf("format: %+v", track)
	}
	if len(progress) == 0 {
		t.Fatal("expected progress events")
	}
	foundStray := false
	for _, s := range scan.StrayFiles {
		if s == "stray.jpg" {
			foundStray = true
		}
	}
	if !foundStray {
		t.Fatalf("stray files: %+v", scan.StrayFiles)
	}
}

func TestScanLibraryCancellation(t *testing.T) {
	root := t.TempDir()
	album := filepath.Join(root, "A")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	flac := filepath.Join(album, "a.flac")
	makeAudioFile(t, flac, map[string]string{"title": "T"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ScanLibrary(ctx, root, nil); err == nil {
		t.Fatal("expected cancellation error")
	}
}
