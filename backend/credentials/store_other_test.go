//go:build !windows

package credentials

import "testing"

// The fallback build must compile and fail closed instead of storing plaintext.
func TestStoreFailsClosedOffWindows(t *testing.T) {
	store, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	if err := store.Put("k", "v"); err == nil {
		t.Fatalf("expected unsupported error on Put")
	}
}
