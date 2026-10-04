package devices

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/vekhyat/Auralis/backend"
	"github.com/vekhyat/Auralis/backend/syncengine"
)

type trackCacheEntry struct {
	track   syncengine.SourceTrack
	modNano int64
	size    int64
}

var (
	trackCacheMu sync.RWMutex
	trackCache   = make(map[string]trackCacheEntry)
)

func isAudioFile(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".flac", ".mp3", ".m4a", ".mp4", ".m4b", ".aac", ".wav", ".aiff", ".aif", ".ogg", ".opus", ".ape", ".wv", ".mpc":
		return true
	default:
		return false
	}
}

// ScanLibraryTracks walks rootDir and parses all audio files into syncengine.SourceTrack.
func ScanLibraryTracks(ctx context.Context, rootDir string) ([]syncengine.SourceTrack, error) {
	if rootDir == "" {
		return nil, fmt.Errorf("library root directory is empty")
	}

	var audioPaths []string
	var fileInfos []fs.FileInfo

	err := filepath.Walk(rootDir, func(p string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(p)
			if strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if isAudioFile(p) && info.Size() > 100*1024 {
			audioPaths = append(audioPaths, p)
			fileInfos = append(fileInfos, info)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(audioPaths) == 0 {
		return nil, nil
	}

	results := make([]syncengine.SourceTrack, len(audioPaths))
	var workers = runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}

	jobs := make(chan int, len(audioPaths))
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if ctx.Err() != nil {
					return
				}
				p := audioPaths[idx]
				info := fileInfos[idx]

				trackCacheMu.RLock()
				cached, ok := trackCache[p]
				trackCacheMu.RUnlock()

				if ok && cached.modNano == info.ModTime().UnixNano() && cached.size == info.Size() {
					results[idx] = cached.track
					continue
				}

				hash, hashErr := syncengine.HashSource(p, info.Size(), info.ModTime().UnixNano())
				if hashErr != nil {
					continue
				}

				meta, metaErr := backend.ExtractFullMetadataFromFile(p)
				tr := syncengine.SourceTrack{
					Path:        p,
					Size:        info.Size(),
					ModTime:     info.ModTime(),
					Hash:        hash,
					Ext:         strings.ToLower(filepath.Ext(p)),
					AddedAt:     info.ModTime(),
				}

				if metaErr == nil {
					tr.Title = meta.Title
					tr.Artist = meta.Artist
					tr.Album = meta.Album
					tr.AlbumArtist = meta.AlbumArtist
					if tr.AlbumArtist == "" {
						tr.AlbumArtist = meta.Artist
					}
					tr.Year = meta.Date
					tr.TrackNumber = meta.TrackNumber
					tr.DiscNumber = meta.DiscNumber
				}

				if tr.Title == "" {
					tr.Title = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
				}
				if tr.Artist == "" {
					tr.Artist = "Unknown Artist"
				}
				if tr.Album == "" {
					tr.Album = "Unknown Album"
				}
				if tr.AlbumArtist == "" {
					tr.AlbumArtist = tr.Artist
				}

				results[idx] = tr

				trackCacheMu.Lock()
				trackCache[p] = trackCacheEntry{
					track:   tr,
					modNano: info.ModTime().UnixNano(),
					size:    info.Size(),
				}
				trackCacheMu.Unlock()
			}
		}()
	}

	for i := range audioPaths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var valid []syncengine.SourceTrack
	for _, tr := range results {
		if tr.Path != "" && tr.Hash != "" {
			valid = append(valid, tr)
		}
	}
	return valid, nil
}
