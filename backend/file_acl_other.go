//go:build !windows

package backend

func restrictPrivateFileACL(path string) error {
	return nil
}
