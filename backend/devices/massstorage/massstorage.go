// Package massstorage implements a SyncTarget over plain local paths:
// removable USB drives mounted as letters (Windows) or mount points, and
// plain export folders.
package massstorage

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/vekhyat/Auralis/backend/syncengine"
)

// Target is a sync destination backed by a local directory: a USB stick's
// music root, a mounted drive, or a plain export folder.
type Target struct {
	ID   string
	Name string
	Root string

	// Removable marks the target as a removable drive (Info.Kind
	// "removable"); otherwise it reports as a plain "folder".
	Removable bool
}

// New returns a Target rooted at root (the music root on this export side).
func New(id, name, root string) *Target {
	return &Target{ID: id, Name: name, Root: root}
}

var (
	_ syncengine.SyncTarget = (*Target)(nil)
	_ syncengine.StatSize   = (*Target)(nil)
	_ syncengine.Open       = (*Target)(nil)
)

// Info describes the target for the device picker.
func (t *Target) Info() syncengine.DeviceInfo {
	kind := "folder"
	if t.Removable {
		kind = "removable"
	}
	info := syncengine.DeviceInfo{
		ID:   t.ID,
		Name: t.Name,
		Kind: kind,
		Root: t.Root,
	}
	if free, err := freeSpace(t.Root); err == nil {
		info.FreeBytes = free
	}
	if _, err := os.Stat(t.Root); err == nil {
		info.Connected = true
	}
	return info
}

// List walks root and returns every file below it.
func (t *Target) List(ctx context.Context, root string) ([]syncengine.RemoteEntry, error) {
	var out []syncengine.RemoteEntry
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // file vanished mid-walk; skip
		}
		out = append(out, syncengine.RemoteEntry{
			Path:  filepath.ToSlash(path),
			Size:  info.Size(),
			Mtime: info.ModTime().Unix(),
			IsDir: false,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Put copies localPath to remotePath, creating parents, and reports the
// cumulative byte count after every 64 KiB chunk.
func (t *Target) Put(ctx context.Context, localPath, remotePath string, progress func(int64)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(remotePath), 0o755); err != nil {
		return err
	}
	src, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	buf := make([]byte, 64*1024)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			dst.Close()
			return err
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				dst.Close()
				return werr
			}
			copied += int64(n)
			if progress != nil {
				progress(copied)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			dst.Close()
			return rerr
		}
	}
	return dst.Close()
}

// Move renames from to to, falling back to copy+remove when the rename
// fails (cross-volume move or an existing destination on Windows).
func (t *Target) Move(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		src.Close()
		return err
	}
	_, cerr := io.Copy(dst, src)
	src.Close()
	if werr := dst.Close(); cerr == nil {
		cerr = werr
	}
	if cerr != nil {
		return cerr
	}
	return os.Remove(from)
}

// Delete removes remotePath.
func (t *Target) Delete(ctx context.Context, remotePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Remove(remotePath)
}

// FreeSpace reports bytes available at the target root.
func (t *Target) FreeSpace(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return freeSpace(t.Root)
}

// Commit is a no-op for plain folders/drives; nothing to flush.
func (t *Target) Commit(ctx context.Context) error { return nil }

// StatSize returns the size of the file at path.
func (t *Target) StatSize(ctx context.Context, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// Open streams the file at path back to the caller.
func (t *Target) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.Open(path)
}
