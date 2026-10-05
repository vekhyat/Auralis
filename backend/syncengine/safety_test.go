package syncengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend/library"
)

func TestValidateRemoteRel(t *testing.T) {
	safe := []string{"Artist/Album/01. One.flac", ".auralis/manifest.json", "Playlists/Mix.m3u8", "a"}
	for _, rel := range safe {
		if err := ValidateRemoteRel(rel); err != nil {
			t.Fatalf("safe %q rejected: %v", rel, err)
		}
	}
	unsafe := []string{"", "/abs/path.flac", "../evil.flac", "a/../../b", "a//b", "a/./b", `a\b`, "a/", "a/.", ".", "..", "a/../b"}
	for _, rel := range unsafe {
		if err := ValidateRemoteRel(rel); err == nil {
			t.Fatalf("unsafe %q accepted", rel)
		}
	}
	if _, err := JoinRemoteChecked("/music", "../evil"); err == nil {
		t.Fatal("JoinRemoteChecked accepted escape")
	}
	if got, err := JoinRemoteChecked("/music", "A/B.flac"); err != nil || got != "/music/A/B.flac" {
		t.Fatalf("join = %q, %v", got, err)
	}
}

func TestPlannerIgnoresUnsafeManifest(t *testing.T) {
	profile := library.ProfileByID(library.ProfileMediaStore)
	manifest := NewManifest(profile.ID)
	manifest.Entries["../evil.flac"] = ManifestEntry{RemotePath: "../evil.flac", Hash: "h-evil", ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"}
	manifest.Entries["/abs.flac"] = ManifestEntry{RemotePath: "/abs.flac", Hash: "h-abs", ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"}
	manifest.Entries["Gone/Album/01. Gone.flac"] = ManifestEntry{RemotePath: "Gone/Album/01. Gone.flac", Hash: "h-gone", ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"}
	plan := PlanSelect(PlanOptions{
		Tracks: nil, Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "keep"}, Manifest: manifest, FreeBytes: 1 << 30,
	})
	if plan.Deletes != 1 {
		t.Fatalf("deletes = %d, want 1 (only the safe entry)", plan.Deletes)
	}
	for _, op := range plan.Ops {
		if err := ValidateRemoteRel(op.Remote); err != nil {
			t.Fatalf("planner emitted unsafe remote %q", op.Remote)
		}
		if op.Kind == OpMove || op.Kind == OpDelete {
			if err := ValidateRemoteRel(op.Source); err != nil {
				t.Fatalf("planner emitted unsafe source %q", op.Source)
			}
		}
	}
}

func TestPlannerStaleTranscodeRecopies(t *testing.T) {
	profile := library.ProfileByID(library.ProfilePoweramp)
	tr := track("A", "T", 1, "h1")
	oldRel := "Old Artist/Old Album/01. T.flac"
	manifest := NewManifest(profile.ID)
	manifest.Set(ManifestEntry{RemotePath: oldRel, Hash: "h1", ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"})
	plan := PlanSelect(PlanOptions{
		Tracks: []SourceTrack{tr}, Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "opus"}, Manifest: manifest, FreeBytes: 1 << 30,
	})
	if countKind(plan, OpMove) != 0 {
		t.Fatal("stale transcode settings must not produce a move")
	}
	if countKind(plan, OpAdd) != 1 || countKind(plan, OpDelete) != 1 {
		t.Fatalf("expected add+delete for stale settings, got %+v", plan.Ops)
	}
}

func TestSnapshotAssociation(t *testing.T) {
	profile := library.ProfileByID(library.ProfileMediaStore)
	tracks := []SourceTrack{track("A", "One", 1, "h1"), track("A", "Two", 2, "h2")}
	plan := PlanSelect(PlanOptions{
		Tracks: tracks, Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 5000,
	})
	snap := NewPlanSnapshotForTarget(plan, profile.ID, 1, FormatPolicy{Mode: "keep"}, "dev-1", "/music")
	if !snap.Matches(profile.ID, 1, FormatPolicy{Mode: "keep"}, "dev-1", "/music") {
		t.Fatal("exact match should pass")
	}
	if snap.Matches(profile.ID, 2, FormatPolicy{Mode: "keep"}, "dev-1", "/music") {
		t.Fatal("profile version change must not match")
	}
	if snap.Matches(profile.ID, 1, FormatPolicy{Mode: "opus"}, "dev-1", "/music") {
		t.Fatal("policy change must not match")
	}
	if snap.Matches(profile.ID, 1, FormatPolicy{Mode: "keep"}, "dev-2", "/music") {
		t.Fatal("target change must not match")
	}
	if snap.Matches(profile.ID, 1, FormatPolicy{Mode: "keep"}, "dev-1", "/other") {
		t.Fatal("root change must not match")
	}
	// Old snapshots without association match anything.
	legacy := &PlanSnapshot{Hash: snap.Hash, Ops: snap.Ops, Tracks: snap.Tracks}
	if !legacy.Matches("whatever", 9, FormatPolicy{Mode: "opus"}, "x", "/y") {
		t.Fatal("legacy snapshot should be a wildcard")
	}
	// Round-trip preserves keeps and free-space association.
	keepManifest := NewManifest(profile.ID)
	keep := tracks[0]
	keepManifest.Set(ManifestEntry{RemotePath: relFor(profile, keep), Hash: keep.Hash, ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"})
	plan2 := PlanSelect(PlanOptions{
		Tracks: tracks, Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "keep"}, Manifest: keepManifest, FreeBytes: 5000,
	})
	snap2 := NewPlanSnapshotForTarget(plan2, profile.ID, 1, FormatPolicy{Mode: "keep"}, "dev-1", "/music")
	restored := snap2.Plan()
	if restored.Keeps != 1 {
		t.Fatalf("keeps = %d, want 1", restored.Keeps)
	}
	for _, op := range restored.Ops {
		if op.Kind == OpKeep && op.Track == nil {
			t.Fatal("keep lost its track pointer through the snapshot")
		}
	}
	if restored.FreeAvailable != 5000 || restored.FreeNeeded != plan2.FreeNeeded {
		t.Fatalf("free association lost: avail %d needed %d", restored.FreeAvailable, restored.FreeNeeded)
	}
	// Snapshot save/load is atomic and preserves association.
	p := filepath.Join(t.TempDir(), "snap.json")
	if err := snap2.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPlanSnapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Matches(profile.ID, 1, FormatPolicy{Mode: "keep"}, "dev-1", "/music") {
		t.Fatal("loaded snapshot lost association")
	}
	if got, want := loaded.Plan().Hash(), plan2.Hash(); got != want {
		t.Fatalf("snapshot hash %s != plan hash %s", got, want)
	}
}

func TestExecutorRejectsUnsafePlan(t *testing.T) {
	profile := library.ProfileByID(library.ProfileMediaStore)
	plan := &Plan{Ops: []Operation{{Kind: OpDelete, Source: "../evil", Remote: "../evil"}}}
	ex := &Executor{Target: newMemTarget(), Root: "/music", Profile: profile, Concurrency: 1, JournalPath: filepath.Join(t.TempDir(), "j.json")}
	if err := ex.Run(context.Background(), plan, NewManifest(profile.ID)); err == nil {
		t.Fatal("expected unsafe plan to fail fast")
	}
}

func TestExecutorBoundedConcurrency(t *testing.T) {
	tracks := setupLibrary(t)
	profile := library.ProfileByID(library.ProfileMediaStore)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 40})

	var current atomic.Int64
	var observedMax atomic.Int64
	base := newMemTarget()
	trackConcurrency := &concurrencyTarget{memTarget: base, enter: func() {
		n := current.Add(1)
		for {
			old := observedMax.Load()
			if n <= old || observedMax.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}, exit: func() { current.Add(-1) }}

	ex := &Executor{Target: trackConcurrency, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: filepath.Join(t.TempDir(), "j.json"), Concurrency: 2}
	if err := ex.Run(context.Background(), plan, NewManifest(profile.ID)); err != nil {
		t.Fatal(err)
	}
	if max := observedMax.Load(); max > 2 {
		t.Fatalf("max concurrency %d exceeds bound 2", max)
	}
	if max := observedMax.Load(); max < 1 {
		t.Fatalf("no concurrent work observed")
	}
}

type concurrencyTarget struct {
	*memTarget
	enter func()
	exit  func()
}

func (c *concurrencyTarget) Put(ctx context.Context, localPath, remotePath string, progress func(int64)) error {
	c.enter()
	defer c.exit()
	return c.memTarget.Put(ctx, localPath, remotePath, progress)
}

func TestExecutorContextCancellation(t *testing.T) {
	tracks := setupLibrary(t)
	profile := library.ProfileByID(library.ProfileMediaStore)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 40})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ex := &Executor{Target: newMemTarget(), Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: filepath.Join(t.TempDir(), "j.json"), Concurrency: 2}
	err := ex.Run(ctx, plan, NewManifest(profile.ID))
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestExecutorVerifiesSizes(t *testing.T) {
	tracks := setupLibrary(t)
	profile := library.ProfileByID(library.ProfileMediaStore)
	plan := PlanSelect(PlanOptions{Tracks: tracks[:1], Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 40})
	lying := &lyingSizeTarget{memTarget: newMemTarget()}
	ex := &Executor{Target: lying, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: filepath.Join(t.TempDir(), "j.json"), Concurrency: 1}
	if err := ex.Run(context.Background(), plan, NewManifest(profile.ID)); err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("expected verify error, got %v", err)
	}
}

type lyingSizeTarget struct {
	*memTarget
}

func (l *lyingSizeTarget) StatSize(ctx context.Context, remotePath string) (int64, error) {
	n, err := l.memTarget.StatSize(ctx, remotePath)
	if err != nil {
		return 0, err
	}
	return n + 1, nil
}

func TestManifestCleanUnsafe(t *testing.T) {
	m := NewManifest("mediastore")
	m.Entries["ok/A.flac"] = ManifestEntry{RemotePath: "ok/A.flac", Hash: "h"}
	m.Entries["../evil"] = ManifestEntry{RemotePath: "../evil", Hash: "h"}
	m.Entries["mismatch"] = ManifestEntry{RemotePath: "other", Hash: "h"}
	if n := m.CleanUnsafe(); n != 2 {
		t.Fatalf("removed = %d, want 2", n)
	}
	if len(m.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(m.Entries))
	}
}

func TestWriteRemoteManifestRoundTrip(t *testing.T) {
	m := NewManifest("mediastore")
	m.Set(ManifestEntry{RemotePath: "A/01. T.flac", Hash: "h", Size: 7, ProfileID: "mediastore", ProfileVersion: 1, Transcode: "keep"})
	target := newMemTarget()
	if err := WriteRemoteManifest(context.Background(), target, "/music", m); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRemoteManifest(context.Background(), target, "/music")
	if err != nil || loaded == nil {
		t.Fatalf("load = %v, %v", loaded, err)
	}
	if e := loaded.Entry("A/01. T.flac"); e == nil || e.Hash != "h" {
		t.Fatalf("round trip failed: %+v", e)
	}
}

func TestManifestSaveAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "manifest.json")
	m := NewManifest("mediastore")
	m.Set(ManifestEntry{RemotePath: "A.flac", Hash: "h"})
	if err := m.SaveLocal(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "sub", ".auralis-*.tmp"))
	if len(leftovers) != 0 {
		t.Fatalf("atomic temp files left behind: %v", leftovers)
	}
}

func TestPlaylistConfinesDirAndTracks(t *testing.T) {
	m := NewManifest("mediastore")
	m.Set(ManifestEntry{RemotePath: "A/01. One.flac", Hash: "h"})
	playlists := []DevicePlaylist{{
		Name: "../../evil",
		Tracks: []PlaylistTrack{
			{Rel: "A/01. One.flac", Title: "One", Artist: "A"},
			{Rel: "../escape.flac", Title: "X", Artist: "Y"},
		},
	}}
	target := newMemTarget()
	if err := WriteDevicePlaylists(context.Background(), target, "/music", "../evil", playlists, m, ""); err != nil {
		t.Fatal(err)
	}
	for p := range target.files {
		if strings.Contains(p, "..") {
			t.Fatalf("playlist escaped root: %s", p)
		}
		if strings.HasSuffix(p, ".m3u8") && !strings.HasPrefix(p, "/music/Playlists/") {
			t.Fatalf("playlist outside confined dir: %s", p)
		}
	}
	found := false
	for p, data := range target.files {
		if strings.HasSuffix(p, ".m3u8") {
			found = true
			content := string(data)
			if strings.Contains(content, "escape") {
				t.Fatalf("unsafe track leaked into playlist: %s", content)
			}
			if !strings.Contains(content, "One") {
				t.Fatalf("managed track missing from playlist: %s", content)
			}
		}
	}
	if !found {
		t.Fatalf("confined playlist not written: %v", target.files)
	}
}

func TestTranscodeRejectsEmptyHash(t *testing.T) {
	c := NewTranscodeCache(t.TempDir())
	tr := &SourceTrack{Path: "/lib/a.flac", Ext: ".flac", Hash: ""}
	if _, err := c.Materialize(context.Background(), tr, FormatPolicy{Mode: "opus"}); err == nil {
		t.Fatal("expected empty-hash rejection")
	}
	keep, err := c.Materialize(context.Background(), &SourceTrack{Path: "/lib/a.flac", Ext: ".mp3", Hash: ""}, FormatPolicy{Mode: "keep"})
	if err != nil || keep != "/lib/a.flac" {
		t.Fatalf("keep should pass through: %v %v", keep, err)
	}
	// Bounded concurrency guard: mutex-protected journal done set.
	var wg sync.WaitGroup
	j := &journal{done: map[int]bool{}}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			j.markDone(i)
			_ = j.isDone(i)
			_ = j.count()
		}(i)
	}
	wg.Wait()
	if j.count() != 50 {
		t.Fatalf("journal count = %d, want 50", j.count())
	}
}

func TestIsLosslessDecisions(t *testing.T) {
	opus := FormatPolicy{Mode: "opus"}
	cases := []struct {
		name string
		tr   SourceTrack
		want bool
	}{
		{"flac ext", SourceTrack{Ext: ".flac"}, true},
		{"mp3 ext", SourceTrack{Ext: ".mp3"}, false},
		{"unknown m4a copies", SourceTrack{Ext: ".m4a"}, false},
		{"alac m4a via flag", SourceTrack{Ext: ".m4a", Lossless: true}, true},
		{"alac m4a via codec", SourceTrack{Ext: ".m4a", Codec: "alac"}, true},
		{"aac m4a via codec", SourceTrack{Ext: ".m4a", Codec: "aac"}, false},
		{"codec beats lossy ext", SourceTrack{Ext: ".mp3", Codec: "FLAC"}, true},
		{"codec beats lossless ext", SourceTrack{Ext: ".flac", Codec: "aac"}, false},
		{"nil track", SourceTrack{}, false},
	}
	for _, tc := range cases {
		var tr *SourceTrack
		if tc.name != "nil track" {
			tr = &tc.tr
		}
		if got := IsLossless(tr); got != tc.want {
			t.Fatalf("%s: IsLossless = %v, want %v", tc.name, got, tc.want)
		}
		if got := opus.NeedsTranscodeTrack(tr); got != tc.want {
			t.Fatalf("%s: NeedsTranscodeTrack = %v, want %v", tc.name, got, tc.want)
		}
	}
	alac := &SourceTrack{Ext: ".m4a", Lossless: true, Hash: "h"}
	if got := opus.RemoteExtForTrack(alac); got != ".opus" {
		t.Fatalf("alac m4a remote ext = %q, want .opus", got)
	}
	plain := &SourceTrack{Ext: ".m4a", Hash: "h"}
	if got := opus.RemoteExtForTrack(plain); got != ".m4a" {
		t.Fatalf("plain m4a remote ext = %q, want .m4a", got)
	}
	// Extension-only helpers keep the legacy contract for stale tracks.
	if !opus.NeedsTranscode(".flac") || opus.NeedsTranscode(".m4a") {
		t.Fatal("extension-only NeedsTranscode changed contract")
	}
}

func TestSourceTrackLosslessSnapshotRoundTrip(t *testing.T) {
	profile := library.ProfileByID(library.ProfileMediaStore)
	tr := track("A", "One", 1, "h1")
	tr.Ext = ".m4a"
	tr.Lossless = true
	tr.Codec = "alac"
	plan := PlanSelect(PlanOptions{
		Tracks: []SourceTrack{tr}, Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "opus"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 30,
	})
	if countKind(plan, OpAdd) != 1 {
		t.Fatalf("expected one add, got %+v", plan.Ops)
	}
	if !strings.HasSuffix(plan.Ops[0].Remote, ".opus") {
		t.Fatalf("alac m4a should plan to .opus, got %q", plan.Ops[0].Remote)
	}
	snap := NewPlanSnapshot(plan)
	p := filepath.Join(t.TempDir(), "snap.json")
	if err := snap.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPlanSnapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	restored := loaded.Plan()
	if len(restored.Ops) != 1 || restored.Ops[0].Track == nil {
		t.Fatalf("track lost through snapshot: %+v", restored.Ops)
	}
	rt := restored.Ops[0].Track
	if !rt.Lossless || rt.Codec != "alac" {
		t.Fatalf("lossless flag/codec lost: %+v", rt)
	}
	if !IsLossless(rt) || !(FormatPolicy{Mode: "opus"}).NeedsTranscodeTrack(rt) {
		t.Fatal("restored track no longer transcodes")
	}
}

func TestPlanHashBindsContentAndSettings(t *testing.T) {
	profile := library.ProfileByID(library.ProfileMediaStore)
	base := PlanSelect(PlanOptions{
		Tracks: []SourceTrack{track("A", "One", 1, "h1")}, Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 30,
	})
	retagged := PlanSelect(PlanOptions{
		Tracks: []SourceTrack{track("A", "One", 1, "h2")}, Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 30,
	})
	if base.Hash() == retagged.Hash() {
		t.Fatal("hash must change when the source content hash changes")
	}
	transcoded := PlanSelect(PlanOptions{
		Tracks: []SourceTrack{track("A", "One", 1, "h1")}, Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "opus"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 30,
	})
	if base.Hash() == transcoded.Hash() {
		t.Fatal("hash must change when the format policy changes")
	}
	rebumped := PlanSelect(PlanOptions{
		Tracks: []SourceTrack{track("A", "One", 1, "h1")}, Profile: profile, ProfileVersion: 2,
		Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 30,
	})
	if base.Hash() == rebumped.Hash() {
		t.Fatal("hash must change when the profile version changes")
	}
}

func TestSnapshotPlanRestoresSettingsAndCounts(t *testing.T) {
	profile := library.ProfileByID(library.ProfileMediaStore)
	tracks := []SourceTrack{track("A", "One", 1, "h1"), track("A", "Two", 2, "h2")}
	keepManifest := NewManifest(profile.ID)
	keepManifest.Set(ManifestEntry{RemotePath: relFor(profile, tracks[0]), Hash: "h1", ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"})
	plan := PlanSelect(PlanOptions{
		Tracks: tracks, Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "keep"}, Manifest: keepManifest, FreeBytes: 5000,
	})
	snap := NewPlanSnapshot(plan)
	restored := snap.Plan()
	if restored.ProfileID != profile.ID || restored.ProfileVersion != 1 || restored.Policy.Mode != "keep" {
		t.Fatalf("settings lost: %+v", restored)
	}
	if restored.Adds != plan.Adds || restored.Keeps != plan.Keeps || restored.Deletes != plan.Deletes ||
		restored.Updates != plan.Updates || restored.Moves != plan.Moves {
		t.Fatalf("counts differ: %+v vs %+v", restored, plan)
	}
	if restored.FreeNeeded != plan.FreeNeeded || restored.FreeAvailable != plan.FreeAvailable ||
		restored.Insufficient != plan.Insufficient {
		t.Fatalf("free-space figures differ: %+v vs %+v", restored, plan)
	}
	if len(restored.Groups) != len(plan.Groups) {
		t.Fatalf("groups differ: %d vs %d", len(restored.Groups), len(plan.Groups))
	}
	if got, want := restored.Hash(), plan.Hash(); got != want {
		t.Fatal("restored plan hash differs from the previewed plan")
	}
}

func TestExecutorRefusesRemainingSpace(t *testing.T) {
	tracks := setupLibrary(t)[:2]
	profile := library.ProfileByID(library.ProfileMediaStore)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 40})
	target := newMemTarget()
	target.free = 1 // fits nothing
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: filepath.Join(t.TempDir(), "j.json"), Concurrency: 1}
	err := ex.Run(context.Background(), plan, NewManifest(profile.ID))
	if err == nil || !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("expected ErrInsufficientSpace, got %v", err)
	}
	if target.puts != 0 {
		t.Fatalf("refused run copied %d files", target.puts)
	}
}

func TestExecutorResumeAccountsRemainingSpace(t *testing.T) {
	tracks := setupLibrary(t)[:2]
	profile := library.ProfileByID(library.ProfileMediaStore)
	policy := FormatPolicy{Mode: "keep"}
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: policy, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 40})
	// Whole-plan total exceeds free space, but op 0 is already done.
	need0, need1 := plan.Ops[0].Size, plan.Ops[1].Size
	target := newMemTarget()
	target.free = need1
	journalPath := filepath.Join(t.TempDir(), "j.json")
	header, _ := json.Marshal(journalHeader{PlanHash: plan.Hash()})
	entry0 := SanitizedManifestEntry(plan.Ops[0].Track, plan.Ops[0].Remote, profile.ID, 1, policy, need0)
	rec0, _ := json.Marshal(journalRecord{Index: 0, Set: &entry0})
	data0, err := os.ReadFile(tracks[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	target.files[JoinRemote("/music", plan.Ops[0].Remote)] = data0
	content := append(append(header, '\n'), append(rec0, '\n')...)
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := NewManifest(profile.ID)
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: policy, JournalPath: journalPath, Concurrency: 1}
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatalf("resume should fit remaining bytes, got %v (need0=%d need1=%d free=%d)", err, need0, need1, need1)
	}
	if len(manifest.Entries) != 2 {
		t.Fatalf("manifest entries = %d, want 2", len(manifest.Entries))
	}
}

type errFreeTarget struct {
	*memTarget
	err error
}

func (e *errFreeTarget) FreeSpace(ctx context.Context) (int64, error) {
	return 0, e.err
}

func TestExecutorPropagatesFreeSpaceError(t *testing.T) {
	tracks := setupLibrary(t)[:1]
	profile := library.ProfileByID(library.ProfileMediaStore)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 40})
	boom := errors.New("adb offline")
	target := &errFreeTarget{memTarget: newMemTarget(), err: boom}
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, JournalPath: filepath.Join(t.TempDir(), "j.json"), Concurrency: 1}
	err := ex.Run(context.Background(), plan, NewManifest(profile.ID))
	if err == nil || !strings.Contains(err.Error(), "check free space") || !errors.Is(err, boom) {
		t.Fatalf("expected propagated free-space error, got %v", err)
	}
	if target.puts != 0 {
		t.Fatalf("failed space check copied %d files", target.puts)
	}
}

func TestExecutorRepairsMissingKeep(t *testing.T) {
	tracks := setupLibrary(t)
	profile := library.ProfileByID(library.ProfileMediaStore)
	policy := FormatPolicy{Mode: "keep"}
	target := newMemTarget()
	manifest := NewManifest(profile.ID)
	journal := filepath.Join(t.TempDir(), "j1.json")
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: policy, JournalPath: journal, Concurrency: 1}
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: policy, Manifest: manifest, FreeBytes: 1 << 40})
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	// Delete one managed file on the device behind the manifest's back.
	var victim string
	for _, op := range plan.Ops {
		victim = JoinRemote("/music", op.Remote)
		break
	}
	delete(target.files, victim)
	// Re-planning sees keeps (the manifest still matches the library).
	plan2 := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: policy, Manifest: manifest, FreeBytes: 1 << 40})
	if countKind(plan2, OpKeep) != len(tracks) {
		t.Fatalf("expected all keeps, got %+v", plan2.Ops)
	}
	ex2 := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: policy, JournalPath: filepath.Join(t.TempDir(), "j2.json"), Concurrency: 1}
	if err := ex2.Run(context.Background(), plan2, manifest); err != nil {
		t.Fatal(err)
	}
	got, err := target.StatSize(context.Background(), victim)
	if err != nil {
		t.Fatalf("missing keep was not repaired: %v", err)
	}
	entry := manifest.Entry(plan.Ops[0].Remote)
	if entry == nil || got != entry.Size {
		t.Fatalf("repaired size %d does not match manifest %+v", got, entry)
	}
}

func TestExecutorRepairsMismatchedKeep(t *testing.T) {
	tracks := setupLibrary(t)[:1]
	profile := library.ProfileByID(library.ProfileMediaStore)
	policy := FormatPolicy{Mode: "keep"}
	target := newMemTarget()
	manifest := NewManifest(profile.ID)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: policy, Manifest: manifest, FreeBytes: 1 << 40})
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: policy, JournalPath: filepath.Join(t.TempDir(), "j1.json"), Concurrency: 1}
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	// Corrupt the managed file with wrong-sized bytes.
	target.files[JoinRemote("/music", plan.Ops[0].Remote)] = []byte("x")
	plan2 := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: policy, Manifest: manifest, FreeBytes: 1 << 40})
	if countKind(plan2, OpKeep) != 1 {
		t.Fatalf("expected a keep, got %+v", plan2.Ops)
	}
	ex2 := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: policy, JournalPath: filepath.Join(t.TempDir(), "j2.json"), Concurrency: 1}
	if err := ex2.Run(context.Background(), plan2, manifest); err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(tracks[0].Path)
	if got := target.files[JoinRemote("/music", plan.Ops[0].Remote)]; !bytes.Equal(got, want) {
		t.Fatal("mismatched keep was not repaired with source bytes")
	}
}

type statErrTarget struct {
	*memTarget
	statErr error
	paths   map[string]bool // abs paths to fail; nil means all
}

func (s *statErrTarget) StatSize(ctx context.Context, remotePath string) (int64, error) {
	if s.paths == nil || s.paths[remotePath] {
		return 0, s.statErr
	}
	return s.memTarget.StatSize(ctx, remotePath)
}

func TestRunDeletePropagatesStatErrors(t *testing.T) {
	tracks := setupLibrary(t)[:1]
	profile := library.ProfileByID(library.ProfileMediaStore)
	policy := FormatPolicy{Mode: "keep"}
	target := newMemTarget()
	manifest := NewManifest(profile.ID)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: policy, Manifest: manifest, FreeBytes: 1 << 40})
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: policy, JournalPath: filepath.Join(t.TempDir(), "j1.json"), Concurrency: 1}
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	// Plan the delete, then break StatSize with a non-missing error.
	plan2 := PlanSelect(PlanOptions{Tracks: nil, Profile: profile, ProfileVersion: 1, Policy: policy, Manifest: manifest, FreeBytes: 1 << 40})
	if countKind(plan2, OpDelete) != 1 {
		t.Fatalf("expected one delete, got %+v", plan2.Ops)
	}
	denied := fmt.Errorf("permission denied: %w", os.ErrPermission)
	broken := &statErrTarget{memTarget: target, statErr: denied}
	ex2 := &Executor{Target: broken, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: policy, JournalPath: filepath.Join(t.TempDir(), "j2.json"), Concurrency: 1}
	err := ex2.Run(context.Background(), plan2, manifest)
	if err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected permission error to propagate, got %v", err)
	}
	if _, ok := target.files[JoinRemote("/music", plan.Ops[0].Remote)]; !ok {
		t.Fatal("failed delete must not remove the file")
	}
}

func TestRunMoveReconcilesCompletedMove(t *testing.T) {
	tracks := setupLibrary(t)[:1]
	profile := library.ProfileByID(library.ProfileMediaStore)
	policy := FormatPolicy{Mode: "keep"}
	oldRel := relFor(profile, tracks[0])
	// Retag so the desired path moves but the content hash does not.
	tracks[0].Title = "Renamed"
	tracks[0].TrackNumber = 9
	manifest := NewManifest(profile.ID)
	content, err := os.ReadFile(tracks[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Set(ManifestEntry{RemotePath: oldRel, SourcePath: tracks[0].Path, Hash: tracks[0].Hash, Size: int64(len(content)), ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"})
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: policy, Manifest: manifest, FreeBytes: 1 << 40})
	if countKind(plan, OpMove) != 1 {
		t.Fatalf("expected one move, got %+v", plan.Ops)
	}
	moveOp := plan.Ops[0]
	// Simulate the crash window: the bytes already moved, but no journal.
	target := newMemTarget()
	target.files[JoinRemote("/music", moveOp.Remote)] = append([]byte(nil), content...)
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: policy, JournalPath: filepath.Join(t.TempDir(), "j.json"), Concurrency: 1}
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Entry(moveOp.Source) != nil {
		t.Fatal("stale source entry should be removed by the reconcile")
	}
	entry := manifest.Entry(moveOp.Remote)
	if entry == nil || entry.Hash != tracks[0].Hash || entry.Size != int64(len(content)) {
		t.Fatalf("destination entry wrong after reconcile: %+v", entry)
	}
	if target.moves != 0 || target.musicPuts != 0 {
		t.Fatalf("reconcile must not re-move or re-copy (moves=%d puts=%d)", target.moves, target.musicPuts)
	}
	if len(ex.Skipped) != 0 {
		t.Fatalf("reconciled move must not be reported skipped: %v", ex.Skipped)
	}
}

func TestJournalTruncatesTornTail(t *testing.T) {
	tracks := setupLibrary(t)[:2]
	profile := library.ProfileByID(library.ProfileMediaStore)
	policy := FormatPolicy{Mode: "keep"}
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: policy, Manifest: NewManifest(profile.ID), FreeBytes: 1 << 40})
	journalPath := filepath.Join(t.TempDir(), "j.json")
	header, _ := json.Marshal(journalHeader{PlanHash: plan.Hash()})
	entry0 := SanitizedManifestEntry(plan.Ops[0].Track, plan.Ops[0].Remote, profile.ID, 1, policy, plan.Ops[0].Track.Size)
	rec0, _ := json.Marshal(journalRecord{Index: 0, Set: &entry0})
	entry1 := SanitizedManifestEntry(plan.Ops[1].Track, plan.Ops[1].Remote, profile.ID, 1, policy, plan.Ops[1].Track.Size)
	rec1, _ := json.Marshal(journalRecord{Index: 1, Set: &entry1})
	torn := append(append(append(header, '\n'), append(rec0, '\n')...), rec1[:len(rec1)/2]...)
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, torn, 0o644); err != nil {
		t.Fatal(err)
	}
	ex := &Executor{Target: newMemTarget(), Root: "/music", Profile: profile, Concurrency: 1, JournalPath: journalPath}
	manifest := NewManifest(profile.ID)
	j, err := ex.openJournal(plan.Hash(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !j.isDone(0) || j.isDone(1) {
		t.Fatal("only the intact record should replay")
	}
	if manifest.Entry(plan.Ops[0].Remote) == nil {
		t.Fatal("intact record effect lost")
	}
	// Appending a fresh record must start on a clean line.
	ex.mu.Lock()
	rec := journalRecord{Index: 1, Set: &entry1}
	rec.Index = 1
	if err := ex.record(j, manifest, rec); err != nil {
		ex.mu.Unlock()
		t.Fatal(err)
	}
	ex.mu.Unlock()
	j.file.Close()
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	records := 0
	for i, line := range lines {
		if i == 0 {
			var header journalHeader
			if err := json.Unmarshal(line, &header); err != nil || header.PlanHash != plan.Hash() {
				t.Fatal("invalid journal header")
			}
			continue
		}
		if len(line) == 0 {
			continue
		}
		var probe journalRecord
		if err := json.Unmarshal(line, &probe); err != nil {
			// The header line carries plan_hash, not a record.
			var probeHeader journalHeader
			if herr := json.Unmarshal(line, &probeHeader); herr != nil || probeHeader.PlanHash == "" {
				t.Fatalf("damaged journal line never repaired: %q", line)
			}
			continue
		}
		records++
	}
	if records != 2 {
		t.Fatalf("records = %d, want 2 (intact + appended)", records)
	}
}

func TestExecutorSkipsSameSizedUnmanagedFile(t *testing.T) {
	tracks := setupLibrary(t)[:1]
	profile := library.ProfileByID(library.ProfileMediaStore)
	policy := FormatPolicy{Mode: "keep"}
	manifest := NewManifest(profile.ID)
	plan := PlanSelect(PlanOptions{Tracks: tracks, Profile: profile, ProfileVersion: 1, Policy: policy, Manifest: manifest, FreeBytes: 1 << 40})
	target := newMemTarget()
	remote := JoinRemote("/music", plan.Ops[0].Remote)
	original := bytes.Repeat([]byte{'x'}, int(tracks[0].Size))
	target.files[remote] = append([]byte(nil), original...)
	ex := &Executor{Target: target, Root: "/music", Profile: profile, ProfileVersion: 1, Policy: policy}
	if err := ex.Run(context.Background(), plan, manifest); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(target.files[remote], original) || target.musicPuts != 0 {
		t.Fatal("unmanaged file was overwritten")
	}
	if manifest.Entry(plan.Ops[0].Remote) != nil || len(ex.Skipped) != 1 {
		t.Fatal("unmanaged file was adopted instead of skipped")
	}
}
