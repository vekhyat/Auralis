package syncengine

import (
	"bufio"
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
	file *os.File
	done map[int]bool
}

// openJournal resumes the journal for planHash, replaying recorded effects
// onto manifest, or starts a fresh one.
func (e *Executor) openJournal(planHash string, manifest *Manifest) (*journal, error) {
	j := &journal{done: map[int]bool{}}
	if e.JournalPath == "" {
		return j, nil
	}
	if f, err := os.Open(e.JournalPath); err == nil {
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		matches := false
		if scanner.Scan() {
			var header journalHeader
			matches = json.Unmarshal(scanner.Bytes(), &header) == nil && header.PlanHash == planHash
		}
		for matches && scanner.Scan() {
			var rec journalRecord
			if json.Unmarshal(scanner.Bytes(), &rec) != nil {
				break // torn last line from a crash
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
		}
		f.Close()
		if matches {
			file, err := os.OpenFile(e.JournalPath, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return nil, err
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
	j.file = file
	return j, nil
}

// record applies an operation's manifest effect and appends it to the
// journal. Callers hold e.mu.
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
	j.done[rec.Index] = true
	if j.file == nil {
		return nil
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = j.file.Write(append(line, '\n'))
	return err
}

// Run executes the plan. It honours ctx cancellation, skips operations
// already recorded in the journal, and keeps manifest in step with the
// device. A nil manifest is treated as a device Auralis has never synced.
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
	finish := func(i int, rec journalRecord, name string) {
		rec.Index = i
		e.mu.Lock()
		err := e.record(j, manifest, rec)
		done := len(j.done)
		e.mu.Unlock()
		if err != nil {
			setErr(fmt.Errorf("write sync journal: %w", err))
		}
		e.emit(Progress{Phase: "progress", OpIndex: i, OpTotal: len(plan.Ops), Done: done, Name: name})
	}

	wg := sync.WaitGroup{}
	sem := make(chan struct{}, e.Concurrency)
	for i := range plan.Ops {
		op := plan.Ops[i]
		if j.done[i] || op.Kind == OpDelete || op.Kind == OpKeep {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			rec, err := e.runOp(ctx, op, manifest)
			if err != nil {
				if ctx.Err() == nil {
					setErr(fmt.Errorf("%s %s: %w", op.Kind, op.Remote, err))
				}
				return
			}
			finish(i, rec, op.Remote)
		}(i)
	}
	wg.Wait()
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
		if j.done[i] || op.Kind != OpDelete {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.runDelete(ctx, op); err != nil {
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
	dst := JoinRemote(e.Root, op.Remote)
	// An add targets a path the manifest does not own. If something is
	// already there, it is the user's file: leave it alone.
	if op.Kind == OpAdd && e.occupied(ctx, dst) {
		e.emit(Progress{Phase: "skipped", Name: op.Remote})
		return journalRecord{Skipped: op.Remote}, nil
	}
	local := op.Track.Path
	if e.Materialize != nil {
		produced, err := e.Materialize(ctx, op.Track)
		if err != nil {
			return journalRecord{}, err
		}
		local = produced
	}
	info, err := os.Stat(local)
	if err != nil {
		return journalRecord{}, err
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
	from := JoinRemote(e.Root, op.Source)
	to := JoinRemote(e.Root, op.Remote)
	if e.occupied(ctx, to) {
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

func (e *Executor) runDelete(ctx context.Context, op Operation) error {
	src := JoinRemote(e.Root, op.Remote)
	// A crash can leave the trash move done but unrecorded; a missing
	// source is already gone.
	if ss, ok := e.Target.(StatSize); ok {
		if _, err := ss.StatSize(ctx, src); err != nil {
			return nil
		}
	}
	e.emit(Progress{Phase: "delete", Name: op.Remote})
	if e.HardDelete {
		return e.Target.Delete(ctx, src)
	}
	// Keep the relative path so two "01. Intro.flac" files cannot collide.
	trash := path.Join(TrashDirRel, fmt.Sprintf("%d", e.now().Unix()), op.Remote)
	return e.Target.Move(ctx, src, JoinRemote(e.Root, trash))
}

func (e *Executor) emit(p Progress) {
	if e.OnProgress != nil {
		e.OnProgress(p)
	}
}
