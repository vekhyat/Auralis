package ipod

import (
	"encoding/binary"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
)

const (
	mhodComment = 8

	mhodSortArtist      = 23
	mhodSortTitle       = 27
	mhodSortAlbum       = 28
	mhodSortAlbumArtist = 29
	mhodSortComposer    = 30

	mhodLibIndex = 52
	mhodJump     = 53

	indexSortTitle    = 0x03
	indexSortAlbum    = 0x04
	indexSortArtist   = 0x05
	indexSortGenre    = 0x07
	indexSortComposer = 0x12

	mhipGroupFlag = 256
)

// sourceHashFromComment reads a type-8 comment only when it starts with the
// exact SourceHashPrefix. The remainder is normalized; a bare 64-hex comment
// is left alone.
func sourceHashFromComment(text string) string {
	if !strings.HasPrefix(text, SourceHashPrefix) {
		return ""
	}
	return normalizeSourceHash(text[len(SourceHashPrefix):])
}

func supportedIndexSort(sortType uint32) bool {
	switch sortType {
	case indexSortTitle, indexSortAlbum, indexSortArtist, indexSortGenre, indexSortComposer:
		return true
	default:
		return false
	}
}

func indexMhodProblem(mhod []byte) error {
	if len(mhod) < 16 || string(mhod[0:4]) != "mhod" {
		return nil
	}
	kind := u32(mhod[12:])
	if kind != mhodLibIndex && kind != mhodJump {
		return nil
	}
	if len(mhod) < 28 || !supportedIndexSort(u32(mhod[24:])) {
		return errUnsupportedLayout
	}
	return nil
}

func mhipGrouped(raw []byte) bool {
	if len(raw) < 20 {
		return false
	}
	headerLen := int(u32(raw[4:]))
	if headerLen < 20 || headerLen > len(raw) {
		return false
	}
	if u32(raw[16:]) == mhipGroupFlag {
		return true
	}
	if headerLen >= 36 && len(raw) >= 36 && u32(raw[32:]) != 0 {
		return true
	}
	return false
}

func rebuildLibraryIndexes(mhods [][]byte, tracks []*track) ([][]byte, error) {
	if len(mhods) == 0 {
		return mhods, nil
	}
	out := make([][]byte, len(mhods))
	for i, mhod := range mhods {
		if err := indexMhodProblem(mhod); err != nil {
			return nil, err
		}
		if len(mhod) < 16 || string(mhod[0:4]) != "mhod" {
			out[i] = mhod
			continue
		}
		kind := u32(mhod[12:])
		switch kind {
		case mhodLibIndex:
			built, err := buildMHOD52(u32(mhod[24:]), tracks)
			if err != nil {
				return nil, err
			}
			out[i] = built
		case mhodJump:
			built, err := buildMHOD53(u32(mhod[24:]), tracks)
			if err != nil {
				return nil, err
			}
			out[i] = built
		default:
			out[i] = mhod
		}
	}
	return out, nil
}

type indexKey struct {
	index    int
	title    string
	album    string
	artist   string
	genre    string
	composer string
	trackNr  uint32
	disc     uint32
}

func isSortOverride(kind uint32) bool {
	switch kind {
	case mhodSortArtist, mhodSortTitle, mhodSortAlbum, mhodSortAlbumArtist, mhodSortComposer:
		return true
	default:
		return false
	}
}

func indexKeys(tracks []*track) []indexKey {
	keys := make([]indexKey, len(tracks))
	for i, tr := range tracks {
		keys[i] = indexKey{
			index:    i,
			title:    titleIndexKey(tr),
			album:    albumIndexKey(tr),
			artist:   artistIndexKey(tr),
			genre:    tr.genre,
			composer: composerIndexKey(tr),
			trackNr:  tr.trackNr,
			disc:     tr.disc,
		}
	}
	return keys
}

func titleIndexKey(tr *track) string {
	if tr.sortTitle != "" {
		return tr.sortTitle
	}
	return tr.title
}

func albumIndexKey(tr *track) string {
	if tr.sortAlbum != "" {
		return tr.sortAlbum
	}
	return tr.album
}

// artistIndexKey prefers an explicit sort-artist string, then a sort-album-artist
// string. Otherwise a leading "The " is moved, matching libgpod.
func artistIndexKey(tr *track) string {
	if tr.sortArtist != "" {
		return tr.sortArtist
	}
	if tr.sortAlbumArtist != "" {
		return tr.sortAlbumArtist
	}
	artist := tr.artist
	if len(artist) >= 4 && strings.EqualFold(artist[:4], "the ") {
		return artist[4:] + ", The\x01\x01\x01\x01\x01"
	}
	return artist
}

func composerIndexKey(tr *track) string {
	if tr.sortComposer != "" {
		return tr.sortComposer
	}
	return tr.composer
}

// foldCompare orders case-insensitively so a jump table, which stores the
// uppercase first letter, does not split "A" from "a". The original string
// breaks ties inside that letter.
func foldCompare(a, b string) int {
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func orderedIndexes(tracks []*track, sortType uint32) ([]int, error) {
	if !supportedIndexSort(sortType) {
		return nil, errUnsupportedLayout
	}
	keys := indexKeys(tracks)
	sort.SliceStable(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		switch sortType {
		case indexSortTitle:
			if c := foldCompare(a.title, b.title); c != 0 {
				return c < 0
			}
		case indexSortAlbum:
			if c := compareAlbum(a, b); c != 0 {
				return c < 0
			}
		case indexSortArtist:
			if c := foldCompare(a.artist, b.artist); c != 0 {
				return c < 0
			}
			if c := compareAlbum(a, b); c != 0 {
				return c < 0
			}
		case indexSortGenre:
			if c := foldCompare(a.genre, b.genre); c != 0 {
				return c < 0
			}
			if c := foldCompare(a.artist, b.artist); c != 0 {
				return c < 0
			}
			if c := compareAlbum(a, b); c != 0 {
				return c < 0
			}
		case indexSortComposer:
			if c := foldCompare(a.composer, b.composer); c != 0 {
				return c < 0
			}
			if c := compareAlbum(a, b); c != 0 {
				return c < 0
			}
		}
		return a.index < b.index
	})
	order := make([]int, len(keys))
	for i, key := range keys {
		order[i] = key.index
	}
	return order, nil
}

func compareAlbum(a, b indexKey) int {
	if c := foldCompare(a.album, b.album); c != 0 {
		return c
	}
	if a.disc != b.disc {
		if a.disc < b.disc {
			return -1
		}
		return 1
	}
	if a.trackNr != b.trackNr {
		if a.trackNr < b.trackNr {
			return -1
		}
		return 1
	}
	return foldCompare(a.title, b.title)
}

func primaryIndexText(tr *track, sortType uint32) string {
	switch sortType {
	case indexSortAlbum:
		return albumIndexKey(tr)
	case indexSortArtist:
		return artistIndexKey(tr)
	case indexSortGenre:
		return tr.genre
	case indexSortComposer:
		return composerIndexKey(tr)
	default:
		return titleIndexKey(tr)
	}
}

func jumpLetter(text string) uint16 {
	for _, r := range text {
		if unicode.IsLetter(r) {
			upper := unicode.ToUpper(r)
			units := utf16.Encode([]rune{upper})
			if len(units) == 1 {
				return units[0]
			}
			return '0'
		}
		if unicode.IsDigit(r) {
			return '0'
		}
	}
	return '0'
}

func buildMHOD52(sortType uint32, tracks []*track) ([]byte, error) {
	order, err := orderedIndexes(tracks, sortType)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 72+4*len(order))
	copy(buf[0:4], "mhod")
	binary.LittleEndian.PutUint32(buf[4:], 24)
	binary.LittleEndian.PutUint32(buf[8:], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[12:], mhodLibIndex)
	binary.LittleEndian.PutUint32(buf[24:], sortType)
	binary.LittleEndian.PutUint32(buf[28:], uint32(len(order)))
	for i, index := range order {
		binary.LittleEndian.PutUint32(buf[72+4*i:], uint32(index))
	}
	return buf, nil
}

type jumpEntry struct {
	letter uint16
	start  uint32
	count  uint32
}

func buildMHOD53(sortType uint32, tracks []*track) ([]byte, error) {
	order, err := orderedIndexes(tracks, sortType)
	if err != nil {
		return nil, err
	}
	var entries []jumpEntry
	for i, index := range order {
		letter := jumpLetter(primaryIndexText(tracks[index], sortType))
		if len(entries) == 0 || entries[len(entries)-1].letter != letter {
			entries = append(entries, jumpEntry{letter: letter, start: uint32(i), count: 1})
			continue
		}
		entries[len(entries)-1].count++
	}
	buf := make([]byte, 40+12*len(entries))
	copy(buf[0:4], "mhod")
	binary.LittleEndian.PutUint32(buf[4:], 24)
	binary.LittleEndian.PutUint32(buf[8:], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[12:], mhodJump)
	binary.LittleEndian.PutUint32(buf[24:], sortType)
	binary.LittleEndian.PutUint32(buf[28:], uint32(len(entries)))
	for i, entry := range entries {
		off := 40 + 12*i
		binary.LittleEndian.PutUint16(buf[off:], entry.letter)
		binary.LittleEndian.PutUint32(buf[off+4:], entry.start)
		binary.LittleEndian.PutUint32(buf[off+8:], entry.count)
	}
	return buf, nil
}
