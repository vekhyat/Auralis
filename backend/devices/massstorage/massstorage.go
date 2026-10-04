// Package massstorage implements a SyncTarget over plain local paths:
// removable USB drives mounted as letters (Windows) or mount points, and
// plain export folders.
package massstorage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vekhyat/Auralis/backend/syncengine"
)

// Drive describes an enumerated removable volume or mounted drive.
type Drive struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Root       string `json:"root"`
	FreeBytes  int64  `json:"free_bytes"`
	TotalBytes int64  `json:"total_bytes"`
}

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

// resolve validates and returns the absolute local path for targetPath,
// ensuring that targetPath does not escape t.Root.
func (t *Target) resolve(targetPath string) (string, error) {
	if t.Root == "" {
		return "", fmt.Errorf("target root is empty")
	}
	cleanRoot := filepath.Clean(t.Root)
	cleanTarget := filepath.Clean(filepath.FromSlash(targetPath))
	if !filepath.IsAbs(cleanTarget) {
		cleanTarget = filepath.Clean(filepath.Join(cleanRoot, cleanTarget))
	}
	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes target root %q", targetPath, t.Root)
	}
	return cleanTarget, nil
}

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
		Root: filepath.ToSlash(t.Root),
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
	walkRoot := t.Root
	if root != "" {
		resolved, err := t.resolve(root)
		if err != nil {
			return nil, err
		}
		walkRoot = resolved
	}
	var out []syncengine.RemoteEntry
	err := filepath.WalkDir(walkRoot, func(path string, d os.DirEntry, err error) error {
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

// partSuffix marks an in-progress copy. The file only takes its real name
// once complete, so an interrupted sync never leaves a truncated track (or
// destroys the previous good copy during an update).
const partSuffix = ".auralis-part"

// Put copies localPath to remotePath, creating parents, and reports the
// cumulative byte count after every 64 KiB chunk.
func (t *Target) Put(ctx context.Context, localPath, remotePath string, progress func(int64)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dstPath, err := t.resolve(remotePath)
	if err != nil {
		return err
	}
	if dstPath == filepath.Clean(t.Root) {
		return fmt.Errorf("cannot put file onto root directory %q", t.Root)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	src, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer src.Close()
	partPath := dstPath + partSuffix
	dst, err := os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := copyWithProgress(ctx, dst, src, progress); err != nil {
		dst.Close()
		os.Remove(partPath)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(partPath)
		return err
	}
	if err := os.Rename(partPath, dstPath); err != nil {
		os.Remove(partPath)
		return err
	}
	return nil
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, progress func(int64)) error {
	buf := make([]byte, 64*1024)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			copied += int64(n)
			if progress != nil {
				progress(copied)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// Move renames from to to, falling back to copy+remove when the rename
// fails (e.g. across volumes). It refuses to replace an existing file.
func (t *Target) Move(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fromPath, err := t.resolve(from)
	if err != nil {
		return err
	}
	toPath, err := t.resolve(to)
	if err != nil {
		return err
	}
	if fromPath == filepath.Clean(t.Root) || toPath == filepath.Clean(t.Root) {
		return fmt.Errorf("cannot move target root %q", t.Root)
	}
	if _, err := os.Stat(toPath); err == nil {
		return fmt.Errorf("move destination %q already exists", to)
	}
	if err := os.MkdirAll(filepath.Dir(toPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(fromPath, toPath); err == nil {
		return nil
	}
	src, err := os.Open(fromPath)
	if err != nil {
		return err
	}
	dst, err := os.OpenFile(toPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
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
	return os.Remove(fromPath)
}

// Delete removes remotePath.
func (t *Target) Delete(ctx context.Context, remotePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dstPath, err := t.resolve(remotePath)
	if err != nil {
		return err
	}
	if dstPath == filepath.Clean(t.Root) {
		return fmt.Errorf("cannot delete root directory %q", t.Root)
	}
	return os.Remove(dstPath)
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
	resolved, err := t.resolve(path)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(resolved)
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
	resolved, err := t.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.Open(resolved)
}
