package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupTidalDownloadArtifactsRemovesM4ASibling(t *testing.T) {
	dir := t.TempDir()
	flac := filepath.Join(dir, "Song.flac")
	tmp := flac + ".m4a.tmp"
	m4a := filepath.Join(dir, "Song.m4a")
	for _, path := range []string{flac, tmp, m4a} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cleanupTidalDownloadArtifacts(flac)
	for _, path := range []string{flac, tmp, m4a} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should have been removed", path)
		}
	}
}
