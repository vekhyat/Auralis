package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.senan.xyz/taglib"
)

// safetyFile writes a small plain file. Moves and rmdirs need no real audio,
// so most tests here run without ffmpeg.
func safetyFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func safetyExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// safetyRoot returns an empty library root that is not the temp dir itself.
func safetyRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "lib")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// safetyMovePlan creates n files in root/src, next to a keeper so the folder
// never empties and no folder removal is journaled, and returns a plan that
// moves each into root/dst plus the source and destination paths.
func safetyMovePlan(t *testing.T, root string, n int) (plan *Plan, from, to []string) {
	t.Helper()
	srcDir := filepath.Join(root, "src")
	safetyFile(t, filepath.Join(srcDir, "keep.txt"))
	plan = &Plan{}
	for i := 1; i <= n; i++ {
		src := filepath.Join(srcDir, fmt.Sprintf("f%d.txt", i))
		dst := filepath.Join(root, "dst", fmt.Sprintf("f%d.txt", i))
		safetyFile(t, src)
		plan.Operations = append(plan.Operations, Operation{Type: "move", Path: src, NewPath: dst})
		from = append(from, src)
		to = append(to, dst)
	}
	return plan, from, to
}

func safetyEntries(t *testing.T, journal string) []JournalEntry {
	t.Helper()
	entries, err := ReadJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func safetyCount(entries []JournalEntry, entryType string) int {
	n := 0
	for _, entry := range entries {
		if entry.Type == entryType {
			n++
		}
	}
	return n
}

// safetyRecord is one journal write a safetySink saw.
type safetyRecord struct {
	entry   JournalEntry
	durable bool
}

// safetySink wraps the real journal file so a test can watch every record, or
// make one fail. before runs ahead of the write with the 1-based record count.
type safetySink struct {
	inner  journalSink
	before func(n int, entry JournalEntry) error
	log    []safetyRecord
}

func newSafetySink(journal string, before func(n int, entry JournalEntry) error) (*safetySink, *fileJournal) {
	file := &fileJournal{path: journal}
	return &safetySink{inner: file, before: before}, file
}

func (s *safetySink) record(entry JournalEntry, durable bool) error {
	if s.before != nil {
		if err := s.before(len(s.log)+1, entry); err != nil {
			return err
		}
	}
	s.log = append(s.log, safetyRecord{entry: entry, durable: durable})
	return s.inner.record(entry, durable)
}

// safetyCrash journals op exactly as Apply does and, when change is set,
// performs it, then stops: the state a crash leaves behind.
func safetyCrash(t *testing.T, journal, batch string, seq int, op Operation, change bool) {
	t.Helper()
	prepared, err := prepareOp(op)
	if err != nil {
		t.Fatal(err)
	}
	entry := prepared.entry
	entry.BatchID, entry.Time, entry.Seq = batch, nowStamp(), seq
	if err := AppendJournal(journal, entry); err != nil {
		t.Fatal(err)
	}
	if change {
		if err := prepared.run(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestApplyJournalsEachChangeBeforeTouchingTheDisk(t *testing.T) {
	root := safetyRoot(t)
	plan, from, to := safetyMovePlan(t, root, 3)
	journal := filepath.Join(t.TempDir(), "j.jsonl")

	sink, file := newSafetySink(journal, nil)
	defer file.close()
	sink.before = func(n int, entry JournalEntry) error {
		if entry.Type != "move" {
			return nil
		}
		// Write-ahead: when the entry is recorded the change has not happened.
		i := entry.Seq - 1
		if !safetyExists(from[i]) || safetyExists(to[i]) {
			t.Errorf("entry %d was journaled after its move (src exists=%v dst exists=%v)", entry.Seq, safetyExists(from[i]), safetyExists(to[i]))
		}
		return nil
	}
	result, err := applyBatch(context.Background(), root, sink, plan, nil)
	if err != nil || result.Applied != 3 {
		t.Fatalf("apply: %+v %v", result, err)
	}
	if len(sink.log) != 3 {
		t.Fatalf("want exactly one journal write per change, got %d", len(sink.log))
	}
	for i, rec := range sink.log {
		if rec.entry.Type != "move" || rec.entry.Path != from[i] || rec.entry.NewPath != to[i] {
			t.Fatalf("record %d: %+v", i, rec.entry)
		}
		if rec.entry.Seq != i+1 || rec.entry.BatchID != result.BatchID || rec.entry.Time == "" {
			t.Fatalf("record %d is missing its batch, time or sequence: %+v", i, rec.entry)
		}
		if !rec.durable {
			t.Fatalf("record %d was not flushed before the change", i)
		}
		if !safetyExists(to[i]) {
			t.Fatalf("move %d did not happen", i)
		}
	}
	// The journal on disk is the same three entries.
	if got := safetyEntries(t, journal); len(got) != 3 {
		t.Fatalf("journal holds %d entries, want 3", len(got))
	}
}

func TestApplyStopsWhenJournalWriteFails(t *testing.T) {
	root := safetyRoot(t)
	plan, from, to := safetyMovePlan(t, root, 3)
	journal := filepath.Join(t.TempDir(), "j.jsonl")

	sink, file := newSafetySink(journal, func(n int, entry JournalEntry) error {
		if n == 2 {
			return errors.New("disk full")
		}
		return nil
	})
	defer file.close()
	result, err := applyBatch(context.Background(), root, sink, plan, nil)
	if err == nil || !strings.Contains(err.Error(), "journal") || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("want a clear journal error, got %v", err)
	}
	if result == nil || result.Applied != 1 {
		t.Fatalf("the first change should be reported as applied: %+v", result)
	}
	var sawJournal bool
	for _, e := range result.Errors {
		if strings.HasPrefix(e, "journal: ") {
			sawJournal = true
		}
	}
	if !sawJournal {
		t.Fatalf("result.Errors should carry the journal failure: %v", result.Errors)
	}
	// The change whose entry could not be written never happened, and nothing
	// after it was attempted.
	if !safetyExists(to[0]) || safetyExists(from[0]) {
		t.Fatal("first move should have happened")
	}
	for i := 1; i < 3; i++ {
		if !safetyExists(from[i]) || safetyExists(to[i]) {
			t.Fatalf("move %d ran without a journal entry", i+1)
		}
	}

	// What did happen is undoable.
	undo, err := UndoLast(journal)
	if err != nil || undo.Applied != 1 || len(undo.Errors) != 0 {
		t.Fatalf("undo: %+v %v", undo, err)
	}
	if !safetyExists(from[0]) || safetyExists(to[0]) {
		t.Fatal("undo did not restore the first move")
	}
}

func TestApplyChangesNothingWhenJournalCannotBeOpened(t *testing.T) {
	root := safetyRoot(t)
	plan, from, to := safetyMovePlan(t, root, 2)
	// A journal "inside" a regular file can never be opened.
	blocker := filepath.Join(t.TempDir(), "blocker")
	safetyFile(t, blocker)

	result, err := Apply(context.Background(), root, filepath.Join(blocker, "j.jsonl"), plan)
	if err == nil {
		t.Fatal("expected an error when the journal cannot be written")
	}
	if result == nil || result.Applied != 0 {
		t.Fatalf("nothing should be applied: %+v", result)
	}
	for i := range from {
		if !safetyExists(from[i]) || safetyExists(to[i]) {
			t.Fatalf("move %d happened without a journal", i+1)
		}
	}

	if _, err := Apply(context.Background(), root, "  ", plan); err == nil {
		t.Fatal("expected an error without a journal path")
	}
	for i := range from {
		if !safetyExists(from[i]) {
			t.Fatalf("move %d happened without a journal path", i+1)
		}
	}
}

func TestFailedChangeIsVoidedAndNeverReverted(t *testing.T) {
	root := safetyRoot(t)
	plan, from, to := safetyMovePlan(t, root, 3)
	// The middle move passes every pre-check but cannot create its folder,
	// because a file sits where a directory is needed.
	blocker := filepath.Join(root, "blocker")
	safetyFile(t, blocker)
	plan.Operations[1].NewPath = filepath.Join(blocker, "sub", "f2.txt")
	journal := filepath.Join(t.TempDir(), "j.jsonl")

	result, err := Apply(context.Background(), root, journal, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 2 || result.Skipped != 1 || len(result.Errors) != 1 {
		t.Fatalf("result: %+v", result)
	}
	entries := safetyEntries(t, journal)
	if safetyCount(entries, "move") != 3 || safetyCount(entries, journalTypeAbort) != 1 {
		t.Fatalf("want 3 write-ahead entries and 1 abort marker, got %+v", entries)
	}
	for _, e := range entries {
		if e.Type == journalTypeAbort && (e.Seq != 2 || e.BatchID != result.BatchID || e.Error == "") {
			t.Fatalf("abort marker should void seq 2 and say why: %+v", e)
		}
	}

	batch := LastBatch(entries)
	if len(batch) != 2 {
		t.Fatalf("the failed change must not be part of the batch: %+v", batch)
	}
	for _, e := range batch {
		if e.Seq == 2 {
			t.Fatalf("voided entry leaked into the batch: %+v", e)
		}
	}

	undo, err := UndoLast(journal)
	if err != nil {
		t.Fatal(err)
	}
	if undo.Applied != 2 || undo.Skipped != 0 || len(undo.Errors) != 0 || undo.BatchID != result.BatchID {
		t.Fatalf("undo: %+v", undo)
	}
	for _, i := range []int{0, 2} {
		if !safetyExists(from[i]) || safetyExists(to[i]) {
			t.Fatalf("move %d was not reverted", i+1)
		}
	}
	if !safetyExists(from[1]) {
		t.Fatal("the failed move's file went missing")
	}
}

func TestBatchWithOnlyFailedChangesIsNotUndoable(t *testing.T) {
	root := safetyRoot(t)
	journal := filepath.Join(t.TempDir(), "j.jsonl")

	older, olderFrom, olderTo := safetyMovePlan(t, root, 1)
	if _, err := Apply(context.Background(), root, journal, older); err != nil {
		t.Fatal(err)
	}

	blocker := filepath.Join(root, "blocker")
	safetyFile(t, blocker)
	src := filepath.Join(root, "src", "doomed.txt")
	safetyFile(t, src)
	failing := &Plan{Operations: []Operation{{Type: "move", Path: src, NewPath: filepath.Join(blocker, "x", "doomed.txt")}}}
	res, err := Apply(context.Background(), root, journal, failing)
	if err != nil || res.Applied != 0 || res.Skipped != 1 {
		t.Fatalf("failing batch: %+v %v", res, err)
	}

	// The all-failed batch must not hide the older, real one.
	info, err := LastBatchInfo(journal)
	if err != nil || info == nil || info.Count != 1 || info.BatchID == res.BatchID {
		t.Fatalf("info should describe the older batch: %+v %v", info, err)
	}
	undo, err := UndoLast(journal)
	if err != nil || undo.Applied != 1 || len(undo.Errors) != 0 || undo.BatchID != info.BatchID {
		t.Fatalf("undo: %+v %v", undo, err)
	}
	if !safetyExists(olderFrom[0]) || safetyExists(olderTo[0]) {
		t.Fatal("older batch was not reverted")
	}
}

func TestUndoAfterCrashIsSafeWhetherOrNotTheChangeHappened(t *testing.T) {
	kinds := []struct {
		name string
		// setup returns the operation and whether the original state is back.
		setup func(t *testing.T, root string) (op Operation, restored func() bool)
	}{
		{"move", func(t *testing.T, root string) (Operation, func() bool) {
			src := filepath.Join(root, "old", "a.txt")
			dst := filepath.Join(root, "new", "a.txt")
			safetyFile(t, src)
			return Operation{Type: "move", Path: src, NewPath: dst},
				func() bool { return safetyExists(src) && !safetyExists(dst) }
		}},
		{"rmdir", func(t *testing.T, root string) (Operation, func() bool) {
			dir := filepath.Join(root, "empty")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			return Operation{Type: "rmdir", Path: dir},
				func() bool { return safetyExists(dir) }
		}},
	}
	for _, kind := range kinds {
		for _, changed := range []bool{true, false} {
			name := fmt.Sprintf("%s/change happened=%v", kind.name, changed)
			t.Run(name, func(t *testing.T) {
				root := safetyRoot(t)
				journal := filepath.Join(t.TempDir(), "j.jsonl")
				op, restored := kind.setup(t, root)
				safetyCrash(t, journal, "crashed", 1, op, changed)

				info, err := LastBatchInfo(journal)
				if err != nil || info == nil || info.BatchID != "crashed" || info.Count != 1 {
					t.Fatalf("a crash-left entry must be offered for undo: %+v %v", info, err)
				}
				undo, err := UndoLast(journal)
				if err != nil {
					t.Fatal(err)
				}
				if undo.BatchID != "crashed" || undo.Applied != 1 || undo.Skipped != 0 || len(undo.Errors) != 0 {
					t.Fatalf("undo: %+v", undo)
				}
				if !restored() {
					t.Fatal("state after undo is not the original state")
				}
				if safetyCount(safetyEntries(t, journal), journalTypeUndo) != 1 {
					t.Fatal("batch should be closed by an undo marker")
				}
				if after, _ := LastBatchInfo(journal); after != nil {
					t.Fatalf("nothing should be left to undo: %+v", after)
				}
			})
		}
	}
}

func TestUndoAfterCrashRestoresTagsFromTheEntryAlone(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]string
		op   Operation
		// check reports whether the tags are back to what they were.
		check func(tags map[string][]string) bool
		// changed reports whether the write took effect.
		changed func(tags map[string][]string) bool
	}{
		{
			name: "set over an existing value",
			meta: map[string]string{"title": "T", "albumartist": "Old"},
			op:   Operation{Type: "tags", Set: map[string]string{"ALBUMARTIST": "New"}},
			check: func(tags map[string][]string) bool {
				return firstTag(tags, "ALBUMARTIST") == "Old" && firstTag(tags, "TITLE") == "T"
			},
			changed: func(tags map[string][]string) bool { return firstTag(tags, "ALBUMARTIST") == "New" },
		},
		{
			name: "set a tag the file did not have",
			meta: map[string]string{"title": "T"},
			op:   Operation{Type: "tags", Set: map[string]string{"ALBUMARTIST": "New"}},
			check: func(tags map[string][]string) bool {
				_, has := tags["ALBUMARTIST"]
				return !has && firstTag(tags, "TITLE") == "T"
			},
			changed: func(tags map[string][]string) bool { return firstTag(tags, "ALBUMARTIST") == "New" },
		},
		{
			name: "delete a tag",
			meta: map[string]string{"title": "T", "mood": "keep me"},
			op:   Operation{Type: "tags", Delete: []string{"MOOD"}},
			check: func(tags map[string][]string) bool {
				return firstTag(tags, "MOOD") == "keep me" && firstTag(tags, "TITLE") == "T"
			},
			changed: func(tags map[string][]string) bool { return firstTag(tags, "MOOD") == "" },
		},
	}
	for _, c := range cases {
		for _, changed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/change happened=%v", c.name, changed), func(t *testing.T) {
				root := safetyRoot(t)
				flac := filepath.Join(root, "a.flac")
				makeAudioFile(t, flac, c.meta)
				journal := filepath.Join(t.TempDir(), "j.jsonl")
				op := c.op
				op.Path = flac

				// The entry is written, and the old values read, before the
				// write; after the "crash" the entry alone must be enough.
				safetyCrash(t, journal, "crashed", 1, op, changed)
				now, err := taglib.ReadTags(flac)
				if err != nil {
					t.Fatal(err)
				}
				if got := c.changed(now); got != changed {
					t.Fatalf("precondition: change took effect = %v, want %v (tags %v)", got, changed, now)
				}

				undo, err := UndoLast(journal)
				if err != nil || undo.Applied != 1 || len(undo.Errors) != 0 {
					t.Fatalf("undo: %+v %v", undo, err)
				}
				restored, err := taglib.ReadTags(flac)
				if err != nil {
					t.Fatal(err)
				}
				if !c.check(restored) {
					t.Fatalf("tags not restored: %v", restored)
				}
			})
		}
	}
}

func TestPruneIsJournaledAheadAndNeverRemovesTheRoot(t *testing.T) {
	root := safetyRoot(t)
	src := filepath.Join(root, "a", "b", "f.txt")
	safetyFile(t, src)
	dst := filepath.Join(root, "f.txt")
	journal := filepath.Join(t.TempDir(), "j.jsonl")

	sink, file := newSafetySink(journal, nil)
	defer file.close()
	sink.before = func(n int, entry JournalEntry) error {
		if entry.Type == "rmdir" && !safetyExists(entry.Path) {
			t.Errorf("rmdir %s was journaled after the folder was removed", entry.Path)
		}
		return nil
	}
	plan := &Plan{Operations: []Operation{{Type: "move", Path: src, NewPath: dst}}}
	result, err := applyBatch(context.Background(), root, sink, plan, nil)
	if err != nil || result.Applied != 1 {
		t.Fatalf("apply: %+v %v", result, err)
	}
	if !safetyExists(root) || !safetyExists(dst) {
		t.Fatal("root or the moved file is gone")
	}
	if safetyExists(filepath.Join(root, "a")) {
		t.Fatal("emptied folders should have been removed")
	}
	if len(sink.log) != 3 {
		t.Fatalf("want the move and two folder removals journaled, got %+v", sink.log)
	}
	if sink.log[1].entry.Path != filepath.Join(root, "a", "b") || sink.log[2].entry.Path != filepath.Join(root, "a") {
		t.Fatalf("folders should be removed deepest first: %+v", sink.log)
	}
	for _, rec := range sink.log[1:] {
		if rec.entry.Type != "rmdir" || !rec.durable {
			t.Fatalf("folder removal must be a flushed write-ahead entry: %+v", rec)
		}
	}

	undo, err := UndoLast(journal)
	if err != nil || len(undo.Errors) != 0 || undo.Applied != 3 {
		t.Fatalf("undo: %+v %v", undo, err)
	}
	if !safetyExists(src) {
		t.Fatal("undo did not restore the moved file")
	}
}

func TestApplyRefusesTheLibraryRootItself(t *testing.T) {
	sep := string(os.PathSeparator)
	cases := []struct {
		name string
		ops  func(root string) []Operation
	}{
		{"rmdir of the root", func(root string) []Operation {
			return []Operation{{Type: "rmdir", Path: root}}
		}},
		{"rmdir of the root spelled with a trailing separator", func(root string) []Operation {
			return []Operation{{Type: "rmdir", Path: root + sep}}
		}},
		{"rmdir of the root spelled through a dot", func(root string) []Operation {
			return []Operation{{Type: "rmdir", Path: root + sep + "."}}
		}},
		{"rmdir of the root spelled through a parent hop", func(root string) []Operation {
			return []Operation{{Type: "rmdir", Path: root + sep + "sub" + sep + ".."}}
		}},
		{"tags on the root", func(root string) []Operation {
			return []Operation{{Type: "tags", Path: root, Set: map[string]string{"ALBUMARTIST": "X"}}}
		}},
		{"move of the root", func(root string) []Operation {
			return []Operation{{Type: "move", Path: root, NewPath: filepath.Join(root, "sub", "moved")}}
		}},
		{"move onto the root", func(root string) []Operation {
			return []Operation{{Type: "move", Path: filepath.Join(root, "keep.txt"), NewPath: root}}
		}},
		{"move onto the root spelled through a parent hop", func(root string) []Operation {
			return []Operation{{Type: "move", Path: filepath.Join(root, "keep.txt"), NewPath: root + sep + "sub" + sep + ".."}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := safetyRoot(t)
			safetyFile(t, filepath.Join(root, "keep.txt"))
			if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(t.TempDir(), "j.jsonl")
			ops := c.ops(root)
			result, err := Apply(context.Background(), root, journal, &Plan{Operations: ops})
			if err != nil {
				t.Fatal(err)
			}
			if result.Applied != 0 || result.Skipped != len(ops) || len(result.Errors) != len(ops) {
				t.Fatalf("expected every operation refused, got %+v", result)
			}
			for _, e := range result.Errors {
				if !strings.Contains(e, "library root itself") {
					t.Fatalf("error should say the root was refused, got %q", e)
				}
			}
			if !safetyExists(root) || !safetyExists(filepath.Join(root, "keep.txt")) || !safetyExists(filepath.Join(root, "sub")) {
				t.Fatal("the library was changed")
			}
			if safetyExists(journal) {
				t.Fatal("a refused plan must not touch the journal")
			}
		})
	}
}

func TestApplyRefusesAnEmptyRootFolderEvenWhenNothingIsInsideIt(t *testing.T) {
	// An empty root is exactly what an rmdir of the root would succeed on.
	root := safetyRoot(t)
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	plan := &Plan{Operations: []Operation{{Type: "rmdir", Path: root}}}
	result, err := Apply(context.Background(), root, journal, plan)
	if err != nil || result.Applied != 0 || result.Skipped != 1 {
		t.Fatalf("apply: %+v %v", result, err)
	}
	if !safetyExists(root) {
		t.Fatal("the empty library root was removed")
	}
	// A differently-cased spelling is the same folder on Windows and a foreign
	// path elsewhere; either way it must not be removed.
	upper := strings.ToUpper(root)
	if _, err := Apply(context.Background(), root, journal, &Plan{Operations: []Operation{{Type: "rmdir", Path: upper}}}); err != nil {
		t.Fatal(err)
	}
	if !safetyExists(root) {
		t.Fatal("the library root was removed through another spelling")
	}
}

func TestApplyStillWorksInsideTheRoot(t *testing.T) {
	root := safetyRoot(t)
	empty := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "x.txt")
	safetyFile(t, src)
	dst := filepath.Join(root, "Deep", "er", "x.txt")
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	plan := &Plan{Operations: []Operation{
		{Type: "rmdir", Path: empty},
		{Type: "move", Path: src, NewPath: dst},
		{Type: "move", Path: dst, NewPath: ""}, // no destination
	}}
	result, err := Apply(context.Background(), root, journal, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 2 || result.Skipped != 1 {
		t.Fatalf("result: %+v", result)
	}
	if safetyExists(empty) || !safetyExists(dst) {
		t.Fatal("operations inside the root should apply")
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "no destination") {
		t.Fatalf("a move without a destination needs a clear refusal: %v", result.Errors)
	}
}

func TestApplyCancelStopsBetweenOperationsAndStaysUndoable(t *testing.T) {
	root := safetyRoot(t)
	plan, from, to := safetyMovePlan(t, root, 5)
	journal := filepath.Join(t.TempDir(), "j.jsonl")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress := func(p ApplyProgress) {
		// Cancelling while the third operation is announced: it still runs
		// (never abandoned half way) and the batch stops before the fourth.
		if p.Current == 3 {
			cancel()
		}
	}
	result, err := ApplyWithProgress(ctx, root, journal, plan, progress)
	if err != nil {
		t.Fatalf("a cancelled batch returns its partial result, not an error: %v", err)
	}
	if !result.Cancelled || result.Applied != 3 || result.Skipped != 0 {
		t.Fatalf("result: %+v", result)
	}
	for i := 0; i < 5; i++ {
		moved := i < 3
		if safetyExists(to[i]) != moved || safetyExists(from[i]) == moved {
			t.Fatalf("operation %d: moved=%v, want %v", i+1, safetyExists(to[i]), moved)
		}
	}
	if got := safetyEntries(t, journal); len(got) != 3 {
		t.Fatalf("journal should hold exactly what was done, got %d entries", len(got))
	}
	raw, _ := json.Marshal(result)
	if !strings.Contains(string(raw), `"cancelled":true`) {
		t.Fatalf("result JSON lacks the cancelled flag: %s", raw)
	}

	info, err := LastBatchInfo(journal)
	if err != nil || info == nil || info.Count != 3 || info.BatchID != result.BatchID {
		t.Fatalf("info: %+v %v", info, err)
	}
	undo, err := UndoLast(journal)
	if err != nil || undo.Applied != 3 || len(undo.Errors) != 0 {
		t.Fatalf("undo: %+v %v", undo, err)
	}
	for i := range from {
		if !safetyExists(from[i]) || safetyExists(to[i]) {
			t.Fatalf("operation %d not restored", i+1)
		}
	}
}

func TestApplyCancelledBeforeAnythingRuns(t *testing.T) {
	root := safetyRoot(t)
	plan, from, to := safetyMovePlan(t, root, 2)
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var reports int
	result, err := ApplyWithProgress(ctx, root, journal, plan, func(ApplyProgress) { reports++ })
	if err != nil {
		t.Fatal(err)
	}
	if !result.Cancelled || result.Applied != 0 || reports != 0 {
		t.Fatalf("result: %+v, %d progress reports", result, reports)
	}
	for i := range from {
		if !safetyExists(from[i]) || safetyExists(to[i]) {
			t.Fatal("a cancelled batch changed something")
		}
	}
	if safetyExists(journal) {
		t.Fatal("a batch that did nothing should not create a journal")
	}
	if info, err := LastBatchInfo(journal); info != nil || err != nil {
		t.Fatalf("nothing should be undoable: %+v %v", info, err)
	}
}

func TestApplyProgressReportsEveryOperationInOrder(t *testing.T) {
	root := safetyRoot(t)
	plan, from, to := safetyMovePlan(t, root, 2)
	outside := filepath.Join(t.TempDir(), "elsewhere.txt")
	safetyFile(t, outside)
	// ops: move, planned error, outside the root, move.
	ops := []Operation{
		plan.Operations[0],
		{Type: "move", Path: filepath.Join(root, "src", "ignored.txt"), NewPath: filepath.Join(root, "dst", "ignored.txt"), Error: "cannot be fixed"},
		{Type: "move", Path: outside, NewPath: filepath.Join(root, "dst", "elsewhere.txt")},
		plan.Operations[1],
	}
	journal := filepath.Join(t.TempDir(), "j.jsonl")

	var seen []ApplyProgress
	progress := func(p ApplyProgress) {
		// Each report comes before its own change and after the previous one.
		switch p.Current {
		case 1:
			if !safetyExists(from[0]) || safetyExists(to[0]) {
				t.Errorf("report 1 should precede the first move")
			}
		case 4:
			if !safetyExists(to[0]) || !safetyExists(from[1]) || safetyExists(to[1]) {
				t.Errorf("report 4 should follow the first move and precede the last")
			}
		}
		seen = append(seen, p)
	}
	result, err := ApplyWithProgress(context.Background(), root, journal, &Plan{Operations: ops}, progress)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 2 || result.Skipped != 2 {
		t.Fatalf("result: %+v", result)
	}
	if len(seen) != len(ops) {
		t.Fatalf("want one report per operation (%d), got %d: %+v", len(ops), len(seen), seen)
	}
	for i, p := range seen {
		if p.Current != i+1 || p.Total != len(ops) || p.Path != ops[i].Path {
			t.Fatalf("report %d: %+v", i, p)
		}
	}
	if last := seen[len(seen)-1]; last.Current != last.Total {
		t.Fatalf("the last report must reach the total: %+v", last)
	}
	raw, _ := json.Marshal(seen[0])
	for _, key := range []string{`"current":1`, `"total":4`, `"path":`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("progress JSON %s lacks %s", raw, key)
		}
	}
}

func TestApplyWrapperMatchesApplyWithProgress(t *testing.T) {
	root := safetyRoot(t)
	plan, from, to := safetyMovePlan(t, root, 2)
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	plan.ID = "plan-1"
	result, err := Apply(context.Background(), root, journal, plan)
	if err != nil || result.Applied != 2 || result.Cancelled || result.AbandonedBatchID != "" {
		t.Fatalf("apply: %+v %v", result, err)
	}
	for i := range from {
		if !safetyExists(to[i]) {
			t.Fatalf("move %d missing", i+1)
		}
	}
	if plan.ID != "plan-1" {
		t.Fatal("Apply must carry the plan ID untouched")
	}
}

func TestPlanAndResultJSONShapes(t *testing.T) {
	raw, err := json.Marshal(&Plan{ID: "p-7", Operations: []Operation{}})
	if err != nil || !strings.Contains(string(raw), `"id":"p-7"`) {
		t.Fatalf("plan: %s %v", raw, err)
	}
	var back Plan
	if err := json.Unmarshal(raw, &back); err != nil || back.ID != "p-7" {
		t.Fatalf("plan round trip: %+v %v", back, err)
	}
	raw, _ = json.Marshal(&Plan{})
	if strings.Contains(string(raw), `"id"`) {
		t.Fatalf("empty plan ID must be omitted: %s", raw)
	}
	raw, _ = json.Marshal(&ApplyResult{BatchID: "b", Errors: []string{}})
	if strings.Contains(string(raw), "cancelled") || strings.Contains(string(raw), "abandoned_batch_id") {
		t.Fatalf("unset flags must be omitted: %s", raw)
	}
	raw, _ = json.Marshal(&ApplyResult{AbandonedBatchID: "x", Cancelled: true})
	if !strings.Contains(string(raw), `"abandoned_batch_id":"x"`) || !strings.Contains(string(raw), `"cancelled":true`) {
		t.Fatalf("flags missing: %s", raw)
	}
}

func TestJournalTornLastLineDoesNotSwallowTheNextEntry(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	// A crash mid-write left a fragment with no newline.
	if err := os.WriteFile(journal, []byte(`{"batch_id":"old","type":"move","path":"a","new_path":"b"}`+"\n"+`{"batch_id":"torn","ty`), 0o644); err != nil {
		t.Fatal(err)
	}

	root := safetyRoot(t)
	plan, _, to := safetyMovePlan(t, root, 1)
	result, err := Apply(context.Background(), root, journal, plan)
	if err != nil || result.Applied != 1 {
		t.Fatalf("apply: %+v %v", result, err)
	}
	if !safetyExists(to[0]) {
		t.Fatal("move did not happen")
	}
	entries := safetyEntries(t, journal)
	var found bool
	for _, e := range entries {
		if e.BatchID == result.BatchID && e.Type == "move" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the write-ahead entry was glued to the torn fragment and lost: %+v", entries)
	}

	// The same holds for AppendJournal, which UndoLast uses.
	if err := os.WriteFile(journal, []byte(`{"batch_id":"torn","ty`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendJournal(journal, JournalEntry{BatchID: "next", Type: "undo"}); err != nil {
		t.Fatal(err)
	}
	entries = safetyEntries(t, journal)
	if len(entries) != 1 || entries[0].BatchID != "next" {
		t.Fatalf("AppendJournal lost an entry behind a torn line: %+v", entries)
	}
}

func TestJournalsWithoutSequenceNumbersStillUndo(t *testing.T) {
	// A journal written before write-ahead entries and abort markers existed.
	root := safetyRoot(t)
	src := filepath.Join(root, "old", "a.txt")
	dst := filepath.Join(root, "new", "a.txt")
	safetyFile(t, dst)
	journal := filepath.Join(t.TempDir(), "old.jsonl")
	line := fmt.Sprintf(`{"batch_id":"legacy","time":"2024-05-01T10:00:00Z","type":"move","path":%q,"new_path":%q}`, src, dst)
	if err := os.WriteFile(journal, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := LastBatchInfo(journal)
	if err != nil || info == nil || info.BatchID != "legacy" || info.Count != 1 || info.Time != "2024-05-01T10:00:00Z" {
		t.Fatalf("info: %+v %v", info, err)
	}
	undo, err := UndoLast(journal)
	if err != nil || undo.Applied != 1 || len(undo.Errors) != 0 {
		t.Fatalf("undo: %+v %v", undo, err)
	}
	if !safetyExists(src) || safetyExists(dst) {
		t.Fatal("legacy entry was not reverted")
	}
}

func TestLastBatchHonoursAbortMarkers(t *testing.T) {
	move := func(batch string, seq int) JournalEntry {
		return JournalEntry{BatchID: batch, Type: "move", Path: fmt.Sprintf("%s-%d", batch, seq), NewPath: "n", Seq: seq}
	}
	abort := func(batch string, seq int) JournalEntry {
		return JournalEntry{BatchID: batch, Type: journalTypeAbort, Seq: seq}
	}
	cases := []struct {
		name string
		in   []JournalEntry
		want []string // Paths of the returned batch
	}{
		{"voided entry is dropped",
			[]JournalEntry{move("b", 1), move("b", 2), abort("b", 2)}, []string{"b-1"}},
		{"an abort for another batch does not void it",
			[]JournalEntry{move("b", 1), move("b", 2), abort("c", 2)}, []string{"b-1", "b-2"}},
		{"an abort for another sequence does not void it",
			[]JournalEntry{move("b", 1), move("b", 2), abort("b", 3)}, []string{"b-1", "b-2"}},
		{"an abort without a sequence voids nothing",
			[]JournalEntry{move("b", 0), abort("b", 0)}, []string{"b-0"}},
		{"a fully voided batch reveals the older one",
			[]JournalEntry{move("old", 1), move("new", 1), abort("new", 1)}, []string{"old-1"}},
		{"an abort marker never leaks into the batch",
			[]JournalEntry{move("b", 1), abort("b", 1), move("b", 2)}, []string{"b-2"}},
	}
	for _, c := range cases {
		got := LastBatch(c.in)
		var paths []string
		for _, e := range got {
			paths = append(paths, e.Path)
		}
		if strings.Join(paths, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: got %v, want %v", c.name, paths, c.want)
		}
	}
}
