package syncengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/vekhyat/Auralis/backend/library"
)

// Progress reports executor progress.
type Progress struct {
	Phase   string `json:"phase"` // "copy", "move", "delete", "skipped", "progress", "manifest", "done"
	OpIndex int    `json:"op_index,omitempty"`
	OpTotal int    `json:"op_total"`
	Name    string `json:"name"`
	Done    int    `json:"done"` // completed ops so far
	Bytes   int64  `json:"bytes"`
	// Skipped counts files left alone because an unmanaged file was there.
	Skipped int `json:"skipped,omitempty"`
	// Error is set on the final "error" event.
	Error string `json:"error,omitempty"`
}

// ProgressFunc receives progress updates.
type ProgressFunc func(Progress)

// Executor runs a Plan against a SyncTarget with a resumable journal.
type Executor struct {
	Target         SyncTarget
	Root           string // music root on the device
	Profile        library.Profile
	ProfileVersion int
	Policy         FormatPolicy
	// JournalPath persists per-operation results so a restart resumes.
	JournalPath string
	// Concurrency bounds parallel byte-copying operations (1 = serial).
	Concurrency int
	// HardDelete removes files outright. Default moves them to trash.
	HardDelete bool
	// Materialize produces the local file to upload for a track (copy or
	// transcoded cache output). Overridable for tests.
	Materialize func(ctx context.Context, tr *SourceTrack) (localPath string, err error)
	OnProgress  ProgressFunc
	now         func() time.Time

	mu sync.Mutex // guards manifest, journal, skipped
	// Skipped lists remote paths left alone because a file Auralis does not
	// manage already exists there. Populated by Run.
	Skipped []string
}

// --- journal ---
//
// The journal is append-only JSONL: a header naming the plan, then one line
// per finished operation with its effect on the manifest. Appending keeps
// each completion O(1); replaying the effects on resume rebuilds the
// manifest even though the device copy is only written at the end.

type journalHeader struct {
	PlanHash string `json:"plan_hash"`
}

type journalRecord struct {
	Index   int            `json:"index"`
	Set     *ManifestEntry `json:"set,omitempty"`
	Remove  string         `json:"remove,omitempty"`
	Skipped string         `json:"skipped,omitempty"`
}

type journal struct {
	mu   sync.Mutex
	file *os.File
	done map[int]bool
}

func (j *journal) isDone(i int) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done[i]
}

func (j *journal) markDone(i int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.done[i] = true
}

func (j *journal) count() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.done)
}

// ErrStalePreview is returned when a source file changed after the plan
// was previewed (size, mtime, or content hash no longer matches the
// snapshotted track). The preview no longer describes the library, so the
// caller must re-preview instead of syncing stale bytes.
var ErrStalePreview = errors.New("source changed since preview")

// ErrInsufficientSpace is returned by Run when the remaining work needs
// more bytes than the device reports free. The check runs after journal
// replay against unfinished operations only, so a resume never refuses
// work that already fit, and FreeSpace errors propagate instead of being
// swallowed: every transport implements FreeSpace, and the preview already
// surfaces its failures before planning.
var ErrInsufficientSpace = errors.New("insufficient free space on device")

// manifestFlushInterval bounds how often Run re-uploads the device manifest
// while operations complete.
const manifestFlushInterval = 15 * time.Second

// openJournal resumes the journal for planHash, replaying recorded effects
// onto manifest, or starts a fresh one.
//
// A torn or corrupt line never aborts the replay: it is dropped while later
// valid records still apply, and the file is rebuilt from the intact lines
// (or the damaged tail truncated) before opening for append, so new records
// always land on clean lines and replay on the next resume.
func (e *Executor) openJournal(planHash string, manifest *Manifest) (*journal, error) {
	j := &journal{done: map[int]bool{}}
	if e.JournalPath == "" {
		return j, nil
	}
	if data, err := os.ReadFile(e.JournalPath); err == nil {
		matches := false
		validEnd := 0
		dirty := false
		var valid [][]byte
		lines := bytes.Split(data, []byte{'\n'})
		if len(lines) > 0 {
			var header journalHeader
			if json.Unmarshal(lines[0], &header) == nil && header.PlanHash == planHash {
				matches = true
				validEnd = len(lines[0]) + 1
				valid = append(valid, lines[0])
				for _, line := range lines[1:] {
					if len(line) == 0 {
						continue // trailing newline: clean end of file
					}
					var rec journalRecord
					if json.Unmarshal(line, &rec) != nil {
						// Torn or corrupt line: skip it but keep
						// replaying later valid records instead of
						// abandoning the whole tail.
						dirty = true
						continue
					}
					j.done[rec.Index] = true
					if rec.Remove != "" {
						manifest.Remove(rec.Remove)
					}
					if rec.Set != nil {
						manifest.Set(*rec.Set)
					}
					if rec.Skipped != "" {
						e.Skipped = append(e.Skipped, rec.Skipped)
					}
					valid = append(valid, line)
					validEnd += len(line) + 1
				}
				// A header without its newline (crash mid-write) still
				// parses; cap the end at what is actually on disk.
				if validEnd > len(data) {
					validEnd = len(data)
				}
			}
		}
		if matches {
			if dirty {
				// Rebuild from the intact lines so later appends land on
				// clean records instead of merging into damaged bytes.
				rebuilt := append(bytes.Join(valid, []byte{'\n'}), '\n')
				if err := os.WriteFile(e.JournalPath, rebuilt, 0o644); err != nil {
					return nil, err
				}
			} else if validEnd < len(data) {
				if err := os.Truncate(e.JournalPath, int64(validEnd)); err != nil {
					return nil, err
				}
			}
			file, err := os.OpenFile(e.JournalPath, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return nil, err
			}
			// Ensure the next record starts on its own line even if the
			// surviving tail lost its newline in the crash.
			if needs, err := journalNeedsNewline(e.JournalPath); err == nil && needs {
				if _, err := file.Write([]byte{'\n'}); err != nil {
					file.Close()
					return nil, err
				}
				if err := file.Sync(); err != nil {
					file.Close()
					return nil, err
				}
			}
			j.file = file
			return j, nil
		}
		j.done = map[int]bool{}
		e.Skipped = nil
	}
	if err := os.MkdirAll(filepath.Dir(e.JournalPath), 0o755); err != nil {
		return nil, err
	}
	file, err := os.Create(e.JournalPath)
	if err != nil {
		return nil, err
	}
	header, _ := json.Marshal(journalHeader{PlanHash: planHash})
	if _, err := file.Write(append(header, '\n')); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return nil, err
	}
	j.file = file
	return j, nil
}

// journalNeedsNewline reports whether the file at p is non-empty and does
// not end with a newline.
func journalNeedsNewline(p string) (bool, error) {
	f, err := os.Open(p)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return false, err
	}
	tail := make([]byte, 1)
	if _, err := f.ReadAt(tail, info.Size()-1); err != nil {
		return false, err
	}
	return tail[0] != '\n', nil
}

// record applies an operation's manifest effect and appends it to the
// journal, then flushes to disk. Callers hold e.mu; the journal's done set
// has its own lock so copy workers can finish concurrently without racing
// the dispatch loop. The Sync makes each record durable before the caller
// proceeds, so a crash can lose at most the single in-flight operation.
func (e *Executor) record(j *journal, manifest *Manifest, rec journalRecord) error {
	if rec.Remove != "" {
		manifest.Remove(rec.Remove)
	}
	if rec.Set != nil {
		manifest.Set(*rec.Set)
	}
	if rec.Skipped != "" {
		e.Skipped = append(e.Skipped, rec.Skipped)
	}
	j.markDone(rec.Index)
	if j.file == nil {
		return nil
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err = j.file.Write(append(line, '\n')); err != nil {
		return err
	}
	return j.file.Sync()
}

// Run executes the plan. It honours ctx cancellation, skips operations
// already recorded in the journal, and keeps manifest in step with the
// device. A nil manifest is treated as a device Auralis has never synced.
//
// Every operation's remote paths are validated to stay under Root before
// anything is copied; an unsafe plan fails fast without touching the
// device. The journal replays before any free-space decision, so a resume
// accounts only unfinished work against live free space and refuses with
// ErrInsufficientSpace before copying anything further. Copies run on a
// fixed worker pool of size Concurrency (bounded concurrency), deletes run
// serially afterwards, and cancellation via ctx stops new work while
// letting in-flight copies settle. Re-running the exact Ops against the
// final manifest is idempotent: copies re-Put identical bytes, completed
// moves reconcile from the destination, and finished deletes are no-ops.
func (e *Executor) Run(ctx context.Context, plan *Plan, manifest *Manifest) error {
	if plan == nil {
		return errors.New("nil plan")
	}
	if manifest == nil {
		return errors.New("nil manifest")
	}
	if e.Concurrency < 1 {
		e.Concurrency = 1
	}
	if e.now == nil {
		e.now = time.Now
	}
	for i, op := range plan.Ops {
		if err := ValidateRemoteRel(op.Remote); err != nil {
			return fmt.Errorf("op %d: invalid remote path: %w", i, err)
		}
		if op.Kind == OpMove || op.Kind == OpDelete {
			if err := ValidateRemoteRel(op.Source); err != nil {
				return fmt.Errorf("op %d: invalid source path: %w", i, err)
			}
		}
	}
	trashStamp := e.now().Unix()
	j, err := e.openJournal(plan.Hash(), manifest)
	if err != nil {
		return fmt.Errorf("open sync journal: %w", err)
	}
	defer func() {
		if j.file != nil {
			j.file.Close()
		}
	}()

	var firstErr error
	var errMu sync.Mutex
	setErr := func(err error) {
		errMu.Lock()
		defer errMu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	}
	var lastUpload time.Time
	finish := func(i int, rec journalRecord, name string) {
		rec.Index = i
		e.mu.Lock()
		err := e.record(j, manifest, rec)
		// Publish the device manifest on the first completed operation and
		// then at most every manifestFlushInterval, serialized on the
		// manifest lock. Uploading it after every file would re-send the
		// whole manifest N times (quadratic over ADB); the local journal
		// covers anything finished since the last upload.
		var upErr error
		if err == nil && e.now().Sub(lastUpload) >= manifestFlushInterval {
			upErr = WriteRemoteManifest(ctx, e.Target, e.Root, manifest)
			lastUpload = e.now()
		}
		done := j.count()
		e.mu.Unlock()
		if err != nil {
			setErr(fmt.Errorf("write sync journal: %w", err))
		} else if upErr != nil {
			setErr(fmt.Errorf("upload manifest: %w", upErr))
		}
		e.emit(Progress{Phase: "progress", OpIndex: i, OpTotal: len(plan.Ops), Done: done, Name: name})
	}

	// Fixed worker pool: at most Concurrency copies/moves in flight and
	// no goroutine per operation.
	//
	// Keeps whose remote file went missing (or whose size no longer matches
	// the manifest entry) are re-copied here instead of falsely remaining
	// managed: without this a file deleted on the device would stay in the
	// manifest and in playlists while being gone.
	repair := map[int]bool{}
	if ss, ok := e.Target.(StatSize); ok {
		for i := range plan.Ops {
			op := plan.Ops[i]
			if op.Kind != OpKeep || op.Track == nil || j.isDone(i) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			entry := manifest.Entry(op.Remote)
			if entry == nil {
				repair[i] = true
				continue
			}
			got, err := ss.StatSize(ctx, JoinRemote(e.Root, op.Remote))
			if err != nil {
				// errors.Is, not os.IsNotExist: the latter does not
				// follow %w chains, so wrapped transport errors would
				// misclassify as disconnects instead of missing files.
				if errors.Is(err, os.ErrNotExist) {
					repair[i] = true
					continue
				}
				return fmt.Errorf("verify keep %s: %w", op.Remote, err)
			}
			if got != entry.Size {
				repair[i] = true
			}
		}
	}
	var jobs []int
	for i := range plan.Ops {
		op := plan.Ops[i]
		if j.isDone(i) {
			continue
		}
		if op.Kind == OpDelete {
			continue
		}
		if op.Kind == OpKeep && !repair[i] {
			continue
		}
		jobs = append(jobs, i)
	}
	// Remaining-space check against live free space, after replay: only
	// unfinished adds/updates and repair copies still need bytes, so a
	// resume is refused only when what is left does not fit. This runs
	// before any copy so a tight device never ends mid-sync half-written.
	var remaining int64
	for _, i := range jobs {
		op := plan.Ops[i]
		if op.Kind == OpAdd || op.Kind == OpUpdate || op.Kind == OpKeep {
			remaining += op.Size
		}
	}
	if remaining > 0 {
		free, err := e.Target.FreeSpace(ctx)
		if err != nil {
			return fmt.Errorf("check free space: %w", err)
		}
		if free >= 0 && remaining > free {
			return fmt.Errorf("%w: need %d bytes but only %d are free", ErrInsufficientSpace, remaining, free)
		}
	}
	if len(jobs) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		workers := e.Concurrency
		if workers > len(jobs) {
			workers = len(jobs)
		}
		jobCh := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobCh {
					if ctx.Err() != nil {
						return
					}
					op := plan.Ops[i]
					var rec journalRecord
					var err error
					if op.Kind == OpKeep {
						// Repair copy for a keep whose remote is gone or
						// wrong-sized; runCopy re-Puts and records the Set.
						rec, err = e.runCopy(ctx, op)
					} else {
						rec, err = e.runOp(ctx, op, manifest)
					}
					if err != nil {
						if ctx.Err() == nil {
							setErr(fmt.Errorf("%s %s: %w", op.Kind, op.Remote, err))
						}
						continue
					}
					finish(i, rec, op.Remote)
				}
			}()
		}
		go func() {
			defer close(jobCh)
			for _, i := range jobs {
				select {
				case jobCh <- i:
				case <-ctx.Done():
					return
				}
			}
		}()
		wg.Wait()
	}
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Deletes run serially after every copy succeeded, and never
	// hard-delete unless opted in.
	for i := range plan.Ops {
		op := plan.Ops[i]
		if j.isDone(i) || op.Kind != OpDelete {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.runDelete(ctx, op, trashStamp); err != nil {
			return fmt.Errorf("delete %s: %w", op.Remote, err)
		}
		finish(i, journalRecord{Remove: op.Remote}, op.Remote)
		if firstErr != nil {
			return firstErr
		}
	}
	// Keeps refresh manifest metadata in case tags changed in place.
	for i := range plan.Ops {
		op := plan.Ops[i]
		if op.Kind != OpKeep || op.Track == nil {
			continue
		}
		if entry := manifest.Entry(op.Remote); entry != nil {
			entry.ModTime = op.Track.ModTime.Unix()
			manifest.Set(*entry)
		}
	}
	e.emit(Progress{Phase: "manifest", OpTotal: len(plan.Ops), Done: len(plan.Ops)})
	if err := WriteRemoteManifest(ctx, e.Target, e.Root, manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := e.Target.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	// The device manifest now holds everything the journal did.
	if j.file != nil {
		j.file.Close()
		j.file = nil
		_ = os.Remove(e.JournalPath)
	}
	e.emit(Progress{Phase: "done", OpTotal: len(plan.Ops), Done: len(plan.Ops)})
	return nil
}

func (e *Executor) runOp(ctx context.Context, op Operation, manifest *Manifest) (journalRecord, error) {
	switch op.Kind {
	case OpAdd, OpUpdate:
		return e.runCopy(ctx, op)
	case OpMove:
		return e.runMove(ctx, op, manifest)
	}
	return journalRecord{}, fmt.Errorf("unexpected operation %q", op.Kind)
}

// occupied reports whether a file already exists at a remote path.
func (e *Executor) occupied(ctx context.Context, remote string) bool {
	ss, ok := e.Target.(StatSize)
	if !ok {
		return false
	}
	_, err := ss.StatSize(ctx, remote)
	return err == nil
}

func (e *Executor) runCopy(ctx context.Context, op Operation) (journalRecord, error) {
	if op.Track == nil {
		return journalRecord{}, errors.New("missing track")
	}
	dst, err := JoinRemoteChecked(e.Root, op.Remote)
	if err != nil {
		return journalRecord{}, err
	}
	// Validate the source against the previewed snapshot before touching
	// the device: a file edited after planning must fail with a
	// re-preview signal, never sync stale bytes.
	tr := op.Track
	srcInfo, err := os.Stat(tr.Path)
	if err != nil {
		return journalRecord{}, err
	}
	if srcInfo.Size() != tr.Size || !srcInfo.ModTime().Equal(tr.ModTime) {
		return journalRecord{}, fmt.Errorf("%w: %s", ErrStalePreview, tr.Path)
	}
	// Re-hash the source and compare with the previewed hash. Scanners
	// have hashed with both Unix seconds and UnixNano mtimes, so accept a
	// match under either convention; anything else means the bytes the
	// preview approved are no longer on disk.
	fresh := false
	for _, mtime := range []int64{srcInfo.ModTime().UnixNano(), srcInfo.ModTime().Unix()} {
		h, err := HashSource(tr.Path, srcInfo.Size(), mtime)
		if err != nil {
			return journalRecord{}, err
		}
		if h == tr.Hash {
			fresh = true
			break
		}
	}
	if !fresh {
		return journalRecord{}, fmt.Errorf("%w: %s", ErrStalePreview, tr.Path)
	}
	local := tr.Path
	if e.Materialize != nil {
		produced, err := e.Materialize(ctx, tr)
		if err != nil {
			return journalRecord{}, err
		}
		local = produced
	}
	info, err := os.Stat(local)
	if err != nil {
		return journalRecord{}, err
	}
	if err := ctx.Err(); err != nil {
		return journalRecord{}, err
	}
	// Unmanifested files belong to the user, including same-sized files.
	// Never adopt them or replace them while recovering a journal.
	if op.Kind == OpAdd && e.occupied(ctx, dst) {
		e.emit(Progress{Phase: "skipped", Name: op.Remote})
		return journalRecord{Skipped: op.Remote}, nil
	}
	e.emit(Progress{Phase: "copy", Name: op.Remote, Bytes: 0})
	if err := e.Target.Put(ctx, local, dst, func(n int64) {
		e.emit(Progress{Phase: "copy", Name: op.Remote, Bytes: n})
	}); err != nil {
		return journalRecord{}, err
	}
	if ss, ok := e.Target.(StatSize); ok {
		got, err := ss.StatSize(ctx, dst)
		if err != nil {
			return journalRecord{}, fmt.Errorf("verify: %w", err)
		}
		if got != info.Size() {
			return journalRecord{}, fmt.Errorf("verify: size %d != %d", got, info.Size())
		}
	}
	entry := SanitizedManifestEntry(op.Track, op.Remote, e.Profile.ID, e.ProfileVersion, e.Policy, info.Size())
	return journalRecord{Set: &entry}, nil
}

func (e *Executor) runMove(ctx context.Context, op Operation, manifest *Manifest) (journalRecord, error) {
	from, err := JoinRemoteChecked(e.Root, op.Source)
	if err != nil {
		return journalRecord{}, err
	}
	to, err := JoinRemoteChecked(e.Root, op.Remote)
	if err != nil {
		return journalRecord{}, err
	}
	if e.occupied(ctx, to) {
		// A crash between Move and its journal record leaves exactly this
		// state: the source is gone and the destination holds the moved
		// bytes. Reconcile the manifest instead of dropping the entry,
		// which would orphan the destination, or re-moving, which would
		// fail on the missing source.
		if ss, ok := e.Target.(StatSize); ok {
			e.mu.Lock()
			old := manifest.Entry(op.Source)
			e.mu.Unlock()
			if old != nil {
				if _, serr := ss.StatSize(ctx, from); serr != nil && errors.Is(serr, os.ErrNotExist) {
					got, derr := ss.StatSize(ctx, to)
					if derr == nil && got == old.Size {
						rec := journalRecord{Remove: op.Source}
						if op.Track != nil {
							entry := SanitizedManifestEntry(op.Track, op.Remote, e.Profile.ID, e.ProfileVersion, e.Policy, old.Size)
							rec.Set = &entry
						}
						e.emit(Progress{Phase: "move", Name: op.Remote})
						return rec, nil
					}
					if derr != nil && !errors.Is(derr, os.ErrNotExist) {
						return journalRecord{}, derr
					}
					return journalRecord{}, fmt.Errorf("move %s: source missing and destination does not match the moved file", op.Remote)
				} else if serr != nil {
					return journalRecord{}, serr
				}
			}
		}
		// Genuine collision: the source is still there and something else
		// occupies the destination. Leave both alone and preserve the old
		// manifest entry.
		e.emit(Progress{Phase: "skipped", Name: op.Remote})
		return journalRecord{Skipped: op.Remote}, nil
	}
	e.emit(Progress{Phase: "move", Name: op.Remote})
	if err := e.Target.Move(ctx, from, to); err != nil {
		return journalRecord{}, err
	}
	// The file on the device is unchanged, so keep its delivered size; the
	// track's current tags win so moves caused by retagging stay accurate.
	e.mu.Lock()
	var size int64
	if old := manifest.Entry(op.Source); old != nil {
		size = old.Size
	}
	e.mu.Unlock()
	rec := journalRecord{Remove: op.Source}
	if op.Track != nil {
		entry := SanitizedManifestEntry(op.Track, op.Remote, e.Profile.ID, e.ProfileVersion, e.Policy, size)
		rec.Set = &entry
	}
	return rec, nil
}

func (e *Executor) runDelete(ctx context.Context, op Operation, trashStamp int64) error {
	src, err := JoinRemoteChecked(e.Root, op.Remote)
	if err != nil {
		return err
	}
	// A crash can leave the trash move done but unrecorded; a missing
	// source is already gone. Only a not-exist StatSize error means
	// missing — any other error (disconnect, cancel, permission)
	// propagates instead of silently skipping the delete. Transports map
	// their own "not found" onto os.ErrNotExist (compared with errors.Is,
	// which follows %w chains, unlike os.IsNotExist).
	if ss, ok := e.Target.(StatSize); ok {
		if _, err := ss.StatSize(ctx, src); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
	}
	e.emit(Progress{Phase: "delete", Name: op.Remote})
	if e.HardDelete {
		return e.Target.Delete(ctx, src)
	}
	// Keep the relative path so two "01. Intro.flac" files cannot collide.
	// The stamp is fixed per Run so one sync's trash lands in one folder.
	trash, err := JoinRemoteChecked(TrashDirRel, path.Join(fmt.Sprintf("%d", trashStamp), op.Remote))
	if err != nil {
		return err
	}
	return e.Target.Move(ctx, src, JoinRemote(e.Root, trash))
}

func (e *Executor) emit(p Progress) {
	if e.OnProgress != nil {
		e.OnProgress(p)
	}
}
