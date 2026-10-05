package syncengine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// hashSampleBytes is how much of each end of a file is hashed.
const hashSampleBytes = 64 * 1024

// HashSource computes a cheap, stable content hash of a library file.
//
// Choice: SHA-256 over (file size || mtime as provided by the caller ||
// first 64 KiB || last 64 KiB). The library scanner passes
// info.ModTime().UnixNano() and file size, so sub-second edits change the
// hash; tests may pass Unix seconds and stay consistent within their own
// runs. Full-file hashing was rejected because a first sync of a large
// library would read every byte twice (once for hashing, once for
// copying); the first/last 64 KiB window catches re-tags and re-encodes
// (tag edits touch the head on FLAC/MP3, new audio data changes the tail),
// while size+mtime catches everything else in practice. The hash is
// documented as content-identity for sync diffing, not for integrity
// verification — the executor verifies copied sizes directly.
func HashSource(path string, size int64, mtimeUnix int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sum := sha256.New()
	fmt.Fprintf(sum, "size:%d\nmtime:%d\n", size, mtimeUnix)

	head := make([]byte, hashSampleBytes)
	if n, err := io.ReadFull(f, head); err != nil && err != io.ErrUnexpectedEOF {
		return "", err
	} else {
		sum.Write(head[:n])
	}
	if _, err := f.Seek(-minInt64(hashSampleBytes, size), io.SeekEnd); err != nil {
		return "", err
	}
	tail := make([]byte, hashSampleBytes)
	if n, err := io.ReadFull(f, tail); err != nil && err != io.ErrUnexpectedEOF {
		return "", err
	} else {
		sum.Write(tail[:n])
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
