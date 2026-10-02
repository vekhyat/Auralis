package ipod

import (
	"strconv"
	"strings"

	"go.senan.xyz/taglib"
)

type audioMeta struct {
	title       string
	artist      string
	album       string
	albumArtist string
	genre       string
	composer    string
	trackNr     uint32
	tracks      uint32
	disc        uint32
	discs       uint32
	year        uint32
	bitrate     uint32
	sampleRate  uint32
	bitDepth    uint
	lengthMS    uint32
	codec       string
	size        int64
}

func readMeta(path string, size int64) audioMeta {
	meta := audioMeta{size: size, title: strings.TrimSuffix(filepathBase(path), extOf(path))}
	tags, err := taglib.ReadTags(path)
	if err == nil {
		meta.title = orEmpty(firstTag(tags, taglib.Title), meta.title)
		meta.artist = firstTag(tags, taglib.Artist)
		meta.album = firstTag(tags, taglib.Album)
		meta.albumArtist = firstTag(tags, taglib.AlbumArtist)
		meta.genre = firstTag(tags, taglib.Genre)
		meta.composer = firstTag(tags, taglib.Composer)
		meta.trackNr, meta.tracks = parsePair(firstTag(tags, taglib.TrackNumber))
		meta.disc, meta.discs = parsePair(firstTag(tags, taglib.DiscNumber))
		meta.year = leadingYear(firstTag(tags, taglib.Date))
	}
	props, err := taglib.ReadProperties(path)
	if err == nil {
		meta.sampleRate = uint32(props.SampleRate)
		meta.bitrate = uint32(props.BitRate)
		meta.bitDepth = props.BitDepth
		meta.codec = props.InnerCodec
		if props.Length > 0 {
			meta.lengthMS = uint32(props.Length.Milliseconds())
		}
	}
	return meta
}

func needsTranscode(ext string, meta audioMeta) bool {
	hiRes := meta.sampleRate > 44100 || meta.bitDepth > 16
	switch strings.ToLower(ext) {
	case ".mp3", ".wav", ".aif", ".aiff":
		return hiRes
	case ".m4a", ".mp4", ".m4b":
		codec := strings.ToLower(meta.codec)
		if codec != "" && codec != "aac" && codec != "alac" {
			return true
		}
		return hiRes
	default:
		return true
	}
}

func kindFor(ext, codec, format string, transcode bool) (kind, destExt string, unk126, unk144 uint16) {
	if transcode {
		if format == "aac" {
			return "AAC audio file", ".m4a", 0xffff, 0x0033
		}
		return "Apple Lossless audio file", ".m4a", 0xffff, 0x0033
	}
	switch strings.ToLower(ext) {
	case ".mp3":
		return "MPEG audio file", ".mp3", 0xffff, 0x000c
	case ".wav":
		return "WAV audio file", ".wav", 0, 0
	case ".aif", ".aiff":
		return "AIFF audio file", strings.ToLower(ext), 0xffff, 0
	default:
		if strings.EqualFold(codec, "alac") {
			return "Apple Lossless audio file", ".m4a", 0xffff, 0x0033
		}
		return "AAC audio file", ".m4a", 0xffff, 0x0033
	}
}

func firstTag(tags map[string][]string, key string) string {
	if tags == nil {
		return ""
	}
	if values := tags[key]; len(values) > 0 {
		return strings.TrimSpace(values[0])
	}
	return ""
}

func orEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func parsePair(value string) (uint32, uint32) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0
	}
	head, tail, _ := strings.Cut(value, "/")
	return parseU32(head), parseU32(tail)
}

func parseU32(value string) uint32 {
	value = strings.TrimSpace(value)
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

func leadingYear(value string) uint32 {
	digits := make([]byte, 0, 4)
	for i := 0; i < len(value) && len(digits) < 4; i++ {
		if value[i] < '0' || value[i] > '9' {
			if len(digits) == 0 {
				continue
			}
			break
		}
		digits = append(digits, value[i])
	}
	if len(digits) != 4 {
		return 0
	}
	n, _ := strconv.ParseUint(string(digits), 10, 32)
	return uint32(n)
}

func filepathBase(path string) string {
	path = strings.ReplaceAll(path, `\`, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func extOf(path string) string {
	base := filepathBase(path)
	if i := strings.LastIndex(base, "."); i >= 0 {
		return base[i:]
	}
	return ""
}
