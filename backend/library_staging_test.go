package backend

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLibraryScanExcludesIncomingDownloads(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, ".auralis-incoming-test")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := bytes.Repeat([]byte{0}, 200*1024)
	if err := os.WriteFile(filepath.Join(stage, "partial.flac"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "finished.flac"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := collectLibraryIndexEntries(root, "test", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Filename != "finished.flac" {
		t.Fatalf("visible library entries = %#v", entries)
	}
	files, err := ListAudioFiles(root)
	if err != nil || len(files) != 1 || files[0].Name != "finished.flac" {
		t.Fatalf("visible audio files = %#v, %v", files, err)
	}
	listing, err := ListDirectory(root)
	if err != nil || len(listing) != 1 || listing[0].Name != "finished.flac" {
		t.Fatalf("visible directory entries = %#v, %v", listing, err)
	}
}
