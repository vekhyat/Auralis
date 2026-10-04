package ipod

import (
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf16"
)

const shuffleEntryLen = 0x22e

var errUnknownShuffle = errors.New("iTunesSD is not the shuffle format Auralis can update")

type shuffleEntry struct {
	path string
	kind uint32
}

func shuffleKind(kind string, path string) uint32 {
	text := strings.ToLower(kind + " " + path)
	switch {
	case strings.Contains(text, "wav"):
		return 4
	case strings.Contains(text, "aac"), strings.Contains(text, "m4a"), strings.Contains(text, "alac"):
		return 2
	default:
		return 1
	}
}

func looksLikeClassicShuffle(data []byte) bool {
	if len(data) < 18 {
		return false
	}
	if data[0] >= 'A' && data[0] <= 'Z' || data[0] >= 'a' && data[0] <= 'z' {
		return false
	}
	return true
}

func parseShuffle(data []byte) ([]shuffleEntry, error) {
	if !looksLikeClassicShuffle(data) {
		return nil, errUnknownShuffle
	}
	count := int(be24(data[0:3]))
	if 18+count*shuffleEntryLen != len(data) {
		return nil, errUnknownShuffle
	}
	out := make([]shuffleEntry, 0, count)
	for i := 0; i < count; i++ {
		entry := data[18+i*shuffleEntryLen : 18+(i+1)*shuffleEntryLen]
		kind := be24(entry[0x1b:0x1e])
		name := decodeUTF16(entry[0x21 : 0x21+522])
		name = strings.TrimRight(name, "\x00")
		out = append(out, shuffleEntry{path: name, kind: kind})
	}
	return out, nil
}

func writeShuffle(tracks []*track) []byte {
	buf := make([]byte, 18+len(tracks)*shuffleEntryLen)
	put24(buf[0:], uint32(len(tracks)))
	put24(buf[3:], 0x010600)
	put24(buf[6:], 0x12)
	for i, tr := range tracks {
		entry := buf[18+i*shuffleEntryLen : 18+(i+1)*shuffleEntryLen]
		put24(entry[0:], 0x00022e)
		put24(entry[3:], 0x5aa501)
		put24(entry[0x18:], 0x64)
		put24(entry[0x1b:], shuffleKind(tr.kind, tr.path))
		put24(entry[0x1e:], 0x200)
		path := slashPath(tr.path)
		encoded := utf16.Encode([]rune(path))
		if len(encoded) > 261 {
			encoded = encoded[:261]
		}
		for n, c := range encoded {
			binary.LittleEndian.PutUint16(entry[0x21+n*2:], c)
		}
		entry[0x22b] = 0x01
	}
	return buf
}

func tracksFromShuffle(entries []shuffleEntry) []*track {
	out := make([]*track, 0, len(entries))
	for _, entry := range entries {
		base := entry.path
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		title := base
		if dot := strings.LastIndex(title, "."); dot > 0 {
			title = title[:dot]
		}
		out = append(out, &track{
			title: title,
			path:  colonPath(entry.path),
			kind:  shuffleKindName(entry.kind),
		})
	}
	return out
}

func shuffleKindName(kind uint32) string {
	switch kind {
	case 2:
		return "AAC audio file"
	case 4:
		return "WAV audio file"
	default:
		return "MPEG audio file"
	}
}

func be24(b []byte) uint32 {
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

func put24(b []byte, v uint32) {
	b[0] = byte(v >> 16)
	b[1] = byte(v >> 8)
	b[2] = byte(v)
}
