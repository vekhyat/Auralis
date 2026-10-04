//go:build windows

package massstorage

import "golang.org/x/sys/windows"

// freeSpace reports bytes available to the current user at dir.
func freeSpace(dir string) (int64, error) {
	ptr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &avail, &total, &free); err != nil {
		return 0, err
	}
	if avail == 0 {
		avail = free
	}
	return int64(avail), nil
}
