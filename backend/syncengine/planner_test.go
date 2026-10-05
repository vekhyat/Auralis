package syncengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend/library"
)

// memTarget is an in-memory SyncTarget for planner/executor tests.
type memTarget struct {
	mu        sync.Mutex
	files     map[string][]byte // abs path -> contents
	free      int64
	puts      int
	musicPuts int
	moves     int
	deletes   int
	failPut   error
}

func newMemTarget() *memTarget {
	return &memTarget{files: map[string][]byte{}, free: 1 << 40}
}

func (m *memTarget) Info() DeviceInfo {
	return DeviceInfo{ID: "mem", Name: "Memory", Kind: "folder", Root: "/music"}
}

func (m *memTarget) List(ctx context.Context, root string) ([]RemoteEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []RemoteEntry
	for p, b := range m.files {
		if strings.HasPrefix(p, strings.TrimRight(root, "/")+"/") {
			out = append(out, RemoteEntry{Path: p, Size: int64(len(b))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (m *memTarget) Put(ctx context.Context, localPath, remotePath string, progress func(int64)) error {
	if m.failPut != nil {
		return m.failPut
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := readFileForTarget(localPath)
	if err != nil {
		return err
	}
	m.files[remotePath] = data
	m.puts++
	if !strings.Contains(remotePath, "/.auralis/") {
		m.musicPuts++
	}
	if progress != nil {
		progress(int64(len(data)))
	}
	return nil
}

func (m *memTarget) Move(ctx context.Context, from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[from]
	if !ok {
		return errors.New("missing: " + from)
	}
	m.files[to] = data
	delete(m.files, from)
	m.moves++
	return nil
}

func (m *memTarget) Delete(ctx context.Context, remotePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, remotePath)
	m.deletes++
	return nil
}

func (m *memTarget) FreeSpace(ctx context.Context) (int64, error) { return m.free, nil }

func (m *memTarget) Commit(ctx context.Context) error { return nil }

func (m *memTarget) StatSize(ctx context.Context, remotePath string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[remotePath]
	if !ok {
		// Mirror the transport contract: "not found" maps to
		// os.ErrNotExist so os.IsNotExist distinguishes a missing file
		// from a disconnect, cancel, or permission failure.
		return 0, fmt.Errorf("%s: %w", remotePath, os.ErrNotExist)
	}
	return int64(len(b)), nil
}

func (m *memTarget) Open(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[remotePath]
	if !ok {
		return nil, fmt.Errorf("%s: %w", remotePath, os.ErrNotExist)
	}
	return io.NopCloser(strings.NewReader(string(b))), nil
}

func readFileForTarget(p string) ([]byte, error) {
	return os.ReadFile(p)
}

// counts by kind
func countKind(plan *Plan, k OpKind) int {
	n := 0
	for _, op := range plan.Ops {
		if op.Kind == k {
			n++
		}
	}
	return n
}

func track(album, title string, n int, hash string) SourceTrack {
	return SourceTrack{
		Path: "/lib/" + album + "/" + title + ".flac", Size: 1000,
		ModTime: time.Unix(1700000000, 0), Hash: hash,
		Title: title, Artist: "Artist", AlbumArtist: "Artist", Album: album,
		TrackNumber: n, Ext: ".flac",
	}
}

func TestPlannerAddsUpdatesMovesDeletes(t *testing.T) {
	profile := library.ProfileByID(library.ProfileMediaStore)
	manifest := NewManifest(profile.ID)
	keep := track("Alpha", "Keep", 1, "h-keep")
	update := track("Alpha", "Update", 2, "h-new")
	mover := track("Beta", "Moved", 1, "h-move")
	manifest.Set(ManifestEntry{RemotePath: relFor(profile, keep), SourcePath: keep.Path, Hash: "h-keep", ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"})
	manifest.Set(ManifestEntry{RemotePath: relFor(profile, update), SourcePath: update.Path, Hash: "h-old", ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"})
	manifest.Set(ManifestEntry{RemotePath: "Old Artist/Old Album/01. Moved.flac", SourcePath: mover.Path, Hash: "h-move", ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"})
	manifest.Set(ManifestEntry{RemotePath: "Gone/Album/01. Gone.flac", Hash: "h-gone", ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"})

	plan := PlanSelect(PlanOptions{
		Tracks: []SourceTrack{keep, update, mover}, Profile: profile, ProfileVersion: 1,
		Policy: FormatPolicy{Mode: "keep"}, Manifest: manifest, FreeBytes: 1 << 30,
	})
	if plan.Keeps != 1 || plan.Updates != 1 || plan.Moves != 1 || plan.Deletes != 1 || plan.Adds != 0 {
		t.Fatalf("counts = keeps %d updates %d moves %d deletes %d adds %d", plan.Keeps, plan.Updates, plan.Moves, plan.Deletes, plan.Adds)
	}
	// move carries the same hash to the new remote path
	var moveOp *Operation
	for i := range plan.Ops {
		if plan.Ops[i].Kind == OpMove {
			moveOp = &plan.Ops[i]
		}
	}
	if moveOp == nil || moveOp.Source != "Old Artist/Old Album/01. Moved.flac" || moveOp.Remote != relFor(profile, mover) {
		t.Fatalf("move op wrong: %+v", moveOp)
	}
}

func TestPlannerFreeSpaceRefusal(t *testing.T) {
	profile := library.ProfileByID(library.ProfileMediaStore)
	plan := PlanSelect(PlanOptions{
		Tracks:  []SourceTrack{track("A", "One", 1, "h1"), track("A", "Two", 2, "h2")},
		Profile: profile, ProfileVersion: 1, Policy: FormatPolicy{Mode: "keep"}, FreeBytes: 10,
	})
	if !plan.Insufficient {
		t.Fatal("expected Insufficient when free bytes < needed")
	}
}

func TestPlannerProfileChangeForcesUpdate(t *testing.T) {
	profile := library.ProfileByID(library.ProfilePoweramp)
	tr := track("A", "T", 1, "h1")
	manifest := NewManifest(profile.ID)
	manifest.Set(ManifestEntry{RemotePath: relFor(profile, tr), Hash: "h1", ProfileID: profile.ID, ProfileVersion: 1, Transcode: "keep"})
	plan := PlanSelect(PlanOptions{Tracks: []SourceTrack{tr}, Profile: profile, ProfileVersion: 2, Policy: FormatPolicy{Mode: "keep"}, Manifest: manifest, FreeBytes: -1})
	if plan.Updates != 1 || plan.Keeps != 0 {
		t.Fatalf("updates=%d keeps=%d", plan.Updates, plan.Keeps)
	}
}

func relFor(p library.Profile, tr SourceTrack) string {
	return p.SanitizeRelativePath(p.RelativePath(library.TrackFields{
		Title: tr.Title, Artist: tr.Artist, AlbumArtist: tr.AlbumArtist, Album: tr.Album,
		Year: tr.Year, TrackNumber: tr.TrackNumber, DiscNumber: tr.DiscNumber, DiscTotal: tr.DiscTotal,
	}) + ".flac")
}

var _ = path.Join
