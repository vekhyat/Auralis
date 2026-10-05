package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend"
	"github.com/vekhyat/Auralis/backend/devices/massstorage"
	"github.com/vekhyat/Auralis/backend/syncengine"
)

func syncTestApp(t *testing.T) (*App, string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AURALIS_APP_DIR", filepath.Join(dir, "app"))
	source := filepath.Join(dir, "library")
	dest := filepath.Join(dir, "phone")
	for _, p := range []string{source, dest} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := backend.SaveConfigSettings(map[string]interface{}{"downloadPath": source}); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	target, err := a.AddFolderTarget("Export", dest)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := a.GetDeviceProfile(target.ID)
	p.FormatPolicy.Mode = "keep"
	if err := a.SaveDeviceProfile(p); err != nil {
		t.Fatal(err)
	}
	return a, target.ID, source
}

func TestSyncRequiresStartedJobToResume(t *testing.T) {
	a, id, _ := syncTestApp(t)
	if err := a.StartSync(id); err == nil {
		t.Fatal("start without preview")
	}
	if _, err := a.PlanSync(id); err != nil {
		t.Fatal(err)
	}
	if a.HasInterruptedSync(id) {
		t.Fatal("preview must not create a started recovery job")
	}
	if err := a.ResumeSync(id); err == nil {
		t.Fatal("preview is not authorization to resume")
	}
}

func TestSyncRejectsChangedRootAndCorruptManifest(t *testing.T) {
	a, id, _ := syncTestApp(t)
	if _, err := a.PlanSync(id); err != nil {
		t.Fatal(err)
	}
	p, _ := a.GetDeviceProfile(id)
	root := p.TargetFolder
	p.TargetFolder = t.TempDir()
	// Bypass the binding's preview invalidation to test the start guard itself.
	if err := a.getSyncManager().SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	if err := a.StartSync(id); err == nil {
		t.Fatal("changed root must require fresh preview")
	}
	if err := os.MkdirAll(filepath.Join(root, ".auralis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".auralis", "manifest.json"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDeviceManifest(context.Background(), massstorage.New("test", "test", root)); err == nil {
		t.Fatal("corrupt manifest must not become empty library")
	}
}

func TestFolderSyncWritesSelectedPlaylist(t *testing.T) {
	a, id, source := syncTestApp(t)
	if err := os.WriteFile(filepath.Join(source, "song.flac"), []byte("audio test fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	playlist := filepath.Join(source, "Chosen.m3u8")
	if err := os.WriteFile(playlist, []byte("#EXTM3U\nsong.flac\nmissing.flac\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _ := a.GetDeviceProfile(id)
	p.SelectionRules = syncengine.Selection{Playlists: []string{playlist}}
	if err := a.SaveDeviceProfile(p); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PlanSync(id)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Adds != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if err := a.StartSync(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.syncMu.Lock()
		busy := a.activeSyncCancel != nil
		a.syncMu.Unlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.HasInterruptedSync(id) {
		t.Fatal("sync did not complete")
	}
	data, err := os.ReadFile(filepath.Join(p.TargetFolder, "Playlists", "Chosen.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty playlist")
	}
	saved, _ := a.GetDeviceProfile(id)
	if saved.LastManifestVersion != syncengine.ManifestVersion {
		t.Fatal("manifest version was not persisted")
	}
	// A track removed on the phone must be visible as a repair in the next
	// preview, instead of a keep that leaves a broken playlist entry.
	manifest, err := readDeviceManifest(context.Background(), massstorage.New(id, "Export", p.TargetFolder))
	if err != nil {
		t.Fatal(err)
	}
	for rel := range manifest.Entries {
		if err := os.Remove(filepath.Join(p.TargetFolder, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	repair, err := a.PlanSync(id)
	if err != nil {
		t.Fatal(err)
	}
	if repair.Updates != 1 || repair.Keeps != 0 {
		t.Fatalf("missing track preview = %+v", repair)
	}
}
