package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDownloadedTrackDurationRejectsUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "junk.flac")
	if err := os.WriteFile(path, []byte("not audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	validated, err := ValidateDownloadedTrackDuration(path, 180)
	if err == nil {
		t.Fatal("expected an error for unreadable audio")
	}
	if !validated {
		t.Fatal("unreadable files must fail closed so the caller deletes them")
	}
}

func TestValidateDownloadedTrackDurationSkipsWhenNoExpectedDuration(t *testing.T) {
	validated, err := ValidateDownloadedTrackDuration("missing.flac", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if validated {
		t.Fatal("zero expected duration should skip validation")
	}
}
