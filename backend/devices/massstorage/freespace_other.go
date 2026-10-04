//go:build !windows

package massstorage

import "golang.org/x/sys/unix"

// freeSpace reports bytes available to the current user at dir.
func freeSpace(dir string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}
