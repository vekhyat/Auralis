//go:build windows

package massstorage

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

const driveRemovable = 2 // DRIVE_REMOVABLE

// RemovableDrives enumerates mounted removable drives (USB sticks, SD
// cards) and returns one Drive per drive letter.
func RemovableDrives() ([]Drive, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, err
	}
	var out []Drive
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		ptr, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		if windows.GetDriveType(ptr) != driveRemovable {
			continue
		}
		d := Drive{Root: root}
		var serial uint32
		name := make([]uint16, 261)
		err = windows.GetVolumeInformation(ptr, &name[0], uint32(len(name)), &serial, nil, nil, nil, 0)
		if err == nil {
			d.Name = strings.TrimSpace(windows.UTF16ToString(name))
			d.ID = fmt.Sprintf("usb:%08x", serial)
		} else {
			d.ID = "usb:" + strings.ToLower(root)
		}
		if free, err := freeSpace(root); err == nil {
			d.FreeBytes = free
		}
		var avail, total, fre uint64
		if err := windows.GetDiskFreeSpaceEx(ptr, &avail, &total, &fre); err == nil {
			d.TotalBytes = int64(total)
		}
		out = append(out, d)
	}
	return out, nil
}
