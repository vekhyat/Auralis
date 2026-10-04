package backend

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path by flushing a temp file in the same
// directory and then replacing path. A crash mid-write leaves the previous
// file intact. Windows replacement also requests write-through semantics.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(dir, ".auralis-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if perm != 0 {
		if err := os.Chmod(tmpName, perm); err != nil {
			return fmt.Errorf("set permissions for %s: %w", path, err)
		}
	}
	if err := moveFileReplace(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	committed = true
	return nil
}

// MoveFileReplace moves src onto dst, replacing an existing destination file.
func MoveFileReplace(src, dst string) error {
	if filepath.Clean(src) == filepath.Clean(dst) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return moveFileReplace(src, dst)
}
