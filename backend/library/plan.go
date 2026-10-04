package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vekhyat/Auralis/backend"
	"go.senan.xyz/taglib"
)

// Operation is one planned change. It never executes until Apply runs.
type Operation struct {
	Type    string            `json:"type"` // "tags", "move", "rmdir"
	Path    string            `json:"path"`
	NewPath string            `json:"new_path,omitempty"`
	Set     map[string]string `json:"set,omitempty"` // tag key -> new value
	Delete  []string          `json:"delete,omitempty"`
	Reason  string            `json:"reason"`
	Error   string            `json:"error,omitempty"`
	// Size and ModTime capture the file state at scan time; Apply refuses
	// to touch a file that changed since.
	Size    int64 `json:"size"`
	ModTime int64 `json:"mod_time_unix_nano"`
}

// Plan is a reviewable list of operations.
type Plan struct {
	Operations []Operation `json:"operations"`
}

// ApplyResult summarises one batch.
type ApplyResult struct {
	BatchID string   `json:"batch_id"`
	Applied int      `json:"applied"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors"`
}

// Apply executes a plan, writing each applied operation to the journal.
// It refuses to overwrite existing files and to touch files that changed
// since the scan.
func Apply(ctx context.Context, root, journalPath string, plan *Plan) (*ApplyResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("nil plan")
	}
	result := &ApplyResult{BatchID: uuid.NewString(), Errors: []string{}}
	for i := range plan.Operations {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		op := plan.Operations[i]
		if op.Error != "" {
			result.Skipped++
			result.Errors = append(result.Errors, op.Path+": "+op.Error)
			continue
		}
		entry, err := applyOne(op)
		if err != nil {
			result.Skipped++
			result.Errors = append(result.Errors, op.Path+": "+err.Error())
			continue
		}
		entry.BatchID = result.BatchID
		entry.Time = time.Now().UTC().Format(time.RFC3339)
		if err := AppendJournal(journalPath, entry); err != nil {
			result.Errors = append(result.Errors, "journal: "+err.Error())
		}
		result.Applied++
	}
	return result, nil
}

func applyOne(op Operation) (JournalEntry, error) {
	switch op.Type {
	case "tags":
		return applyTags(op)
	case "move":
		return applyMove(op)
	case "rmdir":
		return applyRmdir(op)
	default:
		return JournalEntry{}, fmt.Errorf("unknown operation type %q", op.Type)
	}
}

func unchangedSince(op Operation) error {
	info, err := os.Stat(op.Path)
	if err != nil {
		return fmt.Errorf("cannot stat: %v", err)
	}
	if op.Size != 0 && info.Size() != op.Size {
		return fmt.Errorf("file size changed since scan")
	}
	if op.ModTime != 0 && info.ModTime().UnixNano() != op.ModTime {
		return fmt.Errorf("file changed since scan")
	}
	return nil
}

func applyTags(op Operation) (JournalEntry, error) {
	if err := unchangedSince(op); err != nil {
		return JournalEntry{}, err
	}
	tags, err := taglib.ReadTags(op.Path)
	if err != nil {
		return JournalEntry{}, err
	}
	old := map[string]string{}
	byUpper := map[string]string{}
	for key := range tags {
		byUpper[strings.ToUpper(key)] = key
	}
	for _, key := range append(keysOf(op.Set), op.Delete...) {
		if actual, ok := byUpper[strings.ToUpper(key)]; ok {
			old[key] = firstTag(tags, actual)
		} else {
			old[key] = ""
		}
	}
	for _, key := range op.Delete {
		if actual, ok := byUpper[strings.ToUpper(key)]; ok {
			delete(tags, actual)
		}
	}
	for key, value := range op.Set {
		tags[key] = []string{value}
	}
	opts := taglib.WriteOption(0)
	if len(op.Delete) > 0 {
		// Only Clear removes tags; preserve everything ReadTags returned.
		opts = taglib.Clear
	}
	if err := taglib.WriteTags(op.Path, tags, opts); err != nil {
		return JournalEntry{}, err
	}
	return JournalEntry{Type: "tags", Path: op.Path, Set: op.Set, Delete: op.Delete, Old: old}, nil
}

func applyMove(op Operation) (JournalEntry, error) {
	if err := unchangedSince(op); err != nil {
		return JournalEntry{}, err
	}
	if _, err := os.Stat(op.NewPath); err == nil {
		return JournalEntry{}, fmt.Errorf("destination already exists: %s", op.NewPath)
	}
	if err := os.MkdirAll(filepath.Dir(op.NewPath), 0o755); err != nil {
		return JournalEntry{}, err
	}
	if err := os.Rename(op.Path, op.NewPath); err != nil {
		return JournalEntry{}, err
	}
	// Keep the library index in sync when the file was indexed.
	if err := backend.MoveLibraryIndexFile(op.Path, op.NewPath); err != nil {
		fmt.Printf("Warning: failed to update library index for move: %v\n", err)
	}
	return JournalEntry{Type: "move", Path: op.Path, NewPath: op.NewPath, Size: op.Size, ModTime: op.ModTime}, nil
}

func applyRmdir(op Operation) (JournalEntry, error) {
	entries, err := os.ReadDir(op.Path)
	if err != nil {
		return JournalEntry{}, err
	}
	if len(entries) != 0 {
		return JournalEntry{}, fmt.Errorf("folder is no longer empty")
	}
	if err := os.Remove(op.Path); err != nil {
		return JournalEntry{}, err
	}
	return JournalEntry{Type: "rmdir", Path: op.Path}, nil
}

func firstTag(tags map[string][]string, key string) string {
	for _, value := range tags[key] {
		if value != "" {
			return value
		}
	}
	return ""
}

func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
