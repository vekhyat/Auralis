package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vekhyat/Auralis/backend"
	"github.com/vekhyat/Auralis/backend/devices"
	"github.com/vekhyat/Auralis/backend/devices/adb"
	"github.com/vekhyat/Auralis/backend/syncengine"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) getSyncManager() *devices.Manager {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	if a.syncManager == nil {
		a.syncManager = devices.NewManager()
	}
	return a.syncManager
}

func (a *App) startSyncWatch() {
	mgr := a.getSyncManager()
	mgr.StartWatch(context.Background(), func(views []devices.SyncTargetView) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "devices:changed", views)
		}
	})
}

func (a *App) stopSyncWatch() {
	if a.syncManager != nil {
		a.syncManager.StopWatch()
	}
	a.CancelSync()
}

func getLocalMusicPath() string {
	settings, err := backend.LoadConfigSettings()
	if err == nil && settings != nil {
		if p, ok := settings["downloadPath"].(string); ok && p != "" {
			return p
		}
	}
	return backend.GetDefaultMusicPath()
}

// ListSyncTargets returns all enumerated sync targets (Android phones, removable USBs, folders).
func (a *App) ListSyncTargets() ([]devices.SyncTargetView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return a.getSyncManager().ListTargets(ctx)
}

// AddFolderTarget adds a custom folder sync target (e.g. for Syncthing).
func (a *App) AddFolderTarget(name, path string) (devices.SyncTargetView, error) {
	return a.getSyncManager().AddFolderTarget(name, path)
}

// RemoveFolderTarget removes a custom folder sync target.
func (a *App) RemoveFolderTarget(id string) error {
	return a.getSyncManager().RemoveFolderTarget(id)
}

// GetDeviceProfile returns the persisted settings profile for a target.
func (a *App) GetDeviceProfile(id string) (devices.DeviceProfile, error) {
	return a.getSyncManager().GetProfile(id), nil
}

// SaveDeviceProfile saves the profile settings for a target.
func (a *App) SaveDeviceProfile(profile devices.DeviceProfile) error {
	return a.getSyncManager().SaveProfile(profile)
}

func targetIDHash(id string) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:8])
}

// PlanSync scans the local library and target device to compute a preview sync plan.
func (a *App) PlanSync(id string) (*syncengine.Plan, error) {
	mgr := a.getSyncManager()
	target, libProfile, policy, err := mgr.ResolveTarget(id)
	if err != nil {
		return nil, err
	}

	appDir, _ := backend.GetAppDir()
	deviceProfile := mgr.GetProfile(id)

	localDir := getLocalMusicPath()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	allTracks, err := devices.ScanLibraryTracks(ctx, localDir)
	if err != nil {
		return nil, fmt.Errorf("failed to scan local library: %w", err)
	}

	selectedTracks := deviceProfile.SelectionRules.Select(allTracks, nil, time.Now())

	freeBytes, _ := target.FreeSpace(ctx)

	existingManifest := readDeviceManifest(ctx, target)

	plan := syncengine.PlanSelect(syncengine.PlanOptions{
		Tracks:         selectedTracks,
		Profile:        libProfile,
		ProfileVersion: 1,
		Policy:         policy,
		Manifest:       existingManifest,
		FreeBytes:      freeBytes,
	})

	// Persist plan snapshot for crash resilience / resumption
	if appDir != "" {
		planDir := filepath.Join(appDir, "sync_plans")
		_ = os.MkdirAll(planDir, 0o755)
		snapshotPath := filepath.Join(planDir, targetIDHash(id)+".json")
		snap := syncengine.NewPlanSnapshot(plan)
		_ = snap.Save(snapshotPath)
	}

	a.syncMu.Lock()
	a.activeSyncPlan = plan
	a.activeSyncPlanTargetID = id
	a.syncMu.Unlock()

	return plan, nil
}

// StartSync starts executing the sync plan asynchronously and emits "sync:progress" events.
func (a *App) StartSync(id string) error {
	a.syncMu.Lock()
	if a.activeSyncCancel != nil {
		a.syncMu.Unlock()
		return errors.New("a sync job is already in progress")
	}

	// Only run a plan the user has previewed: it may move files to trash.
	plan := a.activeSyncPlan
	if plan == nil || a.activeSyncPlanTargetID != id {
		a.syncMu.Unlock()
		return errors.New("preview the sync before starting it")
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.activeSyncCancel = cancel
	a.activeSyncTargetID = id
	a.syncMu.Unlock()

	go func() {
		defer func() {
			a.syncMu.Lock()
			a.activeSyncCancel = nil
			a.activeSyncTargetID = ""
			a.syncMu.Unlock()
		}()

		mgr := a.getSyncManager()
		target, libProfile, policy, err := mgr.ResolveTarget(id)
		if err != nil {
			a.emitSyncProgress(syncengine.Progress{Phase: "error", Error: err.Error()})
			return
		}

		appDir, _ := backend.GetAppDir()
		cacheDir := filepath.Join(appDir, "transcode_cache")
		cache := syncengine.NewTranscodeCache(cacheDir)

		journalDir := filepath.Join(appDir, "sync_journals")
		_ = os.MkdirAll(journalDir, 0o755)
		journalPath := filepath.Join(journalDir, targetIDHash(id)+".json")

		executor := &syncengine.Executor{
			Target:         target,
			Root:           target.Info().Root,
			Profile:        libProfile,
			ProfileVersion: 1,
			Policy:         policy,
			JournalPath:    journalPath,
			Concurrency:    2,
			HardDelete:     false,
			Materialize: func(ctx context.Context, tr *syncengine.SourceTrack) (string, error) {
				return cache.Materialize(ctx, tr, policy)
			},
			OnProgress: func(p syncengine.Progress) {
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "sync:progress", p)
				}
			},
		}

		manifest := readDeviceManifest(ctx, target)
		if manifest == nil {
			// First sync to this device.
			manifest = syncengine.NewManifest(libProfile.ID)
		}

		execErr := executor.Run(ctx, plan, manifest)
		if execErr != nil {
			if errors.Is(execErr, context.Canceled) {
				a.emitSyncProgress(syncengine.Progress{Phase: "cancelled"})
			} else {
				a.emitSyncProgress(syncengine.Progress{Phase: "error", Error: execErr.Error()})
			}
			return
		}

		// Update profile last sync time
		prof := mgr.GetProfile(id)
		prof.LastSyncTime = time.Now().Unix()
		_ = mgr.SaveProfile(prof)

		// The plan is spent: the next sync needs a fresh preview.
		if appDir != "" {
			_ = os.Remove(filepath.Join(appDir, "sync_plans", targetIDHash(id)+".json"))
		}
		a.syncMu.Lock()
		if a.activeSyncPlan == plan {
			a.activeSyncPlan = nil
			a.activeSyncPlanTargetID = ""
		}
		a.syncMu.Unlock()

		a.emitSyncProgress(syncengine.Progress{
			Phase:   "done",
			OpTotal: len(plan.Ops),
			Done:    len(plan.Ops),
			Skipped: len(executor.Skipped),
		})
		if a.ctx != nil {
			// Notify device changed to refresh views
			views, _ := mgr.ListTargets(context.Background())
			runtime.EventsEmit(a.ctx, "devices:changed", views)
		}
	}()

	return nil
}

// readDeviceManifest loads the manifest Auralis keeps on the device, or
// returns nil when the device has never been synced.
func readDeviceManifest(ctx context.Context, target syncengine.SyncTarget) *syncengine.Manifest {
	opener, ok := target.(syncengine.Open)
	if !ok {
		return nil
	}
	rc, err := opener.Open(ctx, syncengine.JoinRemote(target.Info().Root, syncengine.ManifestPathRel))
	if err != nil {
		return nil
	}
	defer rc.Close()
	manifest, err := syncengine.ReadManifest(rc)
	if err != nil {
		return nil
	}
	return manifest
}

func (a *App) emitSyncProgress(p syncengine.Progress) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "sync:progress", p)
	}
}

// CancelSync halts the active sync process.
func (a *App) CancelSync() {
	a.syncMu.Lock()
	cancel := a.activeSyncCancel
	a.syncMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ResumeSync resumes an interrupted sync from the recorded journal.
func (a *App) ResumeSync(id string) error {
	appDir, _ := backend.GetAppDir()
	snapshotPath := filepath.Join(appDir, "sync_plans", targetIDHash(id)+".json")

	snap, err := syncengine.LoadPlanSnapshot(snapshotPath)
	if err == nil && snap != nil {
		plan := snap.Plan()
		a.syncMu.Lock()
		a.activeSyncPlan = plan
		a.activeSyncPlanTargetID = id
		a.syncMu.Unlock()
	}

	return a.StartSync(id)
}

// DownloadPlatformTools downloads official Google platform-tools on demand.
func (a *App) DownloadPlatformTools() error {
	return adb.DownloadPlatformTools(context.Background(), func(percent int, status string) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "platform-tools:progress", map[string]interface{}{
				"percent": percent,
				"status":  status,
			})
		}
	})
}

// IsPlatformToolsInstalled checks if adb is ready to use.
func (a *App) IsPlatformToolsInstalled() bool {
	return adb.IsPlatformToolsInstalled()
}
