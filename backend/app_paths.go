package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// appDataDirEnv overrides the ~/.auralis directory. Tests set it so persistence
// tests do not read or write the real user profile.
const appDataDirEnv = "AURALIS_APP_DIR"

func GetAppDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv(appDataDirEnv)); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("AURALIS_APP_DIR must be an absolute path")
		}
		return filepath.Clean(dir), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".auralis"), nil
}

func ResolveAppDataDir() (string, error) {
	return GetAppDir()
}

func IsolatedWebviewProfilePath() (string, error) {
	if strings.TrimSpace(os.Getenv(appDataDirEnv)) == "" {
		return "", nil
	}
	dir, err := GetAppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "webview"), nil
}

func EnsureAppDir() (string, error) {
	return EnsureAppDataDir()
}

func EnsureAppDataDir() (string, error) {
	dir, err := ResolveAppDataDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create app directory: %w", err)
	}
	return dir, nil
}
