package devices

import (
	"github.com/vekhyat/Auralis/backend/library"
	"github.com/vekhyat/Auralis/backend/syncengine"
)

// DeviceProfile persists user settings for one sync device or folder.
type DeviceProfile struct {
	DeviceID            string                  `json:"device_id"`
	FriendlyName        string                  `json:"friendly_name"`
	TargetFolder        string                  `json:"target_folder"`
	ProfileID           string                  `json:"profile_id"` // "poweramp", "mediastore", "rockbox", "mediaserver", "custom"
	FormatPolicy        syncengine.FormatPolicy `json:"format_policy"`
	SelectionRules      syncengine.Selection    `json:"selection_rules"`
	LastManifestVersion int                     `json:"last_manifest_version"`
	LastSyncTime        int64                   `json:"last_sync_time"`
}

// FolderTarget represents a manually added sync directory (e.g. for Syncthing).
type FolderTarget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// SyncTargetView is the serialized structure passed to the frontend for display.
type SyncTargetView struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Kind       string        `json:"kind"` // "adb", "massstorage", "folder"
	Model      string        `json:"model"`
	Root       string        `json:"root"`
	Connected  bool          `json:"connected"`
	FreeBytes  int64         `json:"freeBytes"`
	TotalBytes int64         `json:"totalBytes"`
	Profile    DeviceProfile `json:"profile"`
}

// DefaultProfileFor returns a newly populated profile with sensible defaults.
func DefaultProfileFor(deviceID, kind, name, model, root string) DeviceProfile {
	friendly := name
	if friendly == "" {
		friendly = model
	}
	if friendly == "" {
		friendly = "Device (" + deviceID + ")"
	}

	targetFolder := root
	if kind == "adb" && targetFolder == "" {
		targetFolder = "/sdcard/Music"
	}

	return DeviceProfile{
		DeviceID:     deviceID,
		FriendlyName: friendly,
		TargetFolder: targetFolder,
		ProfileID:    library.ProfilePoweramp,
		FormatPolicy: syncengine.FormatPolicy{
			Mode: "opus",
		},
		SelectionRules: syncengine.Selection{
			WholeLibrary: true,
		},
	}
}
