package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vekhyat/Auralis/backend"
)

func TestPublishSuffixSupportsMoreThan99Copies(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 99; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("song_%02d.flac", i)), []byte("occupied"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := firstUnoccupiedPublishPath(filepath.Join(dir, "song.flac"))
	if err != nil || got != filepath.Join(dir, "song_100.flac") {
		t.Fatalf("next suffix = %q, %v", got, err)
	}
}

func TestFindExpectedFileRequiresReadableMedia(t *testing.T) {
	dir := t.TempDir()
	name := "Song - Artist.flac"
	path := filepath.Join(dir, name)
	payload := bytes.Repeat([]byte("x"), 200*1024)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	backend.SetAudioDurationReaderForTest(func(string) (float64, error) {
		return 0, os.ErrInvalid
	})
	t.Cleanup(func() { backend.SetAudioDurationReaderForTest(nil) })

	if _, ok := findExpectedFileInTargetDirectory(dir, []string{name}, 180); ok {
		t.Fatal("oversized junk file counted as an existing download")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed check deleted the existing file")
	}

	backend.SetAudioDurationReaderForTest(func(string) (float64, error) {
		return 180, nil
	})
	got, ok := findExpectedFileInTargetDirectory(dir, []string{name}, 180)
	if !ok || got != path {
		t.Fatalf("matching audio = %q, %v", got, ok)
	}
}

func TestPublishStagedDownloadReplacesInvalidAudioOnlyAfterValidation(t *testing.T) {
	stubPublishDuration(t, func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() >= 100*1024
	})

	root := t.TempDir()
	finalDir := filepath.Join(root, "Album")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(finalDir, "song.flac")
	if err := os.WriteFile(userFile, []byte("user-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	stagingDir, err := prepareDownloadStagingDir(finalDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stagingDir) })
	if !pathIsInside(stagingDir, finalDir) {
		t.Fatalf("staging directory %s is outside the writable output folder", stagingDir)
	}
	if !strings.EqualFold(filepath.Clean(filepath.Dir(stagingDir)), filepath.Clean(finalDir)) {
		t.Fatalf("staging directory %s is not inside %s", stagingDir, finalDir)
	}
	staged := filepath.Join(stagingDir, "song.flac")
	payload := bytes.Repeat([]byte("N"), 200*1024)
	if err := os.WriteFile(staged, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	untouched, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(untouched) != "user-file" {
		t.Fatalf("staging wrote the user file early: %q", untouched)
	}

	filename := staged
	kept, err := finalizeStagedDownload(stagingDir, finalDir, stagedPublishOptions{
		expectedSeconds:      180,
		stagedAudioValidated: true,
	}, &filename)
	if err != nil {
		t.Fatal(err)
	}
	if kept {
		t.Fatal("invalid destination audio was treated as an existing download")
	}
	if filename != userFile {
		t.Fatalf("published path = %s", filename)
	}
	got, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("validated audio did not replace the invalid destination")
	}
}

func TestPublishKeepsValidExistingAudio(t *testing.T) {
	stubPublishDuration(t, func(string) bool { return true })

	finalDir := filepath.Join(t.TempDir(), "Album")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(finalDir, "song.flac")
	original := bytes.Repeat([]byte("A"), 200*1024)
	if err := os.WriteFile(userFile, original, 0o644); err != nil {
		t.Fatal(err)
	}
	existingCover := filepath.Join(finalDir, "song.flac.cover.jpg")
	if err := os.WriteFile(existingCover, []byte("old-cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	stagingDir := filepath.Join(t.TempDir(), "staging")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(stagingDir, "song.flac")
	if err := os.WriteFile(staged, bytes.Repeat([]byte("B"), 200*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "song.flac.cover.jpg"), []byte("new-cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "song.lrc"), []byte("new-lrc"), 0o644); err != nil {
		t.Fatal(err)
	}

	filename := staged
	originalFile := staged
	convertedFile := ""
	kept, err := finalizeStagedDownload(stagingDir, finalDir, stagedPublishOptions{
		expectedSeconds:      180,
		stagedAudioValidated: true,
	}, &filename, &originalFile, &convertedFile)
	if err != nil {
		t.Fatal(err)
	}
	if !kept {
		t.Fatal("valid existing audio was not kept")
	}
	if filename != userFile || originalFile != userFile {
		t.Fatalf("filename=%s original=%s, want existing %s", filename, originalFile, userFile)
	}
	if convertedFile != "" {
		t.Fatalf("converted file changed: %s", convertedFile)
	}
	got, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatal("valid existing audio was overwritten")
	}
	cover, err := os.ReadFile(existingCover)
	if err != nil {
		t.Fatal(err)
	}
	if string(cover) != "old-cover" {
		t.Fatalf("existing cover changed: %q", cover)
	}
	if _, err := os.Stat(filepath.Join(finalDir, "song.lrc")); !os.IsNotExist(err) {
		t.Fatal("skip published a new lyric file next to the kept audio")
	}
	if _, err := os.Stat(filepath.Join(finalDir, "song_01.flac")); !os.IsNotExist(err) {
		t.Fatal("skip created a suffixed copy")
	}
}

func TestPublishKeepsSmallInvalidAudioWhenTransferFails(t *testing.T) {
	stubPublishDuration(t, func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Size() >= 100*1024
	})

	finalDir := filepath.Join(t.TempDir(), "Album")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(finalDir, "song.flac")
	if err := os.WriteFile(kept, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	stagingDir := filepath.Join(t.TempDir(), "staging")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(stagingDir, "song.flac")
	if err := os.WriteFile(staged, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	filename := staged
	_, err := finalizeStagedDownload(stagingDir, finalDir, stagedPublishOptions{
		expectedSeconds:      180,
		stagedAudioValidated: true,
	}, &filename)
	if err == nil {
		t.Fatal("unvalidated staged audio replaced an invalid destination")
	}
	got, err := os.ReadFile(kept)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep-me" {
		t.Fatalf("failed transfer changed the invalid file: %q", got)
	}
	if filename != staged {
		t.Fatalf("failed transfer rewrote the staged path: %s", filename)
	}

	abandonedDir := filepath.Join(t.TempDir(), "Album")
	if err := os.MkdirAll(abandonedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	abandonedFile := filepath.Join(abandonedDir, "song.flac")
	if err := os.WriteFile(abandonedFile, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	abandonedStaging, err := prepareDownloadStagingDir(abandonedDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(abandonedStaging, "song.flac"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(abandonedStaging); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(abandonedFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep-me" {
		t.Fatalf("abandoned staging changed the user file: %q", got)
	}
}

func TestPublishSuffixPreservesOccupantAndReturnsNewPaths(t *testing.T) {
	appDir := t.TempDir()
	t.Setenv("AURALIS_APP_DIR", appDir)
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	backend.CloseLibraryIndexDB()
	t.Cleanup(backend.CloseLibraryIndexDB)

	originalPath := filepath.Join(t.TempDir(), "Album", "song.mp3")
	finalDir := filepath.Dir(originalPath)
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	originalAudio := bytes.Repeat([]byte("A"), 200*1024)
	smallOccupant := []byte("tiny-corrupt")
	newMP3 := bytes.Repeat([]byte("B"), 200*1024)
	newFLAC := bytes.Repeat([]byte("C"), 200*1024)
	if err := os.WriteFile(originalPath, originalAudio, 0o644); err != nil {
		t.Fatal(err)
	}
	firstSuffix := filepath.Join(finalDir, "song_01.mp3")
	if err := os.WriteFile(firstSuffix, smallOccupant, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(finalDir, "song.mp3.cover.jpg"), []byte("old-cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(finalDir, "song.lrc"), []byte("old-lrc"), 0o644); err != nil {
		t.Fatal(err)
	}

	stubPublishDuration(t, func(path string) bool {
		return strings.HasSuffix(strings.ToLower(path), "song_02.mp3") || strings.HasSuffix(strings.ToLower(path), "song.flac")
	})

	stagingDir := filepath.Join(t.TempDir(), "staging")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stagedMP3 := filepath.Join(stagingDir, "song.mp3")
	stagedFLAC := filepath.Join(stagingDir, "song.flac")
	if err := os.WriteFile(stagedMP3, newMP3, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedFLAC, newFLAC, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "song.mp3.cover.jpg"), []byte("new-cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "song.lrc"), []byte("new-lrc"), 0o644); err != nil {
		t.Fatal(err)
	}

	filename := stagedMP3
	originalFile := stagedFLAC
	convertedFile := stagedMP3
	kept, err := finalizeStagedDownload(stagingDir, finalDir, stagedPublishOptions{
		redownloadWithSuffix: true,
		expectedSeconds:      180,
		stagedAudioValidated: true,
	}, &filename, &originalFile, &convertedFile)
	if err != nil {
		t.Fatal(err)
	}
	if kept {
		t.Fatal("suffix publish reported the occupied file as already existing")
	}

	wantMP3 := filepath.Join(finalDir, "song_02.mp3")
	wantFLAC := filepath.Join(finalDir, "song.flac")
	if filename != wantMP3 || convertedFile != wantMP3 {
		t.Fatalf("filename=%s converted=%s, want %s", filename, convertedFile, wantMP3)
	}
	if originalFile != wantFLAC {
		t.Fatalf("original file = %s, want %s", originalFile, wantFLAC)
	}
	if pathIsInside(filename, stagingDir) || pathIsInside(originalFile, stagingDir) || pathIsInside(convertedFile, stagingDir) {
		t.Fatal("published paths still point at staging")
	}

	gotOriginal, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotOriginal, originalAudio) {
		t.Fatal("suffix publish overwrote the original audio")
	}
	gotSmall, err := os.ReadFile(firstSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotSmall, smallOccupant) {
		t.Fatal("suffix publish overwrote the small _01 occupant")
	}
	gotMP3, err := os.ReadFile(wantMP3)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotMP3, newMP3) {
		t.Fatal("suffixed mp3 does not contain the new download")
	}
	gotFLAC, err := os.ReadFile(wantFLAC)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotFLAC, newFLAC) {
		t.Fatal("published original flac does not contain the staged audio")
	}
	oldCover, err := os.ReadFile(filepath.Join(finalDir, "song.mp3.cover.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(oldCover) != "old-cover" {
		t.Fatalf("existing cover changed: %q", oldCover)
	}
	newCover, err := os.ReadFile(filepath.Join(finalDir, "song_02.mp3.cover.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(newCover) != "new-cover" {
		t.Fatalf("suffixed cover = %q", newCover)
	}
	oldLRC, err := os.ReadFile(filepath.Join(finalDir, "song.lrc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(oldLRC) != "old-lrc" {
		t.Fatalf("existing lyrics changed: %q", oldLRC)
	}
	newLRC, err := os.ReadFile(filepath.Join(finalDir, "song_02.lrc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(newLRC) != "new-lrc" {
		t.Fatalf("suffixed lyrics = %q", newLRC)
	}

	if err := backend.RegisterLibraryFile(finalDir, filename, "0123456789abcdefghijkl", "USRC17607839"); err != nil {
		t.Fatal(err)
	}
	if err := backend.RegisterLibraryFile(finalDir, originalFile, "0123456789abcdefghijkl", "USRC17607839"); err != nil {
		t.Fatal(err)
	}
	assertLibraryPath(t, finalDir, "song_02.mp3", wantMP3)
	assertLibraryPath(t, finalDir, "song.mp3", originalPath)
	assertLibraryPath(t, finalDir, "song.flac", wantFLAC)
	indexedNew, _, indexedErr := backend.FindExistingLibraryFile(finalDir, backend.LibraryIndexLookupRequest{
		Mode:      "filename",
		Filenames: []string{"song_02.mp3"},
	})
	if indexedErr != nil {
		t.Fatal(indexedErr)
	}
	indexedOld, _, indexedErr := backend.FindExistingLibraryFile(finalDir, backend.LibraryIndexLookupRequest{
		Mode:      "filename",
		Filenames: []string{"song.mp3"},
	})
	if indexedErr != nil {
		t.Fatal(indexedErr)
	}
	if samePublishPath(indexedNew, indexedOld) {
		t.Fatalf("library index mapped the new download and the preserved file to %s", indexedNew)
	}
}

func stubPublishDuration(t *testing.T, valid func(string) bool) {
	t.Helper()
	backend.SetAudioDurationReaderForTest(func(path string) (float64, error) {
		if valid == nil || !valid(path) {
			return 0, os.ErrInvalid
		}
		return 180, nil
	})
	t.Cleanup(func() { backend.SetAudioDurationReaderForTest(nil) })
}

func assertLibraryPath(t *testing.T, root, name, want string) {
	t.Helper()
	got, ok, err := backend.FindExistingLibraryFile(root, backend.LibraryIndexLookupRequest{
		Mode:      "filename",
		Filenames: []string{name},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !samePublishPath(got, want) {
		t.Fatalf("library path for %s = %q (%v), want %s", name, got, ok, want)
	}
}

func samePublishPath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

func TestRecordDownloadHistoryWritesWithoutDelay(t *testing.T) {
	t.Setenv("AURALIS_APP_DIR", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	backend.ResetHistoryStoreForTest()
	t.Cleanup(backend.ResetHistoryStoreForTest)

	started := time.Now()
	recordDownloadHistory(filepath.Join(t.TempDir(), "missing.flac"), DownloadRequest{
		TrackName:   "Song",
		ArtistName:  "Artist",
		AlbumName:   "Album",
		SpotifyID:   "abc",
		AudioFormat: "LOSSLESS",
		Service:     "tidal",
	}, "tidal")
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("history recording took %s", elapsed)
	}

	items, err := backend.GetHistoryItems("Auralis")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Title != "Song" || items[0].Source != "tidal" || items[0].Format != "FLAC" {
		t.Fatalf("item = %#v", items[0])
	}
}
