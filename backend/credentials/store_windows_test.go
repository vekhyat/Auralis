//go:build windows

package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTripWindows(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenAt(filepath.Join(dir, "credentials"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	secret := "spotify-refresh-token-abc123-SECRET"
	if err := store.Put("spotify.refresh", secret); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get("spotify.refresh")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != secret {
		t.Fatalf("round-trip mismatch: %q", got)
	}

	// The secret must not appear in the on-disk blob.
	entries, err := os.ReadDir(store.dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	blob, err := os.ReadFile(filepath.Join(store.dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(blob), secret) || strings.Contains(string(blob), "spotify-refresh-token") {
		t.Fatalf("plaintext secret found in stored blob")
	}

	if err := store.Delete("spotify.refresh"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get("spotify.refresh"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	// Deleting again is fine.
	if err := store.Delete("spotify.refresh"); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
}

func TestStoreOpenAtValidatesDir(t *testing.T) {
	if _, err := OpenAt("   "); err == nil {
		t.Fatalf("expected error for empty dir")
	}
}
