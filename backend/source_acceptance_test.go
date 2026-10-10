package backend

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Exercise the ranked call site: an HTTP-successful route must not stop
// fallback when it delivers a preview or silently changes the audio format.
func TestRankedSourcesRejectPreviewAndWrongCodec(t *testing.T) {
	for _, tc := range []struct {
		name, quality, codec string
		expected             int
		duration             float64
	}{
		{"preview without catalog duration", "16", "flac", 0, 30},
		{"lossy response to lossless request", "16", "mp3", 260, 260},
		{"stereo response to Atmos request", "ATMOS", "flac", 260, 260},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(appDataDirEnv, t.TempDir())
			dir := t.TempDir()
			bad, good := filepath.Join(dir, "bad.flac"), filepath.Join(dir, "good.flac")
			SetAudioDurationReaderForTest(func(path string) (float64, error) {
				if path == bad {
					return tc.duration, nil
				}
				return 260, nil
			})
			t.Cleanup(func() { SetAudioDurationReaderForTest(nil) })
			previous := communityCodecReader
			communityCodecReader = func(path string) (string, error) {
				if path == bad {
					return tc.codec, nil
				}
				if tc.quality == "ATMOS" {
					return "eac3", nil
				}
				return "flac", nil
			}
			t.Cleanup(func() { communityCodecReader = previous })
			var calls []string
			path, err := runDownloadSources("tidal", tc.quality, tc.expected, []sourceDownloadAttempt{
				{"bad", func() (string, error) {
					calls = append(calls, "bad")
					return bad, os.WriteFile(bad, []byte("bad"), 0600)
				}},
				{"good", func() (string, error) {
					calls = append(calls, "good")
					return good, os.WriteFile(good, []byte("good"), 0600)
				}},
			})
			if err != nil || path != good || !reflect.DeepEqual(calls, []string{"bad", "good"}) {
				t.Fatalf("invalid source stopped fallback: path=%q err=%v calls=%v", path, err, calls)
			}
			if _, err := os.Stat(bad); !os.IsNotExist(err) {
				t.Fatalf("invalid audio retained: %v", err)
			}
			for _, row := range SourceBenchmarks() {
				if row.ID == "bad" && (row.Successes != 0 || row.State == "validated") {
					t.Fatalf("invalid audio improved source priority: %+v", row)
				}
			}
		})
	}
}

func TestRankedSourceCannotSucceedWithoutAFile(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	path, err := runDownloadSources("qobuz", "16", 260, []sourceDownloadAttempt{
		{"empty", func() (string, error) { return "", nil }},
	})
	if err == nil || path != "" {
		t.Fatalf("empty route accepted: path=%q err=%v", path, err)
	}
	for _, row := range SourceBenchmarks() {
		if row.ID == "empty" && row.Successes != 0 {
			t.Fatalf("empty route recorded as successful: %+v", row)
		}
	}
}
