package library

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vekhyat/Auralis/backend"
	"go.senan.xyz/taglib"
)

// JournalEntry is one line of the append-only fix journal.
type JournalEntry struct {
	BatchID string            `json:"batch_id"`
	Time    string            `json:"time"`
	Type    string            `json:"type"` // "tags", "move", "rmdir", "undo"
	Path    string            `json:"path,omitempty"`
	NewPath string            `json:"new_path,omitempty"`
	Set     map[string]string `json:"set,omitempty"`
	Delete  []string          `json:"delete,omitempty"`
	Old     map[string][]string `json:"old,omitempty"`
	Size    int64             `json:"size,omitempty"`
	ModTime int64             `json:"mod_time_unix_nano,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// AppendJournal adds one entry to the JSONL journal, creating it if needed.
func AppendJournal(journalPath string, entry JournalEntry) error {
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(journalPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
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

// LastBatch returns the operations of the most recently applied batch that
// has not been undone.
func LastBatch(entries []JournalEntry) []JournalEntry {
	undone := map[string]bool{}
	for _, entry := range entries {
		if entry.Type == "undo" && entry.BatchID != "" {
			undone[entry.BatchID] = true
		}
	}
	byBatch := map[string][]JournalEntry{}
	var order []string
	for _, entry := range entries {
		if entry.Type == "undo" || entry.BatchID == "" {
			continue
		}
		if undone[entry.BatchID] {
			continue
		}
		if _, ok := byBatch[entry.BatchID]; !ok {
			order = append(order, entry.BatchID)
		}
		byBatch[entry.BatchID] = append(byBatch[entry.BatchID], entry)
	}
	if len(order) == 0 {
		return nil
	}
	return byBatch[order[len(order)-1]]
}

// UndoLast reverts the most recent applied batch and appends an undo marker.
func UndoLast(journalPath string) (*ApplyResult, error) {
	entries, err := ReadJournal(journalPath)
	if err != nil {
		return nil, err
	}
	batch := LastBatch(entries)
	result := &ApplyResult{Errors: []string{}}
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
	marker := JournalEntry{Type: "undo", BatchID: result.BatchID, Time: time.Now().UTC().Format(time.RFC3339)}
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
