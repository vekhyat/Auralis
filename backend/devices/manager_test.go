package devices

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeviceManagerFoldersAndProfiles(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("AURALIS_APP_DIR", tmpDir)
	defer os.Unsetenv("AURALIS_APP_DIR")

	mgr := NewManager()

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
