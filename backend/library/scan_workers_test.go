package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"go.senan.xyz/taglib"
)

// writeGarbageAudio writes a non-empty file taglib cannot parse.
func writeGarbageAudio(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("this is not audio data at all"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanWorkerPoolIsDeterministicWithMonotonicProgress(t *testing.T) {
	root := t.TempDir()
	const folders, perFolder = 7, 20
	for f := 0; f < folders; f++ {
		for n := 0; n < perFolder; n++ {
			writeGarbageAudio(t, filepath.Join(root, fmt.Sprintf("Artist %02d", f), "Album", fmt.Sprintf("%02d Song.flac", n)))
		}
	}
	total := folders * perFolder

	var mu sync.Mutex
	var events []ScanProgress
	progress := func(p ScanProgress) {
		mu.Lock()
		events = append(events, p)
		mu.Unlock()
	}

	scan, err := ScanLibrary(context.Background(), root, progress)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Tracks) != total {
		t.Fatalf("tracks: got %d, want %d", len(scan.Tracks), total)
	}
	if !sort.SliceIsSorted(scan.Tracks, func(i, j int) bool { return scan.Tracks[i].RelPath < scan.Tracks[j].RelPath }) {
		t.Fatal("tracks are not sorted by RelPath")
	}
	for _, track := range scan.Tracks {
		if !track.Unreadable || track.Size == 0 || track.RelPath == "" || track.Format != "flac" {
			t.Fatalf("unexpected fallback track: %+v", track)
		}
	}

	// A second scan sees the same thing in the same order.
	again, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range scan.Tracks {
		if scan.Tracks[i].RelPath != again.Tracks[i].RelPath {
			t.Fatalf("order differs at %d: %s vs %s", i, scan.Tracks[i].RelPath, again.Tracks[i].RelPath)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 || len(events) > total/25+3 {
		t.Fatalf("progress events not throttled: %d for %d files", len(events), total)
	}
	last := 0
	for _, e := range events {
		if e.Phase != "tags" || e.Total != total || e.Current <= last {
			t.Fatalf("progress must be monotonic: %+v after %d", e, last)
		}
		last = e.Current
	}
	if last != total {
		t.Fatalf("final progress %d, want %d", last, total)
	}
}

func TestScanCancelledMidwayReturnsContextError(t *testing.T) {
	root := t.TempDir()
	for n := 0; n < 60; n++ {
		writeGarbageAudio(t, filepath.Join(root, "Artist", "Album", fmt.Sprintf("%02d Song.flac", n)))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := ScanLibrary(ctx, root, func(p ScanProgress) {
		if p.Current >= 25 {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
}

func TestCoverCacheListsEachFolderOnce(t *testing.T) {
	root := t.TempDir()
	album := filepath.Join(root, "Artist", "Album")
	cd1 := filepath.Join(album, "CD1")
	cd2 := filepath.Join(album, "CD2")
	for _, dir := range []string{cd1, cd2} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cover := filepath.Join(album, "cover.jpg")
	if err := os.WriteFile(cover, []byte{0xFF, 0xD8, 0xFF, 0xD9}, 0o644); err != nil {
		t.Fatal(err)
	}

	cache := &coverCache{}
	// The disc folders hold no art of their own; the parent's cover counts.
	if !cache.folderHasCover(cd1) || !cache.folderHasCover(cd2) || !cache.folderHasCover(album) {
		t.Fatal("expected the album cover to apply to the album and both disc folders")
	}
	// Removing the file proves later lookups come from the cache, not the disk.
	if err := os.Remove(cover); err != nil {
		t.Fatal(err)
	}
	if !cache.folderHasCover(cd1) || !cache.folderHasCover(album) {
		t.Fatal("expected cached answers")
	}
	if folderHasCoverFile(album) {
		t.Fatal("cover should be gone on disk")
	}
	var nilCache *coverCache
	if nilCache.folderHasCover(cd2) {
		t.Fatal("a nil cache must read the disk, where the cover is gone")
	}

	// A folder that is not a disc folder does not borrow its parent's cover.
	extras := filepath.Join(album, "Extras")
	if err := os.MkdirAll(extras, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cover, []byte{0xFF, 0xD8, 0xFF, 0xD9}, 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := &coverCache{}
	if fresh.folderHasCover(extras) {
		t.Fatal("non-disc folder must not inherit the parent cover")
	}
	if !fresh.folderHasCover(cd1) {
		t.Fatal("disc folder must inherit the parent cover")
	}
}

func TestUnreadableTracksAreReportedOnceAndSkippedByOtherRules(t *testing.T) {
	root := t.TempDir()
	album := filepath.Join(root, "Artist", "Album")
	bad := filepath.Join(album, "01 Broken.flac")
	writeGarbageAudio(t, bad)
	empty := filepath.Join(album, "02 Empty.flac")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// A readable file named like this would be inspected by the PATH rule.
	badName := filepath.Join(root, "Artist", "Other", "03 Song [x].flac")
	writeGarbageAudio(t, badName)

	scan, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Tracks) != 3 {
		t.Fatalf("tracks: %+v", scan.Tracks)
	}
	for _, track := range scan.Tracks {
		if !track.Unreadable {
			t.Fatalf("expected unreadable: %+v", track)
		}
	}
	if len(Albums(scan)) != 0 {
		t.Fatalf("unreadable tracks must not form albums: %v", Albums(scan))
	}

	profile := ProfileByID(ProfilePoweramp)
	profile.FolderCover = true
	issues := DetectAll(scan, profile)
	unreadable := map[string]bool{}
	reports := map[string]int{} // issues of any rule per file
	var zeroByte int
	for _, issue := range issues {
		reports[issue.Path]++
		switch issue.RuleID {
		case "UNREADABLE":
			unreadable[issue.Path] = true
			if issue.Severity != SeverityError || issue.Fixable || issue.Message != "tags could not be read: file may be damaged" {
				t.Fatalf("unexpected UNREADABLE issue: %+v", issue)
			}
		case "ORPHAN":
			if issue.Path == empty && strings.Contains(issue.Message, "zero-byte") {
				zeroByte++
			}
		default:
			t.Errorf("unreadable files must not raise %s: %+v", issue.RuleID, issue)
		}
	}
	for _, p := range []string{bad, badName} {
		if !unreadable[p] {
			t.Errorf("missing UNREADABLE issue for %s", p)
		}
	}
	// A zero-byte file is reported once, as an empty audio file, not again as
	// unreadable.
	if unreadable[empty] {
		t.Errorf("zero-byte file must not also be reported as UNREADABLE")
	}
	if zeroByte != 1 || reports[empty] != 1 {
		t.Errorf("zero-byte file: %d ORPHAN reports, %d issues in all, want exactly one", zeroByte, reports[empty])
	}
	if plan := BuildPlan(scan, profile, issues); len(plan.Operations) != 0 {
		t.Fatalf("report-only rule produced operations: %+v", plan.Operations)
	}
}

func TestOggOpusAndWavAreScannedAndRetagged(t *testing.T) {
	for _, ext := range []string{".ogg", ".opus", ".wav"} {
		t.Run(ext, func(t *testing.T) {
			root := t.TempDir()
			album := filepath.Join(root, "Artist", "Album")
			if err := os.MkdirAll(album, 0o755); err != nil {
				t.Fatal(err)
			}
			src := filepath.Join(album, "03 Song"+ext)
			makeAudioFile(t, src, map[string]string{"title": "Song", "artist": "Artist", "album": "Album", "track": "3/9"})

			scan, err := ScanLibrary(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(scan.Tracks) != 1 {
				t.Fatalf("tracks: %+v", scan.Tracks)
			}
			track := scan.Tracks[0]
			if track.Unreadable || track.Format != strings.TrimPrefix(ext, ".") {
				t.Fatalf("not read as %s: %+v", ext, track)
			}
			if track.Title != "Song" || track.Album != "Album" || track.DurationSeconds <= 0 {
				t.Fatalf("tags/properties missing: %+v", track)
			}
			if track.TrackNumber != 3 || track.TrackTotal != 9 {
				t.Fatalf("track number: %+v", track)
			}

			profile := ProfileByID(ProfilePoweramp)
			plan := BuildPlan(scan, profile, DetectAll(scan, profile))
			if len(plan.Operations) == 0 {
				t.Fatal("expected fixes for the missing ALBUMARTIST")
			}
			result, err := Apply(context.Background(), root, filepath.Join(t.TempDir(), "journal.jsonl"), plan)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Errors) != 0 {
				t.Fatalf("apply errors: %v", result.Errors)
			}
			dest := filepath.Join(album, "03. Song"+ext)
			tags, err := taglib.ReadTags(dest)
			if err != nil {
				t.Fatalf("file not at the template path: %v", err)
			}
			if firstTag(tags, "ALBUMARTIST") != "Artist" {
				t.Fatalf("ALBUMARTIST not written: %v", tags)
			}
		})
	}
}

func TestVorbisCommentFormatsKeepSeparateTotals(t *testing.T) {
	for _, format := range []string{"flac", "ogg", "opus"} {
		track := trackSet(format, 2, 10)
		if track["TRACKNUMBER"] != "2" || track["TRACKTOTAL"] != "10" {
			t.Errorf("%s track: %v", format, track)
		}
		disc := discSet(format, 1, 2)
		if disc["DISCNUMBER"] != "1" || disc["DISCTOTAL"] != "2" {
			t.Errorf("%s disc: %v", format, disc)
		}
	}
	if got := trackSet("wav", 2, 10)["TRACKNUMBER"]; got != "2/10" {
		t.Errorf("wav track number: %q", got)
	}
}
