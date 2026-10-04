package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.senan.xyz/taglib"
)

func TestApplyTagsPreservesOtherTagsAndJournal(t *testing.T) {
	dir := t.TempDir()
	flac := filepath.Join(dir, "a.flac")
	makeAudioFile(t, flac, map[string]string{"title": "T", "artist": "A"})

	// Pre-existing ReplayGain-style tag must survive the merge write.
	tags, err := taglib.ReadTags(flac)
	if err != nil {
		t.Fatal(err)
	}
	tags["REPLAYGAIN_TRACK_GAIN"] = []string{"-6.5 dB"}
	if err := taglib.WriteTags(flac, tags, 0); err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(flac)
	plan := &Plan{Operations: []Operation{{
		Type: "tags", Path: flac, Set: map[string]string{"ALBUMARTIST": "Someone"},
		Size: info.Size(), ModTime: info.ModTime().UnixNano(),
	}}}
	journal := filepath.Join(dir, "journal.jsonl")
	result, err := Apply(context.Background(), dir, journal, plan)
	if err != nil || result.Applied != 1 {
		t.Fatalf("apply: %+v %v", result, err)
	}

	updated, err := taglib.ReadTags(flac)
	if err != nil {
		t.Fatal(err)
	}
	if got := firstTag(updated, "ALBUMARTIST"); got != "Someone" {
		t.Fatalf("ALBUMARTIST = %q", got)
	}
	if got := firstTag(updated, "REPLAYGAIN_TRACK_GAIN"); got != "-6.5 dB" {
		t.Fatalf("ReplayGain lost: %q", got)
	}

	entries, err := ReadJournal(journal)
	if err != nil || len(entries) != 1 {
		t.Fatalf("journal: %d entries %v", len(entries), err)
	}
	if entries[0].Set["ALBUMARTIST"] != "Someone" {
		t.Fatalf("journal entry: %+v", entries[0])
	}
}

func TestApplyRefusesChangedFileAndUndoRestores(t *testing.T) {
	dir := t.TempDir()
	flac := filepath.Join(dir, "a.flac")
	makeAudioFile(t, flac, map[string]string{"title": "T"})

	info, _ := os.Stat(flac)
	plan := &Plan{Operations: []Operation{{
		Type: "tags", Path: flac, Set: map[string]string{"ALBUMARTIST": "Someone"},
		Size: info.Size() + 10, ModTime: info.ModTime().UnixNano(),
	}}}
	result, err := Apply(context.Background(), dir, filepath.Join(dir, "j.jsonl"), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 0 || len(result.Errors) == 0 {
		t.Fatalf("expected skip on changed file, got %+v", result)
	}

	// Apply for real, then undo must restore the missing albumartist.
	plan.Operations[0].Size = info.Size()
	journal := filepath.Join(dir, "j.jsonl")
	if _, err := Apply(context.Background(), dir, journal, plan); err != nil {
		t.Fatal(err)
	}
	undo, err := UndoLast(journal)
	if err != nil || undo.Applied != 1 {
		t.Fatalf("undo: %+v %v", undo, err)
	}
	tags, _ := taglib.ReadTags(flac)
	if firstTag(tags, "ALBUMARTIST") != "" {
		t.Fatalf("expected albumartist removed, got %q", firstTag(tags, "ALBUMARTIST"))
	}

	// Undo of an already-undone batch is a no-op.
	again, err := UndoLast(journal)
	if err != nil || again.Applied != 0 {
		t.Fatalf("second undo: %+v %v", again, err)
	}
}

func TestMoveThroughIndexAndUndo(t *testing.T) {
	dir := t.TempDir()
	oldDir := filepath.Join(dir, "Old")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	flac := filepath.Join(oldDir, "a.flac")
	makeAudioFile(t, flac, map[string]string{"title": "T"})
	info, _ := os.Stat(flac)

	dest := filepath.Join(dir, "New", "a.flac")
	plan := &Plan{Operations: []Operation{{
		Type: "move", Path: flac, NewPath: dest,
		Size: info.Size(), ModTime: info.ModTime().UnixNano(),
	}}}
	journal := filepath.Join(dir, "j.jsonl")
	result, err := Apply(context.Background(), dir, journal, plan)
	if err != nil || result.Applied != 1 {
		t.Fatalf("apply: %+v %v", result, err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("destination missing: %v", err)
	}

	// Never overwrite an existing destination.
	dest2 := filepath.Join(dir, "Old", "b.flac")
	makeAudioFile(t, dest2, map[string]string{"title": "B"})
	colliding := &Plan{Operations: []Operation{{
		Type: "move", Path: dest, NewPath: dest2, Size: 0, ModTime: 0,
	}}}
	res2, _ := Apply(context.Background(), dir, filepath.Join(dir, "j2.jsonl"), colliding)
	if res2.Applied != 0 {
		t.Fatalf("expected collision skip, got %+v", res2)
	}

	if _, err := UndoLast(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(flac); err != nil {
		t.Fatalf("undo did not restore old path: %v", err)
	}
}

func TestJournalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "j.jsonl")
	entries := []JournalEntry{
		{BatchID: "b1", Time: time.Now().UTC().Format(time.RFC3339), Type: "tags", Path: `C:\m\a.flac`, Set: map[string]string{"A": "1"}, Old: map[string]string{"A": ""}},
		{BatchID: "b1", Time: time.Now().UTC().Format(time.RFC3339), Type: "move", Path: `C:\m\a.flac`, NewPath: `C:\m\b\a.flac`},
		{BatchID: "b2", Time: time.Now().UTC().Format(time.RFC3339), Type: "tags", Path: `C:\m\c.flac`, Set: map[string]string{"B": "2"}},
	}
	for _, entry := range entries {
		if err := AppendJournal(journal, entry); err != nil {
			t.Fatal(err)
		}
	}
	read, err := ReadJournal(journal)
	if err != nil || len(read) != 3 {
		t.Fatalf("read: %d %v", len(read), err)
	}
	last := LastBatch(read)
	if len(last) != 1 || last[0].BatchID != "b2" {
		t.Fatalf("last batch: %+v", last)
	}
	// Mark b2 undone, then b1 is the tail.
	if err := AppendJournal(journal, JournalEntry{Type: "undo", BatchID: "b2"}); err != nil {
		t.Fatal(err)
	}
	read, _ = ReadJournal(journal)
	last = LastBatch(read)
	if len(last) != 2 || last[0].BatchID != "b1" {
		t.Fatalf("last batch after undo: %+v", last)
	}
}

func TestUndoLastEndToEndWithJournal(t *testing.T) {
	dir := t.TempDir()
	flac := filepath.Join(dir, "a.flac")
	makeAudioFile(t, flac, map[string]string{"title": "T", "albumartist": "Old"})
	info, _ := os.Stat(flac)
	journal := filepath.Join(dir, "j.jsonl")
	plan := &Plan{Operations: []Operation{{
		Type: "tags", Path: flac, Set: map[string]string{"ALBUMARTIST": "New"},
		Size: info.Size(), ModTime: info.ModTime().UnixNano(),
	}}}
	if _, err := Apply(context.Background(), dir, journal, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLast(journal); err != nil {
		t.Fatal(err)
	}
	tags, _ := taglib.ReadTags(flac)
	if got := firstTag(tags, "ALBUMARTIST"); got != "Old" {
		t.Fatalf("expected Old restored, got %q", got)
	}
	entries, _ := ReadJournal(journal)
	var sawUndo bool
	for _, e := range entries {
		if e.Type == "undo" {
			sawUndo = true
		}
	}
	if !sawUndo {
		t.Fatal("expected undo marker in journal")
	}
}
