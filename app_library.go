package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/vekhyat/Auralis/backend"
	"github.com/vekhyat/Auralis/backend/library"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// LibraryReport bundles the scan findings and calculated health score.
type LibraryReport struct {
	Root        string          `json:"root"`
	ProfileID   string          `json:"profile_id"`
	HealthScore int             `json:"health_score"`
	TotalTracks int             `json:"total_tracks"`
	TotalAlbums int             `json:"total_albums"`
	Issues      []library.Issue `json:"issues"`
	// SkippedDirs counts folders the scan could not list (permissions, a
	// vanished mount). Whatever they hold is missing from every other figure, so
	// a non-zero count means the library was only partly read.
	SkippedDirs int `json:"skipped_dirs,omitempty"`
	// SkippedSample names the first few of them, slash-relative to Root.
	SkippedSample []string `json:"skipped_sample,omitempty"`
	// Persisted is set on a report restored from disk after a restart. It is
	// for display only: fixes are planned against the live scan, so the
	// library has to be scanned again before they can be previewed or applied.
	Persisted bool `json:"persisted,omitempty"`
}

// libraryState is the most recent scan, kept so fixes are planned against
// exactly what the user reviewed.
type libraryState struct {
	scan    *library.Scan
	issues  []library.Issue
	profile library.Profile
	report  *LibraryReport

	// The fields below are guarded by App.libraryScanMu. They live with the scan
	// because a plan is only meaningful against the scan it was built from: a
	// new scan replaces the state, which drops every preview with it.

	// previews holds the plans handed to the UI, by plan ID, so that applying
	// one runs what the backend built and never what the client sends back.
	// previewOrder lists the IDs oldest first, to evict the oldest past
	// maxLibraryPreviews.
	previews     map[string]*library.Plan
	previewOrder []string
	// changing is non-nil while a batch of fixes is being applied or undone
	// against this scan. Calling it cancels the batch (an undo ignores that).
	changing context.CancelFunc
}

const (
	libraryJournalName = "library_journal.jsonl"
	libraryReportName  = "library_report.json"

	// maxLibraryPreviews bounds how many previewed plans one scan keeps. Each
	// holds a copy of its operations, and nobody reviews more than a few at once.
	maxLibraryPreviews = 8
)

// errLibraryScanNeeded is returned when fixes are requested but there is no
// live scan to plan against. That includes the state after a restart, where
// only the persisted report exists: it holds findings, not the scanned files.
var errLibraryScanNeeded = errors.New("scan the library again before previewing or applying fixes")

// errLibraryPreviewStale is returned when a plan ID names no stored preview:
// it was applied already, evicted, or belongs to an earlier scan.
var errLibraryPreviewStale = errors.New("this preview is out of date; preview the fixes again")

// errLibraryBusy is returned while fixes are being applied or undone.
var errLibraryBusy = errors.New("fixes are being applied or undone; wait for them to finish")

// libraryReportSaveMu serialises writes of the persisted report, so a slow
// older save can never land after a newer one.
var libraryReportSaveMu sync.Mutex

// Package-level seams for tests. Production code never assigns them.
var (
	// libraryApplyProgressInterval is the shortest gap between two
	// "library:apply-progress" events. The first and the last always go out.
	libraryApplyProgressInterval = 50 * time.Millisecond
	// libraryApplyProgressHook sees every progress report of an apply, before
	// throttling, on the goroutine that runs the apply.
	libraryApplyProgressHook func(library.ApplyProgress)
	// libraryEventHook sees every library event, whether or not a Wails context
	// exists to carry it.
	libraryEventHook func(name string, payload interface{})
)

// LibraryProfiles returns all built-in target profiles.
func (a *App) LibraryProfiles() []library.Profile {
	return library.Profiles()
}

// StartLibraryScan starts a cancellable background scan. Progress arrives as
// "library:scan-progress"; the run ends with exactly one of
// "library:scan-complete", "library:scan-error", or "library:scan-cancelled".
// It is refused while fixes are being applied or undone.
func (a *App) StartLibraryScan(root string, profileID string) error {
	cleanRoot, err := libraryRoot(root)
	if err != nil {
		return err
	}

	a.libraryScanMu.Lock()
	if a.libraryChangeRunningLocked() {
		a.libraryScanMu.Unlock()
		return errLibraryBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	if a.libraryScanCancel != nil {
		a.libraryScanCancel()
	}
	a.libraryScanGeneration++
	generation := a.libraryScanGeneration
	a.libraryScanCancel = cancel
	a.libraryScanMu.Unlock()

	go func() {
		defer func() {
			a.libraryScanMu.Lock()
			// A newer scan may have replaced this one; leave its cancel alone.
			if a.libraryScanGeneration == generation {
				a.libraryScanCancel = nil
			}
			a.libraryScanMu.Unlock()
			cancel()
		}()

		report, err := a.scanLibrary(ctx, cleanRoot, profileID, func(p library.ScanProgress) {
			a.emitLibraryEvent("library:scan-progress", p)
		})
		// A scan that was replaced must not report over the newer one.
		if !a.isCurrentLibraryScan(generation) {
			return
		}
		switch {
		case errors.Is(err, context.Canceled):
			a.emitLibraryEvent("library:scan-cancelled", nil)
		case err != nil:
			a.emitLibraryEvent("library:scan-error", err.Error())
		default:
			a.emitLibraryEvent("library:scan-complete", report)
		}
	}()
	return nil
}

// CancelLibraryScan cancels the running library scan, if any.
func (a *App) CancelLibraryScan() {
	a.libraryScanMu.Lock()
	defer a.libraryScanMu.Unlock()
	if a.libraryScanCancel != nil {
		a.libraryScanCancel()
		a.libraryScanCancel = nil
	}
}

// GetLibraryReport returns the report from the most recent scan. Before any
// scan in this session it falls back to the report saved by the last one, so
// the page survives a restart. That report has Persisted set and cannot be
// used to preview or apply fixes.
func (a *App) GetLibraryReport() (*LibraryReport, error) {
	if state := a.currentLibraryState(); state != nil {
		return state.report, nil
	}
	// No saved report (or an unreadable one) is not an error.
	return loadLibraryReport(), nil
}

// PreviewLibraryFixes builds a plan for exactly the fixable issues named in
// issueIDs; an empty list yields an empty plan, never "everything". Nothing is
// changed on disk.
//
// The plan gets a fresh ID and is also kept by the backend, bound to the
// current scan. ApplyLibraryPlan runs that stored plan, looked up by ID, so the
// operations the UI sends back are never trusted. At most maxLibraryPreviews
// plans are kept per scan, oldest evicted first; a new scan discards them all.
func (a *App) PreviewLibraryFixes(issueIDs []string) (*library.Plan, error) {
	state := a.currentLibraryState()
	if state == nil {
		return nil, errLibraryScanNeeded
	}
	a.libraryScanMu.Lock()
	busy := a.libraryChangeRunningLocked()
	a.libraryScanMu.Unlock()
	if busy {
		// Files are changing under the scan this plan would be built from.
		return nil, errLibraryBusy
	}

	wanted := make(map[string]bool, len(issueIDs))
	for _, id := range issueIDs {
		wanted[id] = true
	}
	var selected []library.Issue
	for _, issue := range state.issues {
		if issue.Fixable && wanted[issue.ID] {
			selected = append(selected, issue)
		}
	}
	plan := &library.Plan{}
	if len(selected) > 0 {
		plan = library.BuildPlan(state.scan, state.profile, selected)
	}
	if plan.Operations == nil {
		plan.Operations = []library.Operation{} // "[]" on the wire, never null
	}
	plan.ID = uuid.NewString()
	if err := a.storeLibraryPreview(state, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// ApplyLibraryPlan applies a previewed plan inside the scanned library root and
// records every change in the undo journal. Only plan.ID is read: the
// operations are the ones PreviewLibraryFixes stored under that ID, so a
// tampered or invented plan cannot change what is written. A preview can be
// applied once; after that, or after a rescan, its ID is out of date.
//
// It runs one batch at a time and is refused while a scan is running. Progress
// arrives as "library:apply-progress" (library.ApplyProgress); CancelLibraryApply
// stops it between operations, returning the partial result with Cancelled set.
func (a *App) ApplyLibraryPlan(plan *library.Plan) (*library.ApplyResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("no plan to apply")
	}
	state, stored, ctx, err := a.beginLibraryApply(plan.ID)
	if err != nil {
		return nil, err
	}
	defer a.endLibraryChange(state)

	journalPath, err := libraryJournalPath()
	if err != nil {
		return nil, err // nothing ran, so the preview stays usable
	}
	// Past this point the preview is spent, whatever the outcome.
	a.libraryScanMu.Lock()
	state.forgetPreview(plan.ID)
	a.libraryScanMu.Unlock()

	result, err := library.ApplyWithProgress(ctx, state.scan.Root, journalPath, stored, a.libraryApplyProgress())
	if result != nil && result.Applied > 0 {
		// The disk no longer matches the scan the other previews were built from.
		a.libraryScanMu.Lock()
		state.forgetAllPreviews()
		a.libraryScanMu.Unlock()
	}
	return result, err
}

// CancelLibraryApply asks the running ApplyLibraryPlan to stop. It does not
// wait: the batch ends between operations, and ApplyLibraryPlan then returns
// the partial result with Cancelled set. What was applied stays journaled and
// can be undone. It does nothing when no apply is running.
func (a *App) CancelLibraryApply() {
	a.libraryScanMu.Lock()
	defer a.libraryScanMu.Unlock()
	if state := a.libraryLast; state != nil && state.changing != nil {
		state.changing()
	}
}

// GetLastLibraryBatch describes the batch UndoLastLibraryFix would revert next,
// for a confirmation prompt. It returns nil when there is nothing to undo.
func (a *App) GetLastLibraryBatch() (*library.BatchInfo, error) {
	journalPath, err := libraryJournalPath()
	if err != nil {
		return nil, err
	}
	return library.LastBatchInfo(journalPath)
}

// UndoLastLibraryFix reverts the most recent applied fix batch. When that batch
// had already failed to undo too often it is skipped, and the result's
// AbandonedBatchID names it. It is refused while a scan runs or fixes are being
// applied.
func (a *App) UndoLastLibraryFix() (*library.ApplyResult, error) {
	state, err := a.beginLibraryUndo()
	if err != nil {
		return nil, err
	}
	if state != nil {
		defer a.endLibraryChange(state)
	}
	journalPath, err := libraryJournalPath()
	if err != nil {
		return nil, err
	}
	result, err := library.UndoLast(journalPath)
	if state != nil && result != nil && result.Applied > 0 {
		a.libraryScanMu.Lock()
		state.forgetAllPreviews()
		a.libraryScanMu.Unlock()
	}
	return result, err
}

// ExportLibraryPlaylist writes an .m3u8 using mode "relative", "root", or
// "device". Tracks that cannot be expressed in the mode are skipped and
// listed in the result.
func (a *App) ExportLibraryPlaylist(m3u8Path string, trackPaths []string, mode string, musicRoot string, devicePrefix string) (*library.PlaylistResult, error) {
	playlistMode := library.PlaylistMode(mode)
	switch playlistMode {
	case "":
		playlistMode = library.PlaylistRelative
	case library.PlaylistRelative, library.PlaylistRoot, library.PlaylistDevice:
	default:
		return nil, fmt.Errorf("unknown playlist mode %q", mode)
	}
	return library.WritePlaylist(m3u8Path, library.TracksFromPaths(trackPaths), playlistMode, musicRoot, devicePrefix)
}

func (a *App) scanLibrary(ctx context.Context, root, profileID string, progress func(library.ScanProgress)) (*LibraryReport, error) {
	profile := library.ProfileByID(profileID)
	scan, err := library.ScanLibrary(ctx, root, progress)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err // cancelled or replaced: do not publish a stale state
	}
	issues := library.DetectAll(scan, profile)
	report := &LibraryReport{
		Root:          root,
		ProfileID:     profile.ID,
		HealthScore:   calculateHealthScore(scan.Tracks, issues),
		TotalTracks:   len(scan.Tracks),
		TotalAlbums:   countAlbums(scan.Tracks),
		Issues:        issues,
		SkippedDirs:   scan.SkippedDirs,
		SkippedSample: append([]string(nil), scan.SkippedSample...),
	}
	state := &libraryState{scan: scan, issues: issues, profile: profile, report: report}
	a.libraryScanMu.Lock()
	// Checked under the lock that cancelling takes, so a scan cancelled in the
	// meantime is never published; and a state is never swapped out from under a
	// batch that is changing the files it describes.
	if err := ctx.Err(); err != nil {
		a.libraryScanMu.Unlock()
		return nil, err
	}
	if a.libraryChangeRunningLocked() {
		a.libraryScanMu.Unlock()
		return nil, errLibraryBusy
	}
	a.libraryLast = state
	a.libraryScanMu.Unlock()
	a.persistLibraryReport(state)
	return report, nil
}

// persistLibraryReport saves state's report so it can be shown after a
// restart. A failed save is ignored: the scan itself succeeded and the report
// is already live in memory.
func (a *App) persistLibraryReport(state *libraryState) {
	libraryReportSaveMu.Lock()
	defer libraryReportSaveMu.Unlock()
	// A newer scan may have published while this one waited for the lock. Its
	// report is the one worth keeping, and it will write itself.
	if a.currentLibraryState() != state {
		return
	}
	_ = saveLibraryReport(state.report)
}

func (a *App) isCurrentLibraryScan(generation uint64) bool {
	a.libraryScanMu.Lock()
	defer a.libraryScanMu.Unlock()
	return a.libraryScanGeneration == generation
}

// libraryChangeRunningLocked reports whether fixes are being applied or undone.
// The caller holds libraryScanMu.
func (a *App) libraryChangeRunningLocked() bool {
	return a.libraryLast != nil && a.libraryLast.changing != nil
}

func (a *App) currentLibraryState() *libraryState {
	a.libraryScanMu.Lock()
	defer a.libraryScanMu.Unlock()
	return a.libraryLast
}

// beginLibraryApply claims the single slot for changing the library and looks
// up the stored plan for planID. It changes nothing else: the preview is only
// spent once the apply is really going to run. The returned context ends when
// CancelLibraryApply is called or the batch is released.
func (a *App) beginLibraryApply(planID string) (*libraryState, *library.Plan, context.Context, error) {
	a.libraryScanMu.Lock()
	defer a.libraryScanMu.Unlock()
	if a.libraryScanCancel != nil {
		return nil, nil, nil, fmt.Errorf("wait for the library scan to finish before applying fixes")
	}
	if a.libraryChangeRunningLocked() {
		return nil, nil, nil, errLibraryBusy
	}
	state := a.libraryLast
	if state == nil {
		return nil, nil, nil, errLibraryScanNeeded
	}
	stored := state.previews[planID]
	if planID == "" || stored == nil {
		return nil, nil, nil, errLibraryPreviewStale
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.changing = cancel
	return state, stored, ctx, nil
}

// beginLibraryUndo checks that an undo may run now. When a scan state exists it
// also claims the change slot, so no scan or apply starts mid-undo; after a
// restart there is no state, and the journal alone is enough to undo from.
func (a *App) beginLibraryUndo() (*libraryState, error) {
	a.libraryScanMu.Lock()
	defer a.libraryScanMu.Unlock()
	if a.libraryScanCancel != nil {
		return nil, fmt.Errorf("wait for the library scan to finish before undoing")
	}
	if a.libraryChangeRunningLocked() {
		return nil, errLibraryBusy
	}
	state := a.libraryLast
	if state != nil {
		state.changing = func() {} // an undo cannot be cancelled
	}
	return state, nil
}

// endLibraryChange releases the slot claimed by beginLibraryApply or
// beginLibraryUndo.
func (a *App) endLibraryChange(state *libraryState) {
	a.libraryScanMu.Lock()
	cancel := state.changing
	state.changing = nil
	a.libraryScanMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// storeLibraryPreview keeps a copy of plan under its ID for ApplyLibraryPlan.
// It refuses when the scan was replaced while the plan was being built, since
// the plan then describes a library the UI is no longer showing.
func (a *App) storeLibraryPreview(state *libraryState, plan *library.Plan) error {
	a.libraryScanMu.Lock()
	defer a.libraryScanMu.Unlock()
	if a.libraryLast != state {
		return errLibraryPreviewStale
	}
	if state.changing != nil {
		return errLibraryBusy
	}
	if state.previews == nil {
		state.previews = make(map[string]*library.Plan, maxLibraryPreviews)
	}
	state.previews[plan.ID] = clonePlan(plan)
	state.previewOrder = append(state.previewOrder, plan.ID)
	for len(state.previewOrder) > maxLibraryPreviews {
		delete(state.previews, state.previewOrder[0])
		state.previewOrder = append([]string(nil), state.previewOrder[1:]...)
	}
	return nil
}

// forgetPreview drops one stored plan. The caller holds libraryScanMu.
func (s *libraryState) forgetPreview(id string) {
	delete(s.previews, id)
	for i, other := range s.previewOrder {
		if other == id {
			s.previewOrder = append(s.previewOrder[:i:i], s.previewOrder[i+1:]...)
			return
		}
	}
}

// forgetAllPreviews drops every stored plan. The caller holds libraryScanMu.
func (s *libraryState) forgetAllPreviews() {
	s.previews = nil
	s.previewOrder = nil
}

// clonePlan copies plan deeply, so neither the caller of Preview nor anything
// that later edits the returned plan can reach the stored one.
func clonePlan(plan *library.Plan) *library.Plan {
	out := &library.Plan{ID: plan.ID, Operations: make([]library.Operation, len(plan.Operations))}
	for i, op := range plan.Operations {
		op.Set = cloneStringMap(op.Set)
		op.Old = cloneStringMap(op.Old)
		op.Delete = append([]string(nil), op.Delete...)
		out.Operations[i] = op
	}
	return out
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// libraryApplyProgress returns the progress callback for one apply. It forwards
// reports as "library:apply-progress", at most one per
// libraryApplyProgressInterval, except that the first and the last always go.
// The library calls it from a single goroutine, so it needs no lock.
func (a *App) libraryApplyProgress() func(library.ApplyProgress) {
	var last time.Time
	return func(p library.ApplyProgress) {
		if hook := libraryApplyProgressHook; hook != nil {
			hook(p)
		}
		now := time.Now()
		if !last.IsZero() && p.Current < p.Total && now.Sub(last) < libraryApplyProgressInterval {
			return
		}
		last = now
		a.emitLibraryEvent("library:apply-progress", p)
	}
}

func (a *App) emitLibraryEvent(name string, payload interface{}) {
	if hook := libraryEventHook; hook != nil {
		hook(name, payload)
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, payload)
	}
}

func libraryRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("library root path cannot be empty")
	}
	clean := filepath.Clean(root)
	info, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("cannot access library root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("library root is not a directory")
	}
	return clean, nil
}

func libraryJournalPath() (string, error) {
	appDir, err := backend.EnsureAppDataDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve app data dir: %w", err)
	}
	return filepath.Join(appDir, libraryJournalName), nil
}

func libraryReportPath() (string, error) {
	appDir, err := backend.EnsureAppDataDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve app data dir: %w", err)
	}
	return filepath.Join(appDir, libraryReportName), nil
}

// saveLibraryReport writes report as JSON through a temp file and a rename, so
// a crash mid-write leaves the previous report intact.
func saveLibraryReport(report *LibraryReport) error {
	path, err := libraryReportPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode library report: %w", err)
	}
	return backend.WriteFileAtomic(path, data, 0o600)
}

// loadLibraryReport returns the persisted report marked Persisted, or nil when
// there is none. A missing, unreadable, or corrupt file all mean "no report".
func loadLibraryReport() *LibraryReport {
	path, err := libraryReportPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var report LibraryReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil
	}
	// A real scan always has a root; an empty or "null" document is not one.
	if report.Root == "" {
		return nil
	}
	report.Persisted = true
	return &report
}

func countAlbums(tracks []library.Track) int {
	albums := make(map[string]bool)
	for _, t := range tracks {
		albums[t.AlbumDir()] = true
	}
	return len(albums)
}

// Health score weights, in hundredths so the arithmetic stays in integers and
// the result never depends on map iteration order. A track or album counts
// once, at the weight of the worst finding in it, however many it has.
const (
	healthWeightError   = 100
	healthWeightWarning = 60
	healthWeightInfo    = 3
)

// healthWeight maps a severity to its weight; anything unknown counts as info.
func healthWeight(s library.Severity) int {
	switch s {
	case library.SeverityError:
		return healthWeightError
	case library.SeverityWarning:
		return healthWeightWarning
	default:
		return healthWeightInfo
	}
}

// calculateHealthScore scores a library 0-100 by how much of it is affected,
// not by how many issues were raised, so a rule that emits one issue per file
// does not outweigh one that emits a single issue per album.
//
//	albumShare = sum(weight of worst finding in each album)  / (100 * albums)
//	trackShare = sum(weight of worst error/warning per track) / (100 * tracks)
//	score      = round(100 * (1 - (albumShare + trackShare) / 2))
//
// A track is affected by its own error/warning findings and by album-level
// ones (one issue about the whole album), so a bad album of 100 tracks and 100
// tracks that each have the same problem weigh the same. Info findings only
// touch albumShare, at weight 3 against 60 for warnings, and findings in
// folders that hold no tracks (empty folders, stray art) are limited to one
// info-weight album each, never more than the album count: such noise costs a
// few points at most. Shares are capped at 1.
//
// An empty library or one with no issues scores 100; any issue caps the score
// at 99. Adding an issue can only keep or lower the score.
func calculateHealthScore(tracks []library.Track, issues []library.Issue) int {
	if len(tracks) == 0 || len(issues) == 0 {
		return 100
	}

	albums := make(map[string]bool)
	albumOf := make(map[string]string, len(tracks))
	for _, t := range tracks {
		albums[t.AlbumDir()] = true
		albumOf[t.Path] = t.AlbumDir()
	}

	// Worst weight per track, per album (any finding), and per album for the
	// findings about the album as a whole; zero means none.
	trackWorst := make(map[string]int)
	albumWorst := make(map[string]int)
	albumLevel := make(map[string]int)
	strayDirs := make(map[string]bool)
	for _, issue := range issues {
		weight := healthWeight(issue.Severity)
		dir := issue.AlbumDir
		if own, ok := albumOf[issue.Path]; ok {
			dir = own
			trackWorst[issue.Path] = max(trackWorst[issue.Path], weight)
		} else if albums[dir] {
			albumLevel[dir] = max(albumLevel[dir], weight)
		}
		if albums[dir] {
			albumWorst[dir] = max(albumWorst[dir], weight)
		} else {
			strayDirs[dir] = true
		}
	}

	albumLoad := healthWeightInfo * min(len(strayDirs), len(albums))
	for _, weight := range albumWorst {
		albumLoad += weight
	}
	trackLoad := 0
	for _, t := range tracks {
		// Info findings do not make a track "affected".
		if weight := max(trackWorst[t.Path], albumLevel[t.AlbumDir()]); weight >= healthWeightWarning {
			trackLoad += weight
		}
	}

	albumShare := math.Min(1, float64(albumLoad)/float64(healthWeightError*len(albums)))
	trackShare := math.Min(1, float64(trackLoad)/float64(healthWeightError*len(tracks)))
	score := int(math.Round(100 * (1 - (albumShare+trackShare)/2)))
	return min(max(score, 0), 99)
}
