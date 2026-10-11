package library

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func indexFixture() *Scan {
	return &Scan{Root: `C:\m`, Tracks: []Track{
		{Path: `C:\m\A\L\01.flac`, RelPath: "A/L/01.flac", Album: "L"},
		{Path: `C:\m\A\L\02.flac`, RelPath: "A/L/02.flac", Album: "L"},
		{Path: `C:\m\A\L\bad.flac`, RelPath: "A/L/bad.flac", Unreadable: true},
		{Path: `C:\m\B\M\01.flac`, RelPath: "B/M/01.flac", Album: "M"},
		{Path: `C:\m\loose.flac`, RelPath: "loose.flac", Album: "X"},
		{Path: `C:\m\only-bad\x.flac`, RelPath: "only-bad/x.flac", Unreadable: true},
	}}
}

func TestAlbumsGroupsReadableTracksOncePerScan(t *testing.T) {
	scan := indexFixture()
	albums := Albums(scan)
	if len(albums) != 3 || len(albums["A/L"]) != 2 || len(albums["B/M"]) != 1 || len(albums[""]) != 1 {
		t.Fatalf("groups: %v", albums)
	}
	if _, ok := albums["only-bad"]; ok {
		t.Fatal("a folder of unreadable files must not form an album")
	}
	for _, track := range albums["A/L"] {
		if track.Unreadable {
			t.Fatalf("unreadable track in an album: %+v", track)
		}
	}
	// Files keep scan order within a folder, and point into scan.Tracks so a
	// field changed later is seen through the index.
	if albums["A/L"][0] != &scan.Tracks[0] || albums["A/L"][1] != &scan.Tracks[1] {
		t.Fatal("album members must be the scan's own tracks, in scan order")
	}
	scan.Tracks[0].Compilation = true
	if !Albums(scan)["A/L"][0].Compilation {
		t.Fatal("index must see in-place field changes")
	}

	// A caller appending to a group must not write into a shared backing array.
	group := Albums(scan)["A/L"]
	if cap(group) != len(group) {
		t.Fatalf("group has spare capacity %d > %d", cap(group), len(group))
	}
	_ = append(group, &Track{Path: "intruder"})
	if got := Albums(scan)["A/L"]; len(got) != 2 || got[0] != &scan.Tracks[0] || got[1] != &scan.Tracks[1] {
		t.Fatalf("append leaked into the shared group: %v", got)
	}
}

func TestAlbumsOfEmptyScanIsEmptyNotNil(t *testing.T) {
	if albums := Albums(&Scan{}); albums == nil || len(albums) != 0 {
		t.Fatalf("got %#v", albums)
	}
	if TrackByPath(&Scan{}, "x") != nil {
		t.Fatal("empty scan has no tracks")
	}
}

func TestTrackByPathIgnoresCaseAndFirstMatchWins(t *testing.T) {
	scan := indexFixture()
	scan.Tracks = append(scan.Tracks,
		Track{Path: `C:\m\Dup\Song.flac`, RelPath: "Dup/Song.flac", Title: "first"},
		Track{Path: `C:\m\DUP\SONG.FLAC`, RelPath: "DUP/SONG.FLAC", Title: "second"},
	)
	got := TrackByPath(scan, `c:\M\a\l\02.FLAC`)
	if got != &scan.Tracks[1] {
		t.Fatalf("case-insensitive lookup: %+v", got)
	}
	// Unreadable tracks stay findable: fixes and journals address them by path.
	if got := TrackByPath(scan, `C:\m\A\L\bad.flac`); got != &scan.Tracks[2] {
		t.Fatalf("unreadable track: %+v", got)
	}
	if got := TrackByPath(scan, `C:\m\dup\song.flac`); got == nil || got.Title != "first" {
		t.Fatalf("first match must win: %+v", got)
	}
	if TrackByPath(scan, `C:\m\A\L\03.flac`) != nil || TrackByPath(scan, "") != nil {
		t.Fatal("unknown path must not match")
	}
}

// The index must agree with strings.EqualFold, the semantics TrackByPath had
// when it scanned the slice, including folds ToLower alone misses.
func TestTrackByPathFoldsLikeEqualFold(t *testing.T) {
	corpus := []string{
		"", "a", "A", "k", "K", "\u212a", // Kelvin sign folds with k
		"s", "S", "\u017f", // long s folds with s
		"\u03c3", "\u03c2", "\u03a3", // sigma, final sigma, capital sigma
		"\u00df", "\u1e9e", "ss", // sharp s and its capital
		"\u0130", "\u0131", "i", "I", // dotted and dotless i do not fold to i
		"\u00e9", "\u00c9", "e\u0301", // precomposed vs combining accent
		"\u01c4", "\u01c5", "\u01c6", // titlecase digraph
		"C:\\Music\\Ärtist\\Löve.flac", "c:\\music\\ärtist\\löve.FLAC",
		"a\xffb", "a\xfeb", "A\ufffdB", // invalid UTF-8 reads as U+FFFD
		"straSSe", "STRA\u1e9eE", "stra\u00dfe",
	}
	for _, a := range corpus {
		scan := &Scan{Tracks: []Track{{Path: a}}}
		for _, b := range corpus {
			want := strings.EqualFold(a, b)
			got := TrackByPath(scan, b) != nil
			if got != want {
				t.Errorf("TrackByPath(%q, %q) = %v, EqualFold says %v", a, b, got, want)
			}
		}
	}
}

func TestProjectedScanDoesNotShareTheSourceIndex(t *testing.T) {
	scan := indexFixture()
	// Prime the source's index.
	src := Albums(scan)["A/L"]

	ops := []Operation{{Type: "tags", Path: `C:\m\A\L\01.flac`, Set: map[string]string{"ALBUMARTIST": "New"}}}
	projected := projectTags(scan, ops)
	if projected.idx != nil {
		t.Fatal("a projected scan must start without an index")
	}
	if &projected.Tracks[0] == &scan.Tracks[0] {
		t.Fatal("projection must copy the tracks")
	}

	got := Albums(projected)["A/L"]
	if len(got) != 2 || got[0] != &projected.Tracks[0] || got[0] == src[0] {
		t.Fatal("projected albums must point into the projected tracks")
	}
	if got[0].AlbumArtist != "New" || src[0].AlbumArtist != "" {
		t.Fatalf("projected %q, source %q", got[0].AlbumArtist, src[0].AlbumArtist)
	}
	if found := TrackByPath(projected, `c:\m\a\l\01.flac`); found != &projected.Tracks[0] {
		t.Fatalf("projected lookup returned %p, want its own track", found)
	}
	// The source keeps its own index.
	if again := Albums(scan)["A/L"]; again[0] != &scan.Tracks[0] || again[0].AlbumArtist != "" {
		t.Fatal("source scan lost its tracks to the projection")
	}
}

// A Scan copied by value with new Tracks must not answer from the original's
// index even when nothing reset it.
func TestIndexFollowsReplacedTracks(t *testing.T) {
	scan := indexFixture()
	if TrackByPath(scan, `C:\m\B\M\01.flac`) == nil {
		t.Fatal("setup")
	}

	copied := *scan
	copied.Tracks = []Track{{Path: `C:\other\1.flac`, RelPath: "X/1.flac", Album: "Z"}}
	if TrackByPath(&copied, `C:\m\B\M\01.flac`) != nil {
		t.Fatal("copy answered from the source's index")
	}
	if found := TrackByPath(&copied, `C:\OTHER\1.flac`); found != &copied.Tracks[0] {
		t.Fatalf("copy lookup: %+v", found)
	}
	if got := Albums(&copied); len(got) != 1 || len(got["X"]) != 1 {
		t.Fatalf("copy albums: %v", got)
	}
	if TrackByPath(scan, `C:\m\B\M\01.flac`) == nil || len(Albums(scan)["B/M"]) != 1 {
		t.Fatal("source index was disturbed by the copy")
	}

	// Growing the slice in place is noticed too.
	scan.Tracks = append(scan.Tracks, Track{Path: `C:\m\B\M\02.flac`, RelPath: "B/M/02.flac", Album: "M"})
	if TrackByPath(scan, `C:\m\B\M\02.flac`) == nil || len(Albums(scan)["B/M"]) != 2 {
		t.Fatal("index did not follow an appended track")
	}
}

func TestScanIndexIsSafeForConcurrentUse(t *testing.T) {
	const albums, perAlbum = 40, 25
	scan := perfLibrary(t.TempDir(), albums, perAlbum)

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				idx := (g*37 + i*13) % len(scan.Tracks)
				want := &scan.Tracks[idx]
				if got := TrackByPath(scan, strings.ToUpper(want.Path)); got != want {
					errs <- fmt.Errorf("goroutine %d: TrackByPath(%d) = %p, want %p", g, idx, got, want)
					return
				}
				if n := len(Albums(scan)[want.AlbumDir()]); n != perAlbum {
					errs <- fmt.Errorf("goroutine %d: album size %d, want %d", g, n, perAlbum)
					return
				}
				if i%50 == 0 {
					// Copying and projecting while others read must be safe.
					if p := projectTags(scan, nil); len(Albums(p)) != albums {
						errs <- fmt.Errorf("goroutine %d: projected albums %d", g, len(Albums(p)))
						return
					}
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
