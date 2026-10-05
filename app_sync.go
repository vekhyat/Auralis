package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vekhyat/Auralis/backend"
	"github.com/vekhyat/Auralis/backend/devices"
	"github.com/vekhyat/Auralis/backend/devices/adb"
	"github.com/vekhyat/Auralis/backend/library"
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

func getLocalMusicPath() (string, error) {
	settings, err := backend.LoadConfigSettings()
	if err != nil {
		return "", fmt.Errorf("load library settings: %w", err)
	}
	if err == nil && settings != nil {
		if p, ok := settings["downloadPath"].(string); ok && p != "" {
			return p, nil
		}
	}
	return backend.GetDefaultMusicPath(), nil
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
	mgr := a.getSyncManager()
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	if a.activeSyncCancel != nil && a.activeSyncTargetID == id {
		return errors.New("cancel the active sync before removing its target")
	}
	if err := mgr.RemoveFolderTarget(id); err != nil {
		return err
	}
	if a.activeSyncPlanTargetID == id {
		a.activeSyncSession = nil
		a.activeSyncPlan = nil
		a.activeSyncPlanTargetID = ""
	}
	return nil
}

// GetDeviceProfile returns the persisted settings profile for a target.
func (a *App) GetDeviceProfile(id string) (devices.DeviceProfile, error) {
	return a.getSyncManager().GetProfile(id), nil
}

// SaveDeviceProfile saves the profile settings for a target.
func (a *App) SaveDeviceProfile(profile devices.DeviceProfile) error {
	mgr := a.getSyncManager()
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	if a.activeSyncCancel != nil {
		return errors.New("wait for the active sync to finish before changing settings")
	}
	if err := mgr.SaveProfile(profile); err != nil {
		return err
	}
	a.activeSyncSession = nil
	a.activeSyncPlan = nil
	return nil
}

func targetIDHash(id string) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:8])
}

// syncSession binds a preview and its recovery record to the original target
// root, profile and playlists. A preview alone is never a resumable job.
type syncSession struct {
	TargetID       string                      `json:"target_id"`
	Root           string                      `json:"root"`
	Profile        devices.DeviceProfile       `json:"profile"`
	Snapshot       *syncengine.PlanSnapshot    `json:"snapshot"`
	Playlists      []syncengine.DevicePlaylist `json:"playlists"`
	ManifestDigest string                      `json:"manifest_digest"`
	FilesDone      bool                        `json:"files_done"`
}

func manifestDigest(m *syncengine.Manifest) string {
	data, _ := json.Marshal(m)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// PlanSync scans the local library and target device to compute a preview.
func (a *App) PlanSync(id string) (*syncengine.Plan, error) {
	mgr := a.getSyncManager()
	a.syncMu.Lock()
	busy := a.activeSyncCancel != nil
	a.activeSyncSession = nil
	a.activeSyncPlan = nil
	a.syncMu.Unlock()
	if busy {
		return nil, errors.New("a sync job is already in progress")
	}
	target, libProfile, policy, err := mgr.ResolveTarget(id)
	if err != nil {
		return nil, err
	}
	deviceProfile := mgr.GetProfile(id)
	localPath, err := getLocalMusicPath()
	if err != nil {
		return nil, err
	}
	localDir, err := filepath.Abs(localPath)
	if err != nil {
		return nil, err
	}
	root := target.Info().Root
	if !strings.HasPrefix(id, "adb:") && pathsOverlap(localDir, filepath.FromSlash(root)) {
		return nil, errors.New("the sync folder must be separate from the source library")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tracks, err := devices.ScanLibraryTracks(ctx, localDir)
	if err != nil {
		return nil, fmt.Errorf("scan library: %w", err)
	}
	playlistPaths, err := devices.LoadLibraryPlaylists(ctx, localDir, deviceProfile.SelectionRules)
	if err != nil {
		return nil, err
	}
	selected := deviceProfile.SelectionRules.Select(tracks, playlistPaths, time.Now())
	free, err := target.FreeSpace(ctx)
	if err != nil {
		return nil, fmt.Errorf("check free space: %w", err)
	}
	manifest, remoteEntries, err := readDeviceState(ctx, target)
	if err != nil {
		return nil, err
	}
	// Reconcile the recorded library with the device listing. Missing or
	// changed managed files are updates the user can see in the preview.
	planningManifest := manifest
	if manifest != nil {
		planningManifest = syncengine.NewManifest(manifest.ProfileID)
		sizes := map[string]int64{}
		for _, entry := range remoteEntries {
			sizes[entry.Path] = entry.Size
		}
		for rel, entry := range manifest.Entries {
			size, exists := sizes[syncengine.JoinRemote(root, rel)]
			if !exists || size != entry.Size {
				entry.Hash = ""
			}
			planningManifest.Set(entry)
		}
	}
	plan := syncengine.PlanSelect(syncengine.PlanOptions{
		Tracks: selected, Profile: libProfile, ProfileVersion: 1, Policy: policy, Manifest: planningManifest, FreeBytes: free,
	})
	session := &syncSession{TargetID: id, Root: root, Profile: deviceProfile,
		Snapshot: syncengine.NewPlanSnapshot(plan), ManifestDigest: manifestDigest(manifest)}
	// Use the planner's final collision-resolved paths, rather than rebuilding
	// paths separately and risking playlists pointing to the wrong file.
	bySource := map[string]syncengine.PlaylistTrack{}
	for _, op := range plan.Ops {
		if op.Track == nil || op.Kind == syncengine.OpDelete {
			continue
		}
		tr := op.Track
		bySource[tr.Path] = syncengine.PlaylistTrack{Rel: op.Remote, Title: tr.Title, Artist: tr.Artist, DurationSec: tr.DurationSec}
	}
	names := make([]string, 0, len(playlistPaths))
	for name := range playlistPaths {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		pl := syncengine.DevicePlaylist{Name: strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))}
		for _, source := range playlistPaths[name] {
			if tr, ok := bySource[source]; ok {
				pl.Tracks = append(pl.Tracks, tr)
			}
		}
		session.Playlists = append(session.Playlists, pl)
	}
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	if a.activeSyncCancel != nil {
		return nil, errors.New("a sync job is already in progress")
	}
	a.activeSyncPlan, a.activeSyncPlanTargetID, a.activeSyncSession = plan, id, session
	return plan, nil
}

func pathsOverlap(a, b string) bool {
	contains := func(root, other string) bool {
		rel, err := filepath.Rel(root, other)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return contains(a, b) || contains(b, a)
}

func syncSessionPath(id string) (string, error) {
	dir, err := backend.GetAppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sync_plans", targetIDHash(id)+".json"), nil
}

func (a *App) StartSync(id string) error { return a.startSync(id, false) }

func (a *App) startSync(id string, resume bool) error {
	mgr := a.getSyncManager()
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	if a.activeSyncCancel != nil {
		return errors.New("a sync job is already in progress")
	}
	session := a.activeSyncSession
	if session == nil || session.TargetID != id {
		return errors.New("preview the sync before starting it")
	}
	target, libProfile, policy, err := mgr.ResolveTarget(id)
	if err != nil {
		return err
	}
	if target.Info().Root != session.Root {
		return errors.New("target folder changed; preview the sync again")
	}
	current, _ := json.Marshal(mgr.GetProfile(id))
	original, _ := json.Marshal(session.Profile)
	if !resume && string(current) != string(original) {
		return errors.New("device settings changed; preview the sync again")
	}
	// Recovery always uses the policy and layout that were approved initially.
	libProfile = library.ProfileByID(session.Profile.ProfileID)
	policy = session.Profile.FormatPolicy
	ctx, cancel := context.WithCancel(context.Background())
	checkCtx, checkCancel := context.WithTimeout(ctx, 30*time.Second)
	manifest, err := readDeviceManifest(checkCtx, target)
	checkCancel()
	if err != nil {
		cancel()
		return err
	}
	if !resume && manifestDigest(manifest) != session.ManifestDigest {
		cancel()
		return errors.New("device manifest changed; preview the sync again")
	}
	if manifest == nil {
		manifest = syncengine.NewManifest(libProfile.ID)
	}
	manifest.ProfileID = libProfile.ID
	plan := session.Snapshot.Plan()
	if plan.Hash() != session.Snapshot.Hash {
		cancel()
		return errors.New("sync snapshot failed its integrity check; preview the sync again")
	}
	if !resume && plan.Insufficient {
		cancel()
		return errors.New("not enough free space for this sync")
	}
	appDir, err := backend.GetAppDir()
	if err != nil {
		cancel()
		return err
	}
	snapshotPath, err := syncSessionPath(id)
	if err != nil {
		cancel()
		return err
	}
	if !resume {
		data, err := json.MarshalIndent(session, "", "  ")
		if err != nil {
			cancel()
			return err
		}
		if err := backend.WriteFileAtomic(snapshotPath, data, 0o600); err != nil {
			cancel()
			return fmt.Errorf("save sync job: %w", err)
		}
	}
	a.activeSyncCancel, a.activeSyncTargetID = cancel, id
	go func() {
		defer cancel()
		defer func() { a.syncMu.Lock(); a.activeSyncCancel = nil; a.activeSyncTargetID = ""; a.syncMu.Unlock() }()
		cache := syncengine.NewTranscodeCache(filepath.Join(appDir, "transcode_cache"))
		executor := &syncengine.Executor{Target: target, Root: session.Root, Profile: libProfile,
			ProfileVersion: 1, Policy: policy, JournalPath: filepath.Join(appDir, "sync_journals", targetIDHash(id+"|"+session.Root+"|"+libProfile.ID+"|"+policy.SettingsKey())+".json"),
			Concurrency: 2, Materialize: func(ctx context.Context, tr *syncengine.SourceTrack) (string, error) {
				return cache.Materialize(ctx, tr, policy)
			},
			OnProgress: func(p syncengine.Progress) {
				if p.Phase != "done" {
					a.emitSyncProgress(p)
				}
			},
		}
		var err error
		if !session.FilesDone {
			err = executor.Run(ctx, plan, manifest)
			if err == nil {
				session.FilesDone = true
				var data []byte
				data, err = json.MarshalIndent(session, "", "  ")
				if err == nil {
					err = backend.WriteFileAtomic(snapshotPath, data, 0o600)
				}
			}
		}
		if err == nil {
			err = syncengine.WriteDevicePlaylists(ctx, target, session.Root, libProfile.PlaylistDir, session.Playlists, manifest, "")
		}
		if err == nil {
			err = target.Commit(ctx)
		}
		if err == nil {
			prof := mgr.GetProfile(id)
			prof.LastSyncTime, prof.LastManifestVersion = time.Now().Unix(), manifest.Version
			err = mgr.SaveProfile(prof)
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				a.emitSyncProgress(syncengine.Progress{Phase: "cancelled"})
			} else {
				a.emitSyncProgress(syncengine.Progress{Phase: "error", Error: err.Error()})
			}
			return
		}
		if err := os.Remove(snapshotPath); err != nil && !os.IsNotExist(err) {
			a.emitSyncProgress(syncengine.Progress{Phase: "error", Error: err.Error()})
			return
		}
		a.syncMu.Lock()
		if a.activeSyncSession == session {
			a.activeSyncSession = nil
			a.activeSyncPlan = nil
			a.activeSyncPlanTargetID = ""
		}
		a.syncMu.Unlock()
		a.emitSyncProgress(syncengine.Progress{Phase: "done", OpTotal: len(plan.Ops), Done: len(plan.Ops), Skipped: len(executor.Skipped)})
	}()
	return nil
}

// A corrupt or unreadable manifest is an error, never an empty library.
func readDeviceManifest(ctx context.Context, target syncengine.SyncTarget) (*syncengine.Manifest, error) {
	manifest, _, err := readDeviceState(ctx, target)
	return manifest, err
}

func readDeviceState(ctx context.Context, target syncengine.SyncTarget) (*syncengine.Manifest, []syncengine.RemoteEntry, error) {
	entries, err := target.List(ctx, target.Info().Root)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("list device: %w", err)
		}
		return nil, entries, nil
	}
	manifestPath := syncengine.JoinRemote(target.Info().Root, syncengine.ManifestPathRel)
	found := false
	for _, entry := range entries {
		if entry.Path == manifestPath {
			found = true
			break
		}
	}
	if !found {
		return nil, entries, nil
	}
	opener, ok := target.(syncengine.Open)
	if !ok {
		return nil, nil, errors.New("target cannot read its manifest")
	}
	rc, err := opener.Open(ctx, manifestPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read device manifest: %w", err)
	}
	defer rc.Close()
	m, err := syncengine.ReadManifest(rc)
	if err != nil {
		return nil, nil, err
	}
	if m.Version != syncengine.ManifestVersion {
		return nil, nil, fmt.Errorf("unsupported device manifest version %d", m.Version)
	}
	return m, entries, nil
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
	snapshotPath, err := syncSessionPath(id)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return fmt.Errorf("load interrupted sync: %w", err)
	}
	var session syncSession
	if err := json.Unmarshal(data, &session); err != nil {
		return err
	}
	if session.TargetID != id || session.Snapshot == nil {
		return errors.New("invalid interrupted sync record")
	}
	a.syncMu.Lock()
	if a.activeSyncCancel != nil {
		a.syncMu.Unlock()
		return errors.New("a sync job is already in progress")
	}
	a.activeSyncSession = &session
	a.syncMu.Unlock()
	return a.startSync(id, true)
}

// HasInterruptedSync reports only jobs that were explicitly started.
func (a *App) HasInterruptedSync(id string) bool {
	p, err := syncSessionPath(id)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
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

// SelectSyncPlaylist uses localized UI labels supplied by the sync panel.
func (a *App) SelectSyncPlaylist(title, filterLabel string) (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   title,
		Filters: []runtime.FileFilter{{DisplayName: filterLabel, Pattern: "*.m3u8"}},
	})
}
