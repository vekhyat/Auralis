//go:build windows

package ipod

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows"
)

const (
	driveNoRoot = 1
	driveRemote = 4
	driveCDROM  = 5

	genericRead            = 0x80000000
	genericWrite           = 0x40000000
	ioctlStorageEjectMedia = 0x2D4808
	fsctlLockVolume        = 0x00090018
	fsctlDismountVolume    = 0x00090020
)

func listVolumes() []volumeInfo {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var out []volumeInfo
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		ptr, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		kind := windows.GetDriveType(ptr)
		if kind == driveNoRoot || kind == driveRemote || kind == driveCDROM {
			continue
		}
		vol := volumeInfo{Root: root}
		vol.Label, vol.FileSystem = volumeName(ptr)
		vol.Free, vol.Total = diskSpace(ptr)
		out = append(out, vol)
	}
	return out
}

func volumeName(root *uint16) (label, fs string) {
	name := make([]uint16, 261)
	system := make([]uint16, 64)
	err := windows.GetVolumeInformation(root, &name[0], uint32(len(name)), nil, nil, nil, &system[0], uint32(len(system)))
	if err != nil {
		return "", ""
	}
	return windows.UTF16ToString(name), windows.UTF16ToString(system)
}

func diskSpace(root *uint16) (free, total uint64) {
	var avail, all, freeAll uint64
	if err := windows.GetDiskFreeSpaceEx(root, &avail, &all, &freeAll); err != nil {
		return 0, 0
	}
	if avail == 0 {
		avail = freeAll
	}
	return avail, all
}

func freeBytes(root string) uint64 {
	if !strings.HasSuffix(root, `\`) {
		root += `\`
	}
	ptr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0
	}
	free, _ := diskSpace(ptr)
	return free
}

func hidePath(path string) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	_ = windows.SetFileAttributes(ptr, windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM)
}

func ejectVolume(root string) error {
	letter := strings.TrimRight(strings.TrimSpace(root), `:\`)
	if letter == "" {
		return errors.New("Windows did not eject the iPod. Use Safely Remove Hardware.")
	}
	path := `\\.\` + letter[:1] + `:`
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return errors.New("Windows did not eject the iPod. Use Safely Remove Hardware.")
	}
	handle, err := windows.CreateFile(ptr, genericRead|genericWrite, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return errors.New("Windows did not eject the iPod. Use Safely Remove Hardware.")
	}
	defer windows.CloseHandle(handle)
	var returned uint32
	_ = windows.DeviceIoControl(handle, fsctlLockVolume, nil, 0, nil, 0, &returned, nil)
	_ = windows.DeviceIoControl(handle, fsctlDismountVolume, nil, 0, nil, 0, &returned, nil)
	if err := windows.DeviceIoControl(handle, ioctlStorageEjectMedia, nil, 0, nil, 0, &returned, nil); err != nil {
		return errors.New("Windows did not eject the iPod. Use Safely Remove Hardware.")
	}
	return nil
}

type usbPod struct {
	name        string
	unsupported bool
}

func unmountedPods(mounted int) []Device {
	found := scanIPods()
	if len(found) <= mounted {
		return nil
	}
	extra := found[mounted:]
	out := make([]Device, 0, len(extra))
	for i, pod := range extra {
		id := "unmounted-ipod"
		if i > 0 {
			id = "unmounted-ipod-" + itoa(i+1)
		}
		dev := Device{
			ID:       id,
			Name:     pod.name,
			Mode:     "unreadable",
			Warning:  "no-drive",
			Checksum: "unknown",
		}
		if pod.unsupported {
			dev.Mode = "unsupported"
			dev.Warning = "unsupported"
		}
		if dev.Name == "" {
			dev.Name = "iPod"
		}
		out = append(out, dev)
	}
	return out
}

func scanIPods() []usbPod {
	handle, err := windows.SetupDiGetClassDevsEx(nil, "USB", 0, windows.DIGCF_PRESENT|windows.DIGCF_ALLCLASSES, 0, "")
	if err != nil {
		return nil
	}
	defer handle.Close()
	var found []usbPod
	for i := 0; i < 128; i++ {
		info, err := handle.EnumDeviceInfo(i)
		if err != nil {
			break
		}
		id, err := windows.SetupDiGetDeviceInstanceId(handle, info)
		if err != nil || !strings.Contains(strings.ToUpper(id), "VID_05AC") {
			continue
		}
		name := deviceString(handle, info, windows.SPDRP_FRIENDLYNAME)
		if name == "" {
			name = deviceString(handle, info, windows.SPDRP_DEVICEDESC)
		}
		if !strings.Contains(strings.ToLower(name), "ipod") {
			continue
		}
		lower := strings.ToLower(name)
		found = append(found, usbPod{
			name:        name,
			unsupported: strings.Contains(lower, "touch") || strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad"),
		})
	}
	return found
}

func deviceString(set windows.DevInfo, info *windows.DevInfoData, prop windows.SPDRP) string {
	value, err := windows.SetupDiGetDeviceRegistryProperty(set, info, prop)
	if err != nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
