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

func TestTrackNumberGapIsReportedNotRenumbered(t *testing.T) {
	// Tracks 1, 3, 4 of a 12-track album: the album is incomplete, and the
	// existing numbers are correct and must not be rewritten.
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\A\L\01.flac`, RelPath: "A/L/01.flac", Album: "L", TrackNumber: 1, TrackTotal: 12},
		{Path: `C:\m\A\L\03.flac`, RelPath: "A/L/03.flac", Album: "L", TrackNumber: 3, TrackTotal: 12},
		{Path: `C:\m\A\L\04.flac`, RelPath: "A/L/04.flac", Album: "L", TrackNumber: 4, TrackTotal: 12},
	}}
	issues := DetectAll(scan, ProfileByID(ProfilePoweramp))
	var gap bool
	for _, issue := range issues {
		if issue.RuleID != "TRACKNUMBER" {
			continue
		}
		if issue.Fixable {
			t.Fatalf("incomplete album must not be fixable: %+v", issue)
		}
		gap = gap || strings.Contains(issue.Message, "may be missing")
	}
	if !gap {
		t.Fatalf("expected a missing-tracks report, got %+v", issues)
	}
}

func TestTrackNumberFixes(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		// Total lower than the highest number on the disc.
		{Path: `C:\m\A\L\01.flac`, RelPath: "A/L/01.flac", Album: "L", Format: "flac", TrackNumber: 1, TrackTotal: 2, TrackNumberRaw: "1"},
		{Path: `C:\m\A\L\02.flac`, RelPath: "A/L/02.flac", Album: "L", Format: "flac", TrackNumber: 2, TrackTotal: 2, TrackNumberRaw: "2"},
		// Vorbis "3/3" style value.
		{Path: `C:\m\A\L\03.flac`, RelPath: "A/L/03.flac", Album: "L", Format: "flac", TrackNumber: 3, TrackTotal: 3, TrackNumberRaw: "3/3"},
		// Missing number, recoverable from the file name.
		{Path: `C:\m\A\L\04 - Four.flac`, RelPath: "A/L/04 - Four.flac", Album: "L", Format: "flac"},
	}}
	profile := ProfileByID(ProfilePoweramp)
	var fixable []Issue
	for _, issue := range trackNumberRule.Detect(scan, profile) {
		if issue.Fixable {
			fixable = append(fixable, issue)
		}
	}
	ops := trackNumberRule.Fix(scan, profile, fixable)
	got := map[string]map[string]string{}
	for _, op := range ops {
		got[filepath.Base(op.Path)] = op.Set
	}
	want := map[string]map[string]string{
		"01.flac":        {"TRACKNUMBER": "1", "TRACKTOTAL": "4", "TOTALTRACKS": "4"},
		"02.flac":        {"TRACKNUMBER": "2", "TRACKTOTAL": "4", "TOTALTRACKS": "4"},
		"03.flac":        {"TRACKNUMBER": "3", "TRACKTOTAL": "4", "TOTALTRACKS": "4"},
		"04 - Four.flac": {"TRACKNUMBER": "4", "TRACKTOTAL": "4", "TOTALTRACKS": "4"},
	}
	if len(got) != len(want) {
		t.Fatalf("ops = %+v", got)
	}
	for name, set := range want {
		for k, v := range set {
			if got[name][k] != v {
				t.Fatalf("%s %s = %q, want %q (all: %+v)", name, k, got[name][k], v, got)
			}
		}
	}
}

func TestAlbumWideRulesSkipLooseFiles(t *testing.T) {
	// Loose singles in the library root, and a folder holding several
	// albums, must never be retagged as one compilation.
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\a.flac`, RelPath: "a.flac", Artist: "A", Album: "X"},
		{Path: `C:\m\b.flac`, RelPath: "b.flac", Artist: "B", Album: "Y"},
		{Path: `C:\m\c.flac`, RelPath: "c.flac", Artist: "C", Album: "Z"},
		{Path: `C:\m\Mix\1.flac`, RelPath: "Mix/1.flac", Artist: "A", Album: "One"},
		{Path: `C:\m\Mix\2.flac`, RelPath: "Mix/2.flac", Artist: "B", Album: "Two"},
		{Path: `C:\m\Mix\3.flac`, RelPath: "Mix/3.flac", Artist: "C", Album: "Three"},
	}}
	for _, issue := range DetectAll(scan, ProfileByID(ProfilePoweramp)) {
		switch issue.RuleID {
		case "COMPILATION", "ALBUMARTIST", "TRACKNUMBER", "DISCNUMBER":
			t.Fatalf("album rule fired on loose files: %+v", issue)
		}
	}
}

func TestLooksMultiDisc(t *testing.T) {
	for dir, want := range map[string]bool{"A/Album/CD1": true, "A/Album/Disc 2": true, "ACDC/Back in Black": false, "A/Discovery": false} {
		if got := looksMultiDisc(dir); got != want {
			t.Errorf("looksMultiDisc(%q) = %v, want %v", dir, got, want)
		}
	}
}

func TestIssueIDsAreUnique(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\A\L\1.flac`, RelPath: "A/L/1.flac", Album: "L", Format: "flac", CoverBytes: 2 * 1024 * 1024, CoverWidth: 3000, CoverHeight: 3000},
	}}
	seen := map[string]bool{}
	for _, issue := range DetectAll(scan, ProfileByID(ProfilePoweramp)) {
		if seen[issue.ID] {
			t.Fatalf("duplicate issue ID %q", issue.ID)
		}
		seen[issue.ID] = true
	}
}

func TestTrackNumberUnparseable(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		{Path: `C:\m\A\L\1.flac`, RelPath: "A/L/1.flac", Album: "L", TrackNumberRaw: "not-a-number"},
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

func TestBuildPlanKeepsTagOpsFromDifferentRules(t *testing.T) {
	// ALBUMARTIST and TRACKNUMBER both fix the same file. Both operations must
	// survive de-duplication, and the result must not depend on map order.
	scan := &Scan{Root: `C:\music`, Tracks: []Track{
		{Path: `C:\music\A\L\01. x.flac`, RelPath: "A/L/01. x.flac", Format: "flac", Artist: "A", Album: "L"},
	}}
	profile := ProfileByID(ProfilePoweramp)
	for i := 0; i < 20; i++ {
		plan := BuildPlan(scan, profile, DetectAll(scan, profile))
		var sawAlbumArtist, sawTrack bool
		for _, op := range plan.Operations {
			if op.Type != "tags" {
				continue
			}
			if op.Set["ALBUMARTIST"] != "" {
				sawAlbumArtist = true
			}
			if op.Set["TRACKNUMBER"] != "" {
				sawTrack = true
			}
		}
		if !sawAlbumArtist || !sawTrack {
			t.Fatalf("plan %d lost an operation: %+v", i, plan.Operations)
		}
	}
}
