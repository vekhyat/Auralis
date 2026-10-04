package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
}

// libraryState is the most recent scan, kept so fixes are planned against
// exactly what the user reviewed.
type libraryState struct {
	scan    *library.Scan
	issues  []library.Issue
	profile library.Profile
	report  *LibraryReport
}

const libraryJournalName = "library_journal.jsonl"

// LibraryProfiles returns all built-in target profiles.
func (a *App) LibraryProfiles() []library.Profile {
	return library.Profiles()
}

// StartLibraryScan starts a cancellable background scan. Progress arrives as
// "library:scan-progress"; the run ends with exactly one of
// "library:scan-complete", "library:scan-error", or "library:scan-cancelled".
func (a *App) StartLibraryScan(root string, profileID string) error {
	cleanRoot, err := libraryRoot(root)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.libraryScanMu.Lock()
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

// GetLibraryReport returns the report from the most recent scan.
func (a *App) GetLibraryReport() (*LibraryReport, error) {
	state := a.currentLibraryState()
	if state == nil {
		return nil, fmt.Errorf("no library scan has been performed yet")
	}
	return state.report, nil
}

// PreviewLibraryFixes builds a plan for the selected issue IDs, or for every
// fixable issue when issueIDs is empty. Nothing is changed on disk.
func (a *App) PreviewLibraryFixes(issueIDs []string) (*library.Plan, error) {
	state := a.currentLibraryState()
	if state == nil {
		return nil, fmt.Errorf("no library scan available")
	}
	wanted := make(map[string]bool, len(issueIDs))
	for _, id := range issueIDs {
		wanted[id] = true
	}
	var selected []library.Issue
	for _, issue := range state.issues {
		if issue.Fixable && (len(wanted) == 0 || wanted[issue.ID]) {
			selected = append(selected, issue)
		}
	}
	return library.BuildPlan(state.scan, state.profile, selected), nil
}

// ApplyLibraryPlan executes a previewed plan inside the scanned library root
// and records every change in the undo journal.
func (a *App) ApplyLibraryPlan(plan *library.Plan) (*library.ApplyResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("no plan to apply")
	}
	state := a.currentLibraryState()
	if state == nil {
		return nil, fmt.Errorf("scan the library before applying fixes")
	}
	journalPath, err := libraryJournalPath()
	if err != nil {
		return nil, err
	}
	return library.Apply(context.Background(), state.scan.Root, journalPath, plan)
}

// UndoLastLibraryFix reverts the most recent applied fix batch.
func (a *App) UndoLastLibraryFix() (*library.ApplyResult, error) {
	journalPath, err := libraryJournalPath()
	if err != nil {
		return nil, err
	}
	return library.UndoLast(journalPath)
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
	issues := library.DetectAll(scan, profile)
	report := &LibraryReport{
		Root:        root,
		ProfileID:   profile.ID,
		HealthScore: calculateHealthScore(len(scan.Tracks), issues),
		TotalTracks: len(scan.Tracks),
		TotalAlbums: countAlbums(scan.Tracks),
		Issues:      issues,
	}
	a.libraryScanMu.Lock()
	a.libraryLast = &libraryState{scan: scan, issues: issues, profile: profile, report: report}
	a.libraryScanMu.Unlock()
	return report, nil
}

func (a *App) currentLibraryState() *libraryState {
	a.libraryScanMu.Lock()
	defer a.libraryScanMu.Unlock()
	return a.libraryLast
}

func (a *App) emitLibraryEvent(name string, payload interface{}) {
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

func countAlbums(tracks []library.Track) int {
	albums := make(map[string]bool)
	for _, t := range tracks {
		albums[t.AlbumDir()] = true
	}
	return len(albums)
}

// calculateHealthScore weighs issues by severity against library size. A
// library with any issue never shows a perfect 100.
func calculateHealthScore(totalTracks int, issues []library.Issue) int {
	if totalTracks == 0 || len(issues) == 0 {
		return 100
	}
	penalty := 0
	for _, issue := range issues {
		switch issue.Severity {
		case library.SeverityError:
			penalty += 4
		case library.SeverityWarning:
			penalty += 2
		default:
			penalty++
		}
	}
	ratio := float64(penalty) / float64(totalTracks*3)
	if ratio > 1 {
		ratio = 1
	}
	score := int((1 - ratio) * 100)
	if score >= 100 {
		score = 99
	}
	return score
}
