package ipod

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Device is one iPod Auralis can see. Warning is a short code the UI translates.
type Device struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Mount      string        `json:"mount"`
	Mode       string        `json:"mode"`
	CanSend    bool          `json:"canSend"`
	Warning    string        `json:"warning"`
	HasRockbox bool          `json:"hasRockbox"`
	Generation string        `json:"generation"`
	Checksum   string        `json:"checksum"`
	FirewireID string        `json:"firewireId"`
	TotalBytes uint64        `json:"totalBytes"`
	FreeBytes  uint64        `json:"freeBytes"`
	Tracks     []DeviceTrack `json:"tracks"`
	TrackCount int           `json:"trackCount"`
}

// DeviceTrack is one song already on the iPod.
type DeviceTrack struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Path   string `json:"path"`
	Size   uint64 `json:"size"`
}

// ListDevices returns connected iPods. Volumes that are not iPods are skipped.
func ListDevices() []Device {
	var out []Device
	mounted := 0
	for _, vol := range listVolumes() {
		dev, ok := inspectRoot(vol.Root, vol)
		if !ok {
			continue
		}
		out = append(out, dev)
		if dev.Mount != "" {
			mounted++
		}
	}
	out = append(out, unmountedPods(mounted)...)
	if out == nil {
		return []Device{}
	}
	return out
}

func inspectRoot(root string, vol volumeInfo) (Device, bool) {
	root = filepath.Clean(root)
	control := filepath.Join(root, "iPod_Control")
	rockbox := filepath.Join(root, ".rockbox")
	hasControl := dirExists(control)
	hasRockbox := dirExists(rockbox)
	if !hasControl && !hasRockbox {
		if !unreadableMacPod(vol) {
			return Device{}, false
		}
		return Device{
			ID:         vol.Root,
			Name:       "iPod",
			Mount:      vol.Root,
			Mode:       "unreadable",
			Warning:    "no-drive",
			Checksum:   "unknown",
			TotalBytes: vol.Total,
			FreeBytes:  vol.Free,
		}, true
	}

	info := loadSysInfo(root)
	code, gen, name, known := LookupModel(info.ModelNum)
	if code == "" && name == "" {
		name = "iPod"
	}
	dev := Device{
		Name:       name,
		Mount:      withSlash(root),
		HasRockbox: hasRockbox,
		Generation: gen,
		FirewireID: info.FirewireH,
		TotalBytes: vol.Total,
		FreeBytes:  vol.Free,
	}
	if dev.FirewireID != "" {
		dev.ID = dev.FirewireID
	} else {
		dev.ID = dev.Mount
	}

	if hasSQLite(root) {
		dev.Mode = "unsupported"
		dev.Warning = "unsupported"
		dev.Checksum = ChecksumSQLite.String()
		dev = applyRockboxOnly(dev)
		if dev.Mode == "rockbox" {
			dev.Tracks = rockboxTracks(root)
			dev.TrackCount = len(dev.Tracks)
		}
		return dev, true
	}

	var scheme uint16
	dbState := "missing"
	var tracks []*track
	if hasControl {
		data, err := os.ReadFile(dbPath(root))
		switch {
		case errors.Is(err, os.ErrNotExist):
			dbState = "missing"
		case err != nil:
			dbState = "broken"
		default:
			parsed, perr := parseDatabase(data)
			if perr != nil {
				dbState = "broken"
				if len(data) >= 0x32 {
					scheme = u16At(data, 0x30)
				}
			} else {
				dbState = "readable"
				scheme = parsed.scheme
				tracks = parsed.tracks
			}
		}
	}

	cs, warning := resolveChecksum(known, gen, info.Firewire, scheme, dbState)
	dev.Checksum = cs.String()
	dev.Warning = warning
	dev.Mode = "stock"
	dev.CanSend = cs.writable() && warning != "unsupported" && warning != "no-firewire" && dbState != "broken"
	if dbState == "broken" && dev.Warning != "unsupported" {
		dev.Warning = "database"
		dev.CanSend = false
	}
	if !hasControl && hasRockbox {
		dev.Mode = "rockbox"
		dev.CanSend = true
		dev.Warning = "rockbox"
		dev.Tracks = rockboxTracks(root)
		dev.TrackCount = len(dev.Tracks)
		return dev, true
	}
	dev = applyRockboxOnly(dev)
	if dev.Mode == "stock" && dbState == "readable" {
		dev.Tracks = publicTracks(tracks)
		dev.TrackCount = len(dev.Tracks)
	} else if dev.Mode == "rockbox" {
		dev.Tracks = rockboxTracks(root)
		dev.TrackCount = len(dev.Tracks)
	}
	return dev, true
}

func applyRockboxOnly(dev Device) Device {
	if dev.HasRockbox && !dev.CanSend && dev.Warning != "database" {
		dev.Mode = "rockbox"
		dev.CanSend = true
		dev.Warning = "rockbox"
		return dev
	}
	if !dev.CanSend && dev.Mode == "stock" && (dev.Warning == "unsupported" || dev.Checksum == "hash72" || dev.Checksum == "hashAB" || dev.Checksum == "sqlite") {
		dev.Mode = "unsupported"
	}
	return dev
}

func resolveChecksum(known bool, gen string, firewire []byte, scheme uint16, state string) (Checksum, string) {
	if state == "readable" {
		switch scheme {
		case 2:
			return ChecksumHash72, "unsupported"
		case 3, 4:
			return ChecksumHashAB, "unsupported"
		}
	}
	if known {
		switch gen {
		case "SHUFFLE_3", "SHUFFLE_4":
			// These shuffles play from the later iTunesSD, not the classic iTunesDB.
			return ChecksumUnknown, "unsupported"
		}
		switch checksumForGeneration(gen) {
		case ChecksumHash72:
			return ChecksumHash72, "unsupported"
		case ChecksumHashAB:
			return ChecksumHashAB, "unsupported"
		case ChecksumHash58:
			if len(firewire) == 0 {
				return ChecksumHash58, "no-firewire"
			}
			return ChecksumHash58, ""
		default:
			return ChecksumNone, ""
		}
	}
	if len(firewire) > 0 {
		return ChecksumHash58, "unverified"
	}
	return ChecksumNone, "unverified"
}

func unreadableMacPod(vol volumeInfo) bool {
	if !strings.Contains(strings.ToLower(vol.Label), "ipod") {
		return false
	}
	switch strings.ToUpper(vol.FileSystem) {
	case "FAT", "FAT32", "EXFAT", "NTFS", "REFS":
		return false
	default:
		return true
	}
}

func hasSQLite(root string) bool {
	itunes := filepath.Join(root, "iPod_Control", "iTunes")
	if fileExists(filepath.Join(itunes, "iTunesCDB")) {
		return true
	}
	if dirExists(filepath.Join(root, "iTunes_Control")) {
		return true
	}
	patterns := []string{
		filepath.Join(itunes, "*.itdb"),
		filepath.Join(itunes, "iTunes Library", "*.itdb"),
		filepath.Join(root, "iTunes_Control", "iTunes", "*.itdb"),
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

func publicTracks(tracks []*track) []DeviceTrack {
	out := make([]DeviceTrack, 0, len(tracks))
	for _, tr := range tracks {
		title := tr.title
		if title == "" {
			title = titleFromPath(tr.path)
		}
		out = append(out, DeviceTrack{
			ID:     int(tr.id),
			Title:  title,
			Artist: tr.artist,
			Album:  tr.album,
			Path:   tr.path,
			Size:   uint64(tr.size),
		})
	}
	return out
}

func rockboxTracks(root string) []DeviceTrack {
	music := filepath.Join(root, "Music")
	var out []DeviceTrack
	_ = filepath.Walk(music, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !audioExt(filepath.Ext(path)) {
			return nil
		}
		out = append(out, DeviceTrack{
			Title: strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())),
			Path:  path,
			Size:  uint64(info.Size()),
		})
		return nil
	})
	return out
}

func titleFromPath(path string) string {
	slash := slashPath(path)
	base := slash
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if dot := strings.LastIndex(base, "."); dot > 0 {
		base = base[:dot]
	}
	return base
}

func dbPath(root string) string {
	return filepath.Join(root, "iPod_Control", "iTunes", "iTunesDB")
}

func sdPath(root string) string {
	return filepath.Join(root, "iPod_Control", "iTunes", "iTunesSD")
}

func withSlash(root string) string {
	root = filepath.Clean(root)
	if strings.HasSuffix(root, `\`) || strings.HasSuffix(root, "/") {
		return root
	}
	return root + string(filepath.Separator)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func audioExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".flac", ".mp3", ".m4a", ".mp4", ".m4b", ".aac", ".wav", ".aiff", ".aif", ".ogg", ".opus", ".ape", ".wv", ".mpc", ".alac":
		return true
	default:
		return false
	}
}

func u16At(b []byte, off int) uint16 {
	if off+2 > len(b) {
		return 0
	}
	return uint16(b[off]) | uint16(b[off+1])<<8
}
