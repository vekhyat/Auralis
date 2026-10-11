package library

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// batchInfoStuckMove journals a move that can never be undone: its old
// location lies beneath a regular file, so the folder cannot be recreated.
func batchInfoStuckMove(t *testing.T, journal, batch, root string) {
	t.Helper()
	blocker := filepath.Join(root, "blocker")
	if !safetyExists(blocker) {
		safetyFile(t, blocker)
	}
	moved := filepath.Join(root, "moved", batch+".txt")
	safetyFile(t, moved)
	entry := JournalEntry{BatchID: batch, Time: nowStamp(), Type: "move", Path: filepath.Join(blocker, batch+".txt"), NewPath: moved, Seq: 1}
	if err := AppendJournal(journal, entry); err != nil {
		t.Fatal(err)
	}
}

// batchInfoMove journals a move that undoes cleanly.
func batchInfoMove(t *testing.T, journal, batch, root string) (oldPath string) {
	t.Helper()
	oldPath = filepath.Join(root, "orig", batch+".txt")
	moved := filepath.Join(root, "moved", batch+".txt")
	safetyFile(t, moved)
	entry := JournalEntry{BatchID: batch, Time: nowStamp(), Type: "move", Path: oldPath, NewPath: moved, Seq: 1}
	if err := AppendJournal(journal, entry); err != nil {
		t.Fatal(err)
	}
	return oldPath
}

func TestUndoLastNamesTheAbandonedBatchItSkipped(t *testing.T) {
	root := safetyRoot(t)
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	olderOld := batchInfoMove(t, journal, "older", root)
	batchInfoStuckMove(t, journal, "stuck", root)

	// While it is still retried, the stuck batch is the one asked for and
	// nothing was skipped.
	for attempt := 1; attempt <= maxUndoAttempts; attempt++ {
		info, err := LastBatchInfo(journal)
		if err != nil || info == nil || info.BatchID != "stuck" || info.AbandonedBatchID != "" {
			t.Fatalf("attempt %d info: %+v %v", attempt, info, err)
		}
		res, err := UndoLast(journal)
		if err != nil {
			t.Fatal(err)
		}
		if res.BatchID != "stuck" || res.AbandonedBatchID != "" || len(res.Errors) != 1 {
			t.Fatalf("attempt %d: %+v", attempt, res)
		}
	}

	// Now it is abandoned: the prompt and the undo both fall through to the
	// older batch and both say which batch was skipped.
	info, err := LastBatchInfo(journal)
	if err != nil || info == nil {
		t.Fatalf("info: %+v %v", info, err)
	}
	if info.BatchID != "older" || info.AbandonedBatchID != "stuck" || info.Count != 1 {
		t.Fatalf("info should point at the older batch and name the skipped one: %+v", info)
	}
	res, err := UndoLast(journal)
	if err != nil {
		t.Fatal(err)
	}
	if res.BatchID != "older" || res.AbandonedBatchID != "stuck" || res.Applied != 1 || len(res.Errors) != 0 {
		t.Fatalf("undo of the older batch: %+v", res)
	}
	if !safetyExists(olderOld) {
		t.Fatal("older batch was not restored")
	}
	raw, _ := json.Marshal(res)
	if !strings.Contains(string(raw), `"abandoned_batch_id":"stuck"`) {
		t.Fatalf("result JSON lacks the abandoned batch: %s", raw)
	}

	// Nothing reachable is left, but the stuck batch is still reported so the
	// caller can say why the last change cannot be undone.
	if info, err := LastBatchInfo(journal); info != nil || err != nil {
		t.Fatalf("nothing should be undoable: %+v %v", info, err)
	}
	res, err = UndoLast(journal)
	if err != nil || res.BatchID != "" || res.Applied != 0 || res.AbandonedBatchID != "stuck" {
		t.Fatalf("final undo: %+v %v", res, err)
	}
}

func TestUndoLastReportsOnlyTheNewestAbandonedBatch(t *testing.T) {
	root := safetyRoot(t)
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	batchInfoMove(t, journal, "good", root)
	batchInfoStuckMove(t, journal, "stuck-a", root)
	batchInfoStuckMove(t, journal, "stuck-b", root)
	for i := 0; i < maxUndoAttempts; i++ {
		if err := AppendJournal(journal, JournalEntry{BatchID: "stuck-a", Type: journalTypeUndoPartial, Attempts: i + 1}); err != nil {
			t.Fatal(err)
		}
		if err := AppendJournal(journal, JournalEntry{BatchID: "stuck-b", Type: journalTypeUndoPartial, Attempts: i + 1}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := UndoLast(journal)
	if err != nil {
		t.Fatal(err)
	}
	if res.BatchID != "good" || res.AbandonedBatchID != "stuck-b" {
		t.Fatalf("undo: %+v", res)
	}
}

func TestAbandonedBatchIDEmptyWhenNewestBatchIsHealthy(t *testing.T) {
	root := safetyRoot(t)
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	// An older batch is abandoned, but the newest one is not and goes first.
	batchInfoStuckMove(t, journal, "stuck", root)
	for i := 1; i <= maxUndoAttempts; i++ {
		if err := AppendJournal(journal, JournalEntry{BatchID: "stuck", Type: journalTypeUndoPartial, Attempts: i}); err != nil {
			t.Fatal(err)
		}
	}
	batchInfoMove(t, journal, "newest", root)
	res, err := UndoLast(journal)
	if err != nil || res.BatchID != "newest" || res.AbandonedBatchID != "" || res.Applied != 1 {
		t.Fatalf("undo: %+v %v", res, err)
	}
}

func TestLastBatchInfoMatchesWhatUndoLastReverts(t *testing.T) {
	root := safetyRoot(t)
	journal := filepath.Join(t.TempDir(), "j.jsonl")

	older, olderFrom, _ := safetyMovePlan(t, filepath.Join(root, "a"), 2)
	olderRes, err := Apply(context.Background(), root, journal, older)
	if err != nil || olderRes.Applied != 2 {
		t.Fatalf("older apply: %+v %v", olderRes, err)
	}

	// Seven moves out of one folder that is then emptied (two nested levels),
	// so the batch holds moves and automatic folder removals.
	var ops []Operation
	var srcs []string
	for i := 0; i < 7; i++ {
		src := filepath.Join(root, "b", "deep", "n"+string(rune('a'+i))+".txt")
		safetyFile(t, src)
		srcs = append(srcs, src)
		ops = append(ops, Operation{Type: "move", Path: src, NewPath: filepath.Join(root, "out", "n"+string(rune('a'+i))+".txt")})
	}
	newerRes, err := Apply(context.Background(), root, journal, &Plan{Operations: ops})
	if err != nil || newerRes.Applied != 7 {
		t.Fatalf("newer apply: %+v %v", newerRes, err)
	}

	info, err := LastBatchInfo(journal)
	if err != nil || info == nil {
		t.Fatalf("info: %+v %v", info, err)
	}
	if info.BatchID != newerRes.BatchID {
		t.Fatalf("info names %q, want the newer batch %q", info.BatchID, newerRes.BatchID)
	}
	if info.Types["move"] != 7 || info.Types["rmdir"] != 2 || len(info.Types) != 2 {
		t.Fatalf("types: %v", info.Types)
	}
	if info.Count != 9 {
		t.Fatalf("count: %d", info.Count)
	}
	if len(info.Paths) != 5 {
		t.Fatalf("want 5 sample paths, got %v", info.Paths)
	}
	seen := map[string]bool{}
	for _, p := range info.Paths {
		if seen[p] {
			t.Fatalf("duplicate sample path %q", p)
		}
		seen[p] = true
		if !strings.HasPrefix(p, root) {
			t.Fatalf("sample path outside the library: %q", p)
		}
	}
	if _, err := time.Parse(time.RFC3339, info.Time); err != nil {
		t.Fatalf("time %q: %v", info.Time, err)
	}
	if info.AbandonedBatchID != "" {
		t.Fatalf("nothing was abandoned: %+v", info)
	}

	// UndoLast reverts exactly that.
	undo, err := UndoLast(journal)
	if err != nil {
		t.Fatal(err)
	}
	if undo.BatchID != info.BatchID || undo.Applied+undo.Skipped != info.Count || len(undo.Errors) != 0 {
		t.Fatalf("undo %+v does not match info %+v", undo, info)
	}
	for _, src := range srcs {
		if !safetyExists(src) {
			t.Fatalf("%s was not restored", src)
		}
	}

	// And the next prompt describes the older batch, then nothing.
	info, err = LastBatchInfo(journal)
	if err != nil || info == nil || info.BatchID != olderRes.BatchID || info.Count != 2 || info.Types["move"] != 2 {
		t.Fatalf("older info: %+v %v", info, err)
	}
	if len(info.Paths) != 2 || (info.Paths[0] != olderFrom[0] && info.Paths[0] != olderFrom[1]) {
		t.Fatalf("older paths: %v", info.Paths)
	}
	if undo, err := UndoLast(journal); err != nil || undo.BatchID != olderRes.BatchID || undo.Applied != 2 {
		t.Fatalf("older undo: %+v %v", undo, err)
	}
	if info, err := LastBatchInfo(journal); info != nil || err != nil {
		t.Fatalf("nothing should be left: %+v %v", info, err)
	}
}

func TestLastBatchInfoCountsOnlyOperations(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	write := func(entries ...JournalEntry) {
		t.Helper()
		for _, e := range entries {
			if err := AppendJournal(journal, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(
		// A fully undone older batch must not show up.
		JournalEntry{BatchID: "done", Time: "2024-01-01T00:00:00Z", Type: "move", Path: "d1", NewPath: "d2", Seq: 1},
		JournalEntry{BatchID: "done", Type: journalTypeUndo},
		// The batch that is undoable: two live entries, one voided entry, a
		// failed undo attempt and an abort marker.
		JournalEntry{BatchID: "live", Time: "2024-02-02T10:00:00Z", Type: "tags", Path: "t1", Set: map[string]string{"A": "1"}, Seq: 1},
		JournalEntry{BatchID: "live", Time: "2024-02-02T10:00:01Z", Type: "move", Path: "m1", NewPath: "m2", Seq: 2},
		JournalEntry{BatchID: "live", Time: "2024-02-02T10:00:02Z", Type: "move", Path: "failed", NewPath: "failed2", Seq: 3},
		JournalEntry{BatchID: "live", Type: journalTypeAbort, Seq: 3},
		JournalEntry{BatchID: "live", Type: journalTypeUndoPartial, Attempts: 1},
	)
	info, err := LastBatchInfo(journal)
	if err != nil || info == nil {
		t.Fatalf("info: %+v %v", info, err)
	}
	if info.BatchID != "live" || info.Count != 2 || info.Time != "2024-02-02T10:00:00Z" {
		t.Fatalf("info: %+v", info)
	}
	if len(info.Types) != 2 || info.Types["tags"] != 1 || info.Types["move"] != 1 {
		t.Fatalf("types must hold only operation types: %v", info.Types)
	}
	if strings.Join(info.Paths, ",") != "t1,m1" {
		t.Fatalf("paths: %v", info.Paths)
	}
	raw, _ := json.Marshal(info)
	for _, key := range []string{`"batch_id":"live"`, `"time":`, `"count":2`, `"types":{`, `"paths":[`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("JSON %s lacks %s", raw, key)
		}
	}
	if strings.Contains(string(raw), "abandoned_batch_id") {
		t.Fatalf("unset abandoned id must be omitted: %s", raw)
	}
}

func TestLastBatchInfoNilWhenNothingIsUndoable(t *testing.T) {
	dir := t.TempDir()
	if info, err := LastBatchInfo(filepath.Join(dir, "missing.jsonl")); info != nil || err != nil {
		t.Fatalf("missing journal: %+v %v", info, err)
	}
	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if info, err := LastBatchInfo(empty); info != nil || err != nil {
		t.Fatalf("empty journal: %+v %v", info, err)
	}
	allUndone := filepath.Join(dir, "undone.jsonl")
	for _, e := range []JournalEntry{
		{BatchID: "b", Type: "move", Path: "a", NewPath: "b", Seq: 1},
		{BatchID: "b", Type: journalTypeUndo},
	} {
		if err := AppendJournal(allUndone, e); err != nil {
			t.Fatal(err)
		}
	}
	if info, err := LastBatchInfo(allUndone); info != nil || err != nil {
		t.Fatalf("undone journal: %+v %v", info, err)
	}
	onlyAbandoned := filepath.Join(dir, "abandoned.jsonl")
	if err := AppendJournal(onlyAbandoned, JournalEntry{BatchID: "b", Type: "move", Path: "a", NewPath: "b", Seq: 1}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= maxUndoAttempts; i++ {
		if err := AppendJournal(onlyAbandoned, JournalEntry{BatchID: "b", Type: journalTypeUndoPartial, Attempts: i}); err != nil {
			t.Fatal(err)
		}
	}
	if info, err := LastBatchInfo(onlyAbandoned); info != nil || err != nil {
		t.Fatalf("abandoned-only journal: %+v %v", info, err)
	}
}

func TestLastBatchInfoDoesNotWriteTheJournal(t *testing.T) {
	root := safetyRoot(t)
	journal := filepath.Join(t.TempDir(), "j.jsonl")
	plan, _, _ := safetyMovePlan(t, root, 2)
	if _, err := Apply(context.Background(), root, journal, plan); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if info, err := LastBatchInfo(journal); err != nil || info == nil || info.Count != 2 {
			t.Fatalf("info: %+v %v", info, err)
		}
	}
	after, _ := os.ReadFile(journal)
	if string(before) != string(after) {
		t.Fatal("LastBatchInfo changed the journal")
	}
}
