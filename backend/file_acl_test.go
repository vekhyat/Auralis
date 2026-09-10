package backend

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRestrictPrivateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restrictPrivateFile(path); err != nil {
		t.Fatal(err)
	}
	if err := restrictPrivateFile(path); err != nil {
		t.Fatalf("second restrict: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestRestrictPrivateFileSurvivesGC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		runtime.GC()
		if err := restrictPrivateFile(path); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
}
