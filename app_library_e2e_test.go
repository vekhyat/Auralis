package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/vekhyat/Auralis/backend"
	"github.com/vekhyat/Auralis/backend/library"
	"go.senan.xyz/taglib"
)

// e2eFFmpeg returns an ffmpeg binary, or skips the test when there is none.
func e2eFFmpeg(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}
	if path, err := backend.GetFFmpegPath(); err == nil && path != "" {
		return path
	}
	t.Skip("ffmpeg unavailable")
	return ""
}

func e2eCoverJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{R: uint8(30 * x), G: uint8(30 * y), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// e2eMakeAudio writes a one-second FLAC with the given tags and an embedded
// cover, so the only cover findings a test sees are the ones it arranged.
func e2eMakeAudio(t *testing.T, ffmpeg, path string, cover []byte, meta map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=44100", "-y"}
	for key, value := range meta {
		args = append(args, "-metadata", key+"="+value)
	}
	args = append(args, path)
	if out, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture %s: %v %s", path, err, out)
	}
	if err := taglib.WriteImage(path, cover); err != nil {
		t.Fatalf("embed cover in %s: %v", path, err)
	}
}

// e2eTree lists every folder ("d ...") and file ("f ...") under root.
func e2eTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		kind := "f "
		if d.IsDir() {
			kind = "d "
		}
		out = append(out, kind+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// e2eTags reads the tags of every audio file under root, keyed by slash path,
// with tag names upper-cased so a rewrite that changes only their case is not
// mistaken for a change of content.
func e2eTags(t *testing.T, root string) map[string]map[string][]string {
	t.Helper()
	out := map[string]map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".flac") {
			return err
		}
		tags, err := taglib.ReadTags(path)
		if err != nil {
			t.Fatalf("read tags of %s: %v", path, err)
		}
		upper := map[string][]string{}
		for key, values := range tags {
			upper[strings.ToUpper(key)] = values
		}
		rel, _ := filepath.Rel(root, path)
		out[filepath.ToSlash(rel)] = upper
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestLibraryDoctorEndToEnd runs the whole Library Doctor loop over a small but
// awkward library: scan, review, preview every safe fix, apply, rescan, undo,
// rescan. It checks that the doctor reports no false positives, that applying
// only ever runs what the backend previewed, and that undo restores the exact
// original state.
func TestLibraryDoctorEndToEnd(t *testing.T) {
	libTestAppDir(t)
	ffmpeg := e2eFFmpeg(t)
	cover := e2eCoverJPEG(t)

	root := filepath.Join(t.TempDir(), "Music")
	outside := t.TempDir() // a sibling of the library that nothing may touch

	jpgBytes := string(cover)
	add := func(rel string, meta map[string]string) {
		t.Helper()
		e2eMakeAudio(t, ffmpeg, filepath.Join(root, filepath.FromSlash(rel)), cover, meta)
	}
	file := func(rel, content string) {
		t.Helper()
		libTestWrite(t, filepath.Join(root, filepath.FromSlash(rel)), content)
	}

	// A compilation: three artists, no album artist, in a folder that is already
	// named the way the profile wants.
	for i, name := range []struct{ title, artist string }{{"Sunrise", "Alpha"}, {"Midday", "Beta"}, {"Sunset", "Gamma"}} {
		n := string(rune('1' + i))
		add("Various Artists/Summer Hits/0"+n+". "+name.title+".flac", map[string]string{
			"title": name.title, "artist": name.artist, "album": "Summer Hits",
			"tracknumber": n, "tracktotal": "3",
		})
	}
	file("Various Artists/Summer Hits/cover.jpg", jpgBytes)

	// A two-disc album in CD1/CD2 folders with one cover at album level, and a
	// lyrics sidecar that has to follow its track.
	for _, track := range []struct{ rel, title, disc, num string }{
		{"CD1/01. One.flac", "One", "1", "1"},
		{"CD1/02. Two.flac", "Two", "1", "2"},
		{"CD2/01. Three.flac", "Three", "2", "1"},
		{"CD2/02. Four.flac", "Four", "2", "2"},
	} {
		add("Artist Two/Double Album/"+track.rel, map[string]string{
			"title": track.title, "artist": "Artist Two", "albumartist": "Artist Two", "album": "Double Album",
			"tracknumber": track.num, "tracktotal": "2", "discnumber": track.disc, "disctotal": "2",
		})
	}
	file("Artist Two/Double Album/CD1/01. One.lrc", "[00:00.00]one")
	file("Artist Two/Double Album/cover.jpg", jpgBytes)

	// A tidy album under an artist folder that carries its own art.
	add("Solo Artist/Solo Album/01. Solo.flac", map[string]string{
		"title": "Solo", "artist": "Solo Artist", "albumartist": "Solo Artist", "album": "Solo Album",
		"tracknumber": "1", "tracktotal": "1",
	})
	file("Solo Artist/Solo Album/cover.jpg", jpgBytes)
	file("Solo Artist/folder.jpg", jpgBytes)

	// An album whose third file has no track number, recoverable from its name.
	for _, track := range []struct{ rel, title string }{{"01. Alpha.flac", "Alpha"}, {"02. Beta.flac", "Beta"}} {
		add("Third Artist/Third Album/"+track.rel, map[string]string{
			"title": track.title, "artist": "Third Artist", "albumartist": "Third Artist", "album": "Third Album",
			"tracknumber": track.rel[1:2], "tracktotal": "3",
		})
	}
	add("Third Artist/Third Album/03. Gamma.flac", map[string]string{
		"title": "Gamma", "artist": "Third Artist", "albumartist": "Third Artist", "album": "Third Album",
	})
	file("Third Artist/Third Album/cover.jpg", jpgBytes)

	// A folder holding nothing but lyrics, and a tree of empty folders.
	file("Stray Lyrics/lonely.lrc", "[00:00.00]alone")
	for _, dir := range []string{"Empty Tree/Level 1/Level 2", "Empty Tree/Other"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Album-level issues carry the folder slash-relative to the root; per-file
	// ones carry the absolute path.
	rel := func(path string) string {
		if !filepath.IsAbs(path) {
			return filepath.ToSlash(path)
		}
		r, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.ToSlash(r)
	}
	originalTree := e2eTree(t, root)
	originalTags := e2eTags(t, root)

	// --- Scan ---------------------------------------------------------------
	app := &App{}
	before := libTestScan(t, app, root)
	if before.TotalTracks != 11 || before.TotalAlbums != 5 {
		t.Fatalf("test premise: %d tracks in %d albums, want 11 in 5", before.TotalTracks, before.TotalAlbums)
	}

	byRule := map[string][]library.Issue{}
	for _, issue := range before.Issues {
		byRule[issue.RuleID] = append(byRule[issue.RuleID], issue)
		if strings.HasPrefix(rel(issue.Path), "Solo Artist") {
			t.Errorf("the tidy album and the artist art must not be reported: %+v", issue)
		}
	}
	// No false positives: the artist and album art are not strays, a disc
	// folder inherits the album's cover, and the compilation is one finding.
	if got := byRule["COMPILATION"]; len(got) != 1 || rel(got[0].Path) != "Various Artists/Summer Hits" {
		t.Errorf("the compilation must be flagged exactly once, got %+v", got)
	}
	if got := byRule["COVER"]; len(got) != 0 {
		t.Errorf("no cover finding expected (embedded covers, album-level folder art): %+v", got)
	}
	var strays []string
	for _, issue := range byRule["ORPHAN"] {
		if strings.Contains(issue.Message, "stray") {
			strays = append(strays, rel(issue.Path))
		}
	}
	if !reflect.DeepEqual(strays, []string{"Stray Lyrics/lonely.lrc"}) {
		t.Errorf("only the lonely lyrics file is stray, got %v", strays)
	}
	wantCounts := map[string]int{
		"ALBUMARTIST": 1, // the compilation has no album artist
		"COMPILATION": 1,
		"PATH":        7, // three compilation tracks under the wrong artist; four tracks in CD1/CD2
		"TRACKNUMBER": 1, // the file without a number
		"ORPHAN":      5, // four empty folders and the stray lyrics
	}
	gotCounts := map[string]int{}
	for rule, issues := range byRule {
		gotCounts[rule] = len(issues)
	}
	if !reflect.DeepEqual(gotCounts, wantCounts) {
		t.Fatalf("issues per rule = %v, want %v\n%+v", gotCounts, wantCounts, before.Issues)
	}

	// --- Preview every safe fix, by issue ID -----------------------------------
	fixable := fixableIssueIDs(before)
	if len(fixable) != 14 {
		t.Fatalf("want 14 fixable issues, got %d", len(fixable))
	}
	plan, err := app.PreviewLibraryFixes(fixable)
	if err != nil {
		t.Fatalf("PreviewLibraryFixes: %v", err)
	}
	opTypes := map[string]int{}
	for _, op := range plan.Operations {
		opTypes[op.Type]++
		if op.Error != "" {
			t.Fatalf("a safe fix came out with an error: %+v", op)
		}
	}
	if want := map[string]int{"tags": 4, "move": 4, "rmdir": 4}; !reflect.DeepEqual(opTypes, want) {
		t.Fatalf("plan operations = %v, want %v", opTypes, want)
	}
	// Previewing changed nothing.
	if !reflect.DeepEqual(e2eTree(t, root), originalTree) || !reflect.DeepEqual(e2eTags(t, root), originalTags) {
		t.Fatal("PreviewLibraryFixes changed the library")
	}

	// --- A plan the backend never issued cannot write anything --------------------
	soloTrack := filepath.Join(root, "Solo Artist", "Solo Album", "01. Solo.flac")
	lyrics := filepath.Join(root, "Stray Lyrics", "lonely.lrc")
	forged := func() []library.Operation {
		return []library.Operation{
			{Type: "tags", Path: soloTrack, Set: map[string]string{"ALBUMARTIST": "Hacked Artist"}, Reason: "forged"},
			{Type: "move", Path: soloTrack, NewPath: filepath.Join(outside, "escaped.flac"), Reason: "forged"},
			{Type: "move", Path: lyrics, NewPath: filepath.Join(root, "Elsewhere", "lonely.lrc"), Reason: "forged"},
			{Type: "rmdir", Path: filepath.Join(root, "Stray Lyrics"), Reason: "forged"},
		}
	}
	if _, err := app.ApplyLibraryPlan(&library.Plan{ID: "made-up", Operations: forged()}); err == nil ||
		!strings.Contains(err.Error(), "preview the fixes again") {
		t.Fatalf("a forged plan must be refused as out of date, got %v", err)
	}
	if !reflect.DeepEqual(e2eTree(t, root), originalTree) || !reflect.DeepEqual(e2eTags(t, root), originalTags) {
		t.Fatal("a forged plan changed the library")
	}

	// --- The real ID with edited operations runs only the stored plan --------------
	tampered := &library.Plan{ID: plan.ID, Operations: append([]library.Operation(nil), plan.Operations...)}
	for i := range tampered.Operations {
		op := &tampered.Operations[i]
		switch op.Type {
		case "tags":
			op.Set = map[string]string{"ALBUMARTIST": "Hacked Artist", "TITLE": "Hacked"}
		case "move":
			op.NewPath = filepath.Join(outside, "escaped", filepath.Base(op.Path))
		}
	}
	tampered.Operations = append(tampered.Operations, forged()...)

	res, err := app.ApplyLibraryPlan(tampered)
	if err != nil {
		t.Fatalf("ApplyLibraryPlan: %v", err)
	}
	// Applied counts the plan's own operations: four retags, four moves and four
	// folder removals. The two disc folders the moves emptied are removed too,
	// but they are journaled side effects, not planned operations.
	if res.Cancelled || res.Skipped != 0 || len(res.Errors) != 0 || res.Applied != len(plan.Operations) {
		t.Fatalf("apply = %+v, want exactly the %d stored operations applied", res, len(plan.Operations))
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("something was written outside the library: %v, %v", entries, err)
	}
	for _, path := range []string{soloTrack, lyrics, filepath.Join(root, "Stray Lyrics")} {
		if !libTestExists(path) {
			t.Fatalf("a forged operation ran: %s is gone", path)
		}
	}
	if libTestExists(filepath.Join(root, "Elsewhere")) {
		t.Fatal("a forged move ran")
	}
	// A spent preview cannot be replayed.
	if _, err := app.ApplyLibraryPlan(plan); err == nil || !strings.Contains(err.Error(), "preview the fixes again") {
		t.Fatalf("a second apply of the same preview: %v", err)
	}

	// --- Rescan: better, and nothing fixable is left --------------------------------
	after := libTestScan(t, app, root)
	if after.HealthScore <= before.HealthScore {
		t.Fatalf("health score %d -> %d, want an improvement", before.HealthScore, after.HealthScore)
	}
	if left := fixableIssueIDs(after); len(left) != 0 {
		t.Fatalf("fixable issues remain: %v", left)
	}
	if len(after.Issues) != 1 || after.Issues[0].RuleID != "ORPHAN" || rel(after.Issues[0].Path) != "Stray Lyrics/lonely.lrc" {
		t.Fatalf("only the stray lyrics should remain, got %+v", after.Issues)
	}
	afterTags := e2eTags(t, root)
	for _, name := range []string{"Sunrise", "Midday", "Sunset"} {
		var tags map[string][]string
		for path, got := range afterTags {
			if strings.HasPrefix(path, "Various Artists/Summer Hits/") && got["TITLE"][0] == name {
				tags = got
			}
		}
		if tags == nil || tags["ALBUMARTIST"][0] != "Various Artists" || tags["COMPILATION"][0] != "1" {
			t.Fatalf("%s: compilation tags not written (or tampered): %v", name, tags)
		}
	}
	if got := afterTags["Third Artist/Third Album/03. Gamma.flac"]["TRACKNUMBER"]; !reflect.DeepEqual(got, []string{"3"}) {
		t.Fatalf("the missing track number was not written: %v", got)
	}
	if got := afterTags["Solo Artist/Solo Album/01. Solo.flac"]["ALBUMARTIST"]; !reflect.DeepEqual(got, []string{"Solo Artist"}) {
		t.Fatalf("the tidy album was retagged: %v", got)
	}
	for _, path := range []string{
		"Artist Two/Double Album/1-01. One.flac", "Artist Two/Double Album/1-01. One.lrc",
		"Artist Two/Double Album/1-02. Two.flac", "Artist Two/Double Album/2-01. Three.flac",
		"Artist Two/Double Album/2-02. Four.flac", "Artist Two/Double Album/cover.jpg",
	} {
		if !libTestExists(filepath.Join(root, filepath.FromSlash(path))) {
			t.Fatalf("%s is missing after the fix", path)
		}
	}
	for _, gone := range []string{"Artist Two/Double Album/CD1", "Artist Two/Double Album/CD2", "Empty Tree"} {
		if libTestExists(filepath.Join(root, filepath.FromSlash(gone))) {
			t.Fatalf("%s should have been removed", gone)
		}
	}

	// --- Undo announces and reverts exactly that batch ---------------------------------
	info, err := app.GetLastLibraryBatch()
	if err != nil || info == nil {
		t.Fatalf("GetLastLibraryBatch: %+v, %v", info, err)
	}
	if info.BatchID != res.BatchID || info.Count != res.Applied+2 ||
		info.Types["tags"] != 4 || info.Types["move"] != 4 || info.Types["rmdir"] != 6 {
		t.Fatalf("batch info %+v does not describe the applied batch %+v", info, res)
	}
	undone, err := app.UndoLastLibraryFix()
	if err != nil {
		t.Fatalf("UndoLastLibraryFix: %v", err)
	}
	if undone.BatchID != res.BatchID || undone.Applied != info.Count || undone.Skipped != 0 || len(undone.Errors) != 0 {
		t.Fatalf("undo = %+v, want the whole batch reverted", undone)
	}
	if info, err := app.GetLastLibraryBatch(); err != nil || info != nil {
		t.Fatalf("nothing should be left to undo, got %+v, %v", info, err)
	}

	// --- Rescan: back to the original state ---------------------------------------------
	restored := libTestScan(t, app, root)
	if !reflect.DeepEqual(restored.Issues, before.Issues) {
		t.Fatalf("issues after undo differ from the original\n got  %+v\n want %+v", restored.Issues, before.Issues)
	}
	if restored.HealthScore != before.HealthScore || restored.TotalTracks != before.TotalTracks || restored.TotalAlbums != before.TotalAlbums {
		t.Fatalf("report after undo %+v, want %+v", restored, before)
	}
	if got := e2eTree(t, root); !reflect.DeepEqual(got, originalTree) {
		t.Fatalf("files after undo differ\n got  %v\n want %v", got, originalTree)
	}
	if got := e2eTags(t, root); !reflect.DeepEqual(got, originalTags) {
		t.Fatalf("tags after undo differ\n got  %v\n want %v", got, originalTags)
	}
}
