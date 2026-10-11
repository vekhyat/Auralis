package library

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vekhyat/Auralis/backend"
	"go.senan.xyz/taglib"
)

// Batch markers (operations use "tags", "move", "rmdir"). UndoLast writes the
// undo pair; Apply writes abort when a journaled change then failed to happen.
const (
	journalTypeUndo        = "undo"         // every entry of the batch was reverted
	journalTypeUndoPartial = "undo-partial" // an undo attempt left some entries failing
	journalTypeAbort       = "abort"        // the entry with the same Seq never took effect
)

// maxUndoAttempts is how many undo-partial records a batch may collect before
// LastBatch stops offering it, so one batch that can never be reverted does
// not hide every older batch.
const maxUndoAttempts = 3

// JournalEntry is one line of the append-only fix journal.
type JournalEntry struct {
	BatchID string              `json:"batch_id"`
	Time    string              `json:"time"`
	Type    string              `json:"type"` // "tags", "move", "rmdir", "undo", "undo-partial", "abort"
	Path    string              `json:"path,omitempty"`
	NewPath string              `json:"new_path,omitempty"`
	Set     map[string]string   `json:"set,omitempty"`
	Delete  []string            `json:"delete,omitempty"`
	Old     map[string][]string `json:"old,omitempty"`
	Size    int64               `json:"size,omitempty"`
	ModTime int64               `json:"mod_time_unix_nano,omitempty"`
	Error   string              `json:"error,omitempty"`
	// Attempts is the ordinal of the failed undo attempt an "undo-partial"
	// record stands for (1 for the first). Zero, and omitted, for other types.
	Attempts int `json:"attempts,omitempty"`
	// Seq numbers the operations of a batch from 1, so an "abort" marker can
	// name the entry it voids. Apply journals each operation before changing
	// the disk, so an entry may describe a change that a crash or a failure
	// stopped; undo is safe either way. Absent from older journals.
	Seq int `json:"seq,omitempty"`
}

// BatchInfo describes the batch UndoLast would revert next, so a confirmation
// prompt can say what is about to happen.
type BatchInfo struct {
	BatchID string         `json:"batch_id"`
	Time    string         `json:"time"`  // when the batch was applied
	Count   int            `json:"count"` // operations UndoLast would revert
	Types   map[string]int `json:"types"` // count per operation type
	Paths   []string       `json:"paths"` // up to batchInfoSamplePaths sample paths
	// AbandonedBatchID is a newer batch that was skipped because it kept
	// failing to undo; see ApplyResult.AbandonedBatchID.
	AbandonedBatchID string `json:"abandoned_batch_id,omitempty"`
}

// batchInfoSamplePaths bounds BatchInfo.Paths.
const batchInfoSamplePaths = 5

func nowStamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// AppendJournal adds one entry to the JSONL journal, creating it if needed.
func AppendJournal(journalPath string, entry JournalEntry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := openJournalAppend(journalPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// openJournalAppend opens the journal for appending, creating it if needed. A
// crash can leave its last line without a newline; the next record would then
// be glued to that fragment and unreadable, so the fragment is ended first.
func openJournalAppend(journalPath string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(journalPath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], info.Size()-1); err == nil && last[0] != '\n' {
			if _, err := f.Write([]byte{'\n'}); err != nil {
				f.Close()
				return nil, err
			}
		}
	}
	return f, nil
}

// journalSink receives the records Apply writes. fileJournal is the real one;
// tests substitute sinks that watch or fail records.
type journalSink interface {
	// record appends one entry. durable asks for it to be flushed to the disk
	// before returning, which a write-ahead entry needs and an abort marker
	// does not.
	record(entry JournalEntry, durable bool) error
}

// fileJournal appends to the JSONL journal over one handle for a whole batch,
// opened on the first record so a batch that changes nothing leaves no file.
type fileJournal struct {
	path string
	f    *os.File
}

func (j *fileJournal) record(entry JournalEntry, durable bool) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if j.f == nil {
		f, err := openJournalAppend(j.path)
		if err != nil {
			return err
		}
		j.f = f
	}
	if _, err := j.f.Write(append(line, '\n')); err != nil {
		return err
	}
	if durable {
		// Best effort: a filesystem that cannot flush still holds the entry in
		// the OS cache, which survives a crash of this process, and failing
		// the whole batch over it would be worse than the old behaviour.
		_ = j.f.Sync()
	}
	return nil
}

func (j *fileJournal) close() {
	if j.f != nil {
		j.f.Close()
		j.f = nil
	}
}

// ReadJournal parses the journal, skipping damaged lines.
func ReadJournal(journalPath string) ([]JournalEntry, error) {
	f, err := os.Open(journalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var entries []JournalEntry
	scanner := bufio.NewScanner(f)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry JournalEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

// markerType reports whether a journal type is a marker rather than an
// operation to revert.
func markerType(entryType string) bool {
	switch entryType {
	case journalTypeUndo, journalTypeUndoPartial, journalTypeAbort:
		return true
	}
	return false
}

// undoAttempts returns, per batch, how many undo attempts have failed. It is
// the larger of the number of undo-partial records and the highest Attempts
// value any of them carries.
func undoAttempts(entries []JournalEntry) map[string]int {
	records := map[string]int{}
	highest := map[string]int{}
	for _, entry := range entries {
		if entry.Type != journalTypeUndoPartial || entry.BatchID == "" {
			continue
		}
		records[entry.BatchID]++
		if entry.Attempts > highest[entry.BatchID] {
			highest[entry.BatchID] = entry.Attempts
		}
	}
	for batch, n := range highest {
		if n > records[batch] {
			records[batch] = n
		}
	}
	return records
}

// LastBatch returns the operations of the most recently applied batch that
// has not been undone. A batch whose undo failed in part stays eligible, so
// the failed entries can be retried, until it has failed maxUndoAttempts
// times; after that it is skipped so older batches are reachable.
//
// Operations whose change failed (their entry was voided by an abort marker)
// are not part of a batch. An entry left by a crash is, since the change may
// have happened.
func LastBatch(entries []JournalEntry) []JournalEntry {
	batch, _ := selectBatch(entries)
	return batch
}

// selectBatch picks the batch UndoLast reverts, and reports the newest batch
// that was skipped because it had been abandoned (empty when none was). It is
// the one place that decides, so UndoLast and LastBatchInfo cannot disagree.
func selectBatch(entries []JournalEntry) (batch []JournalEntry, abandoned string) {
	type opKey struct {
		batch string
		seq   int
	}
	undone := map[string]bool{}
	aborted := map[opKey]bool{}
	for _, entry := range entries {
		if entry.BatchID == "" {
			continue
		}
		switch entry.Type {
		case journalTypeUndo:
			undone[entry.BatchID] = true
		case journalTypeAbort:
			aborted[opKey{entry.BatchID, entry.Seq}] = true
		}
	}
	attempts := undoAttempts(entries)
	byBatch := map[string][]JournalEntry{}
	var order []string
	for _, entry := range entries {
		if markerType(entry.Type) || entry.BatchID == "" || undone[entry.BatchID] {
			continue
		}
		if entry.Seq > 0 && aborted[opKey{entry.BatchID, entry.Seq}] {
			continue // the change failed, so there is nothing to revert
		}
		if _, ok := byBatch[entry.BatchID]; !ok {
			order = append(order, entry.BatchID)
		}
		byBatch[entry.BatchID] = append(byBatch[entry.BatchID], entry)
	}
	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		if attempts[id] >= maxUndoAttempts {
			if abandoned == "" {
				abandoned = id
			}
			continue
		}
		return byBatch[id], abandoned
	}
	return nil, abandoned
}

// LastBatchInfo describes the batch UndoLast would revert, for a confirmation
// prompt. It returns nil, nil when nothing can be undone. It selects the batch
// exactly as UndoLast does, abandonment rule included, and counts only the
// operations UndoLast would revert, never voided entries or markers.
func LastBatchInfo(journalPath string) (*BatchInfo, error) {
	entries, err := ReadJournal(journalPath)
	if err != nil {
		return nil, err
	}
	batch, abandoned := selectBatch(entries)
	if len(batch) == 0 {
		return nil, nil
	}
	info := &BatchInfo{
		BatchID:          batch[0].BatchID,
		Time:             batch[0].Time,
		Count:            len(batch),
		Types:            map[string]int{},
		Paths:            []string{},
		AbandonedBatchID: abandoned,
	}
	seen := map[string]bool{}
	for _, entry := range batch {
		info.Types[entry.Type]++
		if entry.Path == "" || seen[entry.Path] || len(info.Paths) >= batchInfoSamplePaths {
			continue
		}
		seen[entry.Path] = true
		info.Paths = append(info.Paths, entry.Path)
	}
	return info, nil
}

// UndoLast reverts the most recent applied batch.
//
// When every entry reverted, an "undo" marker closes the batch. When some
// failed, the failures are returned in ApplyResult.Errors and an
// "undo-partial" record is appended instead: the batch stays the last one, so
// the next call retries it (entries that already reverted are harmless to
// repeat), until it has failed maxUndoAttempts times. A batch abandoned that
// way is skipped and named in ApplyResult.AbandonedBatchID, so the caller can
// tell the user that an older batch was reverted instead.
//
// An entry written ahead of a change that a crash interrupted is reverted like
// any other: undo is safe whether or not the change happened.
func UndoLast(journalPath string) (*ApplyResult, error) {
	entries, err := ReadJournal(journalPath)
	if err != nil {
		return nil, err
	}
	batch, abandoned := selectBatch(entries)
	result := &ApplyResult{Errors: []string{}, AbandonedBatchID: abandoned}
	if len(batch) == 0 {
		return result, nil
	}
	result.BatchID = batch[0].BatchID
	for i := len(batch) - 1; i >= 0; i-- {
		entry := batch[i]
		if err := undoOne(entry); err != nil {
			result.Errors = append(result.Errors, entry.Path+": "+err.Error())
			result.Skipped++
			continue
		}
		result.Applied++
	}
	now := nowStamp()
	marker := JournalEntry{Type: journalTypeUndo, BatchID: result.BatchID, Time: now}
	if len(result.Errors) > 0 {
		marker = JournalEntry{
			Type:     journalTypeUndoPartial,
			BatchID:  result.BatchID,
			Time:     now,
			Attempts: undoAttempts(entries)[result.BatchID] + 1,
			Error:    fmt.Sprintf("%d of %d undo steps failed; first: %s", len(result.Errors), len(batch), result.Errors[0]),
		}
	}
	if err := AppendJournal(journalPath, marker); err != nil {
		return result, err
	}
	return result, nil
}

func undoOne(entry JournalEntry) error {
	switch entry.Type {
	case "tags":
		tags, err := taglib.ReadTags(entry.Path)
		if err != nil {
			return err
		}
		byUpper := map[string]string{}
		for key := range tags {
			byUpper[strings.ToUpper(key)] = key
		}
		removes := false
		for key, old := range entry.Old {
			actual, ok := byUpper[strings.ToUpper(key)]
			if len(old) == 0 {
				if ok {
					delete(tags, actual)
					removes = true
				}
				continue
			}
			if ok {
				tags[actual] = old
			} else {
				tags[key] = old
			}
		}
		opts := taglib.WriteOption(0)
		if removes {
			opts = taglib.Clear
		}
		return taglib.WriteTags(entry.Path, tags, opts)
	case "move":
		if _, err := os.Stat(entry.NewPath); err != nil {
			return nil // already gone
		}
		if _, err := os.Stat(entry.Path); err == nil {
			return nil // something is already at the old location
		}
		if err := os.MkdirAll(filepath.Dir(entry.Path), 0o755); err != nil {
			return err
		}
		if err := os.Rename(entry.NewPath, entry.Path); err != nil {
			return err
		}
		moveSidecars(entry.NewPath, entry.Path)
		if err := backend.MoveLibraryIndexFile(entry.NewPath, entry.Path); err != nil {
			// Non-fatal: index is a cache of what the files say.
			_ = err
		}
		return nil
	case "rmdir":
		// Cannot restore an empty folder safely; just recreate it.
		return os.MkdirAll(entry.Path, 0o755)
	default:
		return nil
	}
}
