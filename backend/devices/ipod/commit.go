package ipod

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
)

func commitBytes(path string, data []byte, verify func([]byte) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".auralis.tmp"
	bak := path + ".auralis.bak"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		return errors.Join(writeErr, syncErr, closeErr)
	}

	hadOriginal := false
	if _, err := os.Stat(path); err == nil {
		hadOriginal = true
		_ = os.Remove(bak)
		if err := os.Rename(path, bak); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		if hadOriginal {
			_ = os.Rename(bak, path)
		}
		_ = os.Remove(tmp)
		return err
	}

	read, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(read, data) || (verify != nil && verify(read) != nil) {
		if hadOriginal {
			_ = os.Remove(path)
			_ = os.Rename(bak, path)
		} else {
			_ = os.Remove(path)
		}
		if err == nil {
			err = errors.New("iTunesDB read-back did not match")
		}
		return err
	}
	return nil
}
