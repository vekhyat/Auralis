package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.senan.xyz/taglib"
)

// TestApplyThenRescanClearsFixableIssues is the symptom: a preview lists
// fixes, Apply reports them done, and a fresh scan still reports the same
// fixable problems.
func TestApplyThenRescanClearsFixableIssues(t *testing.T) {
	root := t.TempDir()
	album := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	one := filepath.Join(album, "01 Song.flac")
	two := filepath.Join(album, "02 Song.flac")
	makeAudioFile(t, one, map[string]string{
		"title": "One", "artist": "Artist", "album": "Album", "albumartist": "Someone Else",
		"tracknumber": "1/2",
	})
	makeAudioFile(t, two, map[string]string{
		"title": "Two", "artist": "Artist", "album": "Album", "albumartist": "Artist",
		"tracknumber": "2/2",
	})

	profile := ProfileByID(ProfilePoweramp)
	scan, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := DetectAll(scan, profile)
	var fixable []Issue
	for _, issue := range before {
		if issue.Fixable {
			fixable = append(fixable, issue)
		}
	}
	if len(fixable) == 0 {
		t.Fatalf("premise: want fixable issues, got %+v", before)
	}
	plan := BuildPlan(scan, profile, fixable)
	for _, op := range plan.Operations {
		t.Logf("op %s %s -> %s set=%v err=%s", op.Type, op.Path, op.NewPath, op.Set, op.Error)
	}
	journal := filepath.Join(t.TempDir(), "journal.jsonl")
	result, err := Apply(context.Background(), root, journal, plan)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("apply %+v", result)
	rescan, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	after := DetectAll(rescan, profile)
	var still []Issue
	for _, issue := range after {
		if issue.Fixable {
			still = append(still, issue)
		}
	}
	if len(still) > 0 {
		t.Fatalf("fixable issues remain after apply: %+v\ntracks=%+v", still, rescan.Tracks)
	}
}

func TestChainedRenameApplies(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "A.flac")
	b := filepath.Join(root, "B.flac")
	makeAudioFile(t, a, map[string]string{"title": "A", "artist": "Artist", "album": "Album", "albumartist": "Artist", "tracknumber": "1"})
	makeAudioFile(t, b, map[string]string{"title": "B", "artist": "Artist", "album": "Album", "albumartist": "Artist", "tracknumber": "2"})
	infoA, _ := os.Stat(a)
	infoB, _ := os.Stat(b)
	// A takes B's name and B takes a new name. Sorted by path, A is first, so
	// apply has to move B out of the way before A can land there.
	plan := &Plan{Operations: []Operation{
		{Type: "move", Path: a, NewPath: b, Size: infoA.Size(), ModTime: infoA.ModTime().UnixNano()},
		{Type: "move", Path: b, NewPath: filepath.Join(root, "C.flac"), Size: infoB.Size(), ModTime: infoB.ModTime().UnixNano()},
	}}
	result, err := Apply(context.Background(), root, filepath.Join(t.TempDir(), "j.jsonl"), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 2 || len(result.Errors) != 0 {
		t.Fatalf("chained rename: %+v", result)
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatalf("A should now be at B's old path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "C.flac")); err != nil {
		t.Fatalf("B should now be at C: %v", err)
	}
	tags, err := taglib.ReadTags(b)
	if err != nil {
		t.Fatal(err)
	}
	if firstTag(tags, "TITLE") != "A" {
		t.Fatalf("path B has title %q, want A (the file that moved onto it)", firstTag(tags, "TITLE"))
	}
	tags, err = taglib.ReadTags(filepath.Join(root, "C.flac"))
	if err != nil {
		t.Fatal(err)
	}
	if firstTag(tags, "TITLE") != "B" {
		t.Fatalf("path C has title %q, want B", firstTag(tags, "TITLE"))
	}
	if temps := renameTemps(t, root); len(temps) != 0 {
		t.Fatalf("a chain should not leave temporary names: %v", temps)
	}
}

func TestApplyTrackNumberOnCompressedFormats(t *testing.T) {
	for _, ext := range []string{".mp3", ".m4a"} {
		t.Run(ext, func(t *testing.T) {
			root := t.TempDir()
			album := filepath.Join(root, "Artist", "Album")
			if err := os.MkdirAll(album, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(album, "01 Song"+ext)
			makeAudioFile(t, path, map[string]string{
				"title": "Song", "artist": "Artist", "album": "Album", "albumartist": "Other",
			})
			profile := ProfileByID(ProfilePoweramp)
			scan, err := ScanLibrary(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			plan := BuildPlan(scan, profile, DetectAll(scan, profile))
			for _, op := range plan.Operations {
				t.Logf("op %s %s -> %s set=%v err=%s", op.Type, op.Path, op.NewPath, op.Set, op.Error)
			}
			result, err := Apply(context.Background(), root, filepath.Join(t.TempDir(), "j.jsonl"), plan)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("apply %+v", result)
			rescan, err := ScanLibrary(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			var still []Issue
			for _, issue := range DetectAll(rescan, profile) {
				if issue.Fixable {
					still = append(still, issue)
				}
			}
			if len(still) > 0 || result.Applied == 0 || len(result.Errors) > 0 {
				t.Fatalf("apply %+v, remaining %+v, tracks %+v", result, still, rescan.Tracks)
			}
		})
	}
}

// TestSwappedTrackFilenamesApplyAndRescan is the library case of a swap:
// 01. Two.flac is tagged track 2, and 02. Two.flac is tagged track 1, so each
// file's template path is the other's name. Preview used to list both moves
// with no error, and apply then skipped them because each destination existed.
func TestSwappedTrackFilenamesApplyAndRescan(t *testing.T) {
	root := t.TempDir()
	album := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(album, "01. Two.flac")
	second := filepath.Join(album, "02. Two.flac")
	makeAudioFile(t, first, map[string]string{
		"title": "Two", "artist": "Artist", "album": "Album", "albumartist": "Artist",
		"tracknumber": "2",
	})
	makeAudioFile(t, second, map[string]string{
		"title": "Two", "artist": "Artist", "album": "Album", "albumartist": "Artist",
		"tracknumber": "1",
	})

	profile := ProfileByID(ProfilePoweramp)
	scan, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan := BuildPlan(scan, profile, DetectAll(scan, profile))
	var moves int
	for _, op := range plan.Operations {
		if op.Type != "move" || op.Error != "" {
			t.Fatalf("swap preview should be two clean moves, got %+v", plan.Operations)
		}
		moves++
	}
	if moves != 2 {
		t.Fatalf("want 2 moves, got %+v", plan.Operations)
	}
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	result, err := Apply(context.Background(), root, journal, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("apply %+v", result)
	}
	rescan, err := ScanLibrary(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var still []Issue
	for _, issue := range DetectAll(rescan, profile) {
		if issue.Fixable {
			still = append(still, issue)
		}
	}
	if len(still) > 0 {
		t.Fatalf("fixable issues remain: %+v", still)
	}
	if got := trackNumberAt(t, first); got != "1" {
		t.Fatalf("01. Two.flac track number %q, want 1 (the file that used to be 02)", got)
	}
	if got := trackNumberAt(t, second); got != "2" {
		t.Fatalf("02. Two.flac track number %q, want 2 (the file that used to be 01)", got)
	}
	if temps := renameTemps(t, root); len(temps) != 0 {
		t.Fatalf("temporary rename left behind: %v", temps)
	}

	undo, err := UndoLast(journal)
	if err != nil || len(undo.Errors) != 0 {
		t.Fatalf("undo: %+v %v", undo, err)
	}
	if got := trackNumberAt(t, first); got != "2" {
		t.Fatalf("undo left 01. Two.flac with track number %q, want 2", got)
	}
	if got := trackNumberAt(t, second); got != "1" {
		t.Fatalf("undo left 02. Two.flac with track number %q, want 1", got)
	}
}

func trackNumberAt(t *testing.T, path string) string {
	t.Helper()
	tags, err := taglib.ReadTags(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := firstTag(tags, "TRACKNUMBER"); n != "" {
		return n
	}
	return firstTag(tags, "TRACK")
}
