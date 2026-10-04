package backend

import (
	"bytes"
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

func TestAcceptExistingMediaRequiresReadableAudioAndDoesNotDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "already-there.flac")
	payload := bytes.Repeat([]byte("not audio"), 20*1024)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	SetAudioDurationReaderForTest(func(string) (float64, error) {
		return 0, os.ErrInvalid
	})
	t.Cleanup(func() { SetAudioDurationReaderForTest(nil) })

	if AcceptExistingMedia(path, 180) {
		t.Fatal("unreadable file must not be treated as an existing download")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed existing-file check changed the user's file")
	}

	SetAudioDurationReaderForTest(func(string) (float64, error) {
		return 30, nil
	})
	if AcceptExistingMedia(path, 180) {
		t.Fatal("preview-length file must not satisfy a full track")
	}
	after, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("duration mismatch removed the user's file")
	}

	SetAudioDurationReaderForTest(func(string) (float64, error) {
		return 180, nil
	})
	if !AcceptExistingMedia(path, 180) {
		t.Fatal("readable file matching the expected duration should be skipped")
	}
	if !AcceptExistingMedia(path, 0) {
		t.Fatal("readable file should count when the caller has no expected duration")
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
