package backend

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSourceRankReliabilityBeforeLatency(t *testing.T) {
	now := time.Now()
	rows := []SourceBenchmark{
		{ID: "fast-error", Quality: "16", State: "failed", Attempts: 3, MedianMS: 1, CheckedAt: now},
		{ID: "fast-flaky", Quality: "16", State: "validated", Attempts: 3, Successes: 1, MedianMS: 10, CheckedAt: now},
		{ID: "slow-reliable", Quality: "16", State: "validated", Attempts: 3, Successes: 3, MedianMS: 100, CheckedAt: now},
		{ID: "fast-reliable", Quality: "16", State: "validated", Attempts: 3, Successes: 3, MedianMS: 40, CheckedAt: now},
	}
	got := rankSources([]string{"fast-error", "unknown", "fast-flaky", "slow-reliable", "fast-reliable"}, "16", rows, now)
	want := []string{"fast-reliable", "slow-reliable", "fast-flaky", "unknown", "fast-error"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v != %v", got, want)
	}
}

func TestSourceRankIgnoresOtherQualityAndExpiredResults(t *testing.T) {
	now := time.Now()
	rows := []SourceBenchmark{
		{ID: "cd-only", Quality: "16", State: "validated", Attempts: 3, Successes: 3, MedianMS: 1, CheckedAt: now},
		{ID: "expired", Quality: "24", State: "validated", Attempts: 3, Successes: 3, MedianMS: 2, CheckedAt: now.Add(-8 * 24 * time.Hour)},
		{ID: "hires", Quality: "24", State: "validated", Attempts: 3, Successes: 3, MedianMS: 100, CheckedAt: now},
	}
	got := rankSources([]string{"cd-only", "expired", "hires"}, "24", rows, now)
	if !reflect.DeepEqual(got, []string{"hires", "cd-only", "expired"}) {
		t.Fatal(got)
	}
	if got := rankSources([]string{"cd-only", "hires"}, "atmos", rows, now); !reflect.DeepEqual(got, []string{"cd-only", "hires"}) {
		t.Fatal(got)
	}
}

func TestSourceRankingPersistenceAndCancellation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(appDataDirEnv, dir)
	recordSourceOutcome("test-source", "tidal", "16", 25*time.Millisecond, true, nil)
	recordSourceOutcome("test-source", "tidal", "16", 10*time.Millisecond, false, ErrDownloadCancelled)
	sourcePerformance.Lock()
	sourcePerformance.dir = ""
	sourcePerformance.Unlock()
	for _, row := range SourceBenchmarks() {
		if row.ID == "test-source" {
			if row.Attempts != 1 || row.Successes != 1 || row.MedianMS != 25 {
				t.Fatal(row)
			}
			return
		}
	}
	t.Fatal("persisted source missing")
}

func TestDownloadSourcesRejectInvalidAudioAndTryNext(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	file := filepath.Join(t.TempDir(), "sample.flac")
	SetAudioDurationReaderForTest(func(path string) (float64, error) {
		if _, err := os.Stat(path); err != nil {
			return 0, err
		}
		body, _ := os.ReadFile(path)
		if string(body) == "bad" {
			return 30, nil
		}
		return 260, nil
	})
	defer SetAudioDurationReaderForTest(nil)
	var calls []string
	path, err := runDownloadSources("tidal", "16", 260, []sourceDownloadAttempt{
		{"preview", func() (string, error) {
			calls = append(calls, "preview")
			return file, os.WriteFile(file, []byte("bad"), 0600)
		}},
		{"good", func() (string, error) {
			calls = append(calls, "good")
			return file, os.WriteFile(file, []byte("good"), 0600)
		}},
	})
	if err != nil || path != file || !reflect.DeepEqual(calls, []string{"preview", "good"}) {
		t.Fatalf("%s %v %v", path, err, calls)
	}
	if got := RankedSourceIDs([]string{"preview", "good"}, "16"); got[0] != "good" {
		t.Fatal(got)
	}
}

func TestDownloadSourcesStopOnCancellation(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	called := false
	_, err := runDownloadSources("tidal", "16", 0, []sourceDownloadAttempt{{"cancel", func() (string, error) { return "", ErrDownloadCancelled }}, {"next", func() (string, error) { called = true; return "", errors.New("unexpected") }}})
	if !IsDownloadCancelledError(err) || called {
		t.Fatalf("%v, called=%v", err, called)
	}
}

func TestSourceRegistryDoesNotUseMetadataHostForDownloads(t *testing.T) {
	for _, source := range DownloadSources() {
		if source.ID == "samidy-catalog" {
			t.Fatal("metadata-only source enabled for downloads")
		}
	}
	if sourceQuality("lossy") != "lossy" {
		t.Fatal("lossy quality not preserved")
	}
}

func TestRankedServicesKeepLossyAfterLossless(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(appDataDirEnv, dir)
	now := time.Now()
	rows := []SourceBenchmark{
		{ID: "service-jiosaavn", Service: "jiosaavn", Quality: "16", State: "validated", Attempts: 3, Successes: 3, MedianMS: 1, CheckedAt: now},
		{ID: "service-qobuz", Service: "qobuz", Quality: "16", State: "validated", Attempts: 3, Successes: 3, MedianMS: 1000, CheckedAt: now},
	}
	body, _ := json.Marshal(rows)
	if err := os.WriteFile(filepath.Join(dir, "source-performance.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if got := RankedDownloadServices([]string{"jiosaavn", "qobuz"}, "16"); !reflect.DeepEqual(got, []string{"qobuz", "jiosaavn"}) {
		t.Fatal(got)
	}
}

func TestResourceRankingRequiresSuccessfulResponses(t *testing.T) {
	now := time.Now()
	rows := []SourceBenchmark{
		{ID: "blocked", Quality: "resource", State: "authentication_required", Attempts: 1, MedianMS: 1, CheckedAt: now},
		{ID: "working", Quality: "resource", State: "available", Attempts: 3, Successes: 3, MedianMS: 500, CheckedAt: now},
	}
	if got := rankSources([]string{"blocked", "working"}, "resource", rows, now); !reflect.DeepEqual(got, []string{"working", "blocked"}) {
		t.Fatal(got)
	}
}
