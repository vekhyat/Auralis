//go:build !windows

package ipod

import "errors"

func listVolumes() []volumeInfo { return nil }

func freeBytes(root string) uint64 { return 0 }

func hidePath(path string) {}

func ejectVolume(root string) error {
	return errors.New("eject is only implemented on Windows")
}

func unmountedPods(mounted int) []Device { return nil }
