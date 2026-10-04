package library

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-flac/go-flac"
	"github.com/vekhyat/Auralis/backend"
	"go.senan.xyz/taglib"
)

// Audio extensions the doctor can read tags for.
var audioExtensions = map[string]bool{".flac": true, ".mp3": true, ".m4a": true}

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
}

// Scan captures everything the rules need from one walk of a library root.
type Scan struct {
	Root       string    `json:"root"`
	Tracks     []Track   `json:"tracks"`
	Dirs       []string  `json:"dirs"`        // every directory under Root, slash-relative
	StrayFiles []string  `json:"stray_files"` // .jpg/.lrc files in folders with no audio
	ScannedAt  time.Time `json:"scanned_at"`
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
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if walkErr == context.Canceled || ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".auralis-incoming-") {
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

	// Keep stray files only where no audio lives in the same folder.
	kept := scan.StrayFiles[:0]
	for _, stray := range scan.StrayFiles {
		dir := pathpkg.Dir(stray)
		if dir == "." {
			dir = ""
		}
		if !audioDirs[dir] {
			kept = append(kept, stray)
		}
	}
	scan.StrayFiles = kept
	sort.Strings(scan.StrayFiles)
	sort.Strings(scan.Dirs)

	total := len(audioPaths)
	scan.Tracks = make([]Track, 0, total)
	for i, path := range audioPaths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		progress(ScanProgress{Phase: "tags", Current: i + 1, Total: total, Path: path})
		track, err := readTrack(root, path)
		if err != nil {
			// Keep the file as a zero-metadata track so orphans rules still see it.
			track = Track{Path: path, Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")}
			if rel, relErr := filepath.Rel(root, path); relErr == nil {
				track.RelPath = filepath.ToSlash(rel)
			}
			if info, statErr := os.Stat(path); statErr == nil {
				track.Size = info.Size()
				track.ModTimeUnixNano = info.ModTime().UnixNano()
			}
		}
		scan.Tracks = append(scan.Tracks, track)
	}
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

func readTrack(root, path string) (Track, error) {
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

	track.FolderCover = folderHasCover(filepath.Dir(path))
	track.LRC = lrcExists(path)

	if track.Format == "flac" {
		if bits, samples, rate := flacStreamInfo(path); rate > 0 {
			track.BitDepth = bits
			track.DurationSeconds = float64(samples) / float64(rate)
		}
	}
	if track.DurationSeconds <= 0 {
		if d, err := backend.GetAudioDuration(path); err == nil && d > 0 {
			track.DurationSeconds = d
		}
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

func folderHasCover(dir string) bool {
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

func flacStreamInfo(path string) (bitDepth int, totalSamples uint64, sampleRate uint32) {
	f, err := flac.ParseFile(path)
	if err != nil {
		return 0, 0, 0
	}
	for _, block := range f.Meta {
		if block.Type != flac.StreamInfo || len(block.Data) < 18 {
			continue
		}
		data := block.Data
		sampleRate = uint32(data[10])<<12 | uint32(data[11])<<4 | uint32(data[12])>>4
		bitDepth = (((int(data[12]) & 0x01) << 4) | (int(data[13]) >> 4)) + 1
		totalSamples = uint64(data[13]&0x0F)<<32 |
			uint64(data[14])<<24 |
			uint64(data[15])<<16 |
			uint64(data[16])<<8 |
			uint64(data[17])
		return bitDepth, totalSamples, sampleRate
	}
	return 0, 0, 0
}
