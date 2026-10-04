//go:build !windows

package backend

import "os"

func moveFileReplace(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
