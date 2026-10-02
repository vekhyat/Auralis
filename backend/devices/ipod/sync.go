package ipod

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/vekhyat/Auralis/backend"
)

var errBusy = errors.New("iPod is busy")

// Progress is emitted while files are copied and the database is written.
type Progress struct {
	Phase   string `json:"phase"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Name    string `json:"name"`
}

// SendResult counts songs copied and songs left alone.
type SendResult struct {
	Sent    int `json:"sent"`
	Skipped int `json:"skipped"`
}

// DoctorReport is the gap between files on disk and the stock database.
type DoctorReport struct {
	Orphans []DeviceTrack `json:"orphans"`
	Missing []DeviceTrack `json:"missing"`
	Changed bool          `json:"changed"`
}

// Manager serializes sends, removals, and doctor runs.
type Manager struct {
	mu     sync.Mutex
	op     sync.Mutex
	cancel context.CancelFunc
}

func NewManager() *Manager { return &Manager{} }

func (m *Manager) List() []Device { return ListDevices() }

func (m *Manager) Cancel() {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) Send(id string, paths []string, format string, progress func(Progress)) (SendResult, error) {
	result, err := m.with(id, func(dev Device, ctx context.Context) (any, error) {
		return sendMount(ctx, dev, paths, format, progress)
	})
	sent, _ := result.(SendResult)
	return sent, err
}

func (m *Manager) Remove(id string, ids []int) error {
	_, err := m.with(id, func(dev Device, _ context.Context) (any, error) {
		return nil, removeMount(dev, ids)
	})
	return err
}

func (m *Manager) Eject(id string) error {
	_, err := m.with(id, func(dev Device, _ context.Context) (any, error) {
		if dev.Mount == "" {
			return nil, errors.New("Windows did not eject the iPod. Use Safely Remove Hardware.")
		}
		return nil, ejectVolume(dev.Mount)
	})
	return err
}

func (m *Manager) Doctor(id, action string) (DoctorReport, error) {
	result, err := m.with(id, func(dev Device, _ context.Context) (any, error) {
		return doctorMount(dev, action)
	})
	report, _ := result.(DoctorReport)
	return report, err
}

func (m *Manager) with(id string, fn func(Device, context.Context) (any, error)) (any, error) {
	if !m.op.TryLock() {
		return nil, errBusy
	}
	defer m.op.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		m.cancel = nil
		m.mu.Unlock()
	}()
	dev, err := deviceByID(id)
	if err != nil {
		return nil, err
	}
	return fn(dev, ctx)
}

func deviceByID(id string) (Device, error) {
	for _, dev := range ListDevices() {
		if dev.ID == id || strings.EqualFold(dev.Mount, id) {
			return dev, nil
		}
	}
	// Tests and direct mounts pass a filesystem path that is not a scanned volume.
	if dirExists(id) || dirExists(filepath.Clean(id)) {
		dev, ok := inspectRoot(id, volumeInfo{})
		if ok {
			return dev, nil
		}
	}
	return Device{}, errors.New("iPod is not connected")
}

func sendMount(ctx context.Context, dev Device, paths []string, format string, progress func(Progress)) (SendResult, error) {
	if dev.Mount == "" || !dev.CanSend {
		return SendResult{}, sendBlocked(dev)
	}
	if format != "aac" {
		format = "alac"
	}
	files, err := expandAudio(paths)
	if err != nil {
		return SendResult{}, err
	}
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			return SendResult{}, err
		}
		if info.Size() >= 4<<30 {
			return SendResult{}, fmt.Errorf("%s is 4 GB or larger and cannot be stored on this iPod", filepath.Base(path))
		}
	}
	if dev.Mode == "stock" && usesShuffleSD(dev.Generation) {
		if err := refuseUnknownShuffle(dev.Mount); err != nil {
			return SendResult{}, err
		}
	}

	var db *Database
	if dev.Mode == "stock" {
		db, err = openDB(dev)
		if err != nil {
			return SendResult{}, err
		}
	}

	result := SendResult{}
	var copied []string
	committed := false
	defer func() {
		if !committed {
			for _, path := range copied {
				_ = os.Remove(path)
			}
		}
	}()

	for i, path := range files {
		if err := ctx.Err(); err != nil {
			return SendResult{}, errors.New("cancelled")
		}
		emit(progress, Progress{Phase: "copy", Current: i + 1, Total: len(files), Name: filepath.Base(path)})
		if underMount(dev.Mount, path) {
			result.Skipped++
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return SendResult{}, err
		}
		meta := readMeta(path, info.Size())
		if dev.Mode == "stock" && duplicate(db, meta) {
			result.Skipped++
			continue
		}
		dest, tr, err := placeTrack(dev, path, format, meta)
		if err != nil {
			return SendResult{}, err
		}
		if dev.Mode == "stock" {
			if stat, err := os.Stat(dest); err == nil {
				if stat.Size() >= 4<<30 {
					_ = os.Remove(dest)
					return SendResult{}, fmt.Errorf("%s is 4 GB or larger and cannot be stored on this iPod", filepath.Base(dest))
				}
				tr.size = uint32(stat.Size())
			}
			if err := db.addTrack(tr); err != nil {
				_ = os.Remove(dest)
				return SendResult{}, err
			}
		}
		copied = append(copied, dest)
		result.Sent++
	}
	if result.Sent == 0 {
		committed = true
		emit(progress, Progress{Phase: "done", Current: len(files), Total: len(files)})
		return result, nil
	}
	if dev.Mode == "stock" {
		emit(progress, Progress{Phase: "database", Current: len(files), Total: len(files)})
		if err := saveLibrary(dev, db); err != nil {
			// The database commit restores the previous file on failure.
			// Files copied in this send are deleted unless that commit
			// already succeeded and a later shuffle or playlist step failed.
			var kept *afterCommitError
			if errors.As(err, &kept) {
				committed = true
			}
			return SendResult{}, err
		}
	}
	committed = true
	emit(progress, Progress{Phase: "done", Current: len(files), Total: len(files)})
	return result, nil
}

func placeTrack(dev Device, src, format string, meta audioMeta) (string, *track, error) {
	if dev.Mode == "rockbox" {
		dest := filepath.Join(dev.Mount, "Music", sanitize(meta.artist), sanitize(meta.album), sanitize(filepath.Base(src)))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", nil, err
		}
		if _, err := os.Stat(dest); err == nil {
			dest = uniqueSibling(dest)
		}
		if err := requireSpace(dev.Mount, meta.size); err != nil {
			return "", nil, err
		}
		if err := copyFile(src, dest); err != nil {
			return "", nil, err
		}
		return dest, nil, nil
	}
	ext := extOf(src)
	transcode := needsTranscode(ext, meta)
	if !transcode {
		if err := requireSpace(dev.Mount, meta.size); err != nil {
			return "", nil, err
		}
	} else if free := freeBytes(dev.Mount); free > 0 && free < 1024*1024 {
		return "", nil, errors.New("the iPod does not have enough free space")
	}
	kind, destExt, unk126, unk144 := kindFor(ext, meta.codec, format, transcode)
	buckets, err := musicBuckets(dev.Mount)
	if err != nil {
		return "", nil, err
	}
	name := randomName(destExt)
	dest := filepath.Join(buckets[bucketIndex(name, len(buckets))], name)
	for fileExists(dest) {
		name = randomName(destExt)
		dest = filepath.Join(buckets[bucketIndex(name, len(buckets))], name)
	}
	if transcode {
		if err := transcodeFile(src, dest, format); err != nil {
			return "", nil, err
		}
	} else if err := copyFile(src, dest); err != nil {
		return "", nil, err
	}
	rel, err := filepath.Rel(dev.Mount, dest)
	if err != nil {
		return "", nil, err
	}
	tr := &track{
		title:       meta.title,
		artist:      meta.artist,
		album:       meta.album,
		albumArtist: meta.albumArtist,
		genre:       meta.genre,
		composer:    meta.composer,
		kind:        kind,
		path:        colonPath("/" + filepath.ToSlash(rel)),
		size:        uint32(meta.size),
		lengthMS:    meta.lengthMS,
		trackNr:     meta.trackNr,
		tracks:      meta.tracks,
		year:        meta.year,
		bitrate:     meta.bitrate,
		sampleRate:  meta.sampleRate,
		disc:        meta.disc,
		discs:       meta.discs,
		unk126:      unk126,
		unk144:      unk144,
	}
	return dest, tr, nil
}

func removeMount(dev Device, ids []int) error {
	if dev.Mode != "stock" || !dev.CanSend {
		return sendBlocked(dev)
	}
	db, err := openDB(dev)
	if err != nil {
		return err
	}
	wanted := map[uint32]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			return errors.New("track is not on the iPod")
		}
		wanted[uint32(id)] = struct{}{}
	}
	var files []string
	for _, tr := range db.tracks {
		if _, ok := wanted[tr.id]; !ok || tr.path == "" {
			continue
		}
		full, ok := safeInside(dev.Mount, tr.path)
		if !ok {
			continue
		}
		files = append(files, full)
	}
	for id := range wanted {
		if err := db.removeTrack(id); err != nil {
			return err
		}
	}
	if err := saveLibrary(dev, db); err != nil {
		var kept *afterCommitError
		if !errors.As(err, &kept) {
			return err
		}
		if delErr := deleteCopied(files); delErr != nil {
			return delErr
		}
		return err
	}
	return deleteCopied(files)
}

func doctorMount(dev Device, action string) (DoctorReport, error) {
	if action == "" {
		action = "report"
	}
	if dev.Mode == "rockbox" {
		if action != "report" {
			return DoctorReport{}, errors.New("Rockbox does not use the stock database")
		}
		return DoctorReport{}, nil
	}
	if dev.Mode != "stock" {
		return DoctorReport{}, sendBlocked(dev)
	}
	files := listMusicFiles(dev.Mount)
	db, err := openDB(dev)
	broken := err != nil
	report := DoctorReport{}
	if !broken {
		onDisk := map[string]string{}
		for _, path := range files {
			onDisk[pathKey(path)] = path
		}
		seen := map[string]struct{}{}
		for _, tr := range db.tracks {
			full := ""
			if tr.path != "" {
				full = mountFile(dev.Mount, tr.path)
			}
			key := pathKey(full)
			if full == "" || !fileExists(full) {
				report.Missing = append(report.Missing, DeviceTrack{
					ID: int(tr.id), Title: displayTitle(tr), Artist: tr.artist, Album: tr.album, Path: tr.path, Size: uint64(tr.size),
				})
				continue
			}
			seen[key] = struct{}{}
		}
		for key, path := range onDisk {
			if _, ok := seen[key]; ok {
				continue
			}
			info, _ := os.Stat(path)
			var size uint64
			if info != nil {
				size = uint64(info.Size())
			}
			report.Orphans = append(report.Orphans, DeviceTrack{
				Title: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
				Path:  path,
				Size:  size,
			})
		}
	} else if action != "rebuild" {
		return DoctorReport{}, err
	}

	switch action {
	case "report":
		return report, nil
	case "add-orphans":
		if broken {
			return report, err
		}
		for _, orphan := range report.Orphans {
			info, statErr := os.Stat(orphan.Path)
			if statErr != nil || info.Size() >= 4<<30 {
				continue
			}
			meta := readMeta(orphan.Path, info.Size())
			ext := extOf(orphan.Path)
			transcode := needsTranscode(ext, meta)
			if transcode {
				continue
			}
			kind, _, unk126, unk144 := kindFor(ext, meta.codec, "alac", false)
			rel, relErr := filepath.Rel(dev.Mount, orphan.Path)
			if relErr != nil {
				continue
			}
			if err := db.addTrack(&track{
				title: meta.title, artist: meta.artist, album: meta.album, albumArtist: meta.albumArtist,
				genre: meta.genre, composer: meta.composer, kind: kind,
				path: colonPath("/" + filepath.ToSlash(rel)), size: uint32(info.Size()),
				lengthMS: meta.lengthMS, trackNr: meta.trackNr, tracks: meta.tracks, year: meta.year,
				bitrate: meta.bitrate, sampleRate: meta.sampleRate, disc: meta.disc, discs: meta.discs,
				unk126: unk126, unk144: unk144,
			}); err != nil {
				return report, err
			}
		}
		if err := saveLibrary(dev, db); err != nil {
			return report, err
		}
		report.Changed = true
		report.Orphans = nil
		return report, nil
	case "drop-missing":
		if broken {
			return report, err
		}
		for _, missing := range report.Missing {
			if err := db.removeTrack(uint32(missing.ID)); err != nil {
				return report, err
			}
		}
		if err := saveLibrary(dev, db); err != nil {
			return report, err
		}
		report.Changed = true
		report.Missing = nil
		return report, nil
	case "rebuild":
		if dev.Checksum != "none" && dev.Checksum != "hash58" {
			return report, errors.New("this iPod cannot be updated safely")
		}
		fresh := newDatabase(checksumFromDevice(dev), firewireBytes(dev.FirewireID))
		if db != nil {
			fresh.dbID = db.dbID
			fresh.id0x24 = db.id0x24
			fresh.pid = db.pid
			fresh.platform = db.platform
			fresh.language = db.language
		}
		for _, path := range files {
			info, statErr := os.Stat(path)
			if statErr != nil || info.Size() >= 4<<30 || info.Size() < 0 {
				continue
			}
			meta := readMeta(path, info.Size())
			ext := extOf(path)
			if needsTranscode(ext, meta) {
				continue
			}
			kind, _, unk126, unk144 := kindFor(ext, meta.codec, "alac", false)
			rel, relErr := filepath.Rel(dev.Mount, path)
			if relErr != nil {
				continue
			}
			if err := fresh.addTrack(&track{
				title: meta.title, artist: meta.artist, album: meta.album, albumArtist: meta.albumArtist,
				genre: meta.genre, composer: meta.composer, kind: kind,
				path: colonPath("/" + filepath.ToSlash(rel)), size: uint32(info.Size()),
				lengthMS: meta.lengthMS, trackNr: meta.trackNr, tracks: meta.tracks, year: meta.year,
				bitrate: meta.bitrate, sampleRate: meta.sampleRate, disc: meta.disc, discs: meta.discs,
				unk126: unk126, unk144: unk144,
			}); err != nil {
				return report, err
			}
		}
		if err := saveLibrary(dev, fresh); err != nil {
			return report, err
		}
		return DoctorReport{Changed: true}, nil
	default:
		return report, errors.New("unknown doctor action")
	}
}

func openDB(dev Device) (*Database, error) {
	cs := checksumFromDevice(dev)
	fw := firewireBytes(dev.FirewireID)
	data, err := os.ReadFile(dbPath(dev.Mount))
	if errors.Is(err, os.ErrNotExist) {
		db := newDatabase(cs, fw)
		if usesShuffleSD(dev.Generation) {
			if sd, sdErr := os.ReadFile(sdPath(dev.Mount)); sdErr == nil {
				entries, perr := parseShuffle(sd)
				if perr == nil {
					for _, tr := range tracksFromShuffle(entries) {
						_ = db.addTrack(tr)
					}
				}
			}
		}
		return db, nil
	}
	if err != nil {
		return nil, err
	}
	db, err := parseDatabase(data)
	if err != nil {
		return nil, errUnreadableDB
	}
	db.checksum = cs
	db.firewire = fw
	return db, nil
}

func saveLibrary(dev Device, db *Database) error {
	db.checksum = checksumFromDevice(dev)
	db.firewire = firewireBytes(dev.FirewireID)
	data, err := db.Marshal()
	if err != nil {
		return err
	}
	if err := commitBytes(dbPath(dev.Mount), data, func(read []byte) error {
		_, err := parseDatabase(read)
		return err
	}); err != nil {
		return err
	}
	if usesShuffleSD(dev.Generation) {
		if err := commitBytes(sdPath(dev.Mount), writeShuffle(db.tracks), func(read []byte) error {
			_, err := parseShuffle(read)
			return err
		}); err != nil {
			return &afterCommitError{err: err}
		}
	}
	if dev.HasRockbox {
		if err := writeRockboxPlaylist(dev.Mount, db.tracks); err != nil {
			return &afterCommitError{err: err}
		}
	}
	return nil
}

// afterCommitError means the iTunesDB was replaced and then a later file failed.
// Audio copied for that database must be kept.
type afterCommitError struct{ err error }

func (e *afterCommitError) Error() string { return e.err.Error() }
func (e *afterCommitError) Unwrap() error { return e.err }

func writeRockboxPlaylist(root string, tracks []*track) error {
	dir := filepath.Join(root, "Playlists")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, tr := range tracks {
		if tr.path == "" {
			continue
		}
		b.WriteString(slashPath(tr.path))
		b.WriteByte('\n')
	}
	return os.WriteFile(filepath.Join(dir, "Auralis.m3u8"), []byte(b.String()), 0o644)
}

func refuseUnknownShuffle(root string) error {
	data, err := os.ReadFile(sdPath(root))
	if err != nil {
		return nil
	}
	if _, perr := parseShuffle(data); perr == nil {
		return nil
	}
	_ = os.WriteFile(sdPath(root)+".auralis.bak", data, 0o644)
	return errUnknownShuffle
}

func checksumFromDevice(dev Device) Checksum {
	switch dev.Checksum {
	case "hash58":
		return ChecksumHash58
	case "hash72":
		return ChecksumHash72
	case "hashAB":
		return ChecksumHashAB
	case "sqlite":
		return ChecksumSQLite
	case "unknown":
		return ChecksumUnknown
	default:
		return ChecksumNone
	}
}

func firewireBytes(hexText string) []byte {
	raw, _ := decodeFirewire(hexText)
	return raw
}

func sendBlocked(dev Device) error {
	switch dev.Warning {
	case "unsupported":
		return errors.New("this iPod cannot be updated safely")
	case "database":
		return errUnreadableDB
	case "no-firewire":
		return errors.New("FireWire ID is missing, so the database cannot be signed")
	case "no-drive":
		return errors.New("Windows cannot read this iPod")
	default:
		return errors.New("this iPod cannot be updated")
	}
}

func expandAudio(paths []string) ([]string, error) {
	var out []string
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			files, err := backend.ListAudioFiles(path)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				out = append(out, file.Path)
			}
			continue
		}
		if !audioExt(filepath.Ext(path)) {
			return nil, fmt.Errorf("%s is not an audio file", filepath.Base(path))
		}
		out = append(out, path)
	}
	return out, nil
}

func duplicate(db *Database, meta audioMeta) bool {
	if strings.TrimSpace(meta.title) == "" {
		return false
	}
	for _, tr := range db.tracks {
		title := tr.title
		if title == "" {
			title = titleFromPath(tr.path)
		}
		if strings.EqualFold(title, meta.title) && strings.EqualFold(tr.artist, meta.artist) && tr.size == uint32(meta.size) {
			return true
		}
	}
	return false
}

func underMount(mount, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(mount), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func requireSpace(root string, need int64) error {
	if need < 0 {
		return nil
	}
	free := freeBytes(root)
	if free == 0 || free >= uint64(need) {
		return nil
	}
	return errors.New("the iPod does not have enough free space")
}

func musicBuckets(root string) ([]string, error) {
	music := filepath.Join(root, "iPod_Control", "Music")
	entries, err := os.ReadDir(music)
	var dirs []string
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() && len(name) == 3 && name[0] == 'F' && name[1] >= '0' && name[1] <= '9' && name[2] >= '0' && name[2] <= '9' {
				dirs = append(dirs, filepath.Join(music, name))
			}
		}
	}
	if len(dirs) == 0 {
		if err := os.MkdirAll(music, 0o755); err != nil {
			return nil, err
		}
		for i := 0; i < 20; i++ {
			dir := filepath.Join(music, fmt.Sprintf("F%02d", i))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
			dirs = append(dirs, dir)
		}
		hidePath(filepath.Join(root, "iPod_Control"))
	}
	sort.Strings(dirs)
	return dirs, nil
}

func listMusicFiles(root string) []string {
	music := filepath.Join(root, "iPod_Control", "Music")
	var out []string
	_ = filepath.Walk(music, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !audioExt(filepath.Ext(path)) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out
}

func mountFile(root, colon string) string {
	slash := strings.TrimPrefix(slashPath(colon), "/")
	return filepath.Join(root, filepath.FromSlash(slash))
}

func safeInside(root, colon string) (string, bool) {
	full := mountFile(root, colon)
	if !underMount(root, full) {
		return "", false
	}
	return full, true
}

func deleteCopied(paths []string) error {
	var first error
	for _, path := range paths {
		if !fileExists(path) {
			continue
		}
		if err := os.Remove(path); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func pathKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

func displayTitle(tr *track) string {
	if tr.title != "" {
		return tr.title
	}
	return titleFromPath(tr.path)
}

func randomName(ext string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "AUR" + strings.ToUpper(hex.EncodeToString(b[:])) + ext
}

func bucketIndex(name string, n int) int {
	if n <= 1 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return int(h.Sum32() % uint32(n))
}

func sanitize(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Unknown"
	}
	var b strings.Builder
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.Trim(b.String(), " .")
	if out == "" {
		return "Unknown"
	}
	return out
}

func uniqueSibling(path string) string {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; i < 1000; i++ {
		next := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if !fileExists(next) {
			return next
		}
	}
	return path
}

func emit(progress func(Progress), event Progress) {
	if progress != nil {
		progress(event)
	}
}
