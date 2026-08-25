//go:build windows

package backend

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

func RegisterAuralisProtocol() error {
	if err := registerURLProtocol("auralis", "Auralis Protocol"); err != nil {
		return err
	}
	// Zarz's browser challenge still returns grants on spotiflac://.
	if err := registerURLProtocol("spotiflac", "Auralis Protocol"); err != nil {
		fmt.Printf("Could not register spotiflac:// grant handler: %v\n", err)
	}
	return nil
}

func registerURLProtocol(scheme, description string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\`+scheme, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.SetStringValue("", "URL:"+description); err != nil {
		return err
	}
	if err := key.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}

	cmd, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\`+scheme+`\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer cmd.Close()
	return cmd.SetStringValue("", fmt.Sprintf(`"%s" "%s"`, exe, "%1"))
}
