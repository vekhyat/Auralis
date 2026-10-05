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

	"github.com/vekhyat/Auralis/backend"
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
// ensuring that targetPath does not escape t.Root and does not pass
// through a symbolic link. A symlinked directory inside the root could
// otherwise be used to read, write, or delete files outside the root.
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
	if err := rejectSymlinkHops(cleanRoot, cleanTarget); err != nil {
		return "", err
	}
	return cleanTarget, nil
}

// rejectSymlinkHops fails when the target root itself, any ancestor of
// it, or any component between it and full path is a symbolic link or
// junction. That covers roots that are junctions
// (e.g. C:\Music -> D:\media\Music) as well as malicious components
// inside the root; both aliases of the PC library would silently shift
// every write outside the managed tree.
func rejectSymlinkHops(root, full string) error {
	// Root and its ancestors: walk upward until the filesystem root.
	for dir := root; ; {
		if err := checkNoLink(dir, "target root"); err != nil {
			return err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return err
	}
	if rel == "." || rel == "" {
		return nil
	}
	cur := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, filepath.FromSlash(part))
		info, err := os.Lstat(cur)
		if err != nil {
			// Nothing under a missing (or not-directory) component can
			// exist yet; a dangling link would have been caught above.
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path %q passes through symlink %q", full, cur)
		}
	}
	return nil
}

func checkNoLink(dir, what string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s %q is a symlink or junction", what, dir)
	}
	return nil
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
		if d.Type()&os.ModeSymlink != 0 {
			// Files and directories reached through links are not
			// managed: they may live outside the root. Leave them
			// untouched instead of shadowing real entries.
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
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
// cumulative byte count after every 64 KiB chunk. Transfers through
// uniquely named temp files in the destination directory, so a crash
// never leaves a truncated track at the final name.
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
	// Stage under a fresh, uniquely named temp file: a fixed temp path
	// could be planted as a symlink to somewhere outside the root, and
	// deleting/truncating it would touch a user-created file.
	tmp, err := os.CreateTemp(filepath.Dir(dstPath), ".auralis-copy-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := copyWithProgress(ctx, tmp, src, progress); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// renameInto overwrites an existing managed file without the failure
	// mode of a missing mid-state; our unique temp owns the data either way.
	if err := renameInto(tmpName, dstPath); err != nil {
		return err
	}
	return nil
}

// renameInto replaces a managed destination atomically, including on Windows.
// The old file remains intact if the replacement fails.
func renameInto(src, dst string) error {
	return backend.MoveFileReplace(src, dst)
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
	// Refuse to land on a dangling link at the destination.
	if _, err := os.Lstat(toPath); err == nil {
		return fmt.Errorf("move destination %q already exists", to)
	}
	if err := os.MkdirAll(filepath.Dir(toPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(fromPath, toPath); err == nil {
		return nil
	}
	// Cross-volume fallback: copy via a temp name and only replace the
	// source once the copy has landed completely. Partial output must
	// never appear under the final name.
	src, err := os.Open(fromPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.CreateTemp(filepath.Dir(toPath), ".auralis-copy-*")
	if err != nil {
		return err
	}
	tmpName := dst.Name()
	defer os.Remove(tmpName)
	cerr := copyWithProgress(ctx, dst, src, nil)
	if werr := dst.Close(); cerr == nil {
		cerr = werr
	}
	if cerr == nil && ctx.Err() != nil {
		cerr = ctx.Err()
	}
	if cerr != nil {
		return cerr
	}
	if err := renameInto(tmpName, toPath); err != nil {
		return err
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
