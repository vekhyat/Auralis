package ipod

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestHeaderSizes(t *testing.T) {
	db := newDatabase(ChecksumHash58, []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77})
	header := db.build244(2)
	if len(header) != 244 {
		t.Fatalf("mhbd header = %d, want 244", len(header))
	}
	if binary.LittleEndian.Uint32(header[4:]) != 244 {
		t.Fatalf("mhbd length field = %d", binary.LittleEndian.Uint32(header[4:]))
	}
	prefs := longPlaylistMhod()
	if len(prefs) != 0x288 {
		t.Fatalf("playlist prefs = %d, want 0x288", len(prefs))
	}
	tr := &track{id: 52, title: "Song", artist: "Artist", album: "Album", kind: "MPEG audio file", path: ":iPod_Control:Music:F00:AUR00000000.mp3", size: 1000, sampleRate: 44100, unk126: 0xffff, unk144: 0x000c, dbid: 9}
	encoded, err := db.encodeTrack(tr)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(encoded[4:]) != mhitHeaderLen {
		t.Fatalf("mhit header field = %#x", binary.LittleEndian.Uint32(encoded[4:]))
	}
	if got := binary.LittleEndian.Uint32(encoded[0x10:]); got != 52 {
		t.Fatalf("track id = %d", got)
	}
	if got := binary.LittleEndian.Uint32(encoded[0x24:]); got != 1000 {
		t.Fatalf("size = %d", got)
	}
	if got := binary.LittleEndian.Uint32(encoded[0xd0:]); got != 1 {
		t.Fatalf("mediatype = %#x", got)
	}
	if marker := binary.LittleEndian.Uint32(encoded[0x18:]); marker != 0x4d503320 {
		t.Fatalf("marker = %#x", marker)
	}
}

func TestHash58MatchesLibgpod(t *testing.T) {
	// Digest of bytes 0..127 keyed by FireWire 0011223344556677.
	// Computed independently from libgpod's itdb_hash58.c, not from this package.
	const wantHex = "e9a454c1371fb85bd7d6b2ae9b6c09a32fef80ee"
	sample := make([]byte, 128)
	for i := range sample {
		sample[i] = byte(i)
	}
	got := computeHash58([]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77}, sample)
	want, err := hex.DecodeString(wantHex)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("hash58 = %x, libgpod vector = %x", got, want)
	}
}

func TestHash58RoundTrip(t *testing.T) {
	fw := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77}
	db := newDatabase(ChecksumHash58, fw)
	if err := db.addTrack(&track{title: "One", artist: "A", path: ":iPod_Control:Music:F00:A.mp3", kind: "MPEG audio file", size: 10, unk126: 0xffff, unk144: 0x000c}); err != nil {
		t.Fatal(err)
	}
	data, err := db.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 0x6c {
		t.Fatalf("db length %d", len(data))
	}
	got := append([]byte(nil), data[0x58:0x6c]...)
	copyData := append([]byte(nil), data...)
	for i := 0x18; i < 0x20; i++ {
		copyData[i] = 0
	}
	for i := 0x32; i < 0x46; i++ {
		copyData[i] = 0
	}
	for i := 0x58; i < 0x6c; i++ {
		copyData[i] = 0
	}
	binary.LittleEndian.PutUint16(copyData[0x30:], 1)
	want := computeHash58(fw, copyData)
	if !bytes.Equal(got, want) {
		t.Fatalf("hash58 = %x, want %x", got, want)
	}
	again, err := parseDatabase(data)
	if err != nil {
		t.Fatal(err)
	}
	if again.scheme != 1 {
		t.Fatalf("scheme = %d", again.scheme)
	}
	if len(again.tracks) != 1 || again.tracks[0].title != "One" {
		t.Fatalf("tracks = %+v", again.tracks)
	}
}

func TestAddRemovePreservesUnknownSection(t *testing.T) {
	db := newDatabase(ChecksumNone, nil)
	extra := wrapMHSD(9, []byte("TEST"))
	db.sections = append(db.sections, section{raw: extra})
	if err := db.addTrack(&track{title: "One", artist: "A", album: "L", path: ":iPod_Control:Music:F00:A.mp3", kind: "MPEG audio file", size: 4}); err != nil {
		t.Fatal(err)
	}
	if err := db.addTrack(&track{title: "Two", artist: "B", album: "L", path: ":iPod_Control:Music:F01:B.mp3", kind: "MPEG audio file", size: 5}); err != nil {
		t.Fatal(err)
	}
	data, err := db.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDatabase(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.tracks) != 2 || parsed.tracks[0].title != "One" || parsed.tracks[1].title != "Two" {
		t.Fatalf("titles = %q %q", parsed.tracks[0].title, parsed.tracks[1].title)
	}
	master := parsed.master()
	if master == nil || master.opaque || len(master.mhips) != 2 {
		t.Fatalf("master = %#v", master)
	}
	var found bool
	for _, sec := range parsed.sections {
		if bytes.Equal(sec.raw, extra) {
			found = true
		}
	}
	if !found {
		t.Fatal("unknown mhsd was not preserved")
	}
	id := parsed.tracks[0].id
	if err := parsed.removeTrack(id); err != nil {
		t.Fatal(err)
	}
	data, err = parsed.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = parseDatabase(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.tracks) != 1 || parsed.tracks[0].title != "Two" {
		t.Fatalf("after remove: %+v", parsed.tracks)
	}
	if master = parsed.master(); master == nil || len(master.mhips) != 1 || master.mhips[0].trackID != parsed.tracks[0].id {
		t.Fatalf("mhips = %+v", master)
	}
	found = false
	for _, sec := range parsed.sections {
		if bytes.Equal(sec.raw, extra) {
			found = true
		}
	}
	if !found {
		t.Fatal("unknown mhsd dropped on rewrite")
	}
}

func TestShuffleRoundTrip(t *testing.T) {
	tracks := []*track{{
		title: "Song",
		kind:  "MPEG audio file",
		path:  ":iPod_Control:Music:F00:Song.mp3",
	}}
	data := writeShuffle(tracks)
	if len(data) != 18+shuffleEntryLen {
		t.Fatalf("len = %d", len(data))
	}
	entries, err := parseShuffle(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].path != "/iPod_Control/Music/F00/Song.mp3" || entries[0].kind != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	if _, err := parseShuffle([]byte("bdhs")); err == nil {
		t.Fatal("expected unknown shuffle to fail")
	}
}
