package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveOutputPathForDownloadIgnoresTinyLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Song - Artist.flac")
	if err := os.WriteFile(path, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved, exists := ResolveOutputPathForDownload(path, false)
	if exists {
		t.Fatal("a truncated leftover must not count as an existing download")
	}
	if resolved != path {
		t.Fatalf("resolved path = %q", resolved)
	}
}

func TestSanitizeManualRenameNameRejectsTraversal(t *testing.T) {
	got, err := SanitizeManualRenameName("../other")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(got, `/\`) {
		t.Fatalf("path separators survived: %q", got)
	}
	if _, err := SanitizeManualRenameName("   "); err == nil {
		t.Fatal("expected blank name to be rejected")
	}
	got, err = SanitizeManualRenameName("  Track 01  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Track 01" {
		t.Fatalf("got %q", got)
	}
}

func TestManualRenamePathStaysInFolder(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.flac")
	if err := os.WriteFile(oldPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	newPath, err := ManualRenamePath(oldPath, "new name")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(newPath) != dir {
		t.Fatalf("renamed path left folder: %q", newPath)
	}
	if !strings.HasSuffix(newPath, ".flac") {
		t.Fatalf("extension lost: %q", newPath)
	}

	sibling := filepath.Join(dir, "taken.flac")
	if err := os.WriteFile(sibling, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ManualRenamePath(oldPath, "taken"); err == nil {
		t.Fatal("expected overwrite of an existing sibling to be rejected")
	}
}

func TestExistingAudioDownloadFindsSiblingM4A(t *testing.T) {
	dir := t.TempDir()
	flac := filepath.Join(dir, "Song - Artist.flac")
	m4a := filepath.Join(dir, "Song - Artist.m4a")
	if err := os.WriteFile(m4a, make([]byte, existingDownloadMinBytes), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := existingAudioDownload(flac)
	if !ok {
		t.Fatal("a sibling .m4a at the minimum size must count as an existing download")
	}
	if got != m4a {
		t.Fatalf("got %q want %q", got, m4a)
	}
}

func TestExistingAudioDownloadIgnoresTinyLeftovers(t *testing.T) {
	dir := t.TempDir()
	flac := filepath.Join(dir, "Song - Artist.flac")
	m4a := filepath.Join(dir, "Song - Artist.m4a")
	if err := os.WriteFile(m4a, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := existingAudioDownload(flac)
	if ok {
		t.Fatal("a truncated leftover must not count as an existing download")
	}
	if got != flac {
		t.Fatalf("got %q", got)
	}
}
