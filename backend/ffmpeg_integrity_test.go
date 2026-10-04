package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFFmpegArchiveRejectsModifiedContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download.zip")
	data := []byte("expected archive")
	digest := sha256.Sum256(data)
	expected := hex.EncodeToString(digest[:])
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFFmpegArchive(path, expected); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("modified archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFFmpegArchive(path, expected); err == nil {
		t.Fatal("modified download was accepted")
	}
}

type failingArchiveReader struct{}

func (failingArchiveReader) Read([]byte) (int, error) { return 0, errors.New("archive damaged") }

func TestExecutableInstallPreservesOldFileOnReadFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ffmpeg.exe")
	if err := os.WriteFile(path, []byte("previous executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installArchivedExecutable(path, io.MultiReader(strings.NewReader("partial"), failingArchiveReader{})); err == nil {
		t.Fatal("damaged archive succeeded")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "previous executable" {
		t.Fatalf("old executable changed: %q, %v", data, err)
	}
	if err := installArchivedExecutable(path, strings.NewReader("complete executable")); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "complete executable" {
		t.Fatalf("replacement = %q, %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}

func TestBoundedResponseAcceptsLimitAndRejectsOverflow(t *testing.T) {
	body, err := readBoundedBody(strings.NewReader("1234"), 4)
	if err != nil || string(body) != "1234" {
		t.Fatalf("body = %q, %v", body, err)
	}
	if _, err := readBoundedBody(strings.NewReader("12345"), 4); err == nil {
		t.Fatal("oversized response accepted")
	}
}
