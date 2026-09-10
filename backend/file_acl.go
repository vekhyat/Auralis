package backend

import (
	"fmt"
	"os"
)

func restrictPrivateFile(path string) error {
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	if err := restrictPrivateFileACL(path); err != nil {
		fmt.Printf("private file ACL: %v\n", err)
	}
	return nil
}
