package syncengine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend/library"
)

// failingPutTarget fails the Nth Put to simulate a crash.
type failingPutTarget struct {
	*memTarget
	putFailures int
	putCalls    int
}

func (f *failingPutTarget) Put(ctx context.Context, localPath, remotePath string, progress func(int64)) error {
	f.putCalls++
	if f.putCalls > f.putFailures {
		return errors.New("simulated crash")
	}
	return f.memTarget.Put(ctx, localPath, remotePath, progress)
}

func setupLibrary(t *testing.T) []SourceTrack {
	t.Helper()
	dir := t.TempDir()
	var tracks []SourceTrack
	for _, spec := range []struct {
		album, title string
		n            int
	}{
		{"Alpha", "One", 1}, {"Alpha", "Two", 2}, {"Beta", "Three", 1},
	} {
		p := filepath.Join(dir, spec.album+"-"+spec.title+".flac")
		if err := os.WriteFile(p, []byte("audio:"+spec.title), 0o644); err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(p)
		hash, err := HashSource(p, info.Size(), info.ModTime().Unix())
		if err != nil {
			t.Fatal(err)
		}
		tracks = append(tracks, SourceTrack{
			Path: p, Size: info.Size(), ModTime: info.ModTime(), Hash: hash,
			Title: spec.title, Artist: "Artist", AlbumArtist: "Artist", Album: spec.album,
			TrackNumber: spec.n, Ext: ".flac",
		})
	}
	return tracks
}

func TestExecutorCopiesAndManifest(t *testing.T) {
	tracks := setupLibrary(t)
	target := newMemTarget()
	profile := library.ProfileByID(library.ProfileMediaStore)
	manifest := NewManifest(profile.ID)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: manifest, FreeBytes: 1 << 40})
	ex := &Executor{
		Target: target, Root: "/music", Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "keep"}, JournalPath: filepath.Join(t.TempDir(), "journal.json"),
		Concurrency: 2,
	}
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != len(tracks) {
		t.Fatalf("manifest entries = %d, want %d", len(manifest.Entries), len(tracks))
	}
	entries, _ := target.List(context.Background(), "/music")
	audio := 0
	for _, e := range entries {
		if !strings.Contains(e.Path, "/.auralis/") {
			audio++
		}
	}
	if audio != len(tracks) {
		t.Fatalf("target files = %d, want %d", audio, len(tracks))
	}
	// manifest uploaded
	if _, ok := target.files["/music/.auralis/manifest.json"]; !ok {
		t.Fatal("manifest.json not uploaded")
	}
}

func TestExecutorResumeAfterCrash(t *testing.T) {
	tracks := setupLibrary(t)
	manifest := NewManifest("mediastore")
	target := &failingPutTarget{memTarget: newMemTarget(), putFailures: 1}
	profile := library.ProfileByID(library.ProfileMediaStore)
	journalPath := filepath.Join(t.TempDir(), "journal.json")
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: manifest, FreeBytes: 1 << 40})
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: journalPath, Concurrency: 1}
	err := ex.Run(context.Background(), plan, manifest)
	if err == nil || !strings.Contains(err.Error(), "simulated crash") {
		t.Fatalf("expected simulated crash, got %v", err)
	}
	copiedAfterCrash := len(manifest.Entries)
	if copiedAfterCrash == 0 || copiedAfterCrash == len(tracks) {
		t.Fatalf("partial copy expected, manifest has %d", copiedAfterCrash)
	}
	// Resume with a healthy target, same plan, same manifest, same journal.
	target2 := newMemTarget()
	target2.files = target.memTarget.files
	ex2 := &Executor{Target: target2, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: journalPath, Concurrency: 1}
	if err := ex2.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != len(tracks) {
		t.Fatalf("manifest entries after resume = %d", len(manifest.Entries))
	}
}

func TestExecutorDeleteGoesToTrash(t *testing.T) {
	tracks := setupLibrary(t)
	target := newMemTarget()
	manifest := NewManifest("mediastore")
	profile := library.ProfileByID(library.ProfileMediaStore)
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: filepath.Join(t.TempDir(), "j.json"), Concurrency: 1, now: func() time.Time { return time.Unix(1700000000, 0) }}
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: manifest, FreeBytes: 1 << 40})
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	// Second sync with an empty selection deletes everything to trash.
	plan2 := PlanSelect(PlanOptions{Tracks: nil, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: manifest, FreeBytes: 1 << 40})
	if plan2.Deletes != len(tracks) {
		t.Fatalf("deletes = %d", plan2.Deletes)
	}
	if err := ex.Run(context.Background(), plan2, manifest); err != nil {
		t.Fatal(err)
	}
	if target.deletes != 0 {
		t.Fatal("hard delete happened without opt-in")
	}
	trashCount := 0
	for p := range target.files {
		if strings.Contains(p, "/.auralis/trash/") {
			trashCount++
		}
	}
	if trashCount != len(tracks) {
		t.Fatalf("trash files = %d, want %d", trashCount, len(tracks))
	}
}

func TestExecutorResumeRebuildsManifestFromJournal(t *testing.T) {
	// A real restart reloads the old manifest from the device; the journal
	// must restore what finished before the crash.
	tracks := setupLibrary(t)
	profile := library.ProfileByID(library.ProfileMediaStore)
	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 40})
	target := &failingPutTarget{memTarget: newMemTarget(), putFailures: 2}
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: journalPath, Concurrency: 1}
	if err := ex.Run(context.Background(), plan, NewManifest(profile.ID)); err == nil {
		t.Fatal("expected the simulated crash")
	}

	healthy := newMemTarget()
	healthy.files = target.memTarget.files
	counted := &audioPutCounter{memTarget: healthy}
	fresh := NewManifest(profile.ID)
	ex2 := &Executor{Target: counted, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: journalPath, Concurrency: 1}
	if err := ex2.Run(context.Background(), plan, fresh); err != nil {
		t.Fatal(err)
	}
	if len(fresh.Entries) != len(tracks) {
		t.Fatalf("manifest after resume has %d entries, want %d", len(fresh.Entries), len(tracks))
	}
	// Each finished copy also uploads the device manifest, so count only
	// audio Puts: the resume must copy just the unfinished remainder.
	// (The crash left op 0 journaled, so ops 1..2 are the remainder.)
	if counted.audio != len(tracks)-1 {
		t.Fatalf("resume copied %d audio files, want %d", counted.audio, len(tracks)-1)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("journal should be removed after success, stat err = %v", err)
	}
}

// audioPutCounter counts Puts of audio files, excluding the per-operation
// device-manifest uploads, so resume tests can assert unfinished work only.
type audioPutCounter struct {
	*memTarget
	audio int
}

func (a *audioPutCounter) Put(ctx context.Context, localPath, remotePath string, progress func(int64)) error {
	if !strings.HasSuffix(remotePath, ".auralis/manifest.json") {
		a.audio++
	}
	return a.memTarget.Put(ctx, localPath, remotePath, progress)
}

func TestExecutorNeverOverwritesUnmanagedFiles(t *testing.T) {
	tracks := setupLibrary(t)
	profile := library.ProfileByID(library.ProfileMediaStore)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 40})
	target := newMemTarget()
	userFile := JoinRemote("/music", plan.Ops[0].Remote)
	target.files[userFile] = []byte("the user's own copy")
	manifest := NewManifest(profile.ID)
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: filepath.Join(t.TempDir(), "j.jsonl"), Concurrency: 1}
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	if string(target.files[userFile]) != "the user's own copy" {
		t.Fatal("an unmanaged file on the device was overwritten")
	}
	if manifest.Entry(plan.Ops[0].Remote) != nil {
		t.Fatal("a skipped file must not become managed")
	}
	if len(ex.Skipped) != 1 || ex.Skipped[0] != plan.Ops[0].Remote {
		t.Fatalf("skipped = %v", ex.Skipped)
	}
}

func TestExecutorTrashKeepsRelativePath(t *testing.T) {
	tracks := setupLibrary(t)
	// Same title on two albums: both land in trash without colliding.
	tracks[2].Title = tracks[0].Title
	tracks[2].TrackNumber = tracks[0].TrackNumber
	profile := library.ProfileByID(library.ProfileMediaStore)
	target := newMemTarget()
	manifest := NewManifest(profile.ID)
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: filepath.Join(t.TempDir(), "j.jsonl"), Concurrency: 1, now: func() time.Time { return time.Unix(1700000000, 0) }}
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: manifest, FreeBytes: 1 << 40})
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	plan2 := PlanSelect(PlanOptions{Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: manifest, FreeBytes: 1 << 40})
	if err := ex.Run(context.Background(), plan2, manifest); err != nil {
		t.Fatal(err)
	}
	trash := 0
	for p := range target.files {
		if strings.HasPrefix(p, "/music/.auralis/trash/1700000000/") {
			trash++
		}
	}
	if trash != len(tracks) {
		t.Fatalf("trash files = %d, want %d", trash, len(tracks))
	}
}

func TestPlanDisambiguatesCollidingPaths(t *testing.T) {
	tracks := setupLibrary(t)
	dup := tracks[0]
	dup.Path += ".copy"
	dup.Hash = "other"
	dup.Title = strings.ToUpper(dup.Title) // differs only by case
	tracks = append(tracks, dup)
	profile := library.ProfileByID(library.ProfileMediaStore)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 40})
	seen := map[string]bool{}
	for _, op := range plan.Ops {
		key := strings.ToLower(op.Remote)
		if seen[key] {
			t.Fatalf("two operations target %q", op.Remote)
		}
		seen[key] = true
	}
	if plan.Adds != len(tracks) {
		t.Fatalf("adds = %d, want %d", plan.Adds, len(tracks))
	}
}

// manifestCountingTarget counts uploads of the device manifest.
type manifestCountingTarget struct {
	*memTarget
	manifestPuts int
}

func (m *manifestCountingTarget) Put(ctx context.Context, localPath, remotePath string, progress func(int64)) error {
	if strings.HasSuffix(remotePath, "/.auralis/manifest.json") {
		m.manifestPuts++
	}
	return m.memTarget.Put(ctx, localPath, remotePath, progress)
}

func TestExecutorThrottlesManifestUploads(t *testing.T) {
	// Re-uploading the whole manifest per file is quadratic over ADB. With
	// a frozen clock the manifest goes up once early and once at the end.
	tracks := setupLibrary(t)
	target := &manifestCountingTarget{memTarget: newMemTarget()}
	profile := library.ProfileByID(library.ProfileMediaStore)
	manifest := NewManifest(profile.ID)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: manifest, FreeBytes: 1 << 40})
	frozen := time.Unix(1_700_000_000, 0)
	ex := &Executor{
		Target: target, Root: "/music", Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "keep"}, JournalPath: filepath.Join(t.TempDir(), "journal.json"),
		Concurrency: 1, now: func() time.Time { return frozen },
	}
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	if target.manifestPuts != 2 {
		t.Fatalf("manifest uploads = %d, want 2 (first operation and final)", target.manifestPuts)
	}
}
