package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
	// Old holds the tag values currently on disk for the same keys, so the
	// preview can show a real before/after diff. Apply recomputes them at
	// write time for the journal.
	Old    map[string]string `json:"old,omitempty"`
	Delete []string          `json:"delete,omitempty"`
	Reason string            `json:"reason"`
	Error  string            `json:"error,omitempty"`
	// Size and ModTime capture the file state at scan time; Apply refuses
	// to touch a file that changed since.
	Size    int64 `json:"size"`
	ModTime int64 `json:"mod_time_unix_nano"`
}

// Plan is a reviewable list of operations.
type Plan struct {
	// ID names one previewed plan across the preview/apply round trip. The app
	// layer assigns it; this package only carries it.
	ID         string      `json:"id,omitempty"`
	Operations []Operation `json:"operations"`
}

// ApplyResult summarises one batch, or one undo when UndoLast returns it.
type ApplyResult struct {
	BatchID string   `json:"batch_id"`
	Applied int      `json:"applied"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors"`
	// Cancelled is set when ctx ended the batch between operations. Everything
	// applied before that is journaled and can be undone.
	Cancelled bool `json:"cancelled,omitempty"`
	// AbandonedBatchID is set by UndoLast when the newest batch had already
	// failed to undo maxUndoAttempts times and was skipped. BatchID then names
	// the older batch that was reverted instead, or is empty when none is left.
	AbandonedBatchID string `json:"abandoned_batch_id,omitempty"`
}

// ApplyProgress reports where Apply is in a plan. Current counts operations
// started so far, including ones that will be skipped, and the last report
// has Current == Total. A rename whose destination is a file this batch still
// has to move is reported when it actually runs. A swap adds one temporary
// rename, and Total includes that hop.
type ApplyProgress struct {
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Path    string `json:"path"`
}

// Apply executes a plan, writing each applied operation to the journal. It is
// ApplyWithProgress without progress reports.
func Apply(ctx context.Context, root, journalPath string, plan *Plan) (*ApplyResult, error) {
	return ApplyWithProgress(ctx, root, journalPath, plan, nil)
}

// ApplyWithProgress executes a plan inside root and records every change in
// the journal. It refuses to overwrite existing files, to touch files that
// changed since the scan, and to touch the root folder itself or anything
// outside it.
//
// Every change is journaled write-ahead: its entry, holding everything needed
// to undo it, is written and flushed before the disk is touched, and an
// "abort" marker voids the entry if the change then fails. A crash or write
// failure after the change therefore cannot leave it un-undoable (undo is safe
// whether or not the change happened), and a change that failed is never
// reverted. If the journal cannot be written the batch stops with an error
// rather than carrying on unjournaled; what was applied so far stays undoable.
//
// Cancelling ctx stops the batch between operations and returns the partial
// result with Cancelled set and a nil error.
func ApplyWithProgress(ctx context.Context, root, journalPath string, plan *Plan, progress func(ApplyProgress)) (*ApplyResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("nil plan")
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("library root is required")
	}
	if strings.TrimSpace(journalPath) == "" {
		return nil, fmt.Errorf("journal path is required")
	}
	journal := &fileJournal{path: journalPath}
	defer journal.close()
	return applyBatch(ctx, root, journal, plan, progress)
}

// stepOutcome is how one prepared step went.
type stepOutcome struct {
	done       bool  // the change happened and is journaled
	err        error // the change itself failed; nothing was changed
	journalErr error // the journal could not be written, so the batch must stop
}

// batchRun is one Apply: the journal it writes ahead of every change and the
// sequence numbers that let an abort marker name the entry it voids.
type batchRun struct {
	root    string
	journal journalSink
	result  *ApplyResult
	seq     int
}

func applyBatch(ctx context.Context, root string, journal journalSink, plan *Plan, progress func(ApplyProgress)) (*ApplyResult, error) {
	b := &batchRun{root: root, journal: journal, result: &ApplyResult{BatchID: uuid.NewString(), Errors: []string{}}}
	result := b.result
	total := len(plan.Operations)
	// Files retagged in this batch no longer match their scan stamp; the
	// check already passed before that write.
	written := map[string]bool{}
	pruned := map[string]bool{}
	// pending counts move sources this batch has not attempted yet. A move
	// whose destination is one of them waits, so a chain (A→B, B→C) or a swap
	// (A→B, B→A) is not refused with "destination already exists".
	pending := map[string]int{}
	for _, op := range plan.Operations {
		if op.Type == "move" && op.Error == "" && strings.TrimSpace(op.NewPath) != "" && !sameLibraryPath(op.Path, op.NewPath) {
			pending[libraryPathKey(op.Path)]++
		}
	}
	var deferred []Operation
	// reported counts operations actually started, which is also their
	// 1-based progress index. Held-back moves are reported when they run.
	reported := 0
	for i := range plan.Operations {
		if ctx.Err() != nil {
			// Stopping here, between operations, leaves nothing half done, and
			// everything finished so far is already journaled.
			result.Cancelled = true
			return result, nil
		}
		op := plan.Operations[i]
		if moveWaits(op, pending) {
			deferred = append(deferred, op)
			continue
		}
		reported++
		if progress != nil {
			progress(ApplyProgress{Current: reported, Total: total, Path: op.Path})
		}
		if err := b.perform(op, written, pruned, pending); err != nil {
			return b.stop(err)
		}
	}
	if err := b.drainMoves(ctx, deferred, pending, written, pruned, progress, total, &reported); err != nil {
		return b.stop(err)
	}
	return result, nil
}

// moveWaits reports whether op's destination is a file this batch still has
// to move out of the way. The move's own source does not count: a rename onto
// itself is not waiting on another operation.
func moveWaits(op Operation, pending map[string]int) bool {
	if op.Type != "move" || op.Error != "" || strings.TrimSpace(op.NewPath) == "" {
		return false
	}
	if sameLibraryPath(op.Path, op.NewPath) {
		return false
	}
	return pending[libraryPathKey(op.NewPath)] > 0
}

// drainMoves runs moves that were held back, and breaks a cycle by parking
// one file beside itself under a temporary name. The temporary hop is journaled
// like any other move, so undo puts it back.
func (b *batchRun) drainMoves(ctx context.Context, queue []Operation, pending map[string]int, written, pruned map[string]bool, progress func(ApplyProgress), total int, reported *int) error {
	nextTemp := 0
	// stalls counts splits that freed nothing. One split is how a swap makes
	// progress; a run that only manufactures more temporary names is stuck.
	stalls := 0
	for len(queue) > 0 {
		var blocked []Operation
		ran := false
		for _, op := range queue {
			if ctx.Err() != nil {
				b.result.Cancelled = true
				return nil
			}
			if moveWaits(op, pending) {
				blocked = append(blocked, op)
				continue
			}
			*reported++
			if progress != nil {
				progress(ApplyProgress{Current: *reported, Total: total, Path: op.Path})
			}
			if err := b.perform(op, written, pruned, pending); err != nil {
				return err
			}
			ran = true
		}
		if ran {
			queue = blocked
			stalls = 0
			continue
		}
		if len(blocked) == 0 {
			return nil
		}
		stalls++
		if stalls > 8 {
			return b.forceMoves(ctx, queue, pending, written, pruned, progress, total, reported)
		}
		nextTemp++
		hop, next := splitMove(blocked[0], nextTemp, pending, blocked)
		total++ // the temporary hop is an extra operation the progress total must cover
		queue = append([]Operation{hop, next}, blocked[1:]...)
	}
	return nil
}

// forceMoves runs whatever is left without waiting. prepareMove still refuses
// to overwrite a file that did not move, so a stuck cycle is recorded as
// "destination already exists" instead of spinning.
func (b *batchRun) forceMoves(ctx context.Context, queue []Operation, pending map[string]int, written, pruned map[string]bool, progress func(ApplyProgress), total int, reported *int) error {
	forceTotal := *reported + len(queue)
	if total > forceTotal {
		forceTotal = total
	}
	for _, op := range queue {
		if ctx.Err() != nil {
			b.result.Cancelled = true
			return nil
		}
		*reported++
		if progress != nil {
			progress(ApplyProgress{Current: *reported, Total: forceTotal, Path: op.Path})
		}
		if err := b.perform(op, written, pruned, pending); err != nil {
			return err
		}
	}
	return nil
}

// splitMove turns source→dest into source→temp and temp→dest. The second hop
// skips the scan stamp: this batch just placed the file at temp.
func splitMove(op Operation, n int, pending map[string]int, reserved []Operation) (hop, next Operation) {
	temp := tempMovePath(op.Path, n, pending, reserved)
	hop = op
	hop.NewPath = temp
	next = op
	next.Path = temp
	next.Size, next.ModTime = 0, 0
	return hop, next
}

func tempMovePath(src string, n int, pending map[string]int, reserved []Operation) string {
	dir := filepath.Dir(src)
	ext := filepath.Ext(src)
	base := strings.TrimSuffix(filepath.Base(src), ext)
	for {
		candidate := filepath.Join(dir, fmt.Sprintf(".auralis-rename-%d-%s%s", n, base, ext))
		n++
		if pathReserved(candidate, pending, reserved) {
			continue
		}
		return candidate
	}
}

func pathReserved(candidate string, pending map[string]int, reserved []Operation) bool {
	if pending[libraryPathKey(candidate)] > 0 || fileExists(candidate) {
		return true
	}
	for _, op := range reserved {
		if sameLibraryPath(op.Path, candidate) || sameLibraryPath(op.NewPath, candidate) {
			return true
		}
	}
	return false
}

// perform runs one operation. A journal failure is returned so the batch
// stops; a failure of the change itself is recorded on the result and the
// batch continues. pending is updated for moves that were actually attempted.
func (b *batchRun) perform(op Operation, written, pruned map[string]bool, pending map[string]int) error {
	if op.Type == "move" {
		defer b.releaseMove(op, pending)
	}
	result := b.result
	skip := func(path, reason string) {
		result.Skipped++
		result.Errors = append(result.Errors, path+": "+reason)
	}
	if op.Error != "" {
		skip(op.Path, op.Error)
		return nil
	}
	// Plans round-trip through the UI, so re-check that every path they
	// touch is still inside the scanned library.
	if reason := refusePaths(b.root, op); reason != "" {
		skip(op.Path, reason)
		return nil
	}
	if written[op.Path] {
		op.Size, op.ModTime = 0, 0
	}
	if op.Type == "rmdir" && pruned[filepath.Clean(op.Path)] {
		return nil // a move already emptied and removed it
	}
	prepared, err := prepareOp(op)
	if err != nil {
		skip(op.Path, err.Error())
		return nil
	}
	outcome := b.run(prepared)
	if outcome.err != nil {
		skip(op.Path, outcome.err.Error())
	}
	if outcome.done {
		result.Applied++
		if op.Type == "tags" {
			written[op.Path] = true
		}
	}
	if outcome.journalErr != nil {
		return outcome.journalErr
	}
	if !outcome.done || op.Type != "move" {
		return nil
	}
	// A move can leave its old folder empty; remove those folders now,
	// journaled like any other step, rather than waiting for a rescan.
	return b.pruneEmptyParents(filepath.Dir(op.Path), pruned)
}

func (b *batchRun) releaseMove(op Operation, pending map[string]int) {
	key := libraryPathKey(op.Path)
	if pending[key] == 0 {
		return
	}
	pending[key]--
	if pending[key] == 0 {
		delete(pending, key)
	}
}

// libraryPathKey identifies a path for "is this the same file?" checks.
// Windows and macOS volumes are case-insensitive, so a later move onto a
// differently-cased path is still the same file. Other systems keep case.
func libraryPathKey(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return foldKey(clean)
	}
	return clean
}

func sameLibraryPath(a, b string) bool {
	return libraryPathKey(a) == libraryPathKey(b)
}

// run executes one prepared step under the write-ahead protocol.
func (b *batchRun) run(prepared step) stepOutcome {
	b.seq++
	entry := prepared.entry
	entry.BatchID, entry.Time, entry.Seq = b.result.BatchID, nowStamp(), b.seq
	// Flushed before the disk is touched: if this fails nothing has changed.
	if err := b.journal.record(entry, true); err != nil {
		return stepOutcome{journalErr: err}
	}
	if err := prepared.run(); err != nil {
		// The change did not happen; void its entry so undo never acts on it
		// (and a revert that cannot work does not hold up the whole batch).
		void := JournalEntry{BatchID: entry.BatchID, Time: nowStamp(), Type: journalTypeAbort, Seq: entry.Seq, Error: err.Error()}
		return stepOutcome{err: err, journalErr: b.journal.record(void, false)}
	}
	return stepOutcome{done: true}
}

// stop ends the batch because the journal can no longer be written. Whatever
// was applied was journaled first, so it stays undoable.
func (b *batchRun) stop(cause error) (*ApplyResult, error) {
	b.result.Errors = append(b.result.Errors, "journal: "+cause.Error())
	if b.result.Applied == 0 {
		return b.result, fmt.Errorf("could not write the fix journal, so the batch was stopped: %w", cause)
	}
	return b.result, fmt.Errorf("could not write the fix journal, so the batch was stopped after %d change(s), which can still be undone: %w", b.result.Applied, cause)
}

// pruneEmptyParents removes dir and each ancestor that becomes empty, stopping
// at the library root, which it never removes. Each removal is journaled write-
// ahead like any other step. Removed folders are recorded in pruned.
func (b *batchRun) pruneEmptyParents(dir string, pruned map[string]bool) error {
	for dir = filepath.Clean(dir); strictlyWithinRoot(b.root, dir); dir = filepath.Dir(dir) {
		if !dirIsEmpty(dir) {
			break
		}
		target := dir
		outcome := b.run(step{
			entry: JournalEntry{Type: "rmdir", Path: target},
			run:   func() error { return os.Remove(target) },
		})
		if outcome.journalErr != nil {
			return outcome.journalErr
		}
		if !outcome.done {
			break
		}
		pruned[target] = true
	}
	return nil
}

// step is one operation ready to run. entry is its journal record, complete
// before run touches the disk: for tags that means the old values were read
// first, so the record alone is enough to undo the change.
type step struct {
	entry JournalEntry
	run   func() error
}

// prepareOp does every read-only check an operation needs and returns the step
// that performs it. Nothing on disk changes until the step's run is called.
func prepareOp(op Operation) (step, error) {
	switch op.Type {
	case "tags":
		return prepareTags(op)
	case "move":
		return prepareMove(op)
	case "rmdir":
		return prepareRmdir(op)
	default:
		return step{}, fmt.Errorf("unknown operation type %q", op.Type)
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

func prepareTags(op Operation) (step, error) {
	if err := unchangedSince(op); err != nil {
		return step{}, err
	}
	tags, err := taglib.ReadTags(op.Path)
	if err != nil {
		return step{}, err
	}
	old := map[string][]string{}
	byUpper := map[string]string{}
	for key := range tags {
		byUpper[strings.ToUpper(key)] = key
	}
	for _, key := range append(keysOf(op.Set), op.Delete...) {
		if actual, ok := byUpper[strings.ToUpper(key)]; ok {
			old[key] = append([]string(nil), tags[actual]...)
		} else {
			old[key] = nil
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
	return step{
		entry: JournalEntry{Type: "tags", Path: op.Path, Set: op.Set, Delete: op.Delete, Old: old},
		run:   func() error { return taglib.WriteTags(op.Path, tags, opts) },
	}, nil
}

func prepareMove(op Operation) (step, error) {
	if op.NewPath == "" {
		return step{}, fmt.Errorf("move has no destination")
	}
	if err := unchangedSince(op); err != nil {
		return step{}, err
	}
	if _, err := os.Stat(op.NewPath); err == nil {
		return step{}, fmt.Errorf("destination already exists: %s", op.NewPath)
	}
	return step{
		entry: JournalEntry{Type: "move", Path: op.Path, NewPath: op.NewPath, Size: op.Size, ModTime: op.ModTime},
		run: func() error {
			if err := os.MkdirAll(filepath.Dir(op.NewPath), 0o755); err != nil {
				return err
			}
			if err := os.Rename(op.Path, op.NewPath); err != nil {
				return err
			}
			moveSidecars(op.Path, op.NewPath)
			// The index is a cache of what is on disk, so a failed update is
			// not worth failing the move for; the next index refresh corrects it.
			_ = backend.MoveLibraryIndexFile(op.Path, op.NewPath)
			return nil
		},
	}, nil
}

func prepareRmdir(op Operation) (step, error) {
	entries, err := os.ReadDir(op.Path)
	if err != nil {
		return step{}, err
	}
	if len(entries) != 0 {
		return step{}, fmt.Errorf("folder is no longer empty")
	}
	return step{
		entry: JournalEntry{Type: "rmdir", Path: op.Path},
		run:   func() error { return os.Remove(op.Path) },
	}, nil
}

// sidecarExtensions are files that belong to one track and must follow it.
var sidecarExtensions = []string{".lrc"}

// moveSidecars moves e.g. "Song.lrc" next to the moved "Song.flac". It never
// overwrites an existing file.
func moveSidecars(oldAudio, newAudio string) {
	oldBase := strings.TrimSuffix(oldAudio, filepath.Ext(oldAudio))
	newBase := strings.TrimSuffix(newAudio, filepath.Ext(newAudio))
	for _, ext := range sidecarExtensions {
		if !fileExists(oldBase+ext) || fileExists(newBase+ext) {
			continue
		}
		_ = os.Rename(oldBase+ext, newBase+ext)
	}
}

// refusePaths returns why op may not run, or "" when every path it touches is
// strictly inside root. The root folder itself is never a valid target: a
// crafted rmdir or move of it would take the whole library with it.
func refusePaths(root string, op Operation) string {
	if strings.TrimSpace(op.Path) == "" {
		return "operation has no path"
	}
	switch {
	case !withinRoot(root, op.Path):
		return "path is outside the library root"
	case isRoot(root, op.Path):
		return "path is the library root itself"
	}
	switch {
	case op.NewPath == "":
	case !withinRoot(root, op.NewPath):
		return "destination is outside the library root"
	case isRoot(root, op.NewPath):
		return "destination is the library root itself"
	}
	return ""
}

// withinRoot reports whether path is root itself or inside it.
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// isRoot reports whether path is the root folder itself.
func isRoot(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel == "."
}

// strictlyWithinRoot reports whether path is inside root and not root itself.
// Apply uses this for every path it changes.
func strictlyWithinRoot(root, path string) bool {
	return withinRoot(root, path) && !isRoot(root, path)
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
