//go:build !windows

package massstorage

// RemovableDrives returns mounted removable drives on non-Windows platforms (stub).
func RemovableDrives() ([]Drive, error) {
	return nil, nil
}
