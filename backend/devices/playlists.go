package devices

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vekhyat/Auralis/backend/syncengine"
)

// LoadLibraryPlaylists resolves local M3U8 entries relative to their playlist.
// Remote URLs are ignored: only tracks in the scanned local library can sync.
func LoadLibraryPlaylists(ctx context.Context, root string, selection syncengine.Selection) (map[string][]string, error) {
	paths := append([]string(nil), selection.Playlists...)
	if selection.WholeLibrary {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() && p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if d.Type().IsRegular() && strings.EqualFold(filepath.Ext(p), ".m3u8") {
				paths = append(paths, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out := make(map[string][]string)
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.EqualFold(filepath.Ext(p), ".m3u8") {
			return nil, fmt.Errorf("playlist must be an .m3u8 file: %s", p)
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		p = filepath.Clean(p)
		if _, ok := out[p]; ok {
			continue
		}
		entries, err := readPlaylist(p)
		if err != nil {
			return nil, fmt.Errorf("read playlist %s: %w", p, err)
		}
		out[p] = entries
	}
	return out, nil
}

func readPlaylist(p string) ([]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1<<20)
	entries := []string{}
	for scan.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scan.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") || strings.Contains(line, "://") {
			continue
		}
		line = filepath.FromSlash(line)
		if !filepath.IsAbs(line) {
			line = filepath.Join(filepath.Dir(p), line)
		}
		entries = append(entries, filepath.Clean(line))
	}
	return entries, scan.Err()
}
