// Package credentials stores secrets encrypted at rest. On Windows it uses
// DPAPI scoped to the current user; other platforms fail closed (no plaintext).
package credentials

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/vekhyat/Auralis/backend"
)

// ErrNotFound is returned by Get when the key has never been stored.
var ErrNotFound = errors.New("credentials: not found")

// Store is a directory of encrypted secret blobs, one file per key.
type Store struct {
	dir string
	mu  sync.Mutex
}

// Open returns the default store rooted at <app data dir>/credentials.
func Open() (*Store, error) {
	appDir, err := backend.EnsureAppDataDir()
	if err != nil {
		return nil, err
	}
	return OpenAt(filepath.Join(appDir, "credentials"))
}

// OpenAt returns a store rooted at dir. Pass a temp dir in tests.
func OpenAt(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("credentials: empty directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("credentials: create dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

var keySanitizer = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (s *Store) pathFor(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("credentials: empty key")
	}
	safe := keySanitizer.ReplaceAllString(key, "_")
	if len(safe) > 64 {
		sum := sha256.Sum256([]byte(key))
		safe = safe[:48] + "-" + hex.EncodeToString(sum[:8])
	}
	return filepath.Join(s.dir, safe+".bin"), nil
}

// Put encrypts value and stores it under key, replacing any previous value.
func (s *Store) Put(key, value string) error {
	path, err := s.pathFor(key)
	if err != nil {
		return err
	}
	blob, err := protect([]byte(value))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := backend.WriteFileAtomic(path, blob, 0o600); err != nil {
		return fmt.Errorf("credentials: write: %w", err)
	}
	if err := backend.RestrictPrivateFilePath(path); err != nil {
		fmt.Printf("credentials: harden %s: %v\n", path, err)
	}
	return nil
}

// Get returns the decrypted value stored under key.
func (s *Store) Get(key string) (string, error) {
	path, err := s.pathFor(key)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	blob, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("credentials: read: %w", err)
	}
	plain, err := unprotect(blob)
	if err != nil {
		return "", fmt.Errorf("credentials: decrypt: %w", err)
	}
	return string(plain), nil
}

// Delete removes key. Missing keys are not an error.
func (s *Store) Delete(key string) error {
	path, err := s.pathFor(key)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("credentials: delete: %w", err)
	}
	return nil
}
