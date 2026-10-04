package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend/library"
)

func TestAppLibraryProfiles(t *testing.T) {
	app := &App{}
	profiles := app.LibraryProfiles()
	if len(profiles) == 0 {
		t.Fatal("expected profiles, got 0")
	}
	foundPoweramp := false
	for _, p := range profiles {
		if p.ID == library.ProfilePoweramp {
			foundPoweramp = true
			break
		}
	}
	if !foundPoweramp {
		t.Fatalf("expected profile %q in profiles list", library.ProfilePoweramp)
	}
}

func TestAppCreateM3U8File(t *testing.T) {
	dir := t.TempDir()
	app := &App{}

	song1 := filepath.Join(dir, "song1.mp3")
	song2 := filepath.Join(dir, "song2.mp3")
	if err := os.WriteFile(song1, []byte("fake mp3 data 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(song2, []byte("fake mp3 data 2"), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "playlists")
	err := app.CreateM3U8File("My Hits", outDir, []string{song1, song2})
	if err != nil {
		t.Fatalf("CreateM3U8File failed: %v", err)
	}

	m3u8File := filepath.Join(outDir, "My Hits.m3u8")
	content, err := os.ReadFile(m3u8File)
	if err != nil {
		t.Fatalf("failed to read created playlist: %v", err)
	}
	text := string(content)
	if !strings.HasPrefix(text, "#EXTM3U\n") {
		t.Fatalf("expected #EXTM3U header, got:\n%s", text)
	}
	if !strings.Contains(text, "../song1.mp3") {
		t.Fatalf("expected relative path ../song1.mp3, got:\n%s", text)
	}
}

func TestAppLibraryScanAndDoctorWorkflow(t *testing.T) {
	tempAppDir := t.TempDir()
	t.Setenv("AURALIS_APP_DIR", tempAppDir)

	root := t.TempDir()
	albumDir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Empty dir to trigger an orphan rule (fixable)
	emptyDir := filepath.Join(root, "EmptyDir")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &App{}

	// Test synchronous scan
	report, err := app.ScanLibrarySync(root, library.ProfilePoweramp)
	if err != nil {
		t.Fatalf("ScanLibrarySync failed: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}

	// Test GetLibraryReport
	cachedReport, err := app.GetLibraryReport()
	if err != nil || cachedReport == nil {
		t.Fatalf("GetLibraryReport failed: %v", err)
	}

	// Preview fix for empty dir
	plan, err := app.PreviewLibraryFixes(nil)
	if err != nil {
		t.Fatalf("PreviewLibraryFixes failed: %v", err)
	}
	if plan == nil {
		t.Fatal("expected non-nil plan")
	}

	// Apply plan
	applyRes, err := app.ApplyLibraryPlan(plan)
	if err != nil {
		t.Fatalf("ApplyLibraryPlan failed: %v", err)
	}
	if applyRes.Applied == 0 && len(plan.Operations) > 0 {
		t.Fatalf("expected applied operations, got 0")
	}

	// Undo last fix
	undoRes, err := app.UndoLastLibraryFix()
	if err != nil {
		t.Fatalf("UndoLastLibraryFix failed: %v", err)
	}
	if undoRes == nil {
		t.Fatal("expected non-nil undo result")
	}
}

func TestAppLibraryAsyncScanAndCancel(t *testing.T) {
	tempAppDir := t.TempDir()
	t.Setenv("AURALIS_APP_DIR", tempAppDir)

	root := t.TempDir()
	app := &App{} // nil ctx in unit tests so Wails EventsEmit is bypassed

	err := app.StartLibraryScan(root, library.ProfilePoweramp)
	if err != nil {
		t.Fatalf("StartLibraryScan failed: %v", err)
	}

	// Cancel scan immediately
	app.CancelLibraryScan()

	// Wait briefly for goroutine
	time.Sleep(50 * time.Millisecond)
}

func TestAppExportLibraryPlaylist(t *testing.T) {
	dir := t.TempDir()
	app := &App{}

	track := filepath.Join(dir, "Music", "song.flac")
	if err := os.MkdirAll(filepath.Dir(track), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(track, []byte("fake flac"), 0o644); err != nil {
		t.Fatal(err)
	}

	m3u8Path := filepath.Join(dir, "device.m3u8")
	res, err := app.ExportLibraryPlaylist(m3u8Path, []string{track}, "device", filepath.Join(dir, "Music"), "/storage/emulated/0/Music")
	if err != nil {
		t.Fatalf("ExportLibraryPlaylist failed: %v", err)
	}
	if res.Written != 1 {
		t.Fatalf("expected 1 written, got %d", res.Written)
	}
	content, err := os.ReadFile(m3u8Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "/storage/emulated/0/Music/song.flac") {
		t.Fatalf("unexpected content:\n%s", string(content))
	}
}
