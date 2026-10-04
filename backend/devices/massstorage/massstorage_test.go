package massstorage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMassStorageTarget(t *testing.T) {
	tempDir := t.TempDir()
	targetRoot := filepath.Join(tempDir, "Music")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	target := New("folder:1", "My Folder", targetRoot)
	ctx := context.Background()

	info := target.Info()
	if info.Kind != "folder" || !info.Connected {
		t.Fatalf("unexpected info: %+v", info)
	}

	target.Removable = true
	infoRemovable := target.Info()
	if infoRemovable.Kind != "removable" {
		t.Fatalf("expected removable kind, got %s", infoRemovable.Kind)
	}

	// Create a dummy local source file
	srcFile := filepath.Join(tempDir, "source.mp3")
	content := []byte("audio-content-test-data-for-sync")
	if err := os.WriteFile(srcFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Put normal file inside root
	var progressReported int64
	fullDst := filepath.Join(targetRoot, "Artist", "Album", "track.mp3")
	err := target.Put(ctx, srcFile, fullDst, func(bytes int64) {
		progressReported = bytes
	})
	if err != nil {
		t.Fatalf("put failed: %v", err)
	}
	if progressReported != int64(len(content)) {
		t.Errorf("expected %d progress bytes, got %d", len(content), progressReported)
	}

	// Verify StatSize
	sz, err := target.StatSize(ctx, fullDst)
	if err != nil || sz != int64(len(content)) {
		t.Fatalf("StatSize failed: %v, got %d", err, sz)
	}

	// Verify Open
	rc, err := target.Open(ctx, fullDst)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	readBack, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || string(readBack) != string(content) {
		t.Fatalf("readBack mismatch: %v, got %q", err, string(readBack))
	}

	// 2. Put rejecting escaping paths
	escapeDst := filepath.Join(targetRoot, "..", "escaped.mp3")
	if err := target.Put(ctx, srcFile, escapeDst, nil); err == nil {
		t.Fatalf("expected error putting outside target root, got nil")
	}

	// 3. Move normal file
	movedDst := filepath.Join(targetRoot, "Artist", "Album", "renamed.mp3")
	if err := target.Move(ctx, fullDst, movedDst); err != nil {
		t.Fatalf("Move failed: %v", err)
	}
	if _, err := os.Stat(fullDst); !os.IsNotExist(err) {
		t.Fatalf("source file should not exist after move")
	}
	if _, err := os.Stat(movedDst); err != nil {
		t.Fatalf("moved file should exist: %v", err)
	}

	// 4. Move rejecting escaping paths
	if err := target.Move(ctx, movedDst, escapeDst); err == nil {
		t.Fatalf("expected error moving outside target root, got nil")
	}
	if err := target.Move(ctx, escapeDst, movedDst); err == nil {
		t.Fatalf("expected error moving from outside target root, got nil")
	}

	// 5. List files
	entries, err := target.List(ctx, targetRoot)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// 6. Delete rejecting escaping paths
	if err := target.Delete(ctx, escapeDst); err == nil {
		t.Fatalf("expected error deleting outside target root, got nil")
	}

	// 7. Delete normal file
	if err := target.Delete(ctx, movedDst); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := os.Stat(movedDst); !os.IsNotExist(err) {
		t.Fatalf("deleted file should not exist")
	}

	// 8. Commit
	if err := target.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// 9. FreeSpace
	free, err := target.FreeSpace(ctx)
	if err != nil || free <= 0 {
		t.Logf("FreeSpace reported %d (err: %v)", free, err)
	}
}

func TestPutFailureKeepsPreviousCopy(t *testing.T) {
	root := t.TempDir()
	target := New("t", "T", root)
	dst := filepath.Join(root, "A", "song.flac")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old good copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "new.flac")
	if err := os.WriteFile(src, make([]byte, 256*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err := target.Put(ctx, src, "A/song.flac", func(n int64) { cancel() })
	if err == nil {
		t.Fatal("expected the cancelled copy to fail")
	}
	if got, _ := os.ReadFile(dst); string(got) != "old good copy" {
		t.Fatalf("previous copy was damaged: %q", got)
	}
	if _, err := os.Stat(dst + partSuffix); !os.IsNotExist(err) {
		t.Fatal("partial file left behind")
	}
}

func TestMoveRefusesToReplace(t *testing.T) {
	root := t.TempDir()
	target := New("t", "T", root)
	for _, name := range []string{"a.flac", "b.flac"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := target.Move(context.Background(), "a.flac", "b.flac"); err == nil {
		t.Fatal("expected move onto an existing file to fail")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "b.flac")); string(got) != "b.flac" {
		t.Fatal("destination was replaced")
	}
}
