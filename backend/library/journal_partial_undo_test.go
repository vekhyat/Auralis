package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// partialUndoMove creates a file at newPath and journals a move that, when
// undone, puts it back at oldPath.
func partialUndoMove(t *testing.T, journal, batch, oldPath, newPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendJournal(journal, JournalEntry{BatchID: batch, Type: "move", Path: oldPath, NewPath: newPath}); err != nil {
		t.Fatal(err)
	}
}

// partialUndoBlocker makes a regular file where a folder is needed, so a move
// that must recreate folders beneath it cannot be undone until it is removed.
func partialUndoBlocker(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func partialUndoCount(t *testing.T, journal, batch, entryType string) (count int, last JournalEntry) {
	t.Helper()
	entries, err := ReadJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.BatchID == batch && e.Type == entryType {
			count++
			last = e
		}
	}
	return count, last
}

func partialUndoExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestUndoPartialIsRetriedUntilItSucceeds(t *testing.T) {
	root := t.TempDir()
	journal := filepath.Join(root, "j.jsonl")
	blocker := filepath.Join(root, "blocker")
	partialUndoBlocker(t, blocker)

	okOld := filepath.Join(root, "orig", "a.txt")
	okNew := filepath.Join(root, "moved", "a.txt")
	badOld := filepath.Join(blocker, "b.txt")
	badNew := filepath.Join(root, "moved", "b.txt")
	partialUndoMove(t, journal, "b1", okOld, okNew)
	partialUndoMove(t, journal, "b1", badOld, badNew)

	first, err := UndoLast(journal)
	if err != nil {
		t.Fatal(err)
	}
	if first.BatchID != "b1" || first.Applied != 1 || first.Skipped != 1 || len(first.Errors) != 1 {
		t.Fatalf("first undo: %+v", first)
	}
	if !strings.HasPrefix(first.Errors[0], badOld+": ") {
		t.Fatalf("error should name the failed path, got %q", first.Errors[0])
	}
	if !partialUndoExists(okOld) || partialUndoExists(okNew) {
		t.Fatal("the entry that could be undone was not undone")
	}
	if n, _ := partialUndoCount(t, journal, "b1", "undo"); n != 0 {
		t.Fatal("an undo marker was written although an entry failed")
	}
	n, rec := partialUndoCount(t, journal, "b1", "undo-partial")
	if n != 1 || rec.Attempts != 1 || rec.Error == "" {
		t.Fatalf("want one undo-partial with Attempts=1 and an error, got n=%d %+v", n, rec)
	}
	entries, _ := ReadJournal(journal)
	if batch := LastBatch(entries); len(batch) != 2 || batch[0].BatchID != "b1" {
		t.Fatalf("batch must stay retryable, got %+v", batch)
	}

	// Fix the cause; the retry repeats the entry that already reverted
	// harmlessly and finishes the other.
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	second, err := UndoLast(journal)
	if err != nil {
		t.Fatal(err)
	}
	if second.BatchID != "b1" || len(second.Errors) != 0 || second.Applied != 2 || second.Skipped != 0 {
		t.Fatalf("second undo: %+v", second)
	}
	if !partialUndoExists(badOld) || partialUndoExists(badNew) || !partialUndoExists(okOld) {
		t.Fatal("retry did not restore every file")
	}
	if n, _ := partialUndoCount(t, journal, "b1", "undo"); n != 1 {
		t.Fatalf("want one undo marker after the successful retry, got %d", n)
	}
	entries, _ = ReadJournal(journal)
	if batch := LastBatch(entries); batch != nil {
		t.Fatalf("batch should be closed, got %+v", batch)
	}
	third, err := UndoLast(journal)
	if err != nil || third.BatchID != "" || third.Applied != 0 || len(third.Errors) != 0 {
		t.Fatalf("nothing should be left to undo: %+v %v", third, err)
	}
}

func TestUndoPartialBatchIsAbandonedAfterThreeAttempts(t *testing.T) {
	root := t.TempDir()
	journal := filepath.Join(root, "j.jsonl")
	blocker := filepath.Join(root, "blocker")
	partialUndoBlocker(t, blocker)

	oldOld := filepath.Join(root, "orig", "old.txt")
	oldNew := filepath.Join(root, "moved", "old.txt")
	stuckOld := filepath.Join(blocker, "stuck.txt")
	stuckNew := filepath.Join(root, "moved", "stuck.txt")
	partialUndoMove(t, journal, "old", oldOld, oldNew)
	partialUndoMove(t, journal, "stuck", stuckOld, stuckNew)

	for attempt := 1; attempt <= 3; attempt++ {
		res, err := UndoLast(journal)
		if err != nil {
			t.Fatal(err)
		}
		if res.BatchID != "stuck" || len(res.Errors) != 1 || res.Applied != 0 {
			t.Fatalf("attempt %d: %+v", attempt, res)
		}
		n, rec := partialUndoCount(t, journal, "stuck", "undo-partial")
		if n != attempt || rec.Attempts != attempt {
			t.Fatalf("attempt %d: want %d undo-partial records ending at Attempts=%d, got n=%d Attempts=%d", attempt, attempt, attempt, n, rec.Attempts)
		}
		if partialUndoExists(oldOld) {
			t.Fatalf("attempt %d reached the older batch too early", attempt)
		}
	}

	entries, _ := ReadJournal(journal)
	batch := LastBatch(entries)
	if len(batch) != 1 || batch[0].BatchID != "old" {
		t.Fatalf("after 3 failed attempts the older batch must be reachable, got %+v", batch)
	}

	res, err := UndoLast(journal)
	if err != nil {
		t.Fatal(err)
	}
	if res.BatchID != "old" || res.Applied != 1 || len(res.Errors) != 0 {
		t.Fatalf("undo of the older batch: %+v", res)
	}
	if !partialUndoExists(oldOld) {
		t.Fatal("older batch was not restored")
	}
	if n, _ := partialUndoCount(t, journal, "stuck", "undo"); n != 0 {
		t.Fatal("an abandoned batch must not be marked undone")
	}
	if n, _ := partialUndoCount(t, journal, "stuck", "undo-partial"); n != 3 {
		t.Fatalf("abandoned batch should keep exactly 3 records, got %d", n)
	}

	// Nothing reachable is left; the abandoned batch is not touched again.
	before, _ := ReadJournal(journal)
	res, err = UndoLast(journal)
	if err != nil || res.BatchID != "" || res.Applied != 0 {
		t.Fatalf("final undo: %+v %v", res, err)
	}
	after, _ := ReadJournal(journal)
	if len(after) != len(before) {
		t.Fatalf("journal grew from %d to %d entries", len(before), len(after))
	}
}

func TestUndoPartialFailedTagsEntryKeepsErrorsAndRevertsTheRest(t *testing.T) {
	root := t.TempDir()
	journal := filepath.Join(root, "j.jsonl")
	moveOld := filepath.Join(root, "orig", "a.txt")
	moveNew := filepath.Join(root, "moved", "a.txt")
	missing := filepath.Join(root, "gone.flac") // never created: tag undo cannot read it
	if err := AppendJournal(journal, JournalEntry{
		BatchID: "b1", Type: "tags", Path: missing,
		Set: map[string]string{"ALBUMARTIST": "X"}, Old: map[string][]string{"ALBUMARTIST": nil},
	}); err != nil {
		t.Fatal(err)
	}
	partialUndoMove(t, journal, "b1", moveOld, moveNew)

	for attempt := 1; attempt <= 3; attempt++ {
		res, err := UndoLast(journal)
		if err != nil {
			t.Fatal(err)
		}
		if res.BatchID != "b1" || len(res.Errors) != 1 || res.Skipped != 1 || res.Applied != 1 {
			t.Fatalf("attempt %d: %+v", attempt, res)
		}
		if !strings.HasPrefix(res.Errors[0], missing+": ") {
			t.Fatalf("attempt %d: error should name %s, got %q", attempt, missing, res.Errors[0])
		}
		if !partialUndoExists(moveOld) || partialUndoExists(moveNew) {
			t.Fatalf("attempt %d: move was not (or no longer) reverted", attempt)
		}
	}
	entries, _ := ReadJournal(journal)
	if batch := LastBatch(entries); batch != nil {
		t.Fatalf("batch should be abandoned after 3 attempts, got %+v", batch)
	}
	if res, _ := UndoLast(journal); res.BatchID != "" {
		t.Fatalf("an abandoned batch was undone again: %+v", res)
	}
}

func TestLastBatchHonoursUndoPartialRecords(t *testing.T) {
	ops := []JournalEntry{
		{BatchID: "b1", Type: "move", Path: "a", NewPath: "b"},
		{BatchID: "b2", Type: "move", Path: "c", NewPath: "d"},
	}
	with := func(extra ...JournalEntry) []JournalEntry {
		return append(append([]JournalEntry{}, ops...), extra...)
	}
	partial := func(batch string, attempts int) JournalEntry {
		return JournalEntry{BatchID: batch, Type: "undo-partial", Attempts: attempts}
	}

	cases := []struct {
		name string
		in   []JournalEntry
		want string // BatchID of the returned batch, "" for none
	}{
		{"no markers", with(), "b2"},
		{"one partial keeps it retryable", with(partial("b2", 1)), "b2"},
		{"two partials keep it retryable", with(partial("b2", 1), partial("b2", 2)), "b2"},
		{"three partial records abandon it", with(partial("b2", 1), partial("b2", 2), partial("b2", 3)), "b1"},
		{"records without Attempts still count", with(partial("b2", 0), partial("b2", 0), partial("b2", 0)), "b1"},
		{"an Attempts value of 3 abandons it", with(partial("b2", 3)), "b1"},
		{"a partial on another batch does not affect it", with(partial("b1", 1), partial("b1", 2), partial("b1", 3)), "b2"},
		{"undo wins over earlier partials", with(partial("b2", 1), JournalEntry{BatchID: "b2", Type: "undo"}), "b1"},
		{"both abandoned", with(partial("b2", 3), partial("b1", 3)), ""},
		{"partial without batch is ignored", with(JournalEntry{Type: "undo-partial", Attempts: 3}), "b2"},
	}
	for _, c := range cases {
		batch := LastBatch(c.in)
		got := ""
		if len(batch) > 0 {
			got = batch[0].BatchID
		}
		if got != c.want {
			t.Errorf("%s: LastBatch = %q, want %q", c.name, got, c.want)
		}
		for _, e := range batch {
			if e.Type == "undo" || e.Type == "undo-partial" {
				t.Errorf("%s: marker leaked into the batch: %+v", c.name, e)
			}
		}
	}
}

func TestJournalUndoPartialEncodingAndOldJournals(t *testing.T) {
	line, err := json.Marshal(JournalEntry{BatchID: "b", Type: "undo"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(line), "attempts") {
		t.Fatalf("Attempts must be omitted when zero: %s", line)
	}
	line, err = json.Marshal(JournalEntry{BatchID: "b", Type: "undo-partial", Attempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(line), `"attempts":2`) {
		t.Fatalf("Attempts missing from %s", line)
	}

	// A journal written before undo-partial existed, with a damaged line.
	journal := filepath.Join(t.TempDir(), "old.jsonl")
	old := strings.Join([]string{
		`{"batch_id":"b1","time":"2024-01-01T00:00:00Z","type":"move","path":"a","new_path":"b"}`,
		`{"batch_id":"b2","time":"2024-01-02T00:00:00Z","type":"tags","path":"c","set":{"A":"1"}}`,
		`not json`,
		`{"batch_id":"b2","time":"2024-01-03T00:00:00Z","type":"undo"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(journal, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadJournal(journal)
	if err != nil || len(entries) != 3 {
		t.Fatalf("read old journal: %d entries, %v", len(entries), err)
	}
	if batch := LastBatch(entries); len(batch) != 1 || batch[0].BatchID != "b1" {
		t.Fatalf("old journal tail: %+v", batch)
	}
}
