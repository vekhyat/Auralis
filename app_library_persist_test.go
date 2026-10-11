package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vekhyat/Auralis/backend/library"
)

// --- Persisted library report -----------------------------------------------

// persistTestRoot builds a library root holding only empty folders: no audio
// files, so no ffmpeg or tag reader is needed, but the orphan rule still
// reports fixable "empty folder" issues.
func persistTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{filepath.Join("Artist", "Album"), "EmptyDir"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func setPersistAppDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AURALIS_APP_DIR", dir)
	return dir
}

func TestLibraryReportSurvivesRestartButDoesNotEnableFixes(t *testing.T) {
	appDir := setPersistAppDir(t)
	root := persistTestRoot(t)

	first := &App{}
	report, err := first.scanLibrary(context.Background(), root, library.ProfilePoweramp, nil)
	if err != nil {
		t.Fatalf("scanLibrary: %v", err)
	}
	if len(report.Issues) == 0 {
		t.Fatal("test premise: expected the empty folders to be reported")
	}
	plan, err := first.PreviewLibraryFixes(fixableIssueIDs(report))
	if err != nil {
		t.Fatalf("PreviewLibraryFixes on the live scan: %v", err)
	}
	if len(plan.Operations) == 0 {
		t.Fatal("test premise: expected a plan with operations")
	}
	if _, err := os.Stat(filepath.Join(appDir, libraryReportName)); err != nil {
		t.Fatalf("report was not persisted: %v", err)
	}

	// A new App has no in-memory scan, like a fresh process.
	restarted := &App{}
	got, err := restarted.GetLibraryReport()
	if err != nil {
		t.Fatalf("GetLibraryReport after restart: %v", err)
	}
	if got == nil {
		t.Fatal("expected the persisted report after restart")
	}
	if !got.Persisted {
		t.Error("a restored report must be marked Persisted")
	}
	if got.Root != report.Root || got.ProfileID != report.ProfileID ||
		got.HealthScore != report.HealthScore || got.TotalTracks != report.TotalTracks ||
		got.TotalAlbums != report.TotalAlbums || len(got.Issues) != len(report.Issues) {
		t.Fatalf("restored report differs:\n got %+v\nwant %+v", got, report)
	}
	for i := range got.Issues {
		if got.Issues[i] != report.Issues[i] {
			t.Fatalf("issue %d differs: got %+v want %+v", i, got.Issues[i], report.Issues[i])
		}
	}

	// Only findings were persisted, not the scan: fixes need a fresh scan.
	if _, err := restarted.PreviewLibraryFixes(nil); err == nil || !strings.Contains(err.Error(), "scan the library again") {
		t.Fatalf("PreviewLibraryFixes with only a persisted report: want a scan-again error, got %v", err)
	}
	if _, err := restarted.ApplyLibraryPlan(plan); err == nil || !strings.Contains(err.Error(), "scan the library again") {
		t.Fatalf("ApplyLibraryPlan with only a persisted report: want a scan-again error, got %v", err)
	}
	for _, dir := range []string{filepath.Join("Artist", "Album"), "EmptyDir"} {
		if _, err := os.Stat(filepath.Join(root, dir)); err != nil {
			t.Fatalf("a refused apply must not touch the library, %s: %v", dir, err)
		}
	}

	// Scanning again makes fixes available and replaces the persisted view.
	if _, err := restarted.scanLibrary(context.Background(), root, library.ProfilePoweramp, nil); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	live, err := restarted.GetLibraryReport()
	if err != nil || live == nil {
		t.Fatalf("GetLibraryReport after rescan: %v, %v", live, err)
	}
	if live.Persisted {
		t.Error("a report from a scan in this session must not be marked Persisted")
	}
	if _, err := restarted.PreviewLibraryFixes(fixableIssueIDs(live)); err != nil {
		t.Fatalf("PreviewLibraryFixes after rescan: %v", err)
	}
}

func TestLibraryReportUnreadableFileMeansNoReport(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"not json":      "this is not json",
		"truncated":     `{"root":"C:\\Music","issues":[{"id":"a"`,
		"json null":     "null",
		"empty object":  "{}",
		"blank root":    `{"root":"","health_score":80}`,
		"wrong type":    `{"root":42}`,
		"array":         `[1,2,3]`,
		"binary noise":  "\x00\x01\xff\xfe",
		"unclosed root": `{"root":"C:\\Music"`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			appDir := setPersistAppDir(t)
			if err := os.WriteFile(filepath.Join(appDir, libraryReportName), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			report, err := (&App{}).GetLibraryReport()
			if err != nil {
				t.Fatalf("a bad report file must not be an error, got %v", err)
			}
			if report != nil {
				t.Fatalf("expected no report, got %+v", report)
			}
		})
	}

	t.Run("missing", func(t *testing.T) {
		setPersistAppDir(t)
		report, err := (&App{}).GetLibraryReport()
		if err != nil || report != nil {
			t.Fatalf("expected (nil, nil) with no file, got %+v, %v", report, err)
		}
	})
}

func TestLibraryReportFailedSaveDoesNotFailScan(t *testing.T) {
	// The app data dir sits below a regular file, so it cannot be created.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AURALIS_APP_DIR", filepath.Join(blocker, "app"))
	root := persistTestRoot(t)

	app := &App{}
	report, err := app.scanLibrary(context.Background(), root, library.ProfilePoweramp, nil)
	if err != nil {
		t.Fatalf("a failed save must not fail the scan: %v", err)
	}
	if report == nil || len(report.Issues) == 0 {
		t.Fatalf("expected the live report, got %+v", report)
	}
	live, err := app.GetLibraryReport()
	if err != nil || live == nil || live.Persisted {
		t.Fatalf("expected the in-memory report, got %+v, %v", live, err)
	}
	if _, err := app.PreviewLibraryFixes(fixableIssueIDs(report)); err != nil {
		t.Fatalf("fixes must still work from the live scan: %v", err)
	}
}

func TestLibraryReportSaveIsAtomicAndLatestScanWins(t *testing.T) {
	appDir := setPersistAppDir(t)
	rootA := persistTestRoot(t)
	rootB := t.TempDir() // no folders, no issues

	app := &App{}
	if _, err := app.scanLibrary(context.Background(), rootA, library.ProfilePoweramp, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := app.scanLibrary(context.Background(), rootB, library.ProfilePoweramp, nil); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(appDir, libraryReportName))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk LibraryReport
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("persisted report is not valid JSON: %v", err)
	}
	if onDisk.Root != rootB {
		t.Fatalf("persisted root = %q, want the latest scan %q", onDisk.Root, rootB)
	}
	if strings.Contains(string(data), `"persisted"`) {
		t.Error("the Persisted marker is a load-time flag and must not be written")
	}

	entries, err := os.ReadDir(appDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", entry.Name())
		}
	}
}

func TestLibraryReportCancelledScanKeepsPreviousReport(t *testing.T) {
	appDir := setPersistAppDir(t)
	root := persistTestRoot(t)

	app := &App{}
	if _, err := app.scanLibrary(context.Background(), root, library.ProfilePoweramp, nil); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(appDir, libraryReportName))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.scanLibrary(ctx, t.TempDir(), library.ProfilePoweramp, nil); err == nil {
		t.Fatal("expected the cancelled scan to fail")
	}

	after, err := os.ReadFile(filepath.Join(appDir, libraryReportName))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a cancelled scan must not replace the persisted report")
	}
}

// --- Health score -------------------------------------------------------------

// healthLibrary returns albums*perAlbum tracks laid out as Artist/Album NN/NN.flac.
func healthLibrary(albums, perAlbum int) []library.Track {
	tracks := make([]library.Track, 0, albums*perAlbum)
	for a := 0; a < albums; a++ {
		for n := 0; n < perAlbum; n++ {
			rel := fmt.Sprintf("Artist/Album %02d/%02d.flac", a, n)
			tracks = append(tracks, library.Track{Path: "/music/" + rel, RelPath: rel})
		}
	}
	return tracks
}

func healthAlbumDir(a int) string { return fmt.Sprintf("Artist/Album %02d", a) }

// trackIssue is a per-file finding, like a missing cover.
func trackIssue(t library.Track, sev library.Severity, msg string) library.Issue {
	return library.Issue{RuleID: "T", Severity: sev, Path: t.Path, AlbumDir: t.AlbumDir(), Message: msg}
}

// albumIssue is one finding about a whole album, like a missing ALBUMARTIST.
func albumIssue(dir string, sev library.Severity, msg string) library.Issue {
	return library.Issue{RuleID: "A", Severity: sev, Path: dir, AlbumDir: dir, Message: msg}
}

// strayIssue is a finding in a folder that holds no tracks.
func strayIssue(dir string) library.Issue {
	return library.Issue{RuleID: "ORPHAN", Severity: library.SeverityInfo, Path: "/music/" + dir, AlbumDir: dir, Message: "empty folder"}
}

func trackIssues(tracks []library.Track, sev library.Severity) []library.Issue {
	issues := make([]library.Issue, 0, len(tracks))
	for _, t := range tracks {
		issues = append(issues, trackIssue(t, sev, "finding"))
	}
	return issues
}

func tracksOfAlbum(tracks []library.Track, a int) []library.Track {
	var out []library.Track
	for _, t := range tracks {
		if t.AlbumDir() == healthAlbumDir(a) {
			out = append(out, t)
		}
	}
	return out
}

func TestCalculateHealthScore(t *testing.T) {
	lib := healthLibrary(50, 10) // 500 tracks in 50 albums

	manyErrorsOneTrack := make([]library.Issue, 0, 200)
	for i := 0; i < 200; i++ {
		manyErrorsOneTrack = append(manyErrorsOneTrack, trackIssue(lib[0], library.SeverityError, fmt.Sprintf("problem %d", i)))
	}

	infoEverywhere := trackIssues(lib, library.SeverityInfo)
	for a := 0; a < 50; a++ {
		infoEverywhere = append(infoEverywhere, albumIssue(healthAlbumDir(a), library.SeverityInfo, "mixed formats"))
	}
	var emptyFolders []library.Issue
	for i := 0; i < 300; i++ {
		emptyFolders = append(emptyFolders, strayIssue(fmt.Sprintf("Empty/%03d", i)))
	}

	tests := []struct {
		name     string
		tracks   []library.Track
		issues   []library.Issue
		min, max int
	}{
		{name: "no issues", tracks: lib, issues: nil, min: 100, max: 100},
		{name: "empty library", tracks: nil, issues: []library.Issue{strayIssue("Empty")}, min: 100, max: 100},
		{
			name: "one bad album among many: one album-level error", tracks: lib,
			issues: []library.Issue{albumIssue(healthAlbumDir(7), library.SeverityError, "no ALBUMARTIST")},
			min:    98, max: 98,
		},
		{
			name: "one bad album among many: every track of it has an error", tracks: lib,
			issues: trackIssues(tracksOfAlbum(lib, 7), library.SeverityError),
			min:    98, max: 98,
		},
		{
			name: "one bad track among many", tracks: lib,
			issues: []library.Issue{trackIssue(lib[3], library.SeverityError, "bad")},
			min:    99, max: 99,
		},
		{
			name: "issue count on one track does not matter", tracks: lib,
			issues: manyErrorsOneTrack,
			min:    99, max: 99,
		},
		{
			name: "every track has a warning", tracks: lib,
			issues: trackIssues(lib, library.SeverityWarning),
			min:    40, max: 40,
		},
		{
			name: "every track has an error", tracks: lib,
			issues: trackIssues(lib, library.SeverityError),
			min:    0, max: 0,
		},
		{
			name: "every album has an album-level warning", tracks: lib,
			issues: func() []library.Issue {
				var out []library.Issue
				for a := 0; a < 50; a++ {
					out = append(out, albumIssue(healthAlbumDir(a), library.SeverityWarning, "no cover"))
				}
				return out
			}(),
			min: 40, max: 40,
		},
		{
			name: "info only on every track and album", tracks: lib,
			issues: infoEverywhere,
			min:    95, max: 99,
		},
		{
			name: "info only: hundreds of empty folders", tracks: lib,
			issues: emptyFolders,
			min:    95, max: 99,
		},
		{
			name: "info only: one folder issue in a small library", tracks: healthLibrary(1, 3),
			issues: []library.Issue{strayIssue("Empty")},
			min:    95, max: 99,
		},
		{
			name: "a single info issue never reaches 100", tracks: healthLibrary(200, 20),
			issues: []library.Issue{trackIssue(healthLibrary(200, 20)[0], library.SeverityInfo, "dup")},
			min:    99, max: 99,
		},
		{
			name: "half the albums have errors", tracks: lib,
			issues: func() []library.Issue {
				var out []library.Issue
				for a := 0; a < 25; a++ {
					out = append(out, albumIssue(healthAlbumDir(a), library.SeverityError, "bad"))
				}
				return out
			}(),
			min: 50, max: 50,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateHealthScore(tt.tracks, tt.issues)
			if got < 0 || got > 100 {
				t.Fatalf("score %d outside 0..100", got)
			}
			if got < tt.min || got > tt.max {
				t.Fatalf("score = %d, want %d..%d", got, tt.min, tt.max)
			}
			if len(tt.issues) > 0 && len(tt.tracks) > 0 && got == 100 {
				t.Fatalf("a library with issues must not score 100")
			}
		})
	}
}

// A bad album of N tracks (one album-level issue) and N tracks that each carry
// the same problem describe the same damage; the raw issue count must not
// separate them.
func TestCalculateHealthScoreAlbumLevelEqualsTrackLevel(t *testing.T) {
	lib := healthLibrary(20, 10)
	bad := tracksOfAlbum(lib, 4)

	album := calculateHealthScore(lib, []library.Issue{albumIssue(healthAlbumDir(4), library.SeverityWarning, "no cover")})
	perTrack := calculateHealthScore(lib, trackIssues(bad, library.SeverityWarning))
	if album != perTrack {
		t.Fatalf("album-level = %d, per-track = %d, want equal", album, perTrack)
	}
}

func TestCalculateHealthScoreSeverityOrder(t *testing.T) {
	lib := healthLibrary(10, 10)
	score := func(sev library.Severity) int {
		return calculateHealthScore(lib, trackIssues(tracksOfAlbum(lib, 0), sev))
	}
	errScore, warnScore, infoScore := score(library.SeverityError), score(library.SeverityWarning), score(library.SeverityInfo)
	if !(errScore < warnScore && warnScore < infoScore) {
		t.Fatalf("want error < warning < info, got %d, %d, %d", errScore, warnScore, infoScore)
	}
	if infoScore < 95 {
		t.Fatalf("info findings in one album should barely register, got %d", infoScore)
	}
}

// Adding an issue, an error above all, must never raise the score. Every
// addition is tried on top of a range of starting points, and then all of them
// are stacked one after another.
func TestCalculateHealthScoreAddingIssuesNeverRaisesScore(t *testing.T) {
	lib := healthLibrary(6, 4)
	albumsWith := func(sev library.Severity, albums ...int) []library.Issue {
		var out []library.Issue
		for _, a := range albums {
			out = append(out, albumIssue(healthAlbumDir(a), sev, "album finding"))
		}
		return out
	}
	bases := map[string][]library.Issue{
		"none":                   nil,
		"info on tracks":         trackIssues(lib[:7], library.SeverityInfo),
		"info on albums":         albumsWith(library.SeverityInfo, 0, 1, 2, 3, 4, 5),
		"empty folders only":     {strayIssue("a"), strayIssue("b"), strayIssue("c"), strayIssue("d"), strayIssue("e"), strayIssue("f"), strayIssue("g")},
		"warnings on some":       trackIssues(lib[:5], library.SeverityWarning),
		"album warnings":         albumsWith(library.SeverityWarning, 1, 3),
		"errors in one album":    trackIssues(tracksOfAlbum(lib, 2), library.SeverityError),
		"errors in every album":  albumsWith(library.SeverityError, 0, 1, 2, 3, 4, 5),
		"warnings on every file": trackIssues(lib, library.SeverityWarning),
		"errors on every file":   trackIssues(lib, library.SeverityError),
	}

	var additions []library.Issue
	for _, sev := range []library.Severity{library.SeverityError, library.SeverityWarning, library.SeverityInfo} {
		for _, tr := range lib {
			additions = append(additions, trackIssue(tr, sev, "added "+string(sev)))
		}
		for a := 0; a < 6; a++ {
			additions = append(additions, albumIssue(healthAlbumDir(a), sev, "added "+string(sev)))
		}
		additions = append(additions,
			strayIssue("Somewhere/Else"),
			library.Issue{RuleID: "X", Severity: sev, Path: "/elsewhere/file.txt", AlbumDir: "Unknown/Dir", Message: "added"},
		)
	}

	for name, base := range bases {
		t.Run(name, func(t *testing.T) {
			before := calculateHealthScore(lib, base)
			for _, add := range additions {
				after := calculateHealthScore(lib, append(append([]library.Issue(nil), base...), add))
				if after > before {
					t.Fatalf("adding %s issue %q at %q raised the score %d -> %d", add.Severity, add.Message, add.Path, before, after)
				}
				if after < 0 || after > 99 {
					t.Fatalf("score %d out of range after adding an issue", after)
				}
			}

			// Stack them all: the score must only go down or stay.
			stacked := append([]library.Issue(nil), base...)
			prev := before
			for _, add := range additions {
				stacked = append(stacked, add)
				cur := calculateHealthScore(lib, stacked)
				if cur > prev {
					t.Fatalf("stacking %s issue at %q raised the score %d -> %d", add.Severity, add.Path, prev, cur)
				}
				prev = cur
			}
		})
	}
}
