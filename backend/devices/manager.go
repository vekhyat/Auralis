package devices

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vekhyat/Auralis/backend"
	"github.com/vekhyat/Auralis/backend/devices/adb"
	"github.com/vekhyat/Auralis/backend/devices/massstorage"
	"github.com/vekhyat/Auralis/backend/library"
	"github.com/vekhyat/Auralis/backend/syncengine"
)

// Manager coordinates device detection, configuration persistence, and target resolution.
type Manager struct {
	adbClient     *adb.Client
	profilesPath  string
	foldersPath   string
	profiles      map[string]DeviceProfile
	folderTargets map[string]FolderTarget
	mu            sync.RWMutex
	watchStop     chan struct{}
	watchMu       sync.Mutex
}

// NewManager creates a Manager and loads stored configurations from the app directory.
func NewManager() *Manager {
	appDir, _ := backend.GetAppDir()
	m := &Manager{
		adbClient:     adb.NewClient(""),
		profilesPath:  filepath.Join(appDir, "device_profiles.json"),
		foldersPath:   filepath.Join(appDir, "folder_targets.json"),
		profiles:      make(map[string]DeviceProfile),
		folderTargets: make(map[string]FolderTarget),
	}
	m.load()
	return m
}

func (m *Manager) load() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if data, err := os.ReadFile(m.profilesPath); err == nil {
		var list []DeviceProfile
		if err := json.Unmarshal(data, &list); err == nil {
			for _, p := range list {
				m.profiles[p.DeviceID] = p
			}
		}
	}

	if data, err := os.ReadFile(m.foldersPath); err == nil {
		var list []FolderTarget
		if err := json.Unmarshal(data, &list); err == nil {
			for _, ft := range list {
				m.folderTargets[ft.ID] = ft
			}
		}
	}
}

func (m *Manager) saveLocked() {
	if m.profilesPath != "" {
		_ = os.MkdirAll(filepath.Dir(m.profilesPath), 0o755)
		list := make([]DeviceProfile, 0, len(m.profiles))
		for _, p := range m.profiles {
			list = append(list, p)
		}
		if data, err := json.MarshalIndent(list, "", "  "); err == nil {
			_ = os.WriteFile(m.profilesPath, data, 0o644)
		}
	}

	if m.foldersPath != "" {
		_ = os.MkdirAll(filepath.Dir(m.foldersPath), 0o755)
		list := make([]FolderTarget, 0, len(m.folderTargets))
		for _, ft := range m.folderTargets {
			list = append(list, ft)
		}
		if data, err := json.MarshalIndent(list, "", "  "); err == nil {
			_ = os.WriteFile(m.foldersPath, data, 0o644)
		}
	}
}

// ListTargets enumerates all currently available sync targets (ADB, removable, and folders).
func (m *Manager) ListTargets(ctx context.Context) ([]SyncTargetView, error) {
	var views []SyncTargetView

	// 1. ADB devices
	adbCtx, adbCancel := context.WithTimeout(ctx, 600*time.Millisecond)
	adbDevs, _ := m.adbClient.Devices(adbCtx)
	adbCancel()

	for _, d := range adbDevs {
		id := "adb:" + d.Serial
		profile := m.getOrCreateProfileLocked(id, "adb", d.Model, d.Model, "/sdcard/Music")
		connected := (d.State == "device")
		view := SyncTargetView{
			ID:        id,
			Name:      profile.FriendlyName,
			Kind:      "adb",
			Model:     d.Model,
			Root:      profile.TargetFolder,
			Connected: connected,
			Profile:   profile,
		}
		if connected {
			tgt := adb.NewTarget(m.adbClient, d.Serial, d.Model, profile.TargetFolder)
			fsCtx, fsCancel := context.WithTimeout(ctx, 500*time.Millisecond)
			if free, err := tgt.FreeSpace(fsCtx); err == nil {
				view.FreeBytes = free
			}
			fsCancel()
		}
		views = append(views, view)
	}

	// 2. Removable volumes
	drives, _ := massstorage.RemovableDrives()
	for _, dr := range drives {
		id := "drive:" + dr.Root
		defaultRoot := filepath.Join(dr.Root, "Music")
		profile := m.getOrCreateProfileLocked(id, "massstorage", dr.Name, dr.Name, defaultRoot)
		views = append(views, SyncTargetView{
			ID:         id,
			Name:       profile.FriendlyName,
			Kind:       "massstorage",
			Model:      dr.Name,
			Root:       profile.TargetFolder,
			Connected:  true,
			FreeBytes:  dr.FreeBytes,
			TotalBytes: dr.TotalBytes,
			Profile:    profile,
		})
	}

	// 3. User-added folder targets
	m.mu.RLock()
	folders := make([]FolderTarget, 0, len(m.folderTargets))
	for _, f := range m.folderTargets {
		folders = append(folders, f)
	}
	m.mu.RUnlock()

	for _, f := range folders {
		id := "folder:" + f.ID
		profile := m.getOrCreateProfileLocked(id, "folder", f.Name, f.Name, f.Path)
		connected := false
		var freeBytes int64
		if info, err := os.Stat(f.Path); err == nil && info.IsDir() {
			connected = true
			tgt := massstorage.New(id, f.Name, f.Path)
			if fb, err := tgt.FreeSpace(ctx); err == nil {
				freeBytes = fb
			}
		}
		views = append(views, SyncTargetView{
			ID:        id,
			Name:      profile.FriendlyName,
			Kind:      "folder",
			Model:     f.Name,
			Root:      f.Path,
			Connected: connected,
			FreeBytes: freeBytes,
			Profile:   profile,
		})
	}

	return views, nil
}

func (m *Manager) getOrCreateProfileLocked(id, kind, name, model, root string) DeviceProfile {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p, ok := m.profiles[id]; ok {
		return p
	}
	def := DefaultProfileFor(id, kind, name, model, root)
	m.profiles[id] = def
	m.saveLocked()
	return def
}

// AddFolderTarget adds a manual sync folder target.
func (m *Manager) AddFolderTarget(name, path string) (SyncTargetView, error) {
	cleanPath := filepath.Clean(path)
	info, err := os.Stat(cleanPath)
	if err != nil || !info.IsDir() {
		return SyncTargetView{}, fmt.Errorf("invalid directory path: %s", path)
	}

	h := sha256.Sum256([]byte(cleanPath))
	folderID := hex.EncodeToString(h[:8])
	targetID := "folder:" + folderID

	if name == "" {
		name = filepath.Base(cleanPath)
	}

	ft := FolderTarget{
		ID:   folderID,
		Name: name,
		Path: cleanPath,
	}

	m.mu.Lock()
	m.folderTargets[folderID] = ft
	profile := DefaultProfileFor(targetID, "folder", name, name, cleanPath)
	m.profiles[targetID] = profile
	m.saveLocked()
	m.mu.Unlock()

	return SyncTargetView{
		ID:        targetID,
		Name:      name,
		Kind:      "folder",
		Model:     name,
		Root:      cleanPath,
		Connected: true,
		Profile:   profile,
	}, nil
}

// RemoveFolderTarget removes a manual folder target.
func (m *Manager) RemoveFolderTarget(id string) error {
	folderID := strings.TrimPrefix(id, "folder:")
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.folderTargets, folderID)
	delete(m.profiles, id)
	m.saveLocked()
	return nil
}

// GetProfile returns the profile for a device.
func (m *Manager) GetProfile(id string) DeviceProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.profiles[id]; ok {
		return p
	}
	return DefaultProfileFor(id, "unknown", "", "", "")
}

// SaveProfile updates and persists a device profile.
func (m *Manager) SaveProfile(p DeviceProfile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.profiles[p.DeviceID] = p
	m.saveLocked()
	return nil
}

// ResolveTarget creates a live syncengine.SyncTarget and returns its profile.
func (m *Manager) ResolveTarget(id string) (syncengine.SyncTarget, library.Profile, syncengine.FormatPolicy, error) {
	p := m.GetProfile(id)
	libProfile := library.ProfileByID(p.ProfileID)

	switch {
	case strings.HasPrefix(id, "adb:"):
		serial := strings.TrimPrefix(id, "adb:")
		root := p.TargetFolder
		if root == "" {
			root = adb.DefaultMusicRoot
		}
		tgt := adb.NewTarget(m.adbClient, serial, p.FriendlyName, root)
		return tgt, libProfile, p.FormatPolicy, nil

	case strings.HasPrefix(id, "drive:"):
		root := p.TargetFolder
		if root == "" {
			root = strings.TrimPrefix(id, "drive:")
		}
		tgt := massstorage.New(id, p.FriendlyName, root)
		tgt.Removable = true
		return tgt, libProfile, p.FormatPolicy, nil

	case strings.HasPrefix(id, "folder:"):
		root := p.TargetFolder
		if root == "" {
			folderID := strings.TrimPrefix(id, "folder:")
			m.mu.RLock()
			ft, ok := m.folderTargets[folderID]
			m.mu.RUnlock()
			if ok {
				root = ft.Path
			}
		}
		tgt := massstorage.New(id, p.FriendlyName, root)
		return tgt, libProfile, p.FormatPolicy, nil

	default:
		return nil, libProfile, p.FormatPolicy, fmt.Errorf("unknown target kind: %s", id)
	}
}

// StartWatch begins background polling for devices every few seconds.
func (m *Manager) StartWatch(ctx context.Context, onChange func([]SyncTargetView)) {
	m.watchMu.Lock()
	if m.watchStop != nil {
		m.watchMu.Unlock()
		return
	}
	m.watchStop = make(chan struct{})
	stop := m.watchStop
	m.watchMu.Unlock()

	go func() {
		var prevSig string
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		check := func() {
			views, err := m.ListTargets(ctx)
			if err != nil {
				return
			}
			sig := computeSignature(views)
			if sig != prevSig {
				prevSig = sig
				if onChange != nil {
					onChange(views)
				}
			}
		}

		check()
		for {
			select {
			case <-ticker.C:
				check()
			case <-stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// StopWatch halts the background polling.
func (m *Manager) StopWatch() {
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	if m.watchStop != nil {
		close(m.watchStop)
		m.watchStop = nil
	}
}

func computeSignature(views []SyncTargetView) string {
	var b strings.Builder
	for _, v := range views {
		b.WriteString(v.ID)
		b.WriteByte('|')
		if v.Connected {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
		b.WriteByte('|')
		b.WriteString(v.Root)
		b.WriteByte(';')
	}
	return b.String()
}
