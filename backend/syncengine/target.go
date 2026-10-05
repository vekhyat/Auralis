// Package syncengine implements the one-way library→device sync engine:
// manifest diffing, plan preview, and a resumable executor. It is shared by
// every transport (mass storage, ADB, iPod) via the SyncTarget interface.
package syncengine

import (
	"context"
	"io"
	"path"
	"strings"
)

// DeviceInfo describes a sync destination.
type DeviceInfo struct {
	// ID is stable per physical device: ADB serial number or volume serial.
	ID string `json:"id"`
	// Name is a human label, e.g. "Pixel 8" or the volume label.
	Name string `json:"name"`
	// Kind is "adb", "removable", "folder" or "ipod".
	Kind string `json:"kind"`
	// Model is the reported model string, when known.
	Model string `json:"model"`
	// Root is the default music root on the device, e.g. "/sdcard/Music"
	// for ADB targets or an absolute local path for folders.
	Root       string `json:"root"`
	TotalBytes int64  `json:"total_bytes"`
	FreeBytes  int64  `json:"free_bytes"`
	Connected  bool   `json:"connected"`
}

// RemoteEntry is one file or directory reported by a target listing.
type RemoteEntry struct {
	// Path is the full path on the device, slash-separated.
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"` // unix seconds
	IsDir bool   `json:"is_dir"`
}

// SyncTarget is anything Auralis can sync to: a phone over ADB, a USB
// stick, a mounted drive, an export folder, or an iPod.
type SyncTarget interface {
	Info() DeviceInfo
	List(ctx context.Context, root string) ([]RemoteEntry, error)
	Put(ctx context.Context, localPath, remotePath string, progress func(int64)) error
	Move(ctx context.Context, from, to string) error
	Delete(ctx context.Context, remotePath string) error
	FreeSpace(ctx context.Context) (int64, error)
	Commit(ctx context.Context) error // iPod: write iTunesDB; Android: trigger rescan
}

// StatSize is implemented by targets that can report a remote file size.
// The executor uses it to verify copies. Targets that cannot implement it
// skip the verification step.
type StatSize interface {
	StatSize(ctx context.Context, remotePath string) (int64, error)
}

// Open is implemented by targets that can stream a remote file back.
// The planner uses it to read the on-device manifest.
type Open interface {
	Open(ctx context.Context, remotePath string) (io.ReadCloser, error)
}

// JoinRemote joins a remote root with a relative slash path.
func JoinRemote(root, rel string) string {
	root = strings.TrimRight(root, "/")
	rel = strings.TrimLeft(rel, "/")
	if root == "" {
		return "/" + rel
	}
	if rel == "" {
		return root
	}
	return root + "/" + rel
}

// ValidateRemoteRel reports whether rel is a safe root-relative slash path.
// It rejects empty paths, absolute paths, backslashes, and any "." or ".."
// component, so a validated rel joined under any root stays managed.
func ValidateRemoteRel(rel string) error {
	if rel == "" {
		return errUnsafeRel("empty path")
	}
	if strings.Contains(rel, "\\") {
		return errUnsafeRel("backslash in path")
	}
	if path.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return errUnsafeRel("absolute path")
	}
	cleaned := path.Clean(rel)
	if cleaned != rel {
		return errUnsafeRel("unclean path")
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return errUnsafeRel("dot component")
		}
	}
	return nil
}

type unsafeRelError struct{ msg string }

func errUnsafeRel(msg string) error { return &unsafeRelError{msg: msg} }

func (e *unsafeRelError) Error() string { return "unsafe remote path: " + e.msg }

// IsUnsafeRel reports whether err came from ValidateRemoteRel.
func IsUnsafeRel(err error) bool {
	_, ok := err.(*unsafeRelError)
	return ok
}

// JoinRemoteChecked joins root with rel after validating rel, guaranteeing
// the result stays under root. It is the only way the executor addresses
// the device.
func JoinRemoteChecked(root, rel string) (string, error) {
	if err := ValidateRemoteRel(rel); err != nil {
		return "", err
	}
	return JoinRemote(root, rel), nil
}

// RemoteRel strips the music root prefix from an absolute remote path,
// returning the root-relative slash path. Paths outside the root are
// rejected so the engine never reasons about unmanaged files.
func RemoteRel(root, full string) (string, bool) {
	root = strings.TrimRight(root, "/")
	if full == root {
		return "", true
	}
	if strings.HasPrefix(full, root+"/") {
		return strings.TrimPrefix(full, root+"/"), true
	}
	return "", false
}
