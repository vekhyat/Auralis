package ipod

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustMarshal(t *testing.T, db *Database) []byte {
	t.Helper()
	data, err := db.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRegressionRefusesIncompleteLibraries(t *testing.T) {
	valid := mustMarshal(t, newDatabase(ChecksumNone, nil))
	cases := map[string][]byte{}
	for _, kind := range []uint32{1, 2} {
		header := newDatabase(ChecksumNone, nil).build244(1)
		data := append(header, sectionByKind(valid, kind)...)
		binary.LittleEndian.PutUint32(data[8:], uint32(len(data)))
		name := "missing playlists"
		if kind == 2 {
			name = "missing tracks"
		}
		cases[name] = data
	}
	cases["duplicate tracks"] = insertSection(valid, sectionByKind(valid, 1))
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDatabase(data); !errors.Is(err, errUnreadableDB) {
				t.Fatalf("incomplete library was accepted: %v", err)
			}
			root := writePod(t, "MB147", "0x0011223344556677")
			if err := os.MkdirAll(filepath.Dir(dbPath(root)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dbPath(root), data, 0o644); err != nil {
				t.Fatal(err)
			}
			dev, ok := inspectRoot(root, volumeInfo{})
			if !ok || dev.CanSend || dev.Warning != "database" {
				t.Fatalf("incomplete library did not block sync: %+v", dev)
			}
			if !bytes.Equal(data, mustRead(t, dbPath(root))) {
				t.Fatal("inspection changed the incomplete library")
			}
		})
	}
	for size := 16; size < 32; size++ {
		data := make([]byte, size)
		copy(data, "mhbd")
		binary.LittleEndian.PutUint32(data[4:], uint32(size))
		binary.LittleEndian.PutUint32(data[8:], uint32(size))
		if _, err := parseDatabase(data); !errors.Is(err, errUnreadableDB) {
			t.Fatalf("accepted %d-byte root header: %v", size, err)
		}
	}
}

func mustParse(t *testing.T, data []byte) *Database {
	t.Helper()
	db, err := parseDatabase(data)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func roundTrip(t *testing.T, db *Database) *Database {
	t.Helper()
	return mustParse(t, mustMarshal(t, db))
}

func song(title, artist, album, path string) *track {
	return &track{
		title:  title,
		artist: artist,
		album:  album,
		kind:   "MPEG audio file",
		path:   path,
		size:   100,
	}
}

func appendTrackMhod(raw, mhod []byte) []byte {
	out := append(append([]byte(nil), raw...), mhod...)
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	binary.LittleEndian.PutUint32(out[12:], u32(out[12:])+1)
	return out
}

func sectionByKind(data []byte, kind uint32) []byte {
	if len(data) < 16 {
		return nil
	}
	pos := int(u32(data[4:]))
	for pos+16 <= len(data) {
		chunk, next, ok := oneChunk(data, pos)
		if !ok {
			return nil
		}
		if u32(chunk[12:]) == kind {
			return append([]byte(nil), chunk...)
		}
		pos = next
	}
	return nil
}

func insertSection(data, section []byte) []byte {
	headerLen := int(u32(data[4:]))
	out := make([]byte, 0, len(data)+len(section))
	out = append(out, data[:headerLen]...)
	out = append(out, section...)
	out = append(out, data[headerLen:]...)
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	binary.LittleEndian.PutUint32(out[20:], u32(out[20:])+1)
	return out
}

func playlistsIn(t *testing.T, data []byte, kind uint32) []*playlist {
	t.Helper()
	sec := sectionByKind(data, kind)
	if sec == nil {
		t.Fatalf("missing playlist section %d", kind)
	}
	reader := &Database{}
	if err := reader.parsePlaylists(sec); err != nil {
		t.Fatal(err)
	}
	return reader.playlists
}

func playlistNamed(lists []*playlist, name string) *playlist {
	for _, pl := range lists {
		if pl != nil && pl.name == name {
			return pl
		}
	}
	return nil
}

func mhipIDs(pl *playlist) []uint32 {
	if pl == nil {
		return nil
	}
	ids := make([]uint32, 0, len(pl.mhips))
	for _, hip := range pl.mhips {
		ids = append(ids, hip.trackID)
	}
	return ids
}

func sameU32(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mhodByKind(mhods [][]byte, kind uint32) []byte {
	for _, mhod := range mhods {
		if len(mhod) >= 16 && u32(mhod[12:]) == kind {
			return mhod
		}
	}
	return nil
}

func shellMhod(kind, sortType uint32, size int) []byte {
	buf := make([]byte, size)
	copy(buf, "mhod")
	binary.LittleEndian.PutUint32(buf[4:], 24)
	binary.LittleEndian.PutUint32(buf[8:], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[12:], kind)
	if len(buf) >= 28 {
		binary.LittleEndian.PutUint32(buf[24:], sortType)
	}
	return buf
}

func indexValues(t *testing.T, mhod []byte) []uint32 {
	t.Helper()
	if len(mhod) < 72 || u32(mhod[12:]) != mhodLibIndex {
		t.Fatalf("MHOD52 missing or short: %d bytes", len(mhod))
	}
	count := int(u32(mhod[28:]))
	if len(mhod) != 72+4*count {
		t.Fatalf("MHOD52 length %d, count %d", len(mhod), count)
	}
	for off := 32; off < 72; off += 4 {
		if u32(mhod[off:]) != 0 {
			t.Fatalf("MHOD52 reserved field at %d = %d", off, u32(mhod[off:]))
		}
	}
	out := make([]uint32, count)
	for i := range out {
		out[i] = u32(mhod[72+4*i:])
	}
	return out
}

func assertJumps(t *testing.T, mhod []byte, n int, letters []uint16) {
	t.Helper()
	if len(mhod) < 40 || u32(mhod[12:]) != mhodJump {
		t.Fatalf("MHOD53 missing or short: %d bytes", len(mhod))
	}
	entries := int(u32(mhod[28:]))
	if len(mhod) != 40+12*entries || entries != len(letters) {
		t.Fatalf("MHOD53 entries %d length %d, want %d groups", entries, len(mhod), len(letters))
	}
	for off := 32; off < 40; off += 4 {
		if u32(mhod[off:]) != 0 {
			t.Fatalf("MHOD53 reserved field at %d = %d", off, u32(mhod[off:]))
		}
	}
	covered := 0
	for i, want := range letters {
		off := 40 + 12*i
		letter := binary.LittleEndian.Uint16(mhod[off:])
		pad := binary.LittleEndian.Uint16(mhod[off+2:])
		start := int(u32(mhod[off+4:]))
		count := int(u32(mhod[off+8:]))
		if letter != want || pad != 0 || start != covered || count <= 0 || start+count > n {
			t.Fatalf("jump %d letter %c pad %d start %d count %d, covered %d of %d", i, letter, pad, start, count, covered, n)
		}
		covered += count
	}
	if covered != n {
		t.Fatalf("jump table covers %d of %d", covered, n)
	}
}

func assertIndex(t *testing.T, tracks []*track, sortType uint32, want []uint32, letters []uint16) {
	t.Helper()
	index, err := buildMHOD52(sortType, tracks)
	if err != nil {
		t.Fatal(err)
	}
	if got := indexValues(t, index); !sameU32(got, want) {
		t.Fatalf("sort %d indexes %v, want %v", sortType, got, want)
	}
	jumps, err := buildMHOD53(sortType, tracks)
	if err != nil {
		t.Fatal(err)
	}
	assertJumps(t, jumps, len(tracks), letters)
}

func parsedSortTrack(t *testing.T, tr *track, kind uint32, text string) *track {
	t.Helper()
	db := newDatabase(ChecksumNone, nil)
	encoded, err := db.encodeTrack(tr)
	if err != nil {
		t.Fatal(err)
	}
	encoded = appendTrackMhod(encoded, stringMhod(kind, text))
	parsed, err := parseTrack(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func mustNotPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("%s panicked: %v", name, p)
		}
	}()
	fn()
}

func TestRegressionPlaylistSections(t *testing.T) {
	db := newDatabase(ChecksumNone, nil)
	first := song("Before", "A", "L", ":iPod_Control:Music:F00:before.mp3")
	if err := db.addTrack(first); err != nil {
		t.Fatal(err)
	}
	mix := &playlist{
		name:  "Mix",
		mhods: [][]byte{stringMhod(mhodTitle, "Mix")},
		mhips: []mhipRef{{raw: encodeMhip(first.id, 0), trackID: first.id, position: 0}},
	}
	db.playlists = []*playlist{mix}
	data := mustMarshal(t, db)
	cloned := append([]byte(nil), sectionByKind(data, 2)...)
	binary.LittleEndian.PutUint32(cloned[12:], 3)
	both := insertSection(data, cloned)

	parsed := mustParse(t, both)
	oldID := parsed.tracks[0].id
	if err := parsed.addTrack(song("After", "B", "L", ":iPod_Control:Music:F00:after.mp3")); err != nil {
		t.Fatal(err)
	}
	newID := parsed.tracks[1].id
	if err := parsed.removeTrack(oldID); err != nil {
		t.Fatal(err)
	}
	output := mustMarshal(t, parsed)

	for _, kind := range []uint32{2, 3} {
		lists := playlistsIn(t, output, kind)
		master := playlistNamed(lists, "iPod")
		if master == nil {
			for _, pl := range lists {
				if pl.master {
					master = pl
				}
			}
		}
		if got := mhipIDs(master); !sameU32(got, []uint32{newID}) {
			t.Fatalf("section %d master ids %v, want only %d", kind, got, newID)
		}
		user := playlistNamed(lists, "Mix")
		if user == nil {
			t.Fatalf("section %d lost the Mix playlist", kind)
		}
		if got := mhipIDs(user); len(got) != 0 {
			t.Fatalf("section %d Mix still references %v after removing %d", kind, got, oldID)
		}
		if user.master {
			t.Fatalf("section %d Mix was rewritten as the master", kind)
		}
	}

	t.Run("type 3 alone", func(t *testing.T) {
		solo := append([]byte(nil), data...)
		sec := sectionByKind(solo, 2)
		pos := bytes.Index(solo, sec)
		binary.LittleEndian.PutUint32(solo[pos+12:], 3)
		again := mustParse(t, solo)
		old := again.tracks[0].id
		if err := again.addTrack(song("After", "B", "L", ":iPod_Control:Music:F00:after.mp3")); err != nil {
			t.Fatal(err)
		}
		next := again.tracks[1].id
		if err := again.removeTrack(old); err != nil {
			t.Fatal(err)
		}
		wrote := mustMarshal(t, again)
		if sectionByKind(wrote, 2) != nil {
			t.Fatal("type 3-only library grew a type 2 section")
		}
		var masterPL *playlist
		for _, pl := range playlistsIn(t, wrote, 3) {
			if pl.master {
				masterPL = pl
			}
		}
		if got := mhipIDs(masterPL); !sameU32(got, []uint32{next}) {
			t.Fatalf("type 3 master ids %v, want %d", got, next)
		}
	})
}

func TestRegressionMasterIndexes(t *testing.T) {
	db := newDatabase(ChecksumNone, nil)
	for _, title := range []string{"C", "A", "B"} {
		if err := db.addTrack(song(title, "Artist", "Album", ":iPod_Control:Music:F00:"+title+".mp3")); err != nil {
			t.Fatal(err)
		}
	}
	parsed := roundTrip(t, db)
	master := parsed.master()
	titleMhod := append([]byte(nil), mhodByKind(master.mhods, mhodTitle)...)
	prefs := append([]byte(nil), mhodByKind(master.mhods, mhodPosition)...)
	unknown := append(shellMhod(99, 0, 24), []byte("KEEP")...)
	binary.LittleEndian.PutUint32(unknown[8:], uint32(len(unknown)))
	master.mhods = append(master.mhods, shellMhod(mhodLibIndex, indexSortTitle, 80), shellMhod(mhodJump, indexSortTitle, 40), unknown)
	if err := parsed.addTrack(song("a", "Artist", "Album", ":iPod_Control:Music:F00:lower-a.mp3")); err != nil {
		t.Fatal(err)
	}
	if err := parsed.removeTrack(parsed.tracks[0].id); err != nil {
		t.Fatal(err)
	}
	again := roundTrip(t, parsed)
	master = again.master()
	got := indexValues(t, mhodByKind(master.mhods, mhodLibIndex))
	if !sameU32(got, []uint32{0, 2, 1}) {
		t.Fatalf("title index %v, want A, a, B as 0, 2, 1", got)
	}
	assertJumps(t, mhodByKind(master.mhods, mhodJump), len(again.tracks), []uint16{'A', 'B'})
	if !bytes.Equal(mhodByKind(master.mhods, mhodTitle), titleMhod) {
		t.Fatal("title MHOD changed while indexes were rebuilt")
	}
	if !bytes.Equal(mhodByKind(master.mhods, mhodPosition), prefs) {
		t.Fatal("playlist prefs MHOD changed while indexes were rebuilt")
	}
	if !bytes.Equal(mhodByKind(master.mhods, 99), unknown) {
		t.Fatal("unknown playlist MHOD was not kept")
	}
	if u32(mhodByKind(master.mhods, mhodLibIndex)[24:]) != indexSortTitle {
		t.Fatal("index sort type changed")
	}

	t.Run("album disc and track", func(t *testing.T) {
		db := newDatabase(ChecksumNone, nil)
		tracks := []*track{
			{title: "A", album: "B", disc: 1, trackNr: 2, path: ":iPod_Control:Music:F00:0.mp3", kind: "MPEG audio file"},
			{title: "Z", album: "A", disc: 1, trackNr: 1, path: ":iPod_Control:Music:F00:1.mp3", kind: "MPEG audio file"},
			{title: "Z", album: "B", disc: 1, trackNr: 1, path: ":iPod_Control:Music:F00:2.mp3", kind: "MPEG audio file"},
			{title: "M", album: "B", disc: 2, trackNr: 1, path: ":iPod_Control:Music:F00:3.mp3", kind: "MPEG audio file"},
		}
		for _, tr := range tracks {
			if err := db.addTrack(tr); err != nil {
				t.Fatal(err)
			}
		}
		parsed := roundTrip(t, db)
		parsed.master().mhods = append(parsed.master().mhods,
			shellMhod(mhodLibIndex, indexSortAlbum, 72),
			shellMhod(mhodJump, indexSortAlbum, 40))
		again := roundTrip(t, parsed)
		got := indexValues(t, mhodByKind(again.master().mhods, mhodLibIndex))
		if !sameU32(got, []uint32{1, 2, 0, 3}) {
			t.Fatalf("album index %v, want album, disc, track, title order 1, 2, 0, 3", got)
		}
	})
}

func TestRegressionSortKeys(t *testing.T) {
	t.Run("case fold", func(t *testing.T) {
		tracks := []*track{{title: "A"}, {title: "B"}, {title: "a"}, {title: "b"}}
		assertIndex(t, tracks, indexSortTitle, []uint32{0, 2, 1, 3}, []uint16{'A', 'B'})
		genres := []*track{{genre: "Rock"}, {genre: "jazz"}, {genre: "rock"}}
		assertIndex(t, genres, indexSortGenre, []uint32{1, 0, 2}, []uint16{'J', 'R'})
	})

	t.Run("the artist", func(t *testing.T) {
		tracks := []*track{
			{artist: "The Beatles", title: "Come Together"},
			{artist: "Aaron", title: "A"},
			{artist: "beatles", title: "B"},
		}
		assertIndex(t, tracks, indexSortArtist, []uint32{1, 2, 0}, []uint16{'A', 'B'})
	})

	t.Run("overrides", func(t *testing.T) {
		zulu := song("Zulu", "Zulu", "Zulu", ":iPod_Control:Music:F00:z.mp3")
		zulu.composer = "Zulu"
		other := song("Alpha", "Mike", "Beta", ":iPod_Control:Music:F00:a.mp3")
		other.composer = "Beta"

		title := parsedSortTrack(t, zulu, mhodSortTitle, "Aaron")
		assertIndex(t, []*track{other, title}, indexSortTitle, []uint32{1, 0}, []uint16{'A'})

		album := parsedSortTrack(t, zulu, mhodSortAlbum, "Aaron")
		assertIndex(t, []*track{other, album}, indexSortAlbum, []uint32{1, 0}, []uint16{'A', 'B'})

		artist := parsedSortTrack(t, &track{artist: "The Beatles", title: "Come Together"}, mhodSortArtist, "ZZZ")
		assertIndex(t, []*track{artist, {artist: "Aaron", title: "A"}}, indexSortArtist, []uint32{1, 0}, []uint16{'A', 'Z'})

		albumArtist := parsedSortTrack(t, &track{artist: "Zulu", title: "Z"}, mhodSortAlbumArtist, "Aaron")
		if albumArtist.sortAlbumArtist != "Aaron" || albumArtist.sortArtist != "" {
			t.Fatalf("sort album artist = %q, sort artist = %q", albumArtist.sortAlbumArtist, albumArtist.sortArtist)
		}
		assertIndex(t, []*track{albumArtist, {artist: "Mike", title: "M"}}, indexSortArtist, []uint32{0, 1}, []uint16{'A', 'M'})

		both := parsedSortTrack(t, &track{artist: "Zulu", title: "Z"}, mhodSortArtist, "ZZZ")
		both.sortAlbumArtist = "Aaron"
		assertIndex(t, []*track{both, {artist: "Mike", title: "M"}}, indexSortArtist, []uint32{1, 0}, []uint16{'M', 'Z'})

		composer := parsedSortTrack(t, zulu, mhodSortComposer, "Aaron")
		assertIndex(t, []*track{other, composer}, indexSortComposer, []uint32{1, 0}, []uint16{'A', 'B'})
	})

	t.Run("parsed library", func(t *testing.T) {
		db := newDatabase(ChecksumNone, nil)
		if err := db.addTrack(song("Mike", "Mike", "M", ":iPod_Control:Music:F00:m.mp3")); err != nil {
			t.Fatal(err)
		}
		if err := db.addTrack(song("Zulu", "Zulu", "Z", ":iPod_Control:Music:F00:z.mp3")); err != nil {
			t.Fatal(err)
		}
		parsed := roundTrip(t, db)
		parsed.tracks[1].raw = appendTrackMhod(parsed.tracks[1].raw, stringMhod(mhodSortTitle, "Aaron"))
		parsed = roundTrip(t, parsed)
		if parsed.tracks[1].title != "Zulu" || parsed.tracks[1].sortTitle != "Aaron" {
			t.Fatalf("title %q sort title %q", parsed.tracks[1].title, parsed.tracks[1].sortTitle)
		}
		parsed.master().mhods = append(parsed.master().mhods, shellMhod(mhodLibIndex, indexSortTitle, 72))
		again := roundTrip(t, parsed)
		got := indexValues(t, mhodByKind(again.master().mhods, mhodLibIndex))
		if !sameU32(got, []uint32{1, 0}) {
			t.Fatalf("library title index %v, want sort-title Aaron before Mike", got)
		}
	})
}

func TestRegressionRefusesUnsafeLayouts(t *testing.T) {
	base := func(t *testing.T) *Database {
		t.Helper()
		db := newDatabase(ChecksumNone, nil)
		if err := db.addTrack(song("Keep", "A", "L", ":iPod_Control:Music:F00:keep.mp3")); err != nil {
			t.Fatal(err)
		}
		return roundTrip(t, db)
	}

	t.Run("unsupported sort", func(t *testing.T) {
		parsed := base(t)
		unknownSort := shellMhod(mhodLibIndex, 0x1d, 72)
		parsed.master().mhods = append(parsed.master().mhods, unknownSort)
		before := len(parsed.tracks)
		raw := append([]byte(nil), parsed.tracks[0].raw...)
		if err := parsed.addTrack(song("New", "B", "L", ":iPod_Control:Music:F00:new.mp3")); !errors.Is(err, errUnsupportedLayout) {
			t.Fatalf("addTrack err = %v", err)
		}
		if err := parsed.removeTrack(parsed.tracks[0].id); !errors.Is(err, errUnsupportedLayout) {
			t.Fatalf("removeTrack err = %v", err)
		}
		out, err := parsed.Marshal()
		if !errors.Is(err, errUnsupportedLayout) || out != nil {
			t.Fatalf("Marshal = %d bytes, err %v", len(out), err)
		}
		if len(parsed.tracks) != before || !bytes.Equal(parsed.tracks[0].raw, raw) {
			t.Fatal("refused sort mutated the library")
		}
		if !bytes.Equal(mhodByKind(parsed.master().mhods, mhodLibIndex), unknownSort) {
			t.Fatal("unsupported index MHOD was rewritten")
		}
	})

	t.Run("short index", func(t *testing.T) {
		parsed := base(t)
		parsed.master().mhods = append(parsed.master().mhods, shellMhod(mhodLibIndex, indexSortTitle, 24))
		if err := parsed.addTrack(song("New", "B", "L", ":iPod_Control:Music:F00:new.mp3")); !errors.Is(err, errUnsupportedLayout) {
			t.Fatalf("addTrack err = %v", err)
		}
	})

	t.Run("grouped", func(t *testing.T) {
		db := newDatabase(ChecksumNone, nil)
		if err := db.addTrack(song("Keep", "A", "L", ":iPod_Control:Music:F00:keep.mp3")); err != nil {
			t.Fatal(err)
		}
		data := mustMarshal(t, db)
		for _, off := range []int{16, 32} {
			grouped := append([]byte(nil), sectionByKind(data, 2)...)
			idx := bytes.Index(grouped, []byte("mhip"))
			if idx < 0 || idx+36 > len(grouped) {
				t.Fatal("fixture has no mhip")
			}
			value := uint32(mhipGroupFlag)
			if off == 32 {
				value = 7
			}
			binary.LittleEndian.PutUint32(grouped[idx+off:], value)
			binary.LittleEndian.PutUint32(grouped[12:], 3)
			file := insertSection(data, grouped)
			parsed := mustParse(t, file)
			if !parsed.refuseMutation {
				t.Fatalf("offset %d group marker was accepted as writable", off)
			}
			var preserved []byte
			for _, sec := range parsed.sections {
				if sec.preserve {
					preserved = sec.raw
				}
			}
			if !bytes.Equal(preserved, grouped) {
				t.Fatalf("offset %d grouped section was not kept byte-for-byte", off)
			}
			before := append([]byte(nil), parsed.tracks[0].raw...)
			if err := parsed.addTrack(song("New", "B", "L", ":iPod_Control:Music:F00:new.mp3")); !errors.Is(err, errUnsupportedLayout) {
				t.Fatalf("offset %d addTrack err = %v", off, err)
			}
			if err := parsed.removeTrack(parsed.tracks[0].id); !errors.Is(err, errUnsupportedLayout) {
				t.Fatalf("offset %d removeTrack err = %v", off, err)
			}
			out, err := parsed.Marshal()
			if !errors.Is(err, errUnsupportedLayout) || out != nil {
				t.Fatalf("offset %d Marshal err = %v", off, err)
			}
			if !bytes.Equal(parsed.tracks[0].raw, before) || parsed.tracks[0].title != "Keep" {
				t.Fatal("grouped refusal changed the track")
			}
		}
	})

	t.Run("unreadable sort override", func(t *testing.T) {
		parsed := base(t)
		bad := stringMhod(mhodSortTitle, "Aaron")
		binary.LittleEndian.PutUint32(bad[24:], 99)
		parsed.tracks[0].raw = appendTrackMhod(parsed.tracks[0].raw, bad)
		parsed = roundTrip(t, parsed)
		if !parsed.tracks[0].sortUnreadable || parsed.tracks[0].title != "Keep" {
			t.Fatalf("unreadable sort title=%q flag=%v", parsed.tracks[0].title, parsed.tracks[0].sortUnreadable)
		}
		raw := append([]byte(nil), parsed.tracks[0].raw...)
		if err := parsed.addTrack(song("New", "B", "L", ":iPod_Control:Music:F00:new.mp3")); !errors.Is(err, errUnsupportedLayout) {
			t.Fatalf("addTrack err = %v", err)
		}
		if err := parsed.removeTrack(parsed.tracks[0].id); !errors.Is(err, errUnsupportedLayout) {
			t.Fatalf("removeTrack err = %v", err)
		}
		out, err := parsed.Marshal()
		if !errors.Is(err, errUnsupportedLayout) || out != nil {
			t.Fatalf("Marshal err = %v", err)
		}
		if !bytes.Equal(parsed.tracks[0].raw, raw) {
			t.Fatal("unreadable sort override was rewritten")
		}
	})
}

func TestRegressionParserBounds(t *testing.T) {
	db := newDatabase(ChecksumNone, nil)
	if err := db.addTrack(song("Fixture", "A", "L", ":iPod_Control:Music:F00:fixture.mp3")); err != nil {
		t.Fatal(err)
	}
	valid := mustMarshal(t, db)

	t.Run("prefixes", func(t *testing.T) {
		for n := 0; n < len(valid); n++ {
			mustNotPanic(t, "prefix", func() {
				_, _ = parseDatabase(append([]byte(nil), valid[:n]...))
			})
		}
	})

	t.Run("short headers", func(t *testing.T) {
		for _, n := range []int{24, 25, 26, 27} {
			data := make([]byte, n)
			copy(data, "mhbd")
			binary.LittleEndian.PutUint32(data[4:], uint32(n))
			binary.LittleEndian.PutUint32(data[8:], uint32(n))
			mustNotPanic(t, "mhbd", func() {
				if _, err := parseDatabase(data); err == nil {
					t.Errorf("accepted %d-byte mhbd", n)
				}
			})
		}
	})

	t.Run("declared total", func(t *testing.T) {
		for _, size := range []uint32{0, 12, uint32(len(valid) + 1), 0xffffffff} {
			data := append([]byte(nil), valid...)
			binary.LittleEndian.PutUint32(data[8:], size)
			if _, err := parseDatabase(data); err == nil {
				t.Errorf("accepted total %d for %d bytes", size, len(data))
			}
		}
	})

	t.Run("short chunks", func(t *testing.T) {
		for _, magic := range []string{"mhsd", "mhit", "mhod", "mhip", "mhyp"} {
			for n := 12; n <= 40; n++ {
				chunk := make([]byte, n)
				copy(chunk, magic)
				binary.LittleEndian.PutUint32(chunk[4:], uint32(n))
				binary.LittleEndian.PutUint32(chunk[8:], uint32(n))
				mustNotPanic(t, magic, func() {
					switch magic {
					case "mhsd":
						header := newDatabase(ChecksumNone, nil).build244(1)
						data := append(header, chunk...)
						binary.LittleEndian.PutUint32(data[8:], uint32(len(data)))
						_, _ = parseDatabase(data)
					case "mhit":
						_, _ = parseTrack(chunk)
					case "mhod":
						_ = applyString(&track{}, chunk)
					case "mhip":
						_ = mhipID(chunk)
						_ = mhipPosition(chunk)
					case "mhyp":
						_, _ = parsePlaylist(chunk)
					}
				})
			}
		}
	})

	t.Run("inconsistent counts", func(t *testing.T) {
		badCount := append([]byte(nil), valid...)
		pos := bytes.Index(badCount, []byte("mhit"))
		binary.LittleEndian.PutUint32(badCount[pos+12:], 50)
		if _, err := parseDatabase(badCount); err == nil {
			t.Fatal("accepted an mhit whose MHOD count does not match")
		}
		badChildren := append([]byte(nil), valid...)
		binary.LittleEndian.PutUint32(badChildren[20:], 9)
		if _, err := parseDatabase(badChildren); err == nil {
			t.Fatal("accepted an mhbd whose child count does not match")
		}
		badTitle := stringMhod(mhodTitle, "Hello")
		binary.LittleEndian.PutUint32(badTitle[24:], 99)
		parsed := mustParse(t, valid)
		parsed.tracks[0].raw = appendTrackMhod(parsed.tracks[0].raw, badTitle)
		if _, err := parseDatabase(mustMarshal(t, parsed)); err == nil {
			t.Fatal("accepted a title MHOD with an unknown encoding")
		}
	})

	t.Run("arbitrary", func(t *testing.T) {
		for n := 0; n < 128; n++ {
			buf := make([]byte, n%80)
			for i := range buf {
				buf[i] = byte(n + i)
			}
			mustNotPanic(t, "arbitrary", func() {
				_, _ = parseDatabase(buf)
				_ = applyString(&track{}, buf)
				_, _ = parseTrack(buf)
				_, _ = parsePlaylist(buf)
				_ = mhipID(buf)
				_ = mhipPosition(buf)
			})
		}
	})

	t.Run("unknown section", func(t *testing.T) {
		db := newDatabase(ChecksumNone, nil)
		extra := wrapMHSD(9, []byte("TEST"))
		db.sections = append(db.sections, section{raw: extra})
		if err := db.addTrack(song("One", "A", "L", ":iPod_Control:Music:F00:a.mp3")); err != nil {
			t.Fatal(err)
		}
		if err := db.addTrack(song("Two", "B", "L", ":iPod_Control:Music:F00:b.mp3")); err != nil {
			t.Fatal(err)
		}
		parsed := roundTrip(t, db)
		if err := parsed.removeTrack(parsed.tracks[0].id); err != nil {
			t.Fatal(err)
		}
		again := roundTrip(t, parsed)
		if again.tracks[0].title != "Two" {
			t.Fatalf("remaining title %q", again.tracks[0].title)
		}
		found := false
		for _, sec := range again.sections {
			if bytes.Equal(sec.raw, extra) {
				found = true
			}
		}
		if !found {
			t.Fatal("unknown section was dropped")
		}
	})
}

func TestRegressionSourceHashRoundTrip(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	db := newDatabase(ChecksumNone, nil)
	tr := song("Hashed", "A", "L", ":iPod_Control:Music:F00:h.mp3")
	tr.sourceHash = strings.ToUpper(digest)
	if err := db.addTrack(tr); err != nil {
		t.Fatal(err)
	}
	prefixed := song("Prefixed", "A", "L", ":iPod_Control:Music:F00:p.mp3")
	prefixed.sourceHash = SourceHashPrefix + strings.ToUpper(digest)
	if err := db.addTrack(prefixed); err != nil {
		t.Fatal(err)
	}
	blank := song("Blank", "A", "L", ":iPod_Control:Music:F00:b.mp3")
	blank.sourceHash = "not-a-hash"
	if err := db.addTrack(blank); err != nil {
		t.Fatal(err)
	}
	parsed := roundTrip(t, db)
	if parsed.tracks[0].sourceHash != digest {
		t.Fatalf("sourceHash = %q", parsed.tracks[0].sourceHash)
	}
	if !bytes.Contains(parsed.tracks[0].raw, encodeUTF16(SourceHashPrefix+digest)) {
		t.Fatal("stored comment is not the lowercase source hash")
	}
	if parsed.tracks[1].sourceHash != digest {
		t.Fatalf("prefixed sourceHash = %q", parsed.tracks[1].sourceHash)
	}
	if parsed.tracks[2].sourceHash != "" || bytes.Contains(parsed.tracks[2].raw, encodeUTF16(SourceHashPrefix)) {
		t.Fatal("invalid sourceHash was written")
	}

	original := append([]byte(nil), parsed.tracks[0].raw...)
	parsed.tracks[0].sourceHash = strings.Repeat("e", 64)
	again := roundTrip(t, parsed)
	if again.tracks[0].sourceHash != digest || !bytes.Equal(again.tracks[0].raw, original) {
		t.Fatalf("edited sourceHash rewrote an existing track: %q", again.tracks[0].sourceHash)
	}

	t.Run("comments", func(t *testing.T) {
		db := newDatabase(ChecksumNone, nil)
		if err := db.addTrack(song("Noted", "A", "L", ":iPod_Control:Music:F00:n.mp3")); err != nil {
			t.Fatal(err)
		}
		parsed := roundTrip(t, db)
		user := stringMhod(mhodComment, "user note")
		bare := stringMhod(mhodComment, digest)
		wrong := stringMhod(mhodComment, "AURALIS:source-sha256:"+digest)
		mixed := stringMhod(mhodComment, SourceHashPrefix+strings.ToUpper(digest))
		parsed.tracks[0].raw = appendTrackMhod(parsed.tracks[0].raw, user)
		parsed.tracks[0].raw = appendTrackMhod(parsed.tracks[0].raw, bare)
		parsed.tracks[0].raw = appendTrackMhod(parsed.tracks[0].raw, wrong)
		parsed.tracks[0].raw = appendTrackMhod(parsed.tracks[0].raw, mixed)
		again := roundTrip(t, parsed)
		tr := again.tracks[0]
		if tr.sourceHash != digest {
			t.Fatalf("sourceHash = %q", tr.sourceHash)
		}
		for _, mhod := range [][]byte{user, bare, wrong, mixed} {
			if !bytes.Contains(tr.raw, mhod) {
				t.Fatal("a comment MHOD was dropped")
			}
		}
		tr.sourceHash = strings.Repeat("f", 64)
		kept := append([]byte(nil), tr.raw...)
		final := roundTrip(t, again)
		if final.tracks[0].sourceHash != digest || !bytes.Equal(final.tracks[0].raw, kept) {
			t.Fatal("changing sourceHash rewrote preserved comments")
		}
	})
}
