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
