package backend

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The seed contains measured results, never account credentials or media URLs.
//
//go:embed source_benchmarks.json
var sourceBenchmarkSeed []byte

type SourceBenchmark struct {
	ID        string    `json:"id"`
	Service   string    `json:"service"`
	Quality   string    `json:"quality"`
	State     string    `json:"state"`
	Attempts  int       `json:"attempts"`
	Successes int       `json:"successes"`
	MedianMS  float64   `json:"median_ms"`
	SamplesMS []float64 `json:"samples_ms,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type DownloadSource struct {
	ID      string `json:"id"`
	Service string `json:"service"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	URL     string `json:"url,omitempty"`
}

// The registry lists implemented routes. Only validated audio improves their
// download priority; presence here does not assert hosted availability.
func DownloadSources() []DownloadSource {
	var out []DownloadSource
	for _, service := range []string{"tidal", "qobuz", "amazon", "deezer", "apple"} {
		out = append(out, DownloadSource{ID: "antra-" + service, Service: service, Name: "Antra / " + service, Role: "download"})
		if service == "tidal" || service == "qobuz" || service == "amazon" {
			out = append(out, DownloadSource{ID: "community-" + service, Service: service, Name: "Community / " + service, Role: "download"}, DownloadSource{ID: "zarz-" + service, Service: service, Name: "Zarz / " + service, Role: "download"})
		}
	}
	out = append(out,
		DownloadSource{ID: "jiosaavn-official", Service: "jiosaavn", Name: "JioSaavn", Role: "download", URL: jioSaavnOfficialAPI},
		DownloadSource{ID: "jiosaavn-community", Service: "jiosaavn", Name: "Saavn.dev", Role: "download", URL: jioSaavnCommunityAPI},
	)
	if sources, err := ListCommunitySources(); err == nil {
		for _, source := range sources {
			if source.Enabled && source.BaseURL != "" {
				out = append(out, DownloadSource{ID: source.ID, Service: source.Service, Name: source.Name, Role: "download", URL: source.BaseURL})
			}
		}
	}
	return out
}

func ResourceSources() []DownloadSource {
	return []DownloadSource{
		{ID: "resource-tidal", Service: "tidal", Name: "TIDAL catalog", Role: "catalog", URL: tidalPublicAPIBase},
		{ID: "samidy-catalog", Service: "tidal", Name: "Samidy Hi-Fi catalog", Role: "catalog", URL: "https://monochrome-api.samidy.com"},
		{ID: "resource-qobuz", Service: "qobuz", Name: "Qobuz catalog", Role: "catalog", URL: qobuzAPIBaseURL},
		{ID: "resource-deezer", Service: "deezer", Name: "Deezer catalog", Role: "catalog", URL: "https://api.deezer.com"},
		{ID: "resource-musicbrainz", Name: "MusicBrainz", Role: "metadata", URL: musicBrainzAPIBase},
		{ID: "resource-lrclib", Name: "LRCLIB", Role: "lyrics", URL: "https://lrclib.net"},
		{ID: "resource-songstats", Name: "Songstats", Role: "resolver", URL: "https://songstats.com"},
		{ID: "resource-songlink", Name: "song.link", Role: "resolver", URL: "https://song.link"},
		{ID: "resource-spotify", Name: "Spotify metadata", Role: "metadata", URL: "https://open.spotify.com"},
	}
}

func sourceQuality(quality string) string {
	switch strings.ToUpper(strings.TrimSpace(quality)) {
	case "ATMOS", "DOLBY_ATMOS", "EAC3", "EAC3_JOC":
		return "atmos"
	case "24", "27", "7", "HI_RES", "HI_RES_LOSSLESS":
		return "24"
	case "RESOURCE":
		return "resource"
	case "LOSSY":
		return "lossy"
	default:
		return "16"
	}
}

func benchmarkKey(id, quality string) string { return id + "|" + sourceQuality(quality) }

var sourcePerformance = struct {
	sync.Mutex
	dir    string
	values map[string]SourceBenchmark
}{values: map[string]SourceBenchmark{}}

func loadSourcePerformanceLocked() {
	dir, err := GetAppDir()
	if err != nil {
		return
	}
	if sourcePerformance.dir == dir {
		return
	}
	sourcePerformance.dir = dir
	sourcePerformance.values = map[string]SourceBenchmark{}
	for _, body := range [][]byte{sourceBenchmarkSeed, readSourcePerformance(dir)} {
		var rows []SourceBenchmark
		if json.Unmarshal(body, &rows) != nil {
			continue
		}
		for _, row := range rows {
			if row.ID != "" && row.MedianMS >= 0 {
				sourcePerformance.values[benchmarkKey(row.ID, row.Quality)] = row
			}
		}
	}
}

func readSourcePerformance(dir string) []byte {
	body, _ := os.ReadFile(filepath.Join(dir, "source-performance.json"))
	return body
}

func SourceBenchmarks() []SourceBenchmark {
	sourcePerformance.Lock()
	defer sourcePerformance.Unlock()
	loadSourcePerformanceLocked()
	out := make([]SourceBenchmark, 0, len(sourcePerformance.values))
	for _, row := range sourcePerformance.values {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return benchmarkKey(out[i].ID, out[i].Quality) < benchmarkKey(out[j].ID, out[j].Quality)
	})
	return out
}

// Ranking is scoped to the requested quality. A 16-bit success does not prove
// 24-bit or Atmos support, and a fast error never outranks validated audio.
func rankSources(ids []string, quality string, rows []SourceBenchmark, now time.Time) []string {
	metrics := map[string]SourceBenchmark{}
	for _, row := range rows {
		if sourceQuality(row.Quality) == sourceQuality(quality) && now.Sub(row.CheckedAt) < 7*24*time.Hour && !row.CheckedAt.After(now.Add(time.Minute)) {
			metrics[row.ID] = row
		}
	}
	class := func(id string) int {
		row, ok := metrics[id]
		if !ok {
			return 1
		}
		if (row.State == "validated" || (sourceQuality(quality) == "resource" && row.State == "available")) && row.Successes > 0 {
			return 0
		}
		if row.State == "available" {
			return 1
		}
		return 2
	}
	out := append([]string(nil), ids...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := metrics[out[i]], metrics[out[j]]
		ca, cb := class(out[i]), class(out[j])
		if ca != cb {
			return ca < cb
		}
		if ca != 0 {
			return false
		}
		ra, rb := float64(a.Successes)/float64(max(1, a.Attempts)), float64(b.Successes)/float64(max(1, b.Attempts))
		if ra != rb {
			return ra > rb
		}
		return a.MedianMS < b.MedianMS
	})
	return out
}

func RankedSourceIDs(ids []string, quality string) []string {
	return rankSources(ids, quality, SourceBenchmarks(), time.Now())
}

func RankedDownloadServices(services []string, quality string) []string {
	rows := SourceBenchmarks()
	var best []SourceBenchmark
	for _, service := range services {
		for _, row := range rows {
			if row.ID == "service-"+service && sourceQuality(row.Quality) == sourceQuality(quality) && time.Since(row.CheckedAt) < 7*24*time.Hour {
				row.ID = service
				best = append(best, row)
			}
		}
		var choices []string
		for _, source := range DownloadSources() {
			if source.Service == service && source.Role == "download" {
				choices = append(choices, source.ID)
			}
		}
		choices = rankSources(choices, quality, rows, time.Now())
		if len(choices) == 0 {
			continue
		}
		for _, row := range rows {
			if row.ID == choices[0] && sourceQuality(row.Quality) == sourceQuality(quality) && !hasServiceBenchmark(best, service) {
				row.ID = service
				best = append(best, row)
				break
			}
		}
	}
	out := rankSources(services, quality, best, time.Now())
	// AAC remains the final fallback for lossless and Atmos requests.
	if sourceQuality(quality) != "lossy" {
		filtered := make([]string, 0, len(out))
		for _, service := range out {
			if service != "jiosaavn" {
				filtered = append(filtered, service)
			}
		}
		for _, service := range out {
			if service == "jiosaavn" {
				filtered = append(filtered, service)
			}
		}
		out = filtered
	}
	return out
}

func hasServiceBenchmark(rows []SourceBenchmark, service string) bool {
	for _, row := range rows {
		if row.ID == service {
			return true
		}
	}
	return false
}

// Called after the facade's duration validation and before optional conversion.
// Existing files and cancelled requests must not be reported as fast downloads.
func RecordServiceDownload(service, quality string, elapsed time.Duration, validated bool, err error) {
	if service == "jiosaavn" {
		quality = "lossy"
	}
	if service == "deezer" {
		quality = "16"
	}
	recordSourceOutcome("service-"+service, service, quality, elapsed, validated, err)
}

func RecordServiceDownloadedFile(service, quality, path string, elapsed time.Duration, validated bool) {
	if sourceQuality(quality) == "24" && validated {
		metadata, err := GetMetadataWithFFprobe(path)
		validated = err == nil && metadata.BitsPerSample > 16
	}
	RecordServiceDownload(service, quality, elapsed, validated, nil)
}

func recordSourceOutcome(id, service, quality string, elapsed time.Duration, validated bool, err error) {
	if IsDownloadCancelledError(err) {
		return
	}
	sourcePerformance.Lock()
	defer sourcePerformance.Unlock()
	loadSourcePerformanceLocked()
	key := benchmarkKey(id, quality)
	row := sourcePerformance.values[key]
	row.ID, row.Service, row.Quality = id, service, sourceQuality(quality)
	row.Attempts++
	row.CheckedAt = time.Now().UTC()
	if err != nil {
		row.State = "failed"
	} else if validated {
		row.State = "validated"
		row.Successes++
		ms := float64(elapsed) / float64(time.Millisecond)
		row.SamplesMS = append(row.SamplesMS, ms)
		if len(row.SamplesMS) > 9 {
			row.SamplesMS = row.SamplesMS[len(row.SamplesMS)-9:]
		}
		samples := append([]float64(nil), row.SamplesMS...)
		sort.Float64s(samples)
		row.MedianMS = samples[len(samples)/2]
	} else {
		row.State = "available"
	}
	sourcePerformance.values[key] = row
	persistSourcePerformanceLocked()
}

// persistSourcePerformanceLocked writes the in-memory measurements back to disk.
// Callers must already hold sourcePerformance.
func persistSourcePerformanceLocked() {
	var rows []SourceBenchmark
	for _, value := range sourcePerformance.values {
		rows = append(rows, value)
	}
	if body, err := json.MarshalIndent(rows, "", "  "); err == nil && sourcePerformance.dir != "" {
		_ = WriteFileAtomic(filepath.Join(sourcePerformance.dir, "source-performance.json"), body, 0600)
	}
}

// InvalidateSourceRecords drops every stored measurement for a source. It runs
// when a community source's server or credential configuration changes so a
// previous server's timings are never shown as evidence for a replacement.
func InvalidateSourceRecords(id string) {
	if id == "" {
		return
	}
	sourcePerformance.Lock()
	defer sourcePerformance.Unlock()
	loadSourcePerformanceLocked()
	changed := false
	for key, row := range sourcePerformance.values {
		if row.ID == id {
			delete(sourcePerformance.values, key)
			changed = true
		}
	}
	if changed {
		persistSourcePerformanceLocked()
	}
}
