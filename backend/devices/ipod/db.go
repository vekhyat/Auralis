package ipod

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"time"
	"unicode/utf16"
)

const (
	mhbdHeaderLen = 244
	mhitHeaderLen = 0x248
	mhsdHeaderLen = 96
	mhlHeaderLen  = 92
	mhypHeaderLen = 108
	mhipHeaderLen = 76
	longMhodLen   = 0x288

	firstTrackID = 52

	mhodTitle       = 1
	mhodPath        = 2
	mhodAlbum       = 3
	mhodArtist      = 4
	mhodGenre       = 5
	mhodKind        = 6
	mhodComposer    = 12
	mhodAlbumArtist = 22
	mhodPosition    = 100
)

var (
	errUnreadableDB = errors.New("iTunesDB is unreadable")
	errOpaqueMaster = errors.New("a playlist on the iPod could not be read, so nothing was changed")
)

var macEpoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)

type track struct {
	id          uint32
	title       string
	artist      string
	album       string
	albumArtist string
	genre       string
	composer    string
	kind        string
	path        string
	size        uint32
	lengthMS    uint32
	trackNr     uint32
	tracks      uint32
	year        uint32
	bitrate     uint32
	sampleRate  uint32
	disc        uint32
	discs       uint32
	added       time.Time
	dbid        uint64
	unk126      uint16
	unk144      uint16
	raw         []byte
}

type mhipRef struct {
	raw      []byte
	trackID  uint32
	position uint32
}

type playlist struct {
	raw    []byte
	header []byte
	mhods  [][]byte
	mhips  []mhipRef
	opaque bool
	master bool
	name   string
}

type section struct {
	kind int // 1 tracks, 2 playlists, 0 raw mhsd
	raw  []byte
}

// Database is an in-memory iTunesDB. Existing chunks that this writer does
// not understand are kept byte-for-byte.
type Database struct {
	rawHeader []byte
	headerLen int
	scheme    uint16
	dbID      uint64
	id0x24    uint64
	platform  uint16
	language  uint16
	pid       uint64
	unk50     uint32
	unk54     uint32
	hasUnk    bool
	timezone  int32
	hasTZ     bool

	checksum Checksum
	firewire []byte

	tracks     []*track
	mhltHeader []byte
	playlists  []*playlist
	mhlpHeader []byte
	sections   []section
}

func newDatabase(cs Checksum, firewire []byte) *Database {
	return &Database{
		checksum: cs,
		firewire: append([]byte(nil), firewire...),
		sections: []section{{kind: 1}, {kind: 2}},
	}
}

func parseDatabase(data []byte) (*Database, error) {
	if len(data) < 16 || string(data[0:4]) != "mhbd" {
		return nil, errUnreadableDB
	}
	headerLen := int(u32(data[4:]))
	if headerLen < 16 || headerLen > len(data) {
		return nil, errUnreadableDB
	}
	total := int(u32(data[8:]))
	if total < headerLen || total > len(data) {
		total = len(data)
	}
	db := &Database{
		rawHeader: append([]byte(nil), data[:headerLen]...),
		headerLen: headerLen,
	}
	if headerLen >= 0x18 {
		db.dbID = u64(data[0x18:])
	}
	if headerLen >= 0x22 {
		db.platform = binary.LittleEndian.Uint16(data[0x20:])
	}
	if headerLen >= 0x2c {
		db.id0x24 = u64(data[0x24:])
	}
	if headerLen >= 0x32 {
		db.scheme = binary.LittleEndian.Uint16(data[0x30:])
	}
	if headerLen >= 0x48 {
		db.language = binary.LittleEndian.Uint16(data[0x46:])
	}
	if headerLen >= 0x50 {
		db.pid = u64(data[0x48:])
	}
	if headerLen >= 0x58 {
		db.unk50 = u32(data[0x50:])
		db.unk54 = u32(data[0x54:])
		db.hasUnk = true
	}
	if headerLen >= 0x70 {
		db.timezone = int32(u32(data[0x6c:]))
		db.hasTZ = true
	}

	pos := headerLen
	seenTracks := false
	seenLists := false
	for pos+12 <= total {
		if string(data[pos:pos+4]) != "mhsd" {
			return nil, errUnreadableDB
		}
		chunk, next, ok := oneChunk(data[:total], pos)
		if !ok {
			return nil, errUnreadableDB
		}
		kind := u32(chunk[12:])
		switch kind {
		case 1:
			if seenTracks {
				db.sections = append(db.sections, section{raw: append([]byte(nil), chunk...)})
			} else if err := db.parseTracks(chunk); err != nil {
				return nil, err
			} else {
				db.sections = append(db.sections, section{kind: 1})
				seenTracks = true
			}
		case 2:
			if seenLists {
				db.sections = append(db.sections, section{raw: append([]byte(nil), chunk...)})
			} else if err := db.parsePlaylists(chunk); err != nil {
				return nil, err
			} else {
				db.sections = append(db.sections, section{kind: 2})
				seenLists = true
			}
		default:
			db.sections = append(db.sections, section{raw: append([]byte(nil), chunk...)})
		}
		pos = next
	}
	if pos != total {
		return nil, errUnreadableDB
	}
	if !seenTracks {
		db.sections = append(db.sections, section{kind: 1})
	}
	if !seenLists {
		db.sections = append(db.sections, section{kind: 2})
	}
	return db, nil
}

func (db *Database) parseTracks(chunk []byte) error {
	headerLen := int(u32(chunk[4:]))
	if headerLen < 16 || headerLen > len(chunk) {
		return errUnreadableDB
	}
	payload := chunk[headerLen:]
	list, pos, ok := fixedHeader(payload, "mhlt")
	if !ok {
		return errUnreadableDB
	}
	db.mhltHeader = list
	for pos < len(payload) {
		item, n, ok := oneChunk(payload, pos)
		if !ok || string(item[0:4]) != "mhit" {
			return errUnreadableDB
		}
		tr, err := parseTrack(item)
		if err != nil {
			return err
		}
		db.tracks = append(db.tracks, tr)
		pos = n
	}
	if pos != len(payload) {
		return errUnreadableDB
	}
	return nil
}

func parseTrack(chunk []byte) (*track, error) {
	headerLen := int(u32(chunk[4:]))
	if headerLen < 0x14 || headerLen > len(chunk) {
		return nil, errUnreadableDB
	}
	tr := &track{
		id:  u32(chunk[0x10:]),
		raw: append([]byte(nil), chunk...),
	}
	if headerLen >= 0x28 {
		tr.size = u32(chunk[0x24:])
	}
	pos := headerLen
	for pos+12 <= len(chunk) {
		item, n, ok := oneChunk(chunk, pos)
		if !ok {
			return nil, errUnreadableDB
		}
		applyString(tr, item)
		pos = n
	}
	if pos != len(chunk) {
		return nil, errUnreadableDB
	}
	return tr, nil
}

func applyString(tr *track, mhod []byte) {
	if len(mhod) < 16 || string(mhod[0:4]) != "mhod" {
		return
	}
	headerLen := int(u32(mhod[4:]))
	kind := u32(mhod[12:])
	text := mhodString(mhod, headerLen)
	switch kind {
	case mhodTitle:
		tr.title = text
	case mhodPath:
		tr.path = text
	case mhodAlbum:
		tr.album = text
	case mhodArtist:
		tr.artist = text
	case mhodGenre:
		tr.genre = text
	case mhodKind:
		tr.kind = text
	case mhodComposer:
		tr.composer = text
	case mhodAlbumArtist:
		tr.albumArtist = text
	}
}

func mhodString(mhod []byte, headerLen int) string {
	start := headerLen + 16
	if headerLen < 24 || start+4 > len(mhod) {
		return ""
	}
	enc := u32(mhod[headerLen:])
	n := int(u32(mhod[headerLen+4:]))
	if n < 0 || start+n > len(mhod) {
		return ""
	}
	body := mhod[start : start+n]
	if enc == 1 {
		return decodeUTF16(body)
	}
	return string(body)
}

func (db *Database) parsePlaylists(chunk []byte) error {
	headerLen := int(u32(chunk[4:]))
	if headerLen < 16 || headerLen > len(chunk) {
		return errUnreadableDB
	}
	payload := chunk[headerLen:]
	list, pos, ok := fixedHeader(payload, "mhlp")
	if !ok {
		return errUnreadableDB
	}
	db.mhlpHeader = list
	for pos < len(payload) {
		item, n, ok := oneChunk(payload, pos)
		if !ok || string(item[0:4]) != "mhyp" {
			return errUnreadableDB
		}
		db.playlists = append(db.playlists, parsePlaylist(item))
		pos = n
	}
	if pos != len(payload) {
		return errUnreadableDB
	}
	return nil
}

func parsePlaylist(chunk []byte) *playlist {
	pl := &playlist{raw: append([]byte(nil), chunk...)}
	if len(chunk) < 24 {
		pl.opaque = true
		return pl
	}
	headerLen := int(u32(chunk[4:]))
	if headerLen < 24 || headerLen > len(chunk) {
		pl.opaque = true
		return pl
	}
	pl.header = append([]byte(nil), chunk[:headerLen]...)
	pl.master = chunk[20] == 1
	mhods := int(u32(chunk[12:]))
	mhips := int(u32(chunk[16:]))
	pos := headerLen
	for i := 0; i < mhods; i++ {
		item, n, ok := oneChunk(chunk, pos)
		if !ok || string(item[0:4]) != "mhod" {
			pl.opaque = true
			return pl
		}
		pl.mhods = append(pl.mhods, append([]byte(nil), item...))
		if u32(item[12:]) == mhodTitle {
			pl.name = mhodString(item, int(u32(item[4:])))
		}
		pos = n
	}
	for i := 0; i < mhips; i++ {
		item, n, ok := oneChunk(chunk, pos)
		if !ok || string(item[0:4]) != "mhip" {
			pl.opaque = true
			return pl
		}
		pl.mhips = append(pl.mhips, mhipRef{
			raw:      append([]byte(nil), item...),
			trackID:  mhipID(item),
			position: mhipPosition(item),
		})
		pos = n
	}
	if pos != len(chunk) {
		pl.opaque = true
		pl.mhods = nil
		pl.mhips = nil
	}
	return pl
}

func mhipID(raw []byte) uint32 {
	if len(raw) < 28 {
		return 0
	}
	return u32(raw[24:])
}

func mhipPosition(raw []byte) uint32 {
	if len(raw) < 8 {
		return 0
	}
	headerLen := int(u32(raw[4:]))
	if headerLen < 0 || headerLen+28 > len(raw) || string(raw[headerLen:headerLen+4]) != "mhod" {
		return 0
	}
	return u32(raw[headerLen+24:])
}

func (db *Database) master() *playlist {
	for _, pl := range db.playlists {
		if pl.master {
			return pl
		}
	}
	return nil
}

func (db *Database) addTrack(tr *track) error {
	if pl := db.master(); pl != nil && pl.opaque {
		return errOpaqueMaster
	}
	if tr.id == 0 {
		tr.id = db.allocID()
	}
	if tr.dbid == 0 {
		tr.dbid = randU64()
	}
	if tr.added.IsZero() {
		tr.added = time.Now()
	}
	db.tracks = append(db.tracks, tr)
	return nil
}

func (db *Database) removeTrack(id uint32) error {
	for _, pl := range db.playlists {
		if pl.opaque {
			return errOpaqueMaster
		}
	}
	next := db.tracks[:0]
	found := false
	for _, tr := range db.tracks {
		if tr.id == id {
			found = true
			continue
		}
		next = append(next, tr)
	}
	if !found {
		return errors.New("track is not on the iPod")
	}
	db.tracks = next
	return nil
}

func (db *Database) allocID() uint32 {
	max := uint32(firstTrackID - 1)
	for _, tr := range db.tracks {
		if tr.id > max {
			max = tr.id
		}
	}
	return max + 1
}

func (db *Database) Marshal() ([]byte, error) {
	if pl := db.master(); pl != nil && pl.opaque {
		return nil, errOpaqueMaster
	}
	body := make([]byte, 0, 4096)
	children := 0
	wroteTracks := false
	wroteLists := false
	for _, sec := range db.sections {
		switch sec.kind {
		case 1:
			part, err := db.writeTracks()
			if err != nil {
				return nil, err
			}
			body = append(body, part...)
			wroteTracks = true
			children++
		case 2:
			part, err := db.writePlaylists()
			if err != nil {
				return nil, err
			}
			body = append(body, part...)
			wroteLists = true
			children++
		default:
			body = append(body, sec.raw...)
			children++
		}
	}
	if !wroteTracks {
		part, err := db.writeTracks()
		if err != nil {
			return nil, err
		}
		body = append(body, part...)
		children++
	}
	if !wroteLists {
		part, err := db.writePlaylists()
		if err != nil {
			return nil, err
		}
		body = append(body, part...)
		children++
	}

	header, err := db.headerBytes(children)
	if err != nil {
		return nil, err
	}
	out := append(header, body...)
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	if db.checksum == ChecksumHash58 {
		if err := applyHash58(out, db.firewire); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (db *Database) headerBytes(children int) ([]byte, error) {
	if db.checksum == ChecksumNone && db.scheme == 0 && len(db.rawHeader) >= 24 && len(db.rawHeader) < 0x6c {
		h := append([]byte(nil), db.rawHeader...)
		binary.LittleEndian.PutUint32(h[20:], uint32(children))
		return h, nil
	}
	return db.build244(children), nil
}

func (db *Database) build244(children int) []byte {
	if db.dbID == 0 {
		db.dbID = randU64()
	}
	if db.id0x24 == 0 {
		db.id0x24 = randU64()
	}
	if db.pid == 0 {
		db.pid = randU64()
	}
	platform := db.platform
	if platform == 0 {
		platform = 2
	}
	language := db.language
	if language == 0 {
		language = 0x6e65
	}
	unk50, unk54 := db.unk50, db.unk54
	if !db.hasUnk {
		if db.checksum == ChecksumHash58 {
			unk50, unk54 = 5, 0x4d
		} else {
			unk50, unk54 = 1, 0x0f
		}
	}
	var tz int32
	if db.hasTZ {
		tz = db.timezone
	} else {
		_, sec := time.Now().Zone()
		tz = int32(sec)
	}

	w := &writer{}
	w.bytes([]byte("mhbd"))
	w.u32(mhbdHeaderLen)
	w.u32(0)
	w.u32(1)
	w.u32(0x30)
	w.u32(uint32(children))
	w.u64(db.dbID)
	w.u16(platform)
	w.u16(0)
	w.u64(db.id0x24)
	w.u32(0)
	w.u16(0)
	w.zeros(20)
	w.u16(language)
	w.u64(db.pid)
	w.u32(unk50)
	w.u32(unk54)
	w.zeros(20)
	w.u32(uint32(tz))
	w.u16(0)
	w.u16(0)
	w.zeros(44)
	w.u16(0)
	w.u16(0)
	w.u16(0)
	w.u16(0)
	w.u16(0)
	w.u8(0)
	w.u8(0)
	w.zeros(56)
	w.zeros(16)
	if len(w.b) != mhbdHeaderLen {
		panic("mhbd header length")
	}
	return w.b
}

func (db *Database) writeTracks() ([]byte, error) {
	payload := append([]byte(nil), mhlCount(db.mhltHeader, "mhlt", len(db.tracks))...)
	for _, tr := range db.tracks {
		if len(tr.raw) > 0 {
			payload = append(payload, tr.raw...)
			continue
		}
		encoded, err := db.encodeTrack(tr)
		if err != nil {
			return nil, err
		}
		payload = append(payload, encoded...)
	}
	return wrapMHSD(1, payload), nil
}

func (db *Database) encodeTrack(tr *track) ([]byte, error) {
	w := &writer{}
	w.bytes([]byte("mhit"))
	w.u32(mhitHeaderLen)
	w.u32(0)
	w.u32(0)
	w.u32(tr.id)
	w.u32(1)
	w.u32(fileTypeMarker(tr.path))
	w.u8(0)
	w.u8(0)
	w.u8(0)
	w.u8(0)
	added := macTime(tr.added)
	w.u32(added)
	w.u32(tr.size)
	w.u32(tr.lengthMS)
	w.u32(tr.trackNr)
	w.u32(tr.tracks)
	w.u32(tr.year)
	w.u32(tr.bitrate)
	rate := tr.sampleRate
	if rate > 0xffff {
		rate = 0xffff
	}
	w.u32(rate << 16)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(tr.disc)
	w.u32(tr.discs)
	w.u32(0)
	w.u32(added)
	w.u32(0)
	w.u64(tr.dbid)
	w.u8(1)
	w.u8(0)
	w.u16(0)
	w.u16(0)
	w.u16(tr.unk126)
	w.u32(0)
	w.u32(0)
	w.f32(float32(tr.sampleRate))
	w.u32(0)
	w.u16(tr.unk144)
	w.u16(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u8(0)
	w.u8(0)
	w.u8(0)
	w.u8(0)
	w.u64(tr.dbid)
	w.u8(0)
	w.u8(0)
	w.u8(0x01)
	w.u8(0)
	w.u32(0)
	w.u32(0)
	w.u64(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(1)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.u16(0)
	w.u16(0)
	w.zeros(28)
	w.u32(0)
	w.u64(db.libraryID())
	w.u32(tr.size)
	w.u32(0)
	w.u64(0x808080808080)
	w.u32(0)
	w.zeros(8)
	w.u32(0)
	w.zeros(20)
	w.u32(0)
	w.u32(0)
	w.u32(1)
	w.u32(0)
	w.zeros(112)
	w.u32(0)
	w.zeros(16)
	w.u32(0)
	w.zeros(80)
	if len(w.b) != mhitHeaderLen {
		return nil, errors.New("mhit header is the wrong length")
	}
	mhods := 0
	add := func(kind uint32, text string) {
		if text == "" {
			return
		}
		w.b = append(w.b, stringMhod(kind, text)...)
		mhods++
	}
	add(mhodTitle, tr.title)
	add(mhodArtist, tr.artist)
	add(mhodAlbum, tr.album)
	add(mhodKind, tr.kind)
	add(mhodPath, tr.path)
	add(mhodGenre, tr.genre)
	add(mhodComposer, tr.composer)
	add(mhodAlbumArtist, tr.albumArtist)
	binary.LittleEndian.PutUint32(w.b[8:], uint32(len(w.b)))
	binary.LittleEndian.PutUint32(w.b[12:], uint32(mhods))
	return w.b, nil
}

func (db *Database) libraryID() uint64 {
	if db.id0x24 == 0 {
		db.id0x24 = randU64()
	}
	return db.id0x24
}

func (db *Database) writePlaylists() ([]byte, error) {
	lists := db.playlists
	if db.master() == nil {
		master, err := db.freshMaster()
		if err != nil {
			return nil, err
		}
		lists = append(lists, master)
	}
	payload := append([]byte(nil), mhlCount(db.mhlpHeader, "mhlp", len(lists))...)
	kept := map[uint32]struct{}{}
	for _, tr := range db.tracks {
		kept[tr.id] = struct{}{}
	}
	for _, pl := range lists {
		var encoded []byte
		var err error
		if pl.master {
			encoded, err = db.encodeMaster(pl, kept)
		} else if pl.opaque {
			encoded = pl.raw
		} else {
			encoded, err = encodePlaylist(pl, kept)
		}
		if err != nil {
			return nil, err
		}
		payload = append(payload, encoded...)
	}
	return wrapMHSD(2, payload), nil
}

func (db *Database) freshMaster() (*playlist, error) {
	pl := &playlist{master: true, name: "iPod"}
	return pl, nil
}

func (db *Database) encodeMaster(pl *playlist, kept map[uint32]struct{}) ([]byte, error) {
	if pl.opaque {
		return nil, errOpaqueMaster
	}
	mhods := pl.mhods
	if len(mhods) == 0 {
		name := pl.name
		if name == "" {
			name = "iPod"
		}
		mhods = [][]byte{stringMhod(mhodTitle, name), longPlaylistMhod()}
	}
	var hips []mhipRef
	present := map[uint32]struct{}{}
	var maxPos uint32
	for _, hip := range pl.mhips {
		if _, ok := kept[hip.trackID]; !ok {
			continue
		}
		hips = append(hips, hip)
		present[hip.trackID] = struct{}{}
		if hip.position+1 > maxPos {
			maxPos = hip.position + 1
		}
	}
	if maxPos < uint32(len(hips)) {
		maxPos = uint32(len(hips))
	}
	for _, tr := range db.tracks {
		if _, ok := present[tr.id]; ok {
			continue
		}
		hips = append(hips, mhipRef{raw: encodeMhip(tr.id, maxPos), trackID: tr.id, position: maxPos})
		maxPos++
	}
	return joinPlaylist(pl.header, true, mhods, hips), nil
}

func encodePlaylist(pl *playlist, kept map[uint32]struct{}) ([]byte, error) {
	if pl.opaque {
		return nil, errOpaqueMaster
	}
	var hips []mhipRef
	for _, hip := range pl.mhips {
		if _, ok := kept[hip.trackID]; ok {
			hips = append(hips, hip)
		}
	}
	return joinPlaylist(pl.header, pl.master, pl.mhods, hips), nil
}

func joinPlaylist(header []byte, master bool, mhods [][]byte, hips []mhipRef) []byte {
	if len(header) < 24 {
		header = freshMhypHeader(master, len(mhods), len(hips))
	} else {
		header = append([]byte(nil), header...)
		binary.LittleEndian.PutUint32(header[12:], uint32(len(mhods)))
		binary.LittleEndian.PutUint32(header[16:], uint32(len(hips)))
	}
	total := len(header)
	for _, m := range mhods {
		total += len(m)
	}
	for _, h := range hips {
		total += len(h.raw)
	}
	if len(header) >= 12 {
		binary.LittleEndian.PutUint32(header[8:], uint32(total))
	}
	out := make([]byte, 0, total)
	out = append(out, header...)
	for _, m := range mhods {
		out = append(out, m...)
	}
	for _, h := range hips {
		out = append(out, h.raw...)
	}
	return out
}

func freshMhypHeader(master bool, mhods, mhips int) []byte {
	w := &writer{}
	w.bytes([]byte("mhyp"))
	w.u32(mhypHeaderLen)
	w.u32(0)
	w.u32(uint32(mhods))
	w.u32(uint32(mhips))
	if master {
		w.u8(1)
	} else {
		w.u8(0)
	}
	w.u8(0)
	w.u8(0)
	w.u8(0)
	w.u32(macTime(time.Now()))
	w.u64(randU64())
	w.u32(0)
	w.u16(1)
	w.u16(0)
	w.u32(0)
	w.zeros(60)
	if len(w.b) != mhypHeaderLen {
		panic("mhyp header length")
	}
	return w.b
}

func encodeMhip(trackID, position uint32) []byte {
	w := &writer{}
	w.bytes([]byte("mhip"))
	w.u32(mhipHeaderLen)
	w.u32(mhipHeaderLen + 44)
	w.u32(1)
	w.u32(0)
	w.u32(0)
	w.u32(trackID)
	w.u32(0)
	w.u32(0)
	w.zeros(40)
	if len(w.b) != mhipHeaderLen {
		panic("mhip header length")
	}
	w.b = append(w.b, positionMhod(position)...)
	return w.b
}

func positionMhod(position uint32) []byte {
	w := &writer{}
	w.bytes([]byte("mhod"))
	w.u32(24)
	w.u32(44)
	w.u32(mhodPosition)
	w.u32(0)
	w.u32(0)
	w.u32(position)
	w.zeros(16)
	return w.b
}

func longPlaylistMhod() []byte {
	w := &writer{}
	w.bytes([]byte("mhod"))
	w.u32(0x18)
	w.u32(longMhodLen)
	w.u32(mhodPosition)
	w.zeros(24)
	w.u32(0x010084)
	w.u32(0x05)
	w.u32(0x09)
	w.u32(0x03)
	w.u32(0x120001)
	w.zeros(12)
	w.u32(0xc80002)
	w.zeros(12)
	w.u32(0x3c000d)
	w.zeros(12)
	w.u32(0x7d0004)
	w.zeros(12)
	w.u32(0x7d0003)
	w.zeros(12)
	w.u32(0x640008)
	w.zeros(12)
	w.u32(0x640017)
	w.u32(0x01)
	w.zeros(8)
	w.u32(0x500014)
	w.u32(0x01)
	w.zeros(8)
	w.u32(0x7d0015)
	w.u32(0x01)
	w.zeros(114 * 4)
	if len(w.b) != longMhodLen {
		panic("playlist prefs length")
	}
	return w.b
}

func stringMhod(kind uint32, text string) []byte {
	body := encodeUTF16(text)
	w := &writer{}
	w.bytes([]byte("mhod"))
	w.u32(24)
	w.u32(uint32(40 + len(body)))
	w.u32(kind)
	w.u32(0)
	w.u32(0)
	w.u32(1)
	w.u32(uint32(len(body)))
	w.u32(1)
	w.u32(0)
	w.bytes(body)
	return w.b
}

func mhlCount(header []byte, magic string, count int) []byte {
	if len(header) >= 12 && string(header[0:4]) == magic {
		b := append([]byte(nil), header...)
		binary.LittleEndian.PutUint32(b[8:], uint32(count))
		return b
	}
	b := make([]byte, mhlHeaderLen)
	copy(b[0:4], magic)
	binary.LittleEndian.PutUint32(b[4:], mhlHeaderLen)
	binary.LittleEndian.PutUint32(b[8:], uint32(count))
	return b
}

func wrapMHSD(kind uint32, payload []byte) []byte {
	b := make([]byte, mhsdHeaderLen+len(payload))
	copy(b[0:4], "mhsd")
	binary.LittleEndian.PutUint32(b[4:], mhsdHeaderLen)
	binary.LittleEndian.PutUint32(b[8:], uint32(len(b)))
	binary.LittleEndian.PutUint32(b[12:], kind)
	copy(b[mhsdHeaderLen:], payload)
	return b
}

func fileTypeMarker(path string) uint32 {
	ext := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			ext = path[i+1:]
			break
		}
		if path[i] == ':' || path[i] == '/' || path[i] == '\\' {
			ext = ""
			break
		}
	}
	var m uint32
	for i := 0; i < 4; i++ {
		m <<= 8
		if i < len(ext) {
			c := ext[i]
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			m |= uint32(c)
		} else {
			m |= ' '
		}
	}
	return m
}

func colonPath(slash string) string {
	if slash == "" {
		return ""
	}
	out := make([]byte, len(slash))
	for i := 0; i < len(slash); i++ {
		if slash[i] == '/' || slash[i] == '\\' {
			out[i] = ':'
		} else {
			out[i] = slash[i]
		}
	}
	if out[0] != ':' {
		return ":" + string(out)
	}
	return string(out)
}

func slashPath(colon string) string {
	out := make([]byte, len(colon))
	for i := 0; i < len(colon); i++ {
		if colon[i] == ':' {
			out[i] = '/'
		} else {
			out[i] = colon[i]
		}
	}
	s := string(out)
	if s == "" || s[0] != '/' {
		return "/" + s
	}
	return s
}

func applyHash58(data, firewire []byte) error {
	if len(data) < 0x6c || len(firewire) == 0 {
		return errors.New("hash58 needs a FireWire ID and a long enough header")
	}
	dbID := append([]byte(nil), data[0x18:0x20]...)
	unk := append([]byte(nil), data[0x32:0x46]...)
	for i := 0x18; i < 0x20; i++ {
		data[i] = 0
	}
	for i := 0x32; i < 0x46; i++ {
		data[i] = 0
	}
	for i := 0x58; i < 0x6c; i++ {
		data[i] = 0
	}
	binary.LittleEndian.PutUint16(data[0x30:], uint16(ChecksumHash58))
	sum := computeHash58(firewire, data)
	copy(data[0x58:], sum)
	copy(data[0x18:], dbID)
	copy(data[0x32:], unk)
	return nil
}

func fixedHeader(data []byte, magic string) ([]byte, int, bool) {
	if len(data) < 8 || string(data[0:4]) != magic {
		return nil, 0, false
	}
	n := int(u32(data[4:]))
	if n < 8 || n > len(data) {
		return nil, 0, false
	}
	return append([]byte(nil), data[:n]...), n, true
}

func oneChunk(data []byte, pos int) ([]byte, int, bool) {
	if pos < 0 || pos+12 > len(data) {
		return nil, pos, false
	}
	total := int(u32(data[pos+8:]))
	header := int(u32(data[pos+4:]))
	if header < 12 || total < header || pos+total > len(data) {
		return nil, pos, false
	}
	return data[pos : pos+total], pos + total, true
}

func macTime(t time.Time) uint32 {
	if t.IsZero() {
		return 0
	}
	sec := t.UTC().Unix() - macEpoch.Unix()
	if sec <= 0 {
		return 0
	}
	return uint32(sec)
}

func randU64() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint64(time.Now().UnixNano())
	}
	v := binary.BigEndian.Uint64(b[:])
	if v == 0 {
		return 1
	}
	return v
}

func encodeUTF16(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	return b
}

func decodeUTF16(b []byte) string {
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
		if u[i] == 0 {
			u = u[:i]
			break
		}
	}
	return string(utf16.Decode(u))
}

func u32(b []byte) uint32 {
	return binary.LittleEndian.Uint32(b[:4])
}

func u64(b []byte) uint64 {
	return binary.LittleEndian.Uint64(b[:8])
}

type writer struct{ b []byte }

func (w *writer) u32(v uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	w.b = append(w.b, buf[:]...)
}

func (w *writer) u16(v uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], v)
	w.b = append(w.b, buf[:]...)
}

func (w *writer) u64(v uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	w.b = append(w.b, buf[:]...)
}

func (w *writer) u8(v byte) { w.b = append(w.b, v) }

func (w *writer) bytes(p []byte) { w.b = append(w.b, p...) }

func (w *writer) zeros(n int) { w.b = append(w.b, make([]byte, n)...) }

func (w *writer) f32(v float32) { w.u32(math.Float32bits(v)) }
