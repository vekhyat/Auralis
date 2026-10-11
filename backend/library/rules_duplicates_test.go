package library

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func dupTrack(name, isrc, artist, title string, duration float64) Track {
	return Track{
		Path:            `C:\m\` + name + ".flac",
		RelPath:         name + ".flac",
		Format:          "flac",
		ISRC:            isrc,
		Artist:          artist,
		Title:           title,
		DurationSeconds: duration,
	}
}

func dupIssues(scan *Scan) []Issue {
	return duplicateRule.Detect(scan, ProfileByID(ProfilePoweramp))
}

func dupIssueFor(t *testing.T, issues []Issue, path, prefix string) Issue {
	t.Helper()
	for _, issue := range issues {
		if issue.Path == path && strings.HasPrefix(issue.Message, prefix) {
			return issue
		}
	}
	t.Fatalf("no %q issue for %s in %+v", prefix, path, issues)
	return Issue{}
}

func TestDuplicatesISRCListsAllOtherCopiesCapped(t *testing.T) {
	scan := &Scan{Tracks: []Track{
		dupTrack("e", "usrc1", "", "", 0),
		dupTrack("a", "USRC1", "", "", 0),
		dupTrack("d", " usrc1 ", "", "", 0),
		dupTrack("c", "USRC1", "", "", 0),
		dupTrack("b", "USRC1", "", "", 0),
		// Different ISRC with two copies: no "more" tail.
		dupTrack("x", "USRC2", "", "", 0),
		dupTrack("y", "USRC2", "", "", 0),
		dupTrack("lonely", "USRC3", "", "", 0),
	}}
	issues := dupIssues(scan)
	if len(issues) != 7 {
		t.Fatalf("want 7 issues (5 + 2), got %d: %+v", len(issues), issues)
	}

	want := map[string]string{
		`C:\m\a.flac`: `same ISRC USRC1 also at C:\m\b.flac, C:\m\c.flac, C:\m\d.flac (+1 more)`,
		`C:\m\b.flac`: `same ISRC USRC1 also at C:\m\a.flac, C:\m\c.flac, C:\m\d.flac (+1 more)`,
		`C:\m\c.flac`: `same ISRC USRC1 also at C:\m\a.flac, C:\m\b.flac, C:\m\d.flac (+1 more)`,
		`C:\m\d.flac`: `same ISRC USRC1 also at C:\m\a.flac, C:\m\b.flac, C:\m\c.flac (+1 more)`,
		`C:\m\e.flac`: `same ISRC USRC1 also at C:\m\a.flac, C:\m\b.flac, C:\m\c.flac (+1 more)`,
		`C:\m\x.flac`: `same ISRC USRC2 also at C:\m\y.flac`,
		`C:\m\y.flac`: `same ISRC USRC2 also at C:\m\x.flac`,
	}
	ids := map[string]bool{}
	for _, issue := range issues {
		if got, ok := want[issue.Path]; !ok || issue.Message != got {
			t.Errorf("%s: message %q, want %q", issue.Path, issue.Message, got)
		}
		id := issue.RuleID + "|" + issue.Path + "|" + issue.Message
		if ids[id] {
			t.Errorf("duplicate issue ID %q", id)
		}
		ids[id] = true
		if issue.Fixable {
			t.Errorf("duplicate issues must be report-only: %+v", issue)
		}
		if issue.AlbumDir != "" {
			t.Errorf("AlbumDir = %q, want the library root", issue.AlbumDir)
		}
	}

	// DetectAll must keep every one of them: IDs are unique, none is dropped.
	var viaDetectAll int
	for _, issue := range DetectAll(scan, ProfileByID(ProfilePoweramp)) {
		if issue.RuleID == "DUPLICATES" {
			viaDetectAll++
		}
	}
	if viaDetectAll != len(issues) {
		t.Fatalf("DetectAll kept %d of %d DUPLICATES issues", viaDetectAll, len(issues))
	}
}

func TestDuplicatesArtistTitleOneIssuePerFile(t *testing.T) {
	var tracks []Track
	for _, name := range []string{"f", "b", "d", "a", "e", "c"} {
		tracks = append(tracks, dupTrack(name, "", "Artist", "Song", 200))
	}
	scan := &Scan{Tracks: tracks}
	issues := dupIssues(scan)
	if len(issues) != 6 {
		t.Fatalf("a group of 6 must give 6 issues, not one per pair: got %d: %+v", len(issues), issues)
	}
	const prefix = "same artist/title and duration within ±2s of "
	want := map[string]string{
		`C:\m\a.flac`: prefix + `C:\m\b.flac, C:\m\c.flac, C:\m\d.flac (+2 more)`,
		`C:\m\b.flac`: prefix + `C:\m\a.flac, C:\m\c.flac, C:\m\d.flac (+2 more)`,
		`C:\m\c.flac`: prefix + `C:\m\a.flac, C:\m\b.flac, C:\m\d.flac (+2 more)`,
		`C:\m\d.flac`: prefix + `C:\m\a.flac, C:\m\b.flac, C:\m\c.flac (+2 more)`,
		`C:\m\e.flac`: prefix + `C:\m\a.flac, C:\m\b.flac, C:\m\c.flac (+2 more)`,
		`C:\m\f.flac`: prefix + `C:\m\a.flac, C:\m\b.flac, C:\m\c.flac (+2 more)`,
	}
	seen := map[string]bool{}
	for _, issue := range issues {
		if seen[issue.Path] {
			t.Errorf("%s reported twice", issue.Path)
		}
		seen[issue.Path] = true
		if issue.Message != want[issue.Path] {
			t.Errorf("%s: message %q, want %q", issue.Path, issue.Message, want[issue.Path])
		}
	}
}

func TestDuplicatesDurationWindowIsPerFile(t *testing.T) {
	// a~b and b~c are within 2s, a~c is not. c matches only b, b matches both,
	// and d (too long) and e (no duration) are not matches at all.
	scan := &Scan{Tracks: []Track{
		dupTrack("a", "", "Artist", "Song", 100),
		dupTrack("b", "", "Artist", "Song", 101.5),
		dupTrack("c", "", "ARTIST ", "song", 103),
		dupTrack("d", "", "Artist", "Song", 120),
		dupTrack("e", "", "Artist", "Song", 0),
		dupTrack("f", "", "Artist", "Other", 100),
	}}
	issues := dupIssues(scan)
	const prefix = "same artist/title and duration within ±2s of "
	want := map[string]string{
		`C:\m\a.flac`: prefix + `C:\m\b.flac`,
		`C:\m\b.flac`: prefix + `C:\m\a.flac, C:\m\c.flac`,
		`C:\m\c.flac`: prefix + `C:\m\b.flac`,
	}
	if len(issues) != len(want) {
		t.Fatalf("want %d issues, got %d: %+v", len(want), len(issues), issues)
	}
	for _, issue := range issues {
		if issue.Message != want[issue.Path] {
			t.Errorf("%s: message %q, want %q", issue.Path, issue.Message, want[issue.Path])
		}
	}

	// Exactly 2.0s apart still matches; a hair more does not.
	edge := &Scan{Tracks: []Track{
		dupTrack("p", "", "A", "T", 10),
		dupTrack("q", "", "A", "T", 12),
		dupTrack("r", "", "A", "T", 14.01),
	}}
	got := dupIssues(edge)
	if len(got) != 2 {
		t.Fatalf("edge: want issues for p and q only, got %+v", got)
	}
	for _, issue := range got {
		if strings.Contains(issue.Message, "r.flac") || issue.Path == `C:\m\r.flac` {
			t.Errorf("r is 2.01s from q and must not match: %+v", issue)
		}
	}
}

func TestDuplicatesSameISRCIsNotReportedTwice(t *testing.T) {
	// a and b share an ISRC and also artist/title/duration. They are reported
	// once, by the ISRC branch. c has no ISRC but the same artist/title: it is
	// reported against a and b, and they are reported against c only.
	scan := &Scan{Tracks: []Track{
		dupTrack("a", "USRC1", "Artist", "Song", 100),
		dupTrack("b", "USRC1", "Artist", "Song", 100),
		dupTrack("c", "", "Artist", "Song", 100.5),
	}}
	issues := dupIssues(scan)
	const tagPrefix = "same artist/title and duration within ±2s of "

	if got := dupIssueFor(t, issues, `C:\m\a.flac`, "same ISRC").Message; got != `same ISRC USRC1 also at C:\m\b.flac` {
		t.Errorf("a ISRC message = %q", got)
	}
	if got := dupIssueFor(t, issues, `C:\m\a.flac`, tagPrefix).Message; got != tagPrefix+`C:\m\c.flac` {
		t.Errorf("a artist/title message = %q", got)
	}
	if got := dupIssueFor(t, issues, `C:\m\b.flac`, tagPrefix).Message; got != tagPrefix+`C:\m\c.flac` {
		t.Errorf("b artist/title message = %q", got)
	}
	if got := dupIssueFor(t, issues, `C:\m\c.flac`, tagPrefix).Message; got != tagPrefix+`C:\m\a.flac, C:\m\b.flac` {
		t.Errorf("c artist/title message = %q", got)
	}
	if len(issues) != 5 {
		t.Errorf("want 5 issues (2 ISRC + 3 artist/title), got %d: %+v", len(issues), issues)
	}

	// With nothing but the ISRC pair, the artist/title branch stays silent.
	pair := &Scan{Tracks: []Track{
		dupTrack("a", "USRC1", "Artist", "Song", 100),
		dupTrack("b", "USRC1", "Artist", "Song", 100),
	}}
	got := dupIssues(pair)
	if len(got) != 2 {
		t.Fatalf("ISRC pair must give exactly 2 issues, got %+v", got)
	}
	for _, issue := range got {
		if !strings.HasPrefix(issue.Message, "same ISRC ") {
			t.Errorf("unexpected artist/title report for an ISRC pair: %+v", issue)
		}
	}
}

func TestDuplicatesOutputIsDeterministic(t *testing.T) {
	var tracks []Track
	for i := 0; i < 12; i++ {
		isrc := ""
		if i%3 == 0 {
			isrc = fmt.Sprintf("ISRC%d", i%2)
		}
		tracks = append(tracks, dupTrack(fmt.Sprintf("t%02d", i), isrc, fmt.Sprintf("A%d", i%2), "Song", 60+float64(i%4)))
	}
	scan := &Scan{Tracks: tracks}
	first := dupIssues(scan)
	if len(first) == 0 {
		t.Fatal("expected issues")
	}
	for i := 0; i < 20; i++ {
		if again := dupIssues(scan); !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d differs:\n%+v\n%+v", i, first, again)
		}
	}
}
