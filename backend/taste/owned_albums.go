package taste

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// AlbumFilter reports whether an album by artist is already in the library.
type AlbumFilter func(artist, album string) bool

// MatchKey folds a name for loose matching across sources and folder names:
// case, punctuation and bracketed suffixes such as "(Remastered 2011)" or
// "[Deluxe]" are ignored.
func MatchKey(s string) string {
	if i := strings.IndexAny(s, "(["); i > 0 {
		s = s[:i]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FolderAlbumIndex answers "is this album downloaded?" from the library's
// folder names. Auralis files albums as {album_artist}/{album} by default,
// so an album folder under its artist's folder is a reliable signal without
// reading tags. The walk is cached for TTL.
type FolderAlbumIndex struct {
	Root string
	TTL  time.Duration

	mu       sync.Mutex
	builtAt  time.Time
	pairs    map[string]bool // artistKey + "/" + albumKey
	topLevel map[string]bool // album folders directly under the root
}

var audioExtensions = map[string]bool{".flac": true, ".mp3": true, ".m4a": true, ".ogg": true, ".opus": true, ".wav": true}

// holdsAudio tells album folders from artist folders at the top level, so a
// self-titled album is not mistaken for its artist's folder.
func holdsAudio(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && audioExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			return true
		}
	}
	return false
}

// Owned implements AlbumFilter.
func (x *FolderAlbumIndex) Owned(artist, album string) bool {
	albumKey := MatchKey(album)
	if albumKey == "" {
		return false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.pairs == nil || time.Since(x.builtAt) > x.ttl() {
		x.build()
	}
	return x.pairs[MatchKey(artist)+"/"+albumKey] || x.topLevel[albumKey]
}

func (x *FolderAlbumIndex) ttl() time.Duration {
	if x.TTL > 0 {
		return x.TTL
	}
	return 5 * time.Minute
}

// build walks at most three folder levels (artist/album/disc).
func (x *FolderAlbumIndex) build() {
	x.pairs = map[string]bool{}
	x.topLevel = map[string]bool{}
	x.builtAt = time.Now()
	if strings.TrimSpace(x.Root) == "" {
		return
	}
	var walk func(dir, parentKey string, depth int)
	walk = func(dir, parentKey string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			key := MatchKey(entry.Name())
			if key == "" {
				continue
			}
			if depth == 1 {
				if holdsAudio(filepath.Join(dir, entry.Name())) {
					x.topLevel[key] = true
				}
			} else {
				x.pairs[parentKey+"/"+key] = true
			}
			if depth < 3 {
				walk(filepath.Join(dir, entry.Name()), key, depth+1)
			}
		}
	}
	walk(x.Root, "", 1)
}
