package ipod

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// restoreRename publishes a rolled-back database. Tests replace it to force
// the restore itself to fail after the new bytes have been kept aside.
var restoreRename = os.Rename

// statFile is os.Stat for the live database path. Tests return an error other
// than ErrNotExist when the process token can still stat a locked-down file.
var statFile = os.Stat

// commitState reports whether path now holds the bytes passed to commitBytes.
// uncertain means the previous bytes could not be proven back in place.
type commitState struct {
	published   bool
	hadOriginal bool
	uncertain   bool
}

func commitBytes(path string, data []byte, verify func([]byte) error) (commitState, error) {
	var state commitState
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return state, err
	}
	tmp := path + ".auralis.tmp"
	bak := path + ".auralis.bak"
	info, statErr := statFile(path)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return state, statErr
	}
	if statErr == nil && info.IsDir() {
		return state, fmt.Errorf("%s is a directory", filepath.Base(path))
	}
	if err := writeSynced(tmp, data); err != nil {
		_ = os.Remove(tmp)
		return state, err
	}
	if err := verifyFile(tmp, data, verify); err != nil {
		_ = os.Remove(tmp)
		return state, err
	}

	if statErr == nil {
		state.hadOriginal = true
		_ = os.Remove(bak)
		if err := os.Rename(path, bak); err != nil {
			_ = os.Remove(tmp)
			return state, err
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		if state.hadOriginal {
			if rbErr := os.Rename(bak, path); rbErr != nil {
				state.uncertain = true
				return state, fmt.Errorf("replace %s: %w; original restore: %v", filepath.Base(path), err, rbErr)
			}
			if _, rbErr := os.Stat(path); rbErr != nil {
				state.uncertain = true
				return state, fmt.Errorf("replace %s: %w; restored file is missing: %v", filepath.Base(path), err, rbErr)
			}
		} else {
			_ = os.Remove(tmp)
		}
		return state, err
	}
	state.published = true
	if err := verifyFile(path, data, verify); err != nil {
		if !state.hadOriginal {
			_ = os.Remove(path)
			state.published = false
			if fileExists(path) {
				state.uncertain = true
				return state, fmt.Errorf("%w; new file could not be removed", err)
			}
			return state, err
		}
		original, readErr := os.ReadFile(bak)
		if readErr != nil {
			state.uncertain = true
			return state, fmt.Errorf("%w; backup unreadable: %v", err, readErr)
		}
		if rbErr := replaceVerified(path, original); rbErr != nil {
			state.uncertain = true
			return state, fmt.Errorf("%w; restore: %v", err, rbErr)
		}
		state.published = false
		return state, err
	}
	return state, nil
}

// rollbackPublished puts path back to the bytes from before a published commit.
// A missing backup is a failure: the new file is left in place.
func rollbackPublished(path string, state commitState) error {
	if !state.published {
		return nil
	}
	if !state.hadOriginal {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if fileExists(path) {
			return fmt.Errorf("rollback left %s in place", filepath.Base(path))
		}
		return nil
	}
	bak := path + ".auralis.bak"
	original, err := os.ReadFile(bak)
	if err != nil {
		return fmt.Errorf("read database backup: %w", err)
	}
	if err := replaceVerified(path, original); err != nil {
		return err
	}
	read, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(read, original) {
		return errors.New("rollback read-back did not match the backup")
	}
	return nil
}

// replaceVerified writes data over path without destroying path+".auralis.bak".
// If the swap cannot be finished, the previous path contents are put back when
// that is still possible.
func replaceVerified(path string, data []byte) error {
	tmp := path + ".auralis.rollback"
	if err := writeSynced(tmp, data); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := verifyFile(tmp, data, nil); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	failed := path + ".auralis.failed"
	_ = os.Remove(failed)
	if err := restoreRename(path, failed); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := restoreRename(tmp, path); err != nil {
		if backErr := restoreRename(failed, path); backErr != nil {
			return fmt.Errorf("publish rollback: %w; restore new database: %v", err, backErr)
		}
		_ = os.Remove(tmp)
		return err
	}
	if err := verifyFile(path, data, nil); err != nil {
		_ = os.Remove(path)
		if backErr := restoreRename(failed, path); backErr != nil {
			return fmt.Errorf("rollback read-back: %w; restore new database: %v", err, backErr)
		}
		return err
	}
	_ = os.Remove(failed)
	return nil
}

func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(writeErr, syncErr, closeErr)
	}
	return nil
}

func verifyFile(path string, data []byte, verify func([]byte) error) error {
	read, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(read, data) {
		return errors.New("read-back did not match")
	}
	if verify != nil {
		return verify(read)
	}
	return nil
}
