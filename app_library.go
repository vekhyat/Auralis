package main

import (
	"context"
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

// LibraryProfiles returns all built-in target profiles.
func (a *App) LibraryProfiles() []library.Profile {
	return library.Profiles()
}

// GetLibraryProfiles is an alias for LibraryProfiles.
func (a *App) GetLibraryProfiles() []library.Profile {
	return a.LibraryProfiles()
}

// StartLibraryScan starts a cancellable background library scan and emits progress events.
func (a *App) StartLibraryScan(root string, profileID string) error {
	cleanRoot := filepath.Clean(strings.TrimSpace(root))
	if cleanRoot == "" {
		return fmt.Errorf("library root path cannot be empty")
	}
	info, err := os.Stat(cleanRoot)
	if err != nil {
		return fmt.Errorf("cannot access library root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("library root is not a directory")
	}

	a.libraryScanMu.Lock()
	if a.libraryScanCancel != nil {
		a.libraryScanCancel()
		a.libraryScanCancel = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.libraryScanCancel = cancel
	a.libraryScanMu.Unlock()

	go func() {
		defer func() {
			a.libraryScanMu.Lock()
			a.libraryScanCancel = nil
			a.libraryScanMu.Unlock()
		}()

		profile := library.ProfileByID(profileID)
		scan, err := library.ScanLibrary(ctx, cleanRoot, func(p library.ScanProgress) {
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "library:scan-progress", p)
			}
		})
		if err != nil {
			if ctx.Err() != nil {
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "library:scan-cancelled", "scan cancelled")
				}
				return
			}
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "library:scan-error", err.Error())
			}
			return
		}

		issues := library.DetectAll(scan, profile)
		score := calculateHealthScore(len(scan.Tracks), issues)
		report := &LibraryReport{
			Root:        cleanRoot,
			ProfileID:   profile.ID,
			HealthScore: score,
			TotalTracks: len(scan.Tracks),
			TotalAlbums: countAlbums(scan.Tracks),
			Issues:      issues,
		}

		a.libraryScanMu.Lock()
		a.libraryLastScan = scan
		a.libraryLastIssues = issues
		a.libraryLastProfileID = profile.ID
		a.libraryLastRoot = cleanRoot
		a.libraryLastReport = report
		a.libraryScanMu.Unlock()

		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "library:scan-complete", report)
		}
	}()

	return nil
}

// LibraryStartScan is an alias for StartLibraryScan.
func (a *App) LibraryStartScan(root string, profileID string) error {
	return a.StartLibraryScan(root, profileID)
}

// CancelLibraryScan cancels any currently running library scan.
func (a *App) CancelLibraryScan() {
	a.libraryScanMu.Lock()
	defer a.libraryScanMu.Unlock()
	if a.libraryScanCancel != nil {
		a.libraryScanCancel()
		a.libraryScanCancel = nil
	}
}

// LibraryCancelScan is an alias for CancelLibraryScan.
func (a *App) LibraryCancelScan() {
	a.CancelLibraryScan()
}

// ScanLibrarySync executes a scan synchronously (convenient for testing and non-event callers).
func (a *App) ScanLibrarySync(root string, profileID string) (*LibraryReport, error) {
	cleanRoot := filepath.Clean(strings.TrimSpace(root))
	if cleanRoot == "" {
		return nil, fmt.Errorf("library root path cannot be empty")
	}
	info, err := os.Stat(cleanRoot)
	if err != nil {
		return nil, fmt.Errorf("cannot access library root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("library root is not a directory")
	}

	profile := library.ProfileByID(profileID)
	scan, err := library.ScanLibrary(context.Background(), cleanRoot, nil)
	if err != nil {
		return nil, err
	}

	issues := library.DetectAll(scan, profile)
	score := calculateHealthScore(len(scan.Tracks), issues)
	report := &LibraryReport{
		Root:        cleanRoot,
		ProfileID:   profile.ID,
		HealthScore: score,
		TotalTracks: len(scan.Tracks),
		TotalAlbums: countAlbums(scan.Tracks),
		Issues:      issues,
	}

	a.libraryScanMu.Lock()
	a.libraryLastScan = scan
	a.libraryLastIssues = issues
	a.libraryLastProfileID = profile.ID
	a.libraryLastRoot = cleanRoot
	a.libraryLastReport = report
	a.libraryScanMu.Unlock()

	return report, nil
}

// GetLibraryReport returns the report from the most recent scan.
func (a *App) GetLibraryReport() (*LibraryReport, error) {
	a.libraryScanMu.Lock()
	defer a.libraryScanMu.Unlock()
	if a.libraryLastReport == nil {
		return nil, fmt.Errorf("no library scan has been performed yet")
	}
	return a.libraryLastReport, nil
}

// LibraryGetReport is an alias for GetLibraryReport.
func (a *App) LibraryGetReport() (*LibraryReport, error) {
	return a.GetLibraryReport()
}

// PreviewLibraryFixes builds a Plan for the selected issue IDs (or all fixable issues if issueIDs is empty).
func (a *App) PreviewLibraryFixes(issueIDs []string) (*library.Plan, error) {
	a.libraryScanMu.Lock()
	scan := a.libraryLastScan
	issues := a.libraryLastIssues
	profileID := a.libraryLastProfileID
	a.libraryScanMu.Unlock()

	if scan == nil {
		return nil, fmt.Errorf("no library scan available")
	}
	profile := library.ProfileByID(profileID)

	var selected []library.Issue
	if len(issueIDs) == 0 {
		for _, issue := range issues {
			if issue.Fixable {
				selected = append(selected, issue)
			}
		}
	} else {
		idSet := make(map[string]bool, len(issueIDs))
		for _, id := range issueIDs {
			idSet[id] = true
		}
		for _, issue := range issues {
			if idSet[issue.ID] && issue.Fixable {
				selected = append(selected, issue)
			}
		}
	}

	plan := library.BuildPlan(scan, profile, selected)
	a.libraryScanMu.Lock()
	a.libraryLastPlan = plan
	a.libraryScanMu.Unlock()
	return plan, nil
}

// LibraryPreviewFixes is an alias for PreviewLibraryFixes.
func (a *App) LibraryPreviewFixes(issueIDs []string) (*library.Plan, error) {
	return a.PreviewLibraryFixes(issueIDs)
}

// PreviewLibraryRuleFixes builds a Plan for all fixable issues belonging to the given rule IDs.
func (a *App) PreviewLibraryRuleFixes(ruleIDs []string) (*library.Plan, error) {
	a.libraryScanMu.Lock()
	scan := a.libraryLastScan
	issues := a.libraryLastIssues
	profileID := a.libraryLastProfileID
	a.libraryScanMu.Unlock()

	if scan == nil {
		return nil, fmt.Errorf("no library scan available")
	}
	profile := library.ProfileByID(profileID)

	ruleSet := make(map[string]bool, len(ruleIDs))
	for _, rid := range ruleIDs {
		ruleSet[rid] = true
	}
	var selected []library.Issue
	for _, issue := range issues {
		if ruleSet[issue.RuleID] && issue.Fixable {
			selected = append(selected, issue)
		}
	}

	plan := library.BuildPlan(scan, profile, selected)
	a.libraryScanMu.Lock()
	a.libraryLastPlan = plan
	a.libraryScanMu.Unlock()
	return plan, nil
}

// ApplyLibraryPlan executes a plan and writes each applied change to the journal.
// If plan is nil, it applies the last previewed plan.
func (a *App) ApplyLibraryPlan(plan *library.Plan) (*library.ApplyResult, error) {
	a.libraryScanMu.Lock()
	root := a.libraryLastRoot
	if plan == nil {
		plan = a.libraryLastPlan
	}
	a.libraryScanMu.Unlock()

	if plan == nil {
		return nil, fmt.Errorf("no plan to apply")
	}

	appDir, err := backend.EnsureAppDataDir()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve app data dir: %w", err)
	}
	journalPath := filepath.Join(appDir, "library_journal.jsonl")

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return library.Apply(ctx, root, journalPath, plan)
}

// LibraryApplyPlan is an alias for ApplyLibraryPlan.
func (a *App) LibraryApplyPlan(plan *library.Plan) (*library.ApplyResult, error) {
	return a.ApplyLibraryPlan(plan)
}

// UndoLastLibraryFix reverts the most recent applied fix batch recorded in the journal.
func (a *App) UndoLastLibraryFix() (*library.ApplyResult, error) {
	appDir, err := backend.EnsureAppDataDir()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve app data dir: %w", err)
	}
	journalPath := filepath.Join(appDir, "library_journal.jsonl")
	return library.UndoLast(journalPath)
}

// LibraryUndoLastFix is an alias for UndoLastLibraryFix.
func (a *App) LibraryUndoLastFix() (*library.ApplyResult, error) {
	return a.UndoLastLibraryFix()
}

// ExportLibraryPlaylist exports tracks to an M3U8 playlist using the requested mode.
func (a *App) ExportLibraryPlaylist(m3u8Path string, trackPaths []string, mode string, musicRoot string, devicePrefix string) (*library.PlaylistResult, error) {
	tracks := library.TracksFromPaths(trackPaths)
	playlistMode := library.PlaylistMode(mode)
	if playlistMode == "" {
		playlistMode = library.PlaylistRelative
	}
	return library.WritePlaylist(m3u8Path, tracks, playlistMode, musicRoot, devicePrefix)
}

// LibraryExportPlaylist is an alias for ExportLibraryPlaylist.
func (a *App) LibraryExportPlaylist(m3u8Path string, trackPaths []string, mode string, musicRoot string, devicePrefix string) (*library.PlaylistResult, error) {
	return a.ExportLibraryPlaylist(m3u8Path, trackPaths, mode, musicRoot, devicePrefix)
}

func countAlbums(tracks []library.Track) int {
	albums := make(map[string]bool)
	for _, t := range tracks {
		albums[t.AlbumDir()] = true
	}
	return len(albums)
}

func calculateHealthScore(totalTracks int, issues []library.Issue) int {
	if totalTracks == 0 {
		return 100
	}
	if len(issues) == 0 {
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
			penalty += 1
		}
	}
	ratio := float64(penalty) / float64(totalTracks*3)
	if ratio > 1.0 {
		ratio = 1.0
	}
	score := int((1.0 - ratio) * 100)
	if score >= 100 {
		score = 99
	}
	if score < 0 {
		score = 0
	}
	return score
}
