package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend"
	"github.com/vekhyat/Auralis/backend/library"
)

// --- Helpers -----------------------------------------------------------------

// libTestAppDir points the app data dir at a fresh temp dir for one test. Moves
// open the global library index inside that dir, so the index is closed before
// the dir is removed (cleanups run last-in first-out) and again up front, in
// case an earlier test left it open somewhere else.
func libTestAppDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AURALIS_APP_DIR", dir)
	backend.CloseLibraryIndexDB()
	t.Cleanup(backend.CloseLibraryIndexDB)
	return dir
}

// libTestFolders returns a library root holding only the given empty folders
// (slash-separated, nested ones included). No audio, so no ffmpeg is needed,
// yet every folder is a fixable "empty folder" issue.
func libTestFolders(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(name)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func libTestWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func libTestExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func libTestScan(t *testing.T, app *App, root string) *LibraryReport {
	t.Helper()
	report, err := app.scanLibrary(context.Background(), root, library.ProfilePoweramp, nil)
	if err != nil {
		t.Fatalf("scanLibrary: %v", err)
	}
	return report
}

// fixableIssueIDs lists the ID of every fixable issue in the report, which is
// what a UI that selected "all safe fixes" sends to PreviewLibraryFixes.
func fixableIssueIDs(report *LibraryReport) []string {
	var ids []string
	for _, issue := range report.Issues {
		if issue.Fixable {
			ids = append(ids, issue.ID)
		}
	}
	return ids
}

// libTestIssueFor returns the ID of the issue at path, failing the test when
// there is none.
func libTestIssueFor(t *testing.T, report *LibraryReport, path string) string {
	t.Helper()
	for _, issue := range report.Issues {
		if issue.Path == path {
			return issue.ID
		}
	}
	t.Fatalf("no issue for %s in %+v", path, report.Issues)
	return ""
}

func libTestPreview(t *testing.T, app *App, ids []string) *library.Plan {
	t.Helper()
	plan, err := app.PreviewLibraryFixes(ids)
	if err != nil {
		t.Fatalf("PreviewLibraryFixes: %v", err)
	}
	return plan
}

func libTestApply(t *testing.T, app *App, plan *library.Plan) *library.ApplyResult {
	t.Helper()
	res, err := app.ApplyLibraryPlan(plan)
	if err != nil {
		t.Fatalf("ApplyLibraryPlan: %v", err)
	}
	return res
}

func libTestWantStale(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, errLibraryPreviewStale) || !strings.Contains(err.Error(), "preview the fixes again") {
		t.Fatalf("want the out-of-date preview error, got %v", err)
	}
}

type libTestEvent struct {
	name    string
	payload interface{}
}

// libTestCaptureEvents records every library event until the test ends.
func libTestCaptureEvents(t *testing.T) func() []libTestEvent {
	t.Helper()
	var (
		mu     sync.Mutex
		events []libTestEvent
	)
	libraryEventHook = func(name string, payload interface{}) {
		mu.Lock()
		events = append(events, libTestEvent{name, payload})
		mu.Unlock()
	}
	t.Cleanup(func() { libraryEventHook = nil })
	return func() []libTestEvent {
		mu.Lock()
		defer mu.Unlock()
		return append([]libTestEvent(nil), events...)
	}
}

func libTestApplyHook(t *testing.T, hook func(library.ApplyProgress)) {
	t.Helper()
	libraryApplyProgressHook = hook
	t.Cleanup(func() { libraryApplyProgressHook = nil })
}

// libTestDenyListing makes dir impossible to list for the current user and
// restores it when the test ends, or skips the test where that cannot be done.
func libTestDenyListing(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("icacls", dir, "/deny", "*S-1-1-0:(RD)").CombinedOutput(); err != nil {
			t.Skipf("icacls: %v: %s", err, out)
		}
		t.Cleanup(func() { _ = exec.Command("icacls", dir, "/remove:d", "*S-1-1-0").Run() })
	} else {
		if os.Geteuid() == 0 {
			t.Skip("root can list any folder")
		}
		if err := os.Chmod(dir, 0); err != nil {
			t.Skipf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	}
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("folder is still listable on this system")
	}
}

// --- Preview ------------------------------------------------------------------

func TestPreviewLibraryFixesWithoutIssueIDsIsAnEmptyPlan(t *testing.T) {
	libTestAppDir(t)
	root := libTestFolders(t, "Empty A", "Empty B/Nested", "Stray")
	// A stray lyrics file is reported, but nothing can fix it.
	libTestWrite(t, filepath.Join(root, "Stray", "song.lrc"), "[00:00.00]la")

	app := &App{}
	report := libTestScan(t, app, root)
	fixable := fixableIssueIDs(report)
	if len(fixable) != 3 {
		t.Fatalf("test premise: want 3 fixable empty folders, got %d in %+v", len(fixable), report.Issues)
	}
	var stray string
	for _, issue := range report.Issues {
		if !issue.Fixable {
			stray = issue.ID
		}
	}
	if stray == "" {
		t.Fatal("test premise: want a report-only issue for the stray file")
	}

	seen := map[string]bool{}
	cases := []struct {
		name string
		ids  []string
	}{
		{"nil", nil},
		{"empty", []string{}},
		{"unknown ID", []string{"ORPHAN|nowhere|empty folder"}},
		{"report-only issue", []string{stray}},
		{"blank ID", []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := libTestPreview(t, app, tc.ids)
			if plan.ID == "" {
				t.Fatal("an empty plan still needs an ID")
			}
			if seen[plan.ID] {
				t.Fatalf("plan ID %q handed out twice", plan.ID)
			}
			seen[plan.ID] = true
			if plan.Operations == nil || len(plan.Operations) != 0 {
				t.Fatalf("want an empty, non-nil operation list, got %#v", plan.Operations)
			}
			raw, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"operations":[]`) {
				t.Fatalf("operations must serialise as [], got %s", raw)
			}
			res := libTestApply(t, app, plan)
			if res.Applied != 0 || res.Skipped != 0 || res.Cancelled {
				t.Fatalf("applying an empty plan did something: %+v", res)
			}
		})
	}
	for _, dir := range []string{"Empty A", "Empty B/Nested", "Stray"} {
		if !libTestExists(filepath.Join(root, filepath.FromSlash(dir))) {
			t.Fatalf("an empty plan must leave the library alone, %s is gone", dir)
		}
	}

	// Naming the issues is what selects them.
	plan := libTestPreview(t, app, fixable)
	if len(plan.Operations) != len(fixable) {
		t.Fatalf("want one operation per selected issue, got %d for %d", len(plan.Operations), len(fixable))
	}
	for _, op := range plan.Operations {
		if op.Type != "rmdir" {
			t.Fatalf("unexpected operation %+v", op)
		}
	}
}

// --- Apply by stored plan -------------------------------------------------------

func TestApplyLibraryPlanRunsTheStoredPlanNotTheClientOperations(t *testing.T) {
	libTestAppDir(t)
	root := libTestFolders(t, "Wanted Empty", "Unselected Empty", "Keep")
	precious := filepath.Join(root, "Keep", "precious.txt")
	libTestWrite(t, precious, "irreplaceable")
	outside := filepath.Join(t.TempDir(), "outside-empty")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	report := libTestScan(t, app, root)
	plan := libTestPreview(t, app, []string{libTestIssueFor(t, report, filepath.Join(root, "Wanted Empty"))})
	if len(plan.Operations) != 1 || plan.Operations[0].Type != "rmdir" {
		t.Fatalf("test premise: want one rmdir, got %+v", plan.Operations)
	}

	// Every one of these would succeed if the backend believed the client: the
	// move and the rmdir are inside the root and not stale-checked, and the
	// last one is a perfectly removable empty folder (outside the root).
	forged := func() []library.Operation {
		return []library.Operation{
			{Type: "move", Path: precious, NewPath: filepath.Join(root, "Moved", "precious.txt"), Reason: "forged"},
			{Type: "rmdir", Path: filepath.Join(root, "Unselected Empty"), Reason: "forged"},
			{Type: "rmdir", Path: outside, Reason: "forged"},
		}
	}
	untouched := func(stage string) {
		t.Helper()
		for _, path := range []string{precious, filepath.Join(root, "Unselected Empty"), filepath.Join(root, "Wanted Empty"), outside} {
			if stage == "after" && strings.HasSuffix(path, "Wanted Empty") {
				continue
			}
			if !libTestExists(path) {
				t.Fatalf("%s: %s was removed or moved", stage, path)
			}
		}
		if libTestExists(filepath.Join(root, "Moved")) {
			t.Fatalf("%s: the forged move happened", stage)
		}
	}

	// A plan the backend never issued.
	_, err := app.ApplyLibraryPlan(&library.Plan{ID: "made-up-id", Operations: forged()})
	libTestWantStale(t, err)
	// No ID at all, which is what a hand-built plan has.
	_, err = app.ApplyLibraryPlan(&library.Plan{Operations: forged()})
	libTestWantStale(t, err)
	// The real plan's ID, but pointing at another plan's operations.
	other := libTestPreview(t, app, nil)
	_, err = app.ApplyLibraryPlan(&library.Plan{ID: "not-" + other.ID, Operations: plan.Operations})
	libTestWantStale(t, err)
	untouched("before")

	// The real ID with the operations replaced, edited in place on the plan the
	// preview returned (the stored copy must not alias it) and extended.
	plan.Operations[0].Type = "move"
	plan.Operations[0].Path = precious
	plan.Operations[0].NewPath = filepath.Join(root, "Moved", "precious.txt")
	plan.Operations = append(plan.Operations, forged()...)
	res, err := app.ApplyLibraryPlan(plan)
	if err != nil {
		t.Fatalf("ApplyLibraryPlan: %v", err)
	}
	if res.Applied != 1 || res.Skipped != 0 || len(res.Errors) != 0 {
		t.Fatalf("want exactly the stored rmdir applied, got %+v", res)
	}
	if libTestExists(filepath.Join(root, "Wanted Empty")) {
		t.Fatal("the stored operation did not run")
	}
	untouched("after")

	info, err := app.GetLastLibraryBatch()
	if err != nil || info == nil {
		t.Fatalf("GetLastLibraryBatch: %+v, %v", info, err)
	}
	if info.Count != 1 || info.Types["rmdir"] != 1 || len(info.Paths) != 1 || info.Paths[0] != filepath.Join(root, "Wanted Empty") {
		t.Fatalf("the journal should hold only the stored operation, got %+v", info)
	}

	// Nothing to apply.
	if _, err := app.ApplyLibraryPlan(nil); err == nil {
		t.Fatal("a nil plan must be an error")
	}
}

func TestPreviewedPlanCanBeAppliedOnlyOnce(t *testing.T) {
	libTestAppDir(t)
	root := libTestFolders(t, "One", "Two")
	app := &App{}
	report := libTestScan(t, app, root)
	plan := libTestPreview(t, app, fixableIssueIDs(report))

	first := libTestApply(t, app, plan)
	if first.Applied != 2 {
		t.Fatalf("want 2 applied, got %+v", first)
	}
	_, err := app.ApplyLibraryPlan(plan)
	libTestWantStale(t, err)

	info, err := app.GetLastLibraryBatch()
	if err != nil || info == nil || info.BatchID != first.BatchID {
		t.Fatalf("the second apply must not start a batch: %+v, %v", info, err)
	}
}

func TestRescanInvalidatesPreviews(t *testing.T) {
	libTestAppDir(t)
	root := libTestFolders(t, "One", "Two")
	app := &App{}
	report := libTestScan(t, app, root)
	plan := libTestPreview(t, app, fixableIssueIDs(report))

	libTestScan(t, app, root)
	_, err := app.ApplyLibraryPlan(plan)
	libTestWantStale(t, err)
	for _, dir := range []string{"One", "Two"} {
		if !libTestExists(filepath.Join(root, dir)) {
			t.Fatalf("a stale preview changed the library: %s is gone", dir)
		}
	}

	// A fresh preview of the new scan works.
	if res := libTestApply(t, app, libTestPreview(t, app, fixableIssueIDs(report))); res.Applied != 2 {
		t.Fatalf("want 2 applied, got %+v", res)
	}
}

func TestOnlyTheMostRecentPreviewsAreKept(t *testing.T) {
	libTestAppDir(t)
	const folders = maxLibraryPreviews + 2
	names := make([]string, folders)
	for i := range names {
		names[i] = fmt.Sprintf("D%02d", i)
	}
	root := libTestFolders(t, names...)
	app := &App{}
	report := libTestScan(t, app, root)

	plans := make([]*library.Plan, folders)
	for i, name := range names {
		plans[i] = libTestPreview(t, app, []string{libTestIssueFor(t, report, filepath.Join(root, name))})
	}
	for i := 0; i < folders-maxLibraryPreviews; i++ {
		_, err := app.ApplyLibraryPlan(plans[i])
		libTestWantStale(t, err)
		if !libTestExists(filepath.Join(root, names[i])) {
			t.Fatalf("an evicted preview changed the library: %s", names[i])
		}
	}
	kept := folders - maxLibraryPreviews
	if res := libTestApply(t, app, plans[kept]); res.Applied != 1 {
		t.Fatalf("the oldest kept preview must still apply: %+v", res)
	}
	if libTestExists(filepath.Join(root, names[kept])) {
		t.Fatal("the kept preview did not run")
	}
}

func TestApplyThatChangesFilesInvalidatesOtherPreviews(t *testing.T) {
	libTestAppDir(t)
	root := libTestFolders(t, "A", "B")
	app := &App{}
	report := libTestScan(t, app, root)
	planA := libTestPreview(t, app, []string{libTestIssueFor(t, report, filepath.Join(root, "A"))})
	planB := libTestPreview(t, app, []string{libTestIssueFor(t, report, filepath.Join(root, "B"))})

	libTestApply(t, app, planA)
	// The scan planB was built from no longer describes the disk.
	_, err := app.ApplyLibraryPlan(planB)
	libTestWantStale(t, err)
	if !libTestExists(filepath.Join(root, "B")) {
		t.Fatal("a stale preview changed the library")
	}
}

func TestConcurrentAppliesOfOnePlanRunExactlyOnce(t *testing.T) {
	libTestAppDir(t)
	names := make([]string, 20)
	for i := range names {
		names[i] = fmt.Sprintf("D%02d", i)
	}
	root := libTestFolders(t, names...)
	app := &App{}
	plan := libTestPreview(t, app, fixableIssueIDs(libTestScan(t, app, root)))

	const callers = 6
	type outcome struct {
		res *library.ApplyResult
		err error
	}
	results := make(chan outcome, callers)
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			res, err := app.ApplyLibraryPlan(plan)
			results <- outcome{res, err}
		}()
	}
	close(start)
	wins := 0
	for i := 0; i < callers; i++ {
		got := <-results
		switch {
		case got.err == nil:
			wins++
			if got.res.Applied != len(names) {
				t.Fatalf("the winner applied %+v", got.res)
			}
		case errors.Is(got.err, errLibraryBusy), errors.Is(got.err, errLibraryPreviewStale):
		default:
			t.Fatalf("unexpected error %v", got.err)
		}
	}
	if wins != 1 {
		t.Fatalf("want exactly one apply, got %d", wins)
	}
}

// --- Cancel and progress -------------------------------------------------------

func TestCancelLibraryApplyStopsBetweenOperations(t *testing.T) {
	libTestAppDir(t)
	names := []string{"E1", "E2", "E3", "E4", "E5", "E6"}
	root := libTestFolders(t, names...)
	app := &App{}
	plan := libTestPreview(t, app, fixableIssueIDs(libTestScan(t, app, root)))
	if len(plan.Operations) != len(names) {
		t.Fatalf("test premise: want %d operations, got %d", len(names), len(plan.Operations))
	}
	order := make([]string, len(plan.Operations))
	for i, op := range plan.Operations {
		order[i] = op.Path
	}

	// The report for operation 3 goes out just before it runs, so cancelling
	// there lets it finish and stops the batch before operation 4.
	var seen []int
	libTestApplyHook(t, func(p library.ApplyProgress) {
		seen = append(seen, p.Current)
		if p.Current == 3 {
			app.CancelLibraryApply()
		}
	})
	res, err := app.ApplyLibraryPlan(plan)
	if err != nil {
		t.Fatalf("a cancelled apply is a result, not an error: %v", err)
	}
	if !res.Cancelled || res.Applied != 3 {
		t.Fatalf("want 3 applied then cancelled, got %+v", res)
	}
	if !reflect.DeepEqual(seen, []int{1, 2, 3}) {
		t.Fatalf("progress reports = %v, want [1 2 3]", seen)
	}
	for i, path := range order {
		if gone := !libTestExists(path); gone != (i < 3) {
			t.Fatalf("%s: removed=%v, want %v", path, gone, i < 3)
		}
	}

	// The cancelled preview is spent; the user has to preview again.
	_, err = app.ApplyLibraryPlan(plan)
	libTestWantStale(t, err)

	// What was applied is journaled and the slot is free again.
	info, err := app.GetLastLibraryBatch()
	if err != nil || info == nil || info.BatchID != res.BatchID || info.Count != 3 {
		t.Fatalf("GetLastLibraryBatch = %+v, %v; want the partial batch of 3", info, err)
	}
	libTestApplyHook(t, nil)
	rest := libTestPreview(t, app, fixableIssueIDs(libTestScan(t, app, root)))
	if len(rest.Operations) != 3 {
		t.Fatalf("want 3 folders left to remove, got %d", len(rest.Operations))
	}
	if second := libTestApply(t, app, rest); second.Applied != 3 || second.Cancelled {
		t.Fatalf("second apply = %+v", second)
	}

	// Undo walks back through both batches.
	for _, wantBatch := range []string{"", res.BatchID} {
		undone, err := app.UndoLastLibraryFix()
		if err != nil || undone.Applied != 3 || len(undone.Errors) != 0 {
			t.Fatalf("undo = %+v, %v", undone, err)
		}
		if wantBatch != "" && undone.BatchID != wantBatch {
			t.Fatalf("undo reverted %s, want the cancelled batch %s", undone.BatchID, wantBatch)
		}
	}
	for _, name := range names {
		if !libTestExists(filepath.Join(root, name)) {
			t.Fatalf("%s was not restored", name)
		}
	}
}

func TestCancelLibraryApplyWithNothingRunningIsHarmless(t *testing.T) {
	libTestAppDir(t)
	(&App{}).CancelLibraryApply()

	root := libTestFolders(t, "Empty")
	app := &App{}
	report := libTestScan(t, app, root)
	app.CancelLibraryApply() // idle: must not poison the next apply
	res := libTestApply(t, app, libTestPreview(t, app, fixableIssueIDs(report)))
	if res.Cancelled || res.Applied != 1 {
		t.Fatalf("a stray cancel leaked into the next apply: %+v", res)
	}
}

func TestApplyProgressEventsAreThrottledButFirstAndLastAlwaysGo(t *testing.T) {
	libTestAppDir(t)
	const folders = 30
	names := make([]string, folders)
	for i := range names {
		names[i] = fmt.Sprintf("D%02d", i)
	}
	progress := func(events []libTestEvent) []library.ApplyProgress {
		var out []library.ApplyProgress
		for _, e := range events {
			if e.name != "library:apply-progress" {
				continue
			}
			p, ok := e.payload.(library.ApplyProgress)
			if !ok {
				t.Fatalf("payload is %T, want library.ApplyProgress", e.payload)
			}
			out = append(out, p)
		}
		return out
	}

	run := func(interval time.Duration) []library.ApplyProgress {
		root := libTestFolders(t, names...)
		app := &App{}
		plan := libTestPreview(t, app, fixableIssueIDs(libTestScan(t, app, root)))
		old := libraryApplyProgressInterval
		libraryApplyProgressInterval = interval
		defer func() { libraryApplyProgressInterval = old }()
		events := libTestCaptureEvents(t)
		libTestApply(t, app, plan)
		return progress(events())
	}

	// A throttle too long to ever elapse leaves exactly the first and the last.
	got := run(time.Hour)
	if len(got) != 2 || got[0].Current != 1 || got[1].Current != folders || got[0].Total != folders || got[1].Total != folders {
		t.Fatalf("throttled events = %+v, want only the first and the last", got)
	}
	if got[0].Path == "" || got[1].Path == "" {
		t.Fatalf("events must name the path being worked on: %+v", got)
	}

	// No throttle: every operation is reported, in order.
	got = run(0)
	if len(got) != folders {
		t.Fatalf("unthrottled: %d events, want %d", len(got), folders)
	}
	for i, p := range got {
		if p.Current != i+1 || p.Total != folders {
			t.Fatalf("event %d = %+v", i, p)
		}
	}
}

// --- Refusals -------------------------------------------------------------------

func TestChangesAreRefusedWhileAnApplyRuns(t *testing.T) {
	libTestAppDir(t)
	root := libTestFolders(t, "A", "B", "C")
	app := &App{}
	report := libTestScan(t, app, root)
	planRunning := libTestPreview(t, app, []string{
		libTestIssueFor(t, report, filepath.Join(root, "A")),
		libTestIssueFor(t, report, filepath.Join(root, "B")),
	})
	planWaiting := libTestPreview(t, app, []string{libTestIssueFor(t, report, filepath.Join(root, "C"))})
	ids := fixableIssueIDs(report)

	var during struct {
		ran                        bool
		apply, undo, scan, preview error
		cancelScanRan              bool
	}
	libTestApplyHook(t, func(p library.ApplyProgress) {
		if p.Current != 1 {
			return
		}
		during.ran = true
		_, during.apply = app.ApplyLibraryPlan(planWaiting)
		_, during.undo = app.UndoLastLibraryFix()
		during.scan = app.StartLibraryScan(root, library.ProfilePoweramp)
		_, during.preview = app.PreviewLibraryFixes(ids)
		app.CancelLibraryScan() // must neither panic nor stop the apply
		during.cancelScanRan = true
	})
	res, err := app.ApplyLibraryPlan(planRunning)
	if err != nil || res.Applied != 2 || res.Cancelled {
		t.Fatalf("the running apply was disturbed: %+v, %v", res, err)
	}
	if !during.ran || !during.cancelScanRan {
		t.Fatal("the progress hook never ran")
	}
	for name, got := range map[string]error{
		"second apply": during.apply, "undo": during.undo, "scan": during.scan, "preview": during.preview,
	} {
		if !errors.Is(got, errLibraryBusy) {
			t.Errorf("%s during an apply: got %v, want the busy error", name, got)
		}
	}
	if libTestScanRunning(app) {
		t.Fatal("a scan started while fixes were being applied")
	}
	if !libTestExists(filepath.Join(root, "C")) {
		t.Fatal("the refused apply changed the library")
	}

	// Once it finished the slot is free again: scans start and undo works.
	if err := app.StartLibraryScan(root, library.ProfilePoweramp); err != nil {
		t.Fatalf("StartLibraryScan after the apply: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for libTestScanRunning(app) {
		if time.Now().After(deadline) {
			t.Fatal("scan did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	undone, err := app.UndoLastLibraryFix()
	if err != nil || undone.Applied != 2 {
		t.Fatalf("undo after the apply = %+v, %v", undone, err)
	}
}

// libTestScanRunning reports whether a scan goroutine is still in flight.
func libTestScanRunning(app *App) bool {
	app.libraryScanMu.Lock()
	defer app.libraryScanMu.Unlock()
	return app.libraryScanCancel != nil
}

func TestApplyAndUndoAreRefusedWhileAScanRuns(t *testing.T) {
	libTestAppDir(t)
	root := libTestFolders(t, "A", "B")
	app := &App{}
	report := libTestScan(t, app, root)
	planA := libTestPreview(t, app, []string{libTestIssueFor(t, report, filepath.Join(root, "A"))})

	setScanRunning := func(running bool) {
		app.libraryScanMu.Lock()
		defer app.libraryScanMu.Unlock()
		if running {
			app.libraryScanCancel = func() {}
		} else {
			app.libraryScanCancel = nil
		}
	}
	setScanRunning(true)
	_, err := app.ApplyLibraryPlan(planA)
	if err == nil || !strings.Contains(err.Error(), "wait for the library scan to finish before applying") {
		t.Fatalf("apply during a scan: %v", err)
	}
	_, err = app.UndoLastLibraryFix()
	if err == nil || !strings.Contains(err.Error(), "wait for the library scan to finish before undoing") {
		t.Fatalf("undo during a scan: %v", err)
	}
	if !libTestExists(filepath.Join(root, "A")) {
		t.Fatal("a refused apply changed the library")
	}

	// A refusal does not spend the preview.
	setScanRunning(false)
	if res := libTestApply(t, app, planA); res.Applied != 1 {
		t.Fatalf("the preview should have survived the refusal: %+v", res)
	}
}

func TestPersistedOnlyReportStillCannotApplyAStoredID(t *testing.T) {
	libTestAppDir(t)
	root := libTestFolders(t, "Empty")
	first := &App{}
	plan := libTestPreview(t, first, fixableIssueIDs(libTestScan(t, first, root)))

	restarted := &App{}
	if got, err := restarted.GetLibraryReport(); err != nil || got == nil || !got.Persisted {
		t.Fatalf("test premise: want the persisted report, got %+v, %v", got, err)
	}
	_, err := restarted.ApplyLibraryPlan(plan)
	if !errors.Is(err, errLibraryScanNeeded) {
		t.Fatalf("want the scan-again error, got %v", err)
	}
	if !libTestExists(filepath.Join(root, "Empty")) {
		t.Fatal("the refused apply changed the library")
	}
}

// --- Undo detail ------------------------------------------------------------------

func TestGetLastLibraryBatchDescribesWhatUndoReverts(t *testing.T) {
	libTestAppDir(t)
	root := libTestFolders(t, "A1", "A2", "A3", "B1", "B2", "B3", "B4", "B5", "B6")
	app := &App{}

	if info, err := app.GetLastLibraryBatch(); err != nil || info != nil {
		t.Fatalf("with no journal want (nil, nil), got %+v, %v", info, err)
	}

	report := libTestScan(t, app, root)
	idsFor := func(prefix string) []string {
		var ids []string
		for _, issue := range report.Issues {
			if issue.Fixable && strings.HasPrefix(filepath.Base(issue.Path), prefix) {
				ids = append(ids, issue.ID)
			}
		}
		return ids
	}
	first := libTestApply(t, app, libTestPreview(t, app, idsFor("A")))
	report = libTestScan(t, app, root)
	second := libTestApply(t, app, libTestPreview(t, app, idsFor("B")))
	if first.Applied != 3 || second.Applied != 6 {
		t.Fatalf("test premise: applied %d and %d", first.Applied, second.Applied)
	}

	info, err := app.GetLastLibraryBatch()
	if err != nil || info == nil {
		t.Fatalf("GetLastLibraryBatch: %+v, %v", info, err)
	}
	if info.BatchID != second.BatchID || info.Count != 6 || info.Types["rmdir"] != 6 || info.Time == "" {
		t.Fatalf("batch info = %+v, want the second batch of 6 folder removals", info)
	}
	if len(info.Paths) == 0 || len(info.Paths) > 5 {
		t.Fatalf("want a short sample of paths, got %v", info.Paths)
	}
	raw, _ := json.Marshal(info)
	for _, key := range []string{`"batch_id"`, `"time"`, `"count"`, `"types"`, `"paths"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("batch info JSON lacks %s: %s", key, raw)
		}
	}

	undone, err := app.UndoLastLibraryFix()
	if err != nil {
		t.Fatal(err)
	}
	if undone.BatchID != info.BatchID || undone.Applied != info.Count {
		t.Fatalf("undo = %+v does not match the announced batch %+v", undone, info)
	}
	if info, err = app.GetLastLibraryBatch(); err != nil || info == nil || info.BatchID != first.BatchID || info.Count != 3 {
		t.Fatalf("after one undo want the first batch, got %+v, %v", info, err)
	}
	if _, err := app.UndoLastLibraryFix(); err != nil {
		t.Fatal(err)
	}
	if info, err = app.GetLastLibraryBatch(); err != nil || info != nil {
		t.Fatalf("with everything undone want (nil, nil), got %+v, %v", info, err)
	}
	empty, err := app.UndoLastLibraryFix()
	if err != nil || empty.BatchID != "" || empty.Applied != 0 {
		t.Fatalf("undo with nothing left = %+v, %v", empty, err)
	}
}

func TestUndoReportsAnAbandonedBatchAndSkipsToTheOlderOne(t *testing.T) {
	appDir := libTestAppDir(t)
	journal := filepath.Join(appDir, libraryJournalName)
	restorable := filepath.Join(t.TempDir(), "restorable")
	now := time.Now().UTC().Format(time.RFC3339)
	write := func(entry library.JournalEntry) {
		t.Helper()
		if err := library.AppendJournal(journal, entry); err != nil {
			t.Fatal(err)
		}
	}
	write(library.JournalEntry{BatchID: "older", Time: now, Type: "rmdir", Path: restorable, Seq: 1})
	write(library.JournalEntry{BatchID: "stuck", Time: now, Type: "rmdir", Path: filepath.Join(t.TempDir(), "x"), Seq: 1})
	for attempt := 1; attempt <= 3; attempt++ {
		write(library.JournalEntry{BatchID: "stuck", Time: now, Type: "undo-partial", Attempts: attempt, Error: "cannot revert"})
	}

	app := &App{}
	info, err := app.GetLastLibraryBatch()
	if err != nil || info == nil {
		t.Fatalf("GetLastLibraryBatch: %+v, %v", info, err)
	}
	if info.BatchID != "older" || info.AbandonedBatchID != "stuck" {
		t.Fatalf("info = %+v, want the older batch with the stuck one named", info)
	}
	undone, err := app.UndoLastLibraryFix()
	if err != nil {
		t.Fatal(err)
	}
	if undone.BatchID != "older" || undone.AbandonedBatchID != "stuck" || undone.Applied != 1 {
		t.Fatalf("undo = %+v, want the older batch undone and the stuck one named", undone)
	}
	raw, _ := json.Marshal(undone)
	if !strings.Contains(string(raw), `"abandoned_batch_id":"stuck"`) {
		t.Fatalf("the abandoned batch must reach the UI: %s", raw)
	}
	if !libTestExists(restorable) {
		t.Fatal("the older batch was not reverted")
	}
}

// --- Skipped folders ----------------------------------------------------------------

func TestLibraryReportCarriesSkippedFoldersThroughPersistence(t *testing.T) {
	appDir := libTestAppDir(t)
	root := t.TempDir()
	for _, dir := range []string{"Readable", "Locked"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	libTestDenyListing(t, filepath.Join(root, "Locked"))

	app := &App{}
	report := libTestScan(t, app, root)
	if report.SkippedDirs != 1 || !reflect.DeepEqual(report.SkippedSample, []string{"Locked"}) {
		t.Fatalf("report skipped %d %v, want 1 [Locked]", report.SkippedDirs, report.SkippedSample)
	}
	live, err := app.GetLibraryReport()
	if err != nil || live == nil || live.SkippedDirs != 1 {
		t.Fatalf("live report = %+v, %v", live, err)
	}

	raw, err := os.ReadFile(filepath.Join(appDir, libraryReportName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"skipped_dirs":1`, `"skipped_sample":["Locked"]`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("persisted report lacks %s: %s", want, raw)
		}
	}
	restored, err := (&App{}).GetLibraryReport()
	if err != nil || restored == nil || !restored.Persisted {
		t.Fatalf("restored report = %+v, %v", restored, err)
	}
	if restored.SkippedDirs != 1 || !reflect.DeepEqual(restored.SkippedSample, []string{"Locked"}) {
		t.Fatalf("restored report skipped %d %v", restored.SkippedDirs, restored.SkippedSample)
	}
}

func TestLibraryReportSkippedFieldsRoundTripAndStayOutOfCleanReports(t *testing.T) {
	libTestAppDir(t)
	want := &LibraryReport{Root: "/music", ProfileID: library.ProfilePoweramp, SkippedDirs: 7, SkippedSample: []string{"a", "b/c"}}
	if err := saveLibraryReport(want); err != nil {
		t.Fatal(err)
	}
	got := loadLibraryReport()
	if got == nil || got.SkippedDirs != 7 || !reflect.DeepEqual(got.SkippedSample, want.SkippedSample) {
		t.Fatalf("round trip lost the skipped folders: %+v", got)
	}

	clean, err := json.Marshal(&LibraryReport{Root: "/music"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(clean), "skipped") {
		t.Fatalf("a fully read library should not carry skipped fields: %s", clean)
	}

	app := &App{}
	report := libTestScan(t, app, libTestFolders(t, "Empty"))
	if report.SkippedDirs != 0 || len(report.SkippedSample) != 0 {
		t.Fatalf("a readable tree skipped %d %v", report.SkippedDirs, report.SkippedSample)
	}
}
