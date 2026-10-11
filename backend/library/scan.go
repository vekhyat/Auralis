package library

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.senan.xyz/taglib"
)

// Audio extensions the doctor can read tags for. Ogg, Opus and WAV go through
// the same taglib calls as the rest; trackSet/discSet write Vorbis-comment
// style numbers for ogg and opus and "n/total" for the ID3-tagged formats.
var audioExtensions = map[string]bool{
	".flac": true, ".mp3": true, ".m4a": true,
	".ogg": true, ".opus": true, ".wav": true,
}

// Track is one scanned audio file with the tags Library Doctor needs.
type Track struct {
	Path            string  `json:"path"`
	RelPath         string  `json:"rel_path"`
	Format          string  `json:"format"`
	Title           string  `json:"title"`
	Artist          string  `json:"artist"`
	AlbumArtist     string  `json:"album_artist"`
	Album           string  `json:"album"`
	Year            string  `json:"year"`
	ISRC            string  `json:"isrc"`
	TrackNumber     int     `json:"track_number"`
	TrackTotal      int     `json:"track_total"`
	DiscNumber      int     `json:"disc_number"`
	DiscTotal       int     `json:"disc_total"`
	Compilation     bool    `json:"compilation"`
	DurationSeconds float64 `json:"duration_seconds"`
	BitDepth        int     `json:"bit_depth"`
	Size            int64   `json:"size"`
	ModTimeUnixNano int64   `json:"mod_time_unix_nano"`
	CoverBytes      int     `json:"cover_bytes"`
	CoverWidth      int     `json:"cover_width"`
	CoverHeight     int     `json:"cover_height"`
	FolderCover     bool    `json:"folder_cover"`
	LRC             bool    `json:"lrc"`
	TrackNumberRaw  string  `json:"track_number_raw"`
	DiscNumberRaw   string  `json:"disc_number_raw"`
	// Unreadable is set when taglib could not read the file, so every other
	// field except Path, RelPath, Format, Size and ModTime is empty. Rules skip
	// such tracks and the UNREADABLE rule reports them.
	Unreadable bool `json:"unreadable"`
}

// maxSkippedSample bounds Scan.SkippedSample.
const maxSkippedSample = 5

// Scan captures everything the rules need from one walk of a library root.
type Scan struct {
	Root       string    `json:"root"`
	Tracks     []Track   `json:"tracks"`
	Dirs       []string  `json:"dirs"`        // every directory under Root, slash-relative
	StrayFiles []string  `json:"stray_files"` // .jpg/.lrc files in folders with no audio
	ScannedAt  time.Time `json:"scanned_at"`
	// SkippedDirs counts folders the walk could not list (permissions, a
	// vanished mount). Whatever they hold is missing from every other field, so
	// a non-zero count means the library was only partly read.
	SkippedDirs int `json:"skipped_dirs"`
	// SkippedSample names the first few of them, slash-relative, in sorted order.
	SkippedSample []string `json:"skipped_sample,omitempty"`

	// idx caches the lookups Albums and TrackByPath share; see scanIndex. Scan
	// is copied by value, so this is a pointer rather than a sync type.
	idx *scanIndex
}

// noteSkippedDir records a folder the walk could not list. It keeps the
// maxSkippedSample smallest paths, not the first met, so the sample does not
// depend on walk order.
func (s *Scan) noteSkippedDir(root, path string) {
	s.SkippedDirs++
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)
	i := sort.SearchStrings(s.SkippedSample, rel)
	if i >= maxSkippedSample {
		return
	}
	s.SkippedSample = append(s.SkippedSample, "")
	copy(s.SkippedSample[i+1:], s.SkippedSample[i:])
	s.SkippedSample[i] = rel
	if len(s.SkippedSample) > maxSkippedSample {
		s.SkippedSample = s.SkippedSample[:maxSkippedSample]
	}
}

// ScanProgress is emitted while ScanLibrary runs.
type ScanProgress struct {
	Phase   string `json:"phase"` // "discover" or "tags"
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Path    string `json:"path"`
}

// ScanLibrary walks root, reads full tags for FLAC/MP3/M4A files, and
// reports progress. It honours ctx cancellation.
func ScanLibrary(ctx context.Context, root string, progress func(ScanProgress)) (*Scan, error) {
	if progress == nil {
		progress = func(ScanProgress) {}
	}
	scan := &Scan{Root: root, ScannedAt: time.Now()}

	audioDirs := map[string]bool{}
	var audioPaths []string
	// WalkDir rather than Walk: it takes each entry's type from the directory
	// listing instead of an lstat per file, which is much faster on a large
	// library, and it means an error here is always a folder that could not be
	// listed, never a file.
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			// An unreadable folder is skipped rather than failing the scan, but
			// counted so the report does not pass a partial read off as the
			// whole library. d is nil only when the root itself cannot be read.
			if d == nil || d.IsDir() {
				scan.noteSkippedDir(root, path)
			}
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".auralis-incoming-") {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(root, path)
			if err == nil && rel != "." {
				scan.Dirs = append(scan.Dirs, filepath.ToSlash(rel))
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if audioExtensions[ext] {
			audioPaths = append(audioPaths, path)
			if rel, err := filepath.Rel(root, filepath.Dir(path)); err == nil {
				d := filepath.ToSlash(rel)
				if d == "." {
					d = ""
				}
				audioDirs[d] = true
			}
			return nil
		}
		if ext == ".jpg" || ext == ".jpeg" || ext == ".lrc" {
			if rel, err := filepath.Rel(root, path); err == nil {
				scan.StrayFiles = append(scan.StrayFiles, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// A folder above audio folders (artist art, a multi-disc album's cover)
	// is not stray either.
	hasAudioBelow := map[string]bool{}
	for dir := range audioDirs {
		// The library root itself stays out: art loose there belongs to no one.
		for dir = pathpkg.Dir(dir); dir != "." && dir != "/" && dir != ""; dir = pathpkg.Dir(dir) {
			hasAudioBelow[dir] = true
		}
	}

	// Keep stray files only where no audio lives in or below the same folder.
	kept := scan.StrayFiles[:0]
	for _, stray := range scan.StrayFiles {
		dir := pathpkg.Dir(stray)
		if dir == "." {
			dir = ""
		}
		if !audioDirs[dir] && !hasAudioBelow[dir] {
			kept = append(kept, stray)
		}
	}
	scan.StrayFiles = kept
	sort.Strings(scan.StrayFiles)
	sort.Strings(scan.Dirs)

	total := len(audioPaths)
	tracks := make([]Track, total)
	covers := &coverCache{}
	workers := scanWorkers(total)

	// Workers pull the next index from an atomic cursor and write only their
	// own slot, so no result needs a lock and the order stays that of the walk.
	var (
		cursor   atomic.Int64
		finished atomic.Int64
		progMu   sync.Mutex
		lastSent int64
		wg       sync.WaitGroup
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				i := int(cursor.Add(1)) - 1
				if i >= total {
					return
				}
				path := audioPaths[i]
				track, err := readTrackCached(root, path, covers)
				if err != nil {
					// Keep the file as a flagged, zero-metadata track so the
					// orphan rules still see it and UNREADABLE reports it.
					track = Track{Path: path, Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."), Unreadable: true}
					if rel, relErr := filepath.Rel(root, path); relErr == nil {
						track.RelPath = filepath.ToSlash(rel)
					}
					if info, statErr := os.Stat(path); statErr == nil {
						track.Size = info.Size()
						track.ModTimeUnixNano = info.ModTime().UnixNano()
					}
				}
				tracks[i] = track

				// Every file would flood the UI event bridge on a large library.
				// The counter makes Current monotonic; the lock serialises the
				// callback and drops an event a later one already overtook.
				n := finished.Add(1)
				if n == 1 || n == int64(total) || n%25 == 0 {
					progMu.Lock()
					if n > lastSent {
						lastSent = n
						progress(ScanProgress{Phase: "tags", Current: int(n), Total: total, Path: path})
					}
					progMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	scan.Tracks = tracks
	sort.Slice(scan.Tracks, func(i, j int) bool { return scan.Tracks[i].RelPath < scan.Tracks[j].RelPath })
	return scan, nil
}

// AlbumDir returns the slash-relative directory a track lives in.
func (t Track) AlbumDir() string {
	dir := pathpkg.Dir(t.RelPath)
	if dir == "." {
		return ""
	}
	return dir
}

// maxScanWorkers bounds concurrent tag reads. Each taglib call instantiates a
// WASM module, so more workers than this costs memory without finishing sooner.
const maxScanWorkers = 8

func scanWorkers(total int) int {
	workers := runtime.NumCPU()
	if workers > maxScanWorkers {
		workers = maxScanWorkers
	}
	if workers > total {
		workers = total
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

func readTrack(root, path string) (Track, error) {
	return readTrackCached(root, path, nil)
}

func readTrackCached(root, path string, covers *coverCache) (Track, error) {
	track := Track{Path: path, Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")}
	if rel, err := filepath.Rel(root, path); err == nil {
		track.RelPath = filepath.ToSlash(rel)
	}
	info, err := os.Stat(path)
	if err != nil {
		return track, err
	}
	track.Size = info.Size()
	track.ModTimeUnixNano = info.ModTime().UnixNano()

	tags, err := taglib.ReadTags(path)
	if err != nil {
		return track, err
	}
	normalized := map[string][]string{}
	for key, values := range tags {
		normalized[strings.ToUpper(strings.TrimSpace(key))] = values
	}
	first := func(keys ...string) string {
		for _, key := range keys {
			for _, v := range normalized[strings.ToUpper(key)] {
				if strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			}
		}
		return ""
	}
	track.Title = first(taglib.Title)
	track.Artist = first(taglib.Artist)
	track.AlbumArtist = first(taglib.AlbumArtist)
	track.Album = first(taglib.Album)
	track.Year = first(taglib.Date, taglib.ReleaseDate, "YEAR")
	track.ISRC = first(taglib.ISRC)

	track.TrackNumberRaw = first(taglib.TrackNumber, "TRACKNUMBER", "TRACK")
	track.DiscNumberRaw = first(taglib.DiscNumber, "DISCNUMBER", "DISC")
	track.TrackNumber, track.TrackTotal = splitNumberTotal(track.TrackNumberRaw)
	if track.TrackTotal == 0 {
		if v, err := strconv.Atoi(first("TRACKTOTAL", "TOTALTRACKS")); err == nil {
			track.TrackTotal = v
		}
	}
	track.DiscNumber, track.DiscTotal = splitNumberTotal(track.DiscNumberRaw)
	if track.DiscTotal == 0 {
		if v, err := strconv.Atoi(first("DISCTOTAL", "TOTALDISCS")); err == nil {
			track.DiscTotal = v
		}
	}

	switch strings.ToLower(first("COMPILATION")) {
	case "1", "true", "yes":
		track.Compilation = true
	}

	if img, err := taglib.ReadImage(path); err == nil && len(img) > 0 {
		track.CoverBytes = len(img)
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(img)); err == nil {
			track.CoverWidth = cfg.Width
			track.CoverHeight = cfg.Height
		}
	}

	track.FolderCover = covers.folderHasCover(filepath.Dir(path))
	track.LRC = lrcExists(path)

	// ReadProperties parses the stream headers in-process, which avoids
	// spawning ffprobe for every MP3/M4A in a large library.
	if props, err := taglib.ReadProperties(path); err == nil {
		track.DurationSeconds = props.Length.Seconds()
		track.BitDepth = int(props.BitDepth)
	}
	return track, nil
}

func splitNumberTotal(raw string) (int, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0
	}
	parts := strings.SplitN(raw, "/", 2)
	num := parseLeadingInt(parts[0])
	total := 0
	if len(parts) == 2 {
		total = parseLeadingInt(parts[1])
	}
	return num, total
}

func parseLeadingInt(s string) int {
	s = strings.TrimSpace(s)
	digits := ""
	for _, r := range s {
		if r < '0' || r > '9' {
			if digits != "" {
				break
			}
			continue
		}
		digits += string(r)
	}
	if digits == "" {
		return 0
	}
	n, _ := strconv.Atoi(digits)
	return n
}

var discFolderPattern = regexp.MustCompile(`(?i)^(?:disc|disk|cd)\s*\d+$`)

// coverCache remembers, per directory, whether it holds a cover file, so a
// folder is listed once however many tracks live in it. A nil cache reads the
// disk every time. It is safe for concurrent use; two workers asking about the
// same folder share one ReadDir.
type coverCache struct {
	mu      sync.Mutex
	entries map[string]*coverEntry
}

type coverEntry struct {
	once sync.Once
	has  bool
}

// hasCoverFile caches folderHasCoverFile for one directory.
func (c *coverCache) hasCoverFile(dir string) bool {
	if c == nil {
		return folderHasCoverFile(dir)
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*coverEntry{}
	}
	e := c.entries[dir]
	if e == nil {
		e = &coverEntry{}
		c.entries[dir] = e
	}
	c.mu.Unlock()
	e.once.Do(func() { e.has = folderHasCoverFile(dir) })
	return e.has
}

// folderHasCover reports whether dir, or for a "CD1"/"Disc 2" folder its
// parent, holds cover.jpg or folder.jpg.
func (c *coverCache) folderHasCover(dir string) bool {
	if c.hasCoverFile(dir) {
		return true
	}
	if discFolderPattern.MatchString(filepath.Base(dir)) {
		return c.hasCoverFile(filepath.Dir(dir))
	}
	return false
}

func folderHasCoverFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if ext := filepath.Ext(name); ext != ".jpg" && ext != ".jpeg" {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if base == "cover" || base == "folder" {
			return true
		}
	}
	return false
}

func lrcExists(audioPath string) bool {
	return fileExists(strings.TrimSuffix(audioPath, filepath.Ext(audioPath)) + ".lrc")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
