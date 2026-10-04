package backend

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type sourceAudioSample struct {
	ID       string  `json:"id"`
	Service  string  `json:"service"`
	Quality  string  `json:"quality"`
	Passed   bool    `json:"passed"`
	Seconds  float64 `json:"elapsed_seconds"`
	Codec    string  `json:"codec,omitempty"`
	Bits     int     `json:"bit_depth,omitempty"`
	Rate     int     `json:"sample_rate,omitempty"`
	Duration float64 `json:"duration_seconds,omitempty"`
	Error    string  `json:"error,omitempty"`
}

func TestLiveRankedSourceBenchmark(t *testing.T) {
	if os.Getenv("AURALIS_LIVE_SOURCE_CHECK") != "1" {
		t.Skip("live downloads are opt-in")
	}
	root := os.Getenv("AURALIS_LIVE_RESULTS_DIR")
	if !filepath.IsAbs(root) {
		t.Fatal("isolated results directory required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(appDataDirEnv, filepath.Join(root, "app-data"))
	t.Setenv("USERPROFILE", filepath.Join(root, "profile"))
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		id, service, quality string
		download             func(string) (string, error)
	}
	fixtures := []fixture{
		{"antra-tidal", "tidal", "16", func(p string) (string, error) {
			return NewTidalDownloader("").downloadRankedTidal(55130631, "LOSSLESS", p)
		}},
		{"antra-qobuz", "qobuz", "16", func(p string) (string, error) {
			q := &QobuzDownloader{client: &http.Client{Timeout: 12 * time.Second}}
			return q.downloadRankedQobuz(30369895, "6", p, 260, false)
		}},
		{"antra-deezer", "deezer", "16", func(p string) (string, error) {
			path, _, err := downloadDeezerTrack(ExtraDownloadParams{ServiceURL: "https://www.deezer.com/track/116348128"}, p)
			return path, err
		}},
		{"antra-apple", "apple", "16", func(p string) (string, error) { return antraStreamToFile("apple", "6818177515", p, nil) }},
		{"jiosaavn-official", "jiosaavn", "lossy", func(p string) (string, error) {
			raw, err := jioSaavnOfficialStreamURL("buPhYncP")
			if err != nil {
				return "", err
			}
			return antraDownloadURLToFile(raw, p)
		}},
		{"antra-qobuz", "qobuz", "24", func(p string) (string, error) {
			q := &QobuzDownloader{client: &http.Client{Timeout: 12 * time.Second}}
			return q.downloadRankedQobuz(64868955, "27", p, 260, false)
		}},
	}
	var rows []sourceAudioSample
	defer func() {
		body, _ := json.MarshalIndent(rows, "", "  ")
		if err := os.WriteFile(filepath.Join(root, "audio-benchmark.json"), body, 0600); err != nil {
			t.Error(err)
		}
	}()
	rounds := 3
	deadline := 180 * time.Second
	if n, err := strconv.Atoi(os.Getenv("AURALIS_LIVE_BENCHMARK_ROUNDS")); err == nil && n > 0 && n <= 3 {
		rounds = n
	}
	for _, fixture := range fixtures {
		if filter := os.Getenv("AURALIS_LIVE_BENCHMARK_FILTER"); filter != "" && filter != fixture.id+"|"+fixture.quality {
			continue
		}
		for round := 0; round < rounds; round++ {
			row := sourceAudioSample{ID: fixture.id, Service: fixture.service, Quality: fixture.quality}
			start := time.Now()
			dest := filepath.Join(root, fixture.id+"-"+fixture.quality+"-"+strconv.Itoa(round)+".flac")
			_, finish := BeginDownloadCancellationScope()
			timer := time.AfterFunc(deadline, ForceStopActiveDownloads)
			path, downloadErr := fixture.download(dest)
			timer.Stop()
			finish()
			row.Seconds = time.Since(start).Seconds()
			if downloadErr != nil {
				row.Error = "download failed"
				rows = append(rows, row)
				break
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			body, probeErr := exec.CommandContext(ctx, probe, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,sample_rate,bits_per_raw_sample:format=duration", "-of", "json", path).Output()
			var media struct {
				Streams []struct {
					Codec string `json:"codec_name"`
					Rate  string `json:"sample_rate"`
					Bits  string `json:"bits_per_raw_sample"`
				} `json:"streams"`
				Format struct {
					Duration string `json:"duration"`
				} `json:"format"`
			}
			if probeErr != nil || json.Unmarshal(body, &media) != nil || len(media.Streams) == 0 {
				row.Error = "unreadable audio"
			} else {
				stream := media.Streams[0]
				row.Codec = stream.Codec
				row.Bits, _ = strconv.Atoi(stream.Bits)
				row.Rate, _ = strconv.Atoi(stream.Rate)
				row.Duration, _ = strconv.ParseFloat(media.Format.Duration, 64)
				if math.Abs(row.Duration-260) > 15 {
					row.Error = "preview or duration mismatch"
				} else if fixture.quality != "lossy" && stream.Codec != "flac" && stream.Codec != "alac" {
					row.Error = "lossy response for lossless request"
				} else if fixture.quality == "24" && row.Bits <= 16 {
					row.Error = "24-bit request returned lower bit depth"
				} else if _, err := exec.CommandContext(ctx, decoder, "-v", "error", "-xerror", "-i", path, "-map", "0:a:0", "-f", "null", "-").CombinedOutput(); err != nil {
					row.Error = "full decode failed"
				} else {
					row.Passed = true
				}
			}
			cancel()
			_ = os.Remove(path)
			rows = append(rows, row)
			t.Log(strings.Join([]string{fixture.id, fixture.quality, strconv.FormatBool(row.Passed), strconv.FormatFloat(row.Seconds, 'f', 2, 64)}, " "))
			if !row.Passed {
				break
			}
		}
	}
}
