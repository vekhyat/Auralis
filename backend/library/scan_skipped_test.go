package library

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// denyListing makes dir impossible to list for the current user and restores it
// when the test ends. It skips the test where that cannot be arranged (running
// as root, a filesystem without permissions).
func denyListing(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// Deny "Everyone" the read-data / list-directory right.
		if out, err := exec.Command("icacls", dir, "/deny", "*S-1-1-0:(RD)").CombinedOutput(); err != nil {
			t.Skipf("icacls: %v: %s", err, out)
		}
		t.Cleanup(func() { _ = exec.Command("icacls", dir, "/remove:d", "*S-1-1-0").Run() })
	} else {
		if os.Geteuid() == 0 {
			t.Skip("root can list any folder")
		}
		if err := os.Chmod(dir, 0); err != nil {
			t.Skipf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	}
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("folder is still listable on this system")
	}
}

func TestScanCountsUnreadableFoldersAndKeepsGoing(t *testing.T) {
	root := t.TempDir()
	writeGarbageAudio(t, filepath.Join(root, "Artist", "Album", "01 Song.flac"))
	for _, dir := range []string{"Locked B", "Artist/Locked A", "Artist/Album/Locked C"} {
		full := filepath.Join(root, filepath.FromSlash(dir))
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		// Whatever is inside is out of reach; it must not appear in the scan.
		writeGarbageAudio(t, filepath.Join(full, "02 Hidden.flac"))
		denyListing(t, full)
	}

	scan, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatalf("an unreadable folder must not fail the scan: %v", err)
	}
	if len(scan.Tracks) != 1 || scan.Tracks[0].RelPath != "Artist/Album/01 Song.flac" {
		t.Fatalf("tracks: %+v", scan.Tracks)
	}
	if scan.SkippedDirs != 3 {
		t.Fatalf("SkippedDirs = %d, want 3 (sample %v)", scan.SkippedDirs, scan.SkippedSample)
	}
	want := []string{"Artist/Album/Locked C", "Artist/Locked A", "Locked B"}
	if !reflect.DeepEqual(scan.SkippedSample, want) {
		t.Fatalf("SkippedSample = %v, want %v", scan.SkippedSample, want)
	}

	// The same tree scans the same way.
	again, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.SkippedDirs != scan.SkippedDirs || !reflect.DeepEqual(again.SkippedSample, scan.SkippedSample) ||
		!reflect.DeepEqual(again.Dirs, scan.Dirs) || len(again.Tracks) != len(scan.Tracks) {
		t.Fatalf("scans of one tree differ: %+v vs %+v", again, scan)
	}
}

func TestScanSkippedSampleIsCappedAndSorted(t *testing.T) {
	root := t.TempDir()
	const locked = 8
	for n := locked; n >= 1; n-- {
		full := filepath.Join(root, fmt.Sprintf("Locked %02d", n))
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		denyListing(t, full)
	}
	scan, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if scan.SkippedDirs != locked {
		t.Fatalf("SkippedDirs = %d, want %d", scan.SkippedDirs, locked)
	}
	want := []string{"Locked 01", "Locked 02", "Locked 03", "Locked 04", "Locked 05"}
	if !reflect.DeepEqual(scan.SkippedSample, want) {
		t.Fatalf("SkippedSample = %v, want %v", scan.SkippedSample, want)
	}
}

func TestScanOfReadableTreeSkipsNothing(t *testing.T) {
	root := t.TempDir()
	writeGarbageAudio(t, filepath.Join(root, "Artist", "Album", "01 Song.flac"))
	if err := os.MkdirAll(filepath.Join(root, "Empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	scan, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if scan.SkippedDirs != 0 || len(scan.SkippedSample) != 0 {
		t.Fatalf("skipped %d %v in a fully readable tree", scan.SkippedDirs, scan.SkippedSample)
	}
}

func TestScanOfMissingRootCountsTheRootAsSkipped(t *testing.T) {
	root := filepath.Join(t.TempDir(), "gone")
	scan, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatalf("a root that cannot be read must not fail the scan: %v", err)
	}
	if len(scan.Tracks) != 0 || scan.SkippedDirs != 1 || !reflect.DeepEqual(scan.SkippedSample, []string{"."}) {
		t.Fatalf("got %d skipped %v, %d tracks", scan.SkippedDirs, scan.SkippedSample, len(scan.Tracks))
	}
}

// The sample is the smallest paths, whatever order the walk met them in.
func TestNoteSkippedDirKeepsSmallestFiveRegardlessOfOrder(t *testing.T) {
	root := filepath.Join("lib", "root")
	names := []string{"m", "a/z", "z", "b", "a.b", "c/d/e", "a", "y"}
	var forward, backward Scan
	for i := range names {
		forward.noteSkippedDir(root, filepath.Join(root, filepath.FromSlash(names[i])))
		backward.noteSkippedDir(root, filepath.Join(root, filepath.FromSlash(names[len(names)-1-i])))
	}
	want := []string{"a", "a.b", "a/z", "b", "c/d/e"}
	if forward.SkippedDirs != len(names) || backward.SkippedDirs != len(names) {
		t.Fatalf("counts: %d %d", forward.SkippedDirs, backward.SkippedDirs)
	}
	if !reflect.DeepEqual(forward.SkippedSample, want) || !reflect.DeepEqual(backward.SkippedSample, want) {
		t.Fatalf("samples: %v / %v, want %v", forward.SkippedSample, backward.SkippedSample, want)
	}
}
