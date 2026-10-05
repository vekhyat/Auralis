package devices

import (
	"context"
	"github.com/vekhyat/Auralis/backend/devices/adb"
	"github.com/vekhyat/Auralis/backend/devices/massstorage"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeviceManagerFoldersAndProfiles(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AURALIS_APP_DIR", tmpDir)

	mgr := NewManager()
	mgr.discoverADB = func(context.Context) ([]adb.Device, error) { return nil, nil }
	mgr.discoverDrives = func() ([]massstorage.Drive, error) { return nil, nil }

	// 1. Add folder target
	musicFolder := filepath.Join(tmpDir, "MyMusic")
	if err := os.MkdirAll(musicFolder, 0o755); err != nil {
		t.Fatal(err)
	}

	view, err := mgr.AddFolderTarget("Phone Export", musicFolder)
	if err != nil {
		t.Fatalf("AddFolderTarget failed: %v", err)
	}
	if view.Kind != "folder" || view.Name != "Phone Export" || view.Root != musicFolder {
		t.Errorf("unexpected view: %+v", view)
	}

	// 2. List targets
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	views, err := mgr.ListTargets(ctx)
	if err != nil {
		t.Fatalf("ListTargets failed: %v", err)
	}
	var found bool
	for _, v := range views {
		if v.ID == view.ID {
			found = true
			if !v.Connected {
				t.Errorf("expected folder target to be connected")
			}
		}
	}
	if !found {
		t.Errorf("folder target %s not found in views", view.ID)
	}

	// 3. Update and persist profile
	prof := mgr.GetProfile(view.ID)
	prof.FriendlyName = "Syncthing Pixel"
	prof.FormatPolicy.Mode = "keep"
	if err := mgr.SaveProfile(prof); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	mgr2 := NewManager()
	prof2 := mgr2.GetProfile(view.ID)
	if prof2.FriendlyName != "Syncthing Pixel" || prof2.FormatPolicy.Mode != "keep" {
		t.Errorf("reloaded profile mismatch: %+v", prof2)
	}

	// 4. Resolve target
	tgt, libProf, policy, err := mgr.ResolveTarget(view.ID)
	if err != nil {
		t.Fatalf("ResolveTarget failed: %v", err)
	}
	if tgt == nil || libProf.ID == "" || policy.Mode != "keep" {
		t.Errorf("unexpected resolved target: tgt=%v, libProf=%+v, policy=%+v", tgt, libProf, policy)
	}

	// 5. Remove folder target
	if err := mgr.RemoveFolderTarget(view.ID); err != nil {
		t.Fatalf("RemoveFolderTarget failed: %v", err)
	}
	viewsAfter, err := mgr.ListTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range viewsAfter {
		if v.ID == view.ID {
			t.Errorf("removed target still found in ListTargets: %+v", v)
		}
	}
}

func TestDeviceProfileValidationAndPersistenceErrors(t *testing.T) {
	t.Setenv("AURALIS_APP_DIR", t.TempDir())
	mgr := NewManager()
	view, err := mgr.AddFolderTarget("Export", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := mgr.GetProfile(view.ID)
	p.ProfileID = "typo"
	if err := mgr.SaveProfile(p); err == nil {
		t.Fatal("invalid profile accepted")
	}
	if _, _, _, err := mgr.ResolveTarget("folder:unknown"); err == nil {
		t.Fatal("unknown folder accepted")
	}
	p = mgr.GetProfile(view.ID)
	p.FriendlyName = "New name"
	mgr.profilesPath = t.TempDir() // replacing a directory with a file must fail
	if err := mgr.SaveProfile(p); err == nil {
		t.Fatal("persistence failure ignored")
	}
	if mgr.GetProfile(view.ID).FriendlyName != "Export" {
		t.Fatal("failed save changed in-memory profile")
	}
}

func TestCorruptDeviceProfilesArePreserved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURALIS_APP_DIR", dir)
	file := filepath.Join(dir, "device_profiles.json")
	if err := os.WriteFile(file, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager()
	if _, err := mgr.AddFolderTarget("Export", t.TempDir()); err == nil {
		t.Fatal("corrupt settings were ignored")
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "broken" {
		t.Fatal("corrupt source settings were overwritten")
	}
}
