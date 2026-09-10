package backend

import "os"

func restrictPrivateFile(path string) error {
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	return restrictPrivateFileACL(path)
}
