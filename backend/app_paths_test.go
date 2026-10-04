package backend

import (
	"path/filepath"
	"testing"
)

func TestAlternateAppDirectoryAlsoIsolatesWebviewProfile(t *testing.T) {
	t.Setenv(appDataDirEnv, "")
	profile, err := IsolatedWebviewProfilePath()
	if err != nil || profile != "" {
		t.Fatalf("default profile = %q, %v", profile, err)
	}
	dir := t.TempDir()
	t.Setenv(appDataDirEnv, dir)
	profile, err = IsolatedWebviewProfilePath()
	if err != nil || profile != filepath.Join(dir, "webview") {
		t.Fatalf("isolated profile = %q, %v", profile, err)
	}
	appDir, err := GetAppDir()
	if err != nil || appDir != dir {
		t.Fatalf("app data = %q, %v", appDir, err)
	}
	t.Setenv(appDataDirEnv, "relative-directory")
	if _, err := IsolatedWebviewProfilePath(); err == nil {
		t.Fatal("relative profile override was accepted")
	}
}
