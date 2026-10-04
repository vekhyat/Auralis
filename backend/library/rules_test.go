package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scanFixture() *Scan {
	return &Scan{
		Root: `C:\music`,
		Tracks: []Track{
			{Path: `C:\music\Artist\Album\01. Song.flac`, RelPath: "Artist/Album/01. Song.flac", Format: "flac", Title: "Song", Artist: "Artist", AlbumArtist: "Artist", Album: "Album", TrackNumber: 1, TrackTotal: 2, DurationSeconds: 100},
			{Path: `C:\music\Artist\Album\02. Song2.flac`, RelPath: "Artist/Album/02. Song2.flac", Format: "flac", Title: "Song2", Artist: "Artist", AlbumArtist: "Someone Else", Album: "Album", TrackNumber: 2, TrackTotal: 2, DurationSeconds: 100},
		},
	}
}

func TestDetectAlbumArtistMissingAndInconsistent(t *testing.T) {
	scan := scanFixture()
	issues := albumArtistRule.Detect(scan, ProfileByID(ProfilePoweramp))
	// Inconsistent album artist in the folder is the primary issue.
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "inconsistent") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected inconsistency issue, got %+v", issues)
	}

	scan2 := &Scan{Tracks: []Track{
		{Path: `C:\m\A\L\1.flac`, RelPath: "A/L/1.flac", Artist: "X", Album: "L"},
	}}
	issues2 := albumArtistRule.Detect(scan2, ProfileByID(ProfilePoweramp))
	if len(issues2) != 1 || !strings.Contains(issues2[0].Message, "no ALBUMARTIST") {
		t.Fatalf("expected missing albumartist issue, got %+v", issues2)
	}
}

func TestAlbumArtistFixSetsOneValue(t *testing.T) {
	scan := scanFixture()
	issues := albumArtistRule.Detect(scan, ProfileByID(ProfilePoweramp))
	ops := albumArtistRule.Fix(scan, ProfileByID(ProfilePoweramp), issues)
	if len(ops) == 0 {
		t.Fatal("expected a fix operation")
	}
	for _, op := range ops {
		if op.Set["ALBUMARTIST"] != "Artist" {
			t.Fatalf("expected ALBUMARTIST=Artist, got %q", op.Set["ALBUMARTIST"])
		}
	}
}

func TestCompilationDetectAndFix(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\Various\Compilation\1.flac`, RelPath: "Various/Compilation/1.flac", Album: "Compilation", Artist: "A"},
		{Path: `C:\m\Various\Compilation\2.flac`, RelPath: "Various/Compilation/2.flac", Album: "Compilation", Artist: "B"},
		{Path: `C:\m\Various\Compilation\3.flac`, RelPath: "Various/Compilation/3.flac", Album: "Compilation", Artist: "C"},
	}}
	issues := compilationRule.Detect(scan, ProfileByID(ProfilePoweramp))
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %+v", issues)
	}
	ops := compilationRule.Fix(scan, ProfileByID(ProfilePoweramp), issues)
	if len(ops) != 3 {
		t.Fatalf("expected 3 ops, got %d", len(ops))
	}
	for _, op := range ops {
		if op.Set["ALBUMARTIST"] != "Various Artists" || op.Set["COMPILATION"] != "1" {
			t.Fatalf("bad fix: %+v", op.Set)
		}
	}

	// Already-marked compilation is not flagged.
	for i := range scan.Tracks {
		scan.Tracks[i].Compilation = true
	}
	if issues := compilationRule.Detect(scan, ProfileByID(ProfilePoweramp)); len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}

func TestTrackNumberDetectGapAndFix(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\A\L\1.flac`, RelPath: "A/L/1.flac", TrackNumber: 1, TrackTotal: 3},
		{Path: `C:\m\A\L\3.flac`, RelPath: "A/L/3.flac", TrackNumber: 3, TrackTotal: 3},
		{Path: `C:\m\A\L\4.flac`, RelPath: "A/L/4.flac", TrackNumber: 4, TrackTotal: 3},
	}}
	issues := trackNumberRule.Detect(scan, ProfileByID(ProfilePoweramp))
	if len(issues) == 0 {
		t.Fatal("expected gap/total issues")
	}
	ops := trackNumberRule.Fix(scan, ProfileByID(ProfilePoweramp), issues)
	if len(ops) == 0 {
		t.Fatal("expected fix ops")
	}
	// After applying the normalised numbering, detection must be quiet.
	byOp := map[string]Operation{}
	for _, op := range ops {
		byOp[op.Path] = op
	}
	for i := range scan.Tracks {
		if op, ok := byOp[scan.Tracks[i].Path]; ok {
			var total int
			var num int
			if v, ok := op.Set["TRACKTOTAL"]; ok && v != "" {
				if n := parseLeadingInt(v); n > 0 {
					total = n
				}
			}
			if n := parseLeadingInt(op.Set["TRACKNUMBER"]); n > 0 {
				num = n
			}
			scan.Tracks[i].TrackNumber = num
			scan.Tracks[i].TrackTotal = total
		}
	}
	if issues := trackNumberRule.Detect(scan, ProfileByID(ProfilePoweramp)); len(issues) != 0 {
		t.Fatalf("expected clean after fix, got %+v", issues)
	}
}

func TestTrackNumberUnparseable(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\A\L\1.flac`, RelPath: "A/L/1.flac", TrackNumberRaw: "not-a-number"},
	}}
	issues := trackNumberRule.Detect(scan, ProfileByID(ProfilePoweramp))
	if len(issues) == 0 || !strings.Contains(issues[0].Message, "unparseable") {
		t.Fatalf("expected unparseable issue, got %+v", issues)
	}
}

func TestDiscNumberInconsistent(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\A\L\1.flac`, RelPath: "A/L/1.flac", DiscNumber: 1},
		{Path: `C:\m\A\L\2.flac`, RelPath: "A/L/2.flac", DiscNumber: 0},
	}}
	issues := discNumberRule.Detect(scan, ProfileByID(ProfilePoweramp))
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %+v", issues)
	}
}

func TestCoverRules(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\A\L\1.flac`, RelPath: "A/L/1.flac", Format: "flac", CoverBytes: 0},
		{Path: `C:\m\A\L\2.flac`, RelPath: "A/L/2.flac", Format: "flac", CoverBytes: 2048 * 2048, CoverWidth: 3000, CoverHeight: 3000},
	}}
	issues := coverRule.Detect(scan, ProfileByID(ProfilePoweramp))
	if len(issues) < 3 {
		t.Fatalf("expected missing+oversized issues, got %+v", issues)
	}
}

func TestPathRuleIllegalAndTemplate(t *testing.T) {
	profile := ProfileByID(ProfilePoweramp) // FATSafe
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\A&B\L\01. X?.flac`, RelPath: "A&B/L/01. X?.flac", Format: "flac", Title: "X?", Artist: "A&B", AlbumArtist: "A&B", Album: "L", TrackNumber: 1},
	}}
	issues := pathRule.Detect(scan, profile)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %+v", issues)
	}
	ops := pathRule.Fix(scan, profile, issues)
	if len(ops) != 1 || ops[0].Type != "move" {
		t.Fatalf("expected move, got %+v", ops)
	}
	if strings.ContainsAny(ops[0].NewPath, `?`) {
		t.Fatalf("destination still illegal: %s", ops[0].NewPath)
	}
}

func TestMixedFormatRule(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		{RelPath: "A/L/1.flac", Format: "flac", BitDepth: 16},
		{RelPath: "A/L/2.mp3", Format: "mp3"},
	}}
	issues := mixedFormatRule.Detect(scan, ProfileByID(ProfilePoweramp))
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "mixes formats") {
		t.Fatalf("expected mixed format, got %+v", issues)
	}
}

func TestDuplicatesByISRCAndDuration(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\A\L\1.flac`, RelPath: "A/L/1.flac", ISRC: "USRC12345678", Artist: "A", Title: "T", DurationSeconds: 100},
		{Path: `C:\m\A\L\dup.flac`, RelPath: "A/L/dup.flac", ISRC: "usrc12345678", Artist: "A", Title: "T", DurationSeconds: 100},
		{Path: `C:\m\B\M\1.flac`, RelPath: "B/M/1.flac", Artist: "a", Title: "t", DurationSeconds: 101.5},
	}}
	issues := duplicateRule.Detect(scan, ProfileByID(ProfilePoweramp))
	if len(issues) < 2 {
		t.Fatalf("expected duplicate issues, got %+v", issues)
	}
}

func TestOrphanEmptyFolder(t *testing.T) {
	root := t.TempDir()
	album := filepath.Join(root, "A", "L")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(root, "Empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	scan := &Scan{
		Root:   root,
		Tracks: []Track{{RelPath: "A/L/1.flac", Path: filepath.Join(album, "1.flac")}},
		Dirs:   []string{"A", "A/L", "Empty"},
	}
	issues := orphanRule.Detect(scan, ProfileByID(ProfilePoweramp))
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "empty folder") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected empty folder issue, got %+v", issues)
	}
}
