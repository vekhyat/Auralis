package syncengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/vekhyat/Auralis/backend"
	"github.com/vekhyat/Auralis/backend/library"
)

// Progress reports executor progress.
type Progress struct {
	Phase   string `json:"phase"` // "copy", "move", "delete", "manifest", "done"
	OpIndex int    `json:"op_index,omitempty"`
	OpTotal int    `json:"op_total"`
	Name    string `json:"name"`
	Done    int    `json:"done"` // completed ops so far
	Bytes   int64  `json:"bytes"`
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
	// JournalPath persists per-operation state so a restart resumes.
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
	mu          sync.Mutex // guards manifest, journal writes, and doneSet
}

// --- journal ---

type journalOp struct {
	Index  int    `json:"index"`
	Status string `json:"status"` // "pending", "done"
}

type journal struct {
	PlanHash string      `json:"plan_hash"`
	Ops      []journalOp `json:"ops"`
}

func (e *Executor) loadJournal(planHash string) *journal {
	data, err := os.ReadFile(e.JournalPath)
	if err != nil {
		return nil
	}
	var j journal
	if err := json.Unmarshal(data, &j); err != nil || j.PlanHash != planHash {
		return nil
	}
	return &j
}

func (e *Executor) saveJournal(j *journal) {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return
	}
	_ = backend.WriteFileAtomic(e.JournalPath, data, 0o644)
}

// Run executes the plan. It honours ctx cancellation, skips operations
// already marked done in the journal, and updates the manifest per op.
func (e *Executor) Run(ctx context.Context, plan *Plan, manifest *Manifest) error {
	if plan == nil {
		return errors.New("nil plan")
	}
	if e.Concurrency < 1 {
		e.Concurrency = 1
	}
	if e.now == nil {
		e.now = time.Now
	}
	j := e.loadJournal(plan.Hash())
	if j == nil {
		j = &journal{PlanHash: plan.Hash()}
		for i := range plan.Ops {
			j.Ops = append(j.Ops, journalOp{Index: i, Status: "pending"})
		}
	}
	doneSet := map[int]bool{}
	for _, op := range j.Ops {
		if op.Status == "done" {
			doneSet[op.Index] = true
		}
	}

	markDone := func(i int) {
		e.mu.Lock()
		defer e.mu.Unlock()
		doneSet[i] = true
		for k := range j.Ops {
			if j.Ops[k].Index == i {
				j.Ops[k].Status = "done"
			}
		}
		e.saveJournal(j)
	}

	var firstErr error
	var errMu sync.Mutex
	setErr := func(err error) {
		errMu.Lock()
		defer errMu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	}

	wg := sync.WaitGroup{}
	sem := make(chan struct{}, e.Concurrency)
	for i := range plan.Ops {
		op := plan.Ops[i]
		if doneSet[i] || op.Kind == OpDelete || op.Kind == OpKeep {
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
			if err := e.runOp(ctx, op, manifest); err != nil {
				if ctx.Err() == nil {
					setErr(fmt.Errorf("%s %s: %w", op.Kind, op.Remote, err))
				}
				return
			}
			markDone(i)
			e.mu.Lock()
			doneCount := len(doneSet)
			e.mu.Unlock()
			e.emit(Progress{Phase: "progress", OpTotal: len(plan.Ops), Done: doneCount, Name: op.Remote})
		}(i)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Deletes run serially and never hard-delete unless opted in.
	for i := range plan.Ops {
		op := plan.Ops[i]
		if doneSet[i] || op.Kind != OpDelete {
			continue
		}
		if err := e.runDelete(ctx, op, manifest); err != nil {
			return fmt.Errorf("delete %s: %w", op.Remote, err)
		}
		markDone(i)
	}
	// Keeps refresh manifest metadata in case tags changed in place.
	for i := range plan.Ops {
		op := plan.Ops[i]
		if op.Kind == OpKeep && op.Track != nil {
			if entry := manifest.Entry(op.Remote); entry != nil {
				entry.ModTime = op.Track.ModTime.Unix()
				manifest.Set(*entry)
			}
			doneSet[i] = true
		}
	}
	e.emit(Progress{Phase: "manifest", OpTotal: len(plan.Ops), Done: len(doneSet)})
	if err := WriteRemoteManifest(ctx, e.Target, e.Root, manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := e.Target.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	e.emit(Progress{Phase: "done", OpTotal: len(plan.Ops), Done: len(doneSet)})
	return nil
}

func (e *Executor) runOp(ctx context.Context, op Operation, manifest *Manifest) error {
	switch op.Kind {
	case OpAdd, OpUpdate:
		return e.runCopy(ctx, op, manifest)
	case OpMove:
		return e.runMove(ctx, op, manifest)
	case OpDelete:
		return e.runDelete(ctx, op, manifest)
	case OpKeep:
		return nil
	}
	return nil
}

func (e *Executor) runCopy(ctx context.Context, op Operation, manifest *Manifest) error {
	if op.Track == nil {
		return errors.New("missing track")
	}
	local := op.Track.Path
	if e.Materialize != nil {
		produced, err := e.Materialize(ctx, op.Track)
		if err != nil {
			return err
		}
		local = produced
	}
	info, err := os.Stat(local)
	if err != nil {
		return err
	}
	e.emit(Progress{Phase: "copy", Name: op.Remote, Bytes: info.Size()})
	dst := JoinRemote(e.Root, op.Remote)
	if err := e.Target.Put(ctx, local, dst, func(n int64) {
		e.emit(Progress{Phase: "copy", Name: op.Remote, Bytes: n})
	}); err != nil {
		return err
	}
	if ss, ok := e.Target.(StatSize); ok {
		got, err := ss.StatSize(ctx, dst)
		if err != nil {
			return fmt.Errorf("verify: %w", err)
		}
		if got != info.Size() {
			return fmt.Errorf("verify: size %d != %d", got, info.Size())
		}
	}
	e.mu.Lock()
	manifest.Set(SanitizedManifestEntry(op.Track, op.Remote, e.Profile.ID, e.ProfileVersion, e.Policy, info.Size()))
	e.mu.Unlock()
	return nil
}

func (e *Executor) runMove(ctx context.Context, op Operation, manifest *Manifest) error {
	from := JoinRemote(e.Root, op.Source)
	to := JoinRemote(e.Root, op.Remote)
	e.emit(Progress{Phase: "move", Name: op.Remote})
	if err := e.Target.Move(ctx, from, to); err != nil {
		return err
	}
	e.mu.Lock()
	if old := manifest.Entry(op.Source); old != nil {
		old.RemotePath = op.Remote
		manifest.Remove(op.Source)
		manifest.Set(*old)
	}
	e.mu.Unlock()
	return nil
}

func (e *Executor) runDelete(ctx context.Context, op Operation, manifest *Manifest) error {
	// A crash can leave the trash move done but the journal pending; treat a
	// missing source as already deleted instead of failing the resume.
	if ss, ok := e.Target.(StatSize); ok {
		if _, err := ss.StatSize(ctx, JoinRemote(e.Root, op.Remote)); err != nil {
			manifest.Remove(op.Remote)
			return nil
		}
	}
	if e.HardDelete {
		e.emit(Progress{Phase: "delete", Name: op.Remote})
		if err := e.Target.Delete(ctx, JoinRemote(e.Root, op.Remote)); err != nil {
			return err
		}
		manifest.Remove(op.Remote)
		return nil
	}
	stamp := e.now().Unix()
	trash := path.Join(".auralis", "trash", fmt.Sprintf("%d", stamp), path.Base(op.Remote))
	e.emit(Progress{Phase: "delete", Name: op.Remote})
	if err := e.Target.Move(ctx, JoinRemote(e.Root, op.Remote), JoinRemote(e.Root, trash)); err != nil {
		return err
	}
	manifest.Remove(op.Remote)
	return nil
}

func (e *Executor) emit(p Progress) {
	if e.OnProgress != nil {
		e.OnProgress(p)
	}
}
