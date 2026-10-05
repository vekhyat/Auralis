// Package library holds player-ready library conventions shared by the
// Library Doctor, the playlist manager, and device sync.
package library

import (
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ArtistMode controls how multiple artists are written for a target.
type ArtistMode string

const (
	// ArtistMulti keeps every artist, joined with the profile separator.
	ArtistMulti ArtistMode = "multi"
	// ArtistPrimary writes only the first credited artist.
	ArtistPrimary ArtistMode = "primary"
)

// Profile bundles the conventions a player or device expects. The same
// profile is used to organise the main library and to export to devices.
type Profile struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// FolderTemplate and FileTemplate use {album_artist}, {album}, {year},
	// {disc}, {track}, {title}, {artist}. FileTemplate has no extension.
	FolderTemplate string `json:"folder_template"`
	FileTemplate   string `json:"file_template"`
	// MaxCoverPx is the longest edge for embedded art; 0 keeps the original.
	MaxCoverPx int `json:"max_cover_px"`
	// BaselineJPEG is needed by Rockbox, which cannot decode progressive JPEG.
	BaselineJPEG    bool       `json:"baseline_jpeg"`
	FolderCover     bool       `json:"folder_cover"`
	LRCSidecars     bool       `json:"lrc_sidecars"`
	ArtistMode      ArtistMode `json:"artist_mode"`
	ArtistSeparator string     `json:"artist_separator"`
	// PlaylistDir is relative to the music root.
	PlaylistDir string `json:"playlist_dir"`
	// FATSafe restricts names to what FAT32/exFAT and Android storage accept.
	FATSafe bool `json:"fat_safe"`
	// MaxComponentBytes caps each path component in UTF-8 bytes.
	MaxComponentBytes int `json:"max_component_bytes"`
}

const (
	ProfilePoweramp    = "poweramp"
	ProfileMediaStore  = "mediastore"
	ProfileRockbox     = "rockbox"
	ProfileMediaServer = "mediaserver"
	ProfileCustom      = "custom"
)

var builtinProfiles = []Profile{
	{
		ID: ProfilePoweramp, Label: "Poweramp",
		FolderTemplate: "{album_artist}/{album}", FileTemplate: "{disc}{track}. {title}",
		MaxCoverPx: 1000, FolderCover: true, LRCSidecars: true,
		ArtistMode: ArtistMulti, ArtistSeparator: "; ",
		PlaylistDir: "Playlists", FATSafe: true, MaxComponentBytes: 255,
	},
	{
		ID: ProfileMediaStore, Label: "Android (Samsung Music, Files)",
		FolderTemplate: "{album_artist}/{album}", FileTemplate: "{disc}{track}. {title}",
		MaxCoverPx: 1000, FolderCover: false, LRCSidecars: false,
		ArtistMode: ArtistPrimary, ArtistSeparator: "; ",
		PlaylistDir: "Playlists", FATSafe: true, MaxComponentBytes: 255,
	},
	{
		ID: ProfileRockbox, Label: "Rockbox / iPod",
		FolderTemplate: "{album_artist}/{album}", FileTemplate: "{disc}{track}. {title}",
		MaxCoverPx: 500, BaselineJPEG: true, FolderCover: true, LRCSidecars: true,
		ArtistMode: ArtistPrimary, ArtistSeparator: "; ",
		PlaylistDir: "Playlists", FATSafe: true, MaxComponentBytes: 255,
	},
	{
		ID: ProfileMediaServer, Label: "Plex / Jellyfin / Navidrome",
		FolderTemplate: "{album_artist}/{album} ({year})", FileTemplate: "{disc}{track}. {title}",
		MaxCoverPx: 0, FolderCover: true, LRCSidecars: true,
		ArtistMode: ArtistMulti, ArtistSeparator: "; ",
		PlaylistDir: "Playlists", FATSafe: false, MaxComponentBytes: 255,
	},
}

// Profiles returns the built-in profiles in display order.
func Profiles() []Profile {
	out := make([]Profile, len(builtinProfiles))
	copy(out, builtinProfiles)
	return out
}

// ProfileByID returns a built-in profile. Unknown IDs fall back to Poweramp,
// which is the most widely compatible preset.
func ProfileByID(id string) Profile {
	for _, p := range builtinProfiles {
		if p.ID == id {
			return p
		}
	}
	return builtinProfiles[0]
}

// TrackFields are the tag values a profile template can reference.
type TrackFields struct {
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	Year        string
	TrackNumber int
	DiscNumber  int
	DiscTotal   int
}

// RelativePath builds the slash-separated path (without extension) for a
// track under a music root. Every component is sanitised for the profile.
func (p Profile) RelativePath(t TrackFields) string {
	albumArtist := firstNonEmpty(t.AlbumArtist, t.Artist, "Unknown Artist")
	if p.ArtistMode == ArtistPrimary {
		albumArtist = PrimaryArtist(albumArtist)
	}
	disc := ""
	if t.DiscTotal > 1 && t.DiscNumber > 0 {
		disc = strconv.Itoa(t.DiscNumber) + "-"
	}
	track := ""
	if t.TrackNumber > 0 {
		track = twoDigits(t.TrackNumber)
	}
	values := map[string]string{
		"album_artist": albumArtist,
		"artist":       firstNonEmpty(t.Artist, albumArtist),
		"album":        firstNonEmpty(t.Album, "Unknown Album"),
		"year":         t.Year,
		"disc":         disc,
		"track":        track,
		"title":        firstNonEmpty(t.Title, "Unknown Title"),
	}

	var parts []string
	for _, seg := range strings.Split(p.FolderTemplate, "/") {
		if name := p.SanitizeComponent(expand(seg, values)); name != "" {
			parts = append(parts, name)
		}
	}
	file := expand(p.FileTemplate, values)
	// A missing track number leaves a dangling ". " prefix.
	file = strings.TrimLeft(file, ". -")
	parts = append(parts, p.SanitizeComponent(file))
	return path.Join(parts...)
}

func expand(template string, values map[string]string) string {
	out := template
	for key, value := range values {
		out = strings.ReplaceAll(out, "{"+key+"}", value)
	}
	// "Album ()" when the year is unknown.
	out = strings.ReplaceAll(out, "()", "")
	return strings.TrimSpace(out)
}

// fatIllegal are characters rejected by FAT32/exFAT and Android's MTP and
// storage layers. Slashes are included because a component never holds one.
const fatIllegal = `<>:"/\|?*`

var reservedWindowsNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SanitizeComponent makes one path component safe for the profile's target.
// It never returns a string containing a path separator.
func (p Profile) SanitizeComponent(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			continue
		case r == '/' || r == '\\':
			b.WriteRune(' ')
		case p.FATSafe && strings.ContainsRune(fatIllegal, r):
			if r == ':' {
				b.WriteString(" -")
			} else {
				b.WriteRune(' ')
			}
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	// Windows and FAT drop trailing dots and spaces, which makes the name on
	// disk differ from the name in a playlist.
	out = strings.TrimRight(out, ". ")
	out = strings.TrimLeft(out, " ")
	if base := strings.ToUpper(strings.SplitN(out, ".", 2)[0]); reservedWindowsNames[base] {
		out = "_" + out
	}
	if p.MaxComponentBytes > 0 {
		out = truncateUTF8(out, p.MaxComponentBytes)
		out = strings.TrimRight(out, ". ")
	}
	return out
}

// SanitizeRelativePath sanitises each component of a slash-separated path.
// Empty components and "." / ".." are dropped so the result can never escape
// the root it is joined to.
func (p Profile) SanitizeRelativePath(rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	var parts []string
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		if clean := p.SanitizeComponent(seg); clean != "" && clean != "." && clean != ".." {
			parts = append(parts, clean)
		}
	}
	return path.Join(parts...)
}

// PrimaryArtist returns the first credited artist from a joined artist string.
// "&", "," and "x" are deliberately not separators: they appear inside real
// names such as "Simon & Garfunkel" and "Earth, Wind & Fire".
func PrimaryArtist(artists string) string {
	lower := strings.ToLower(artists)
	cut := len(artists)
	for _, sep := range []string{";", " feat. ", " ft. ", " featuring "} {
		if idx := strings.Index(lower, sep); idx > 0 && idx < cut {
			cut = idx
		}
	}
	return strings.TrimSpace(artists[:cut])
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
