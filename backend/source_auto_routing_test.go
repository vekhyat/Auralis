package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var retiredBuiltInRouteIDs = []string{
	"antra-tidal",
	"antra-amazon",
	"community-tidal",
	"community-qobuz",
	"community-amazon",
	"zarz-tidal",
	"zarz-qobuz",
	"zarz-amazon",
	"jiosaavn-community",
}

func supportedAutoServiceSet() map[string]bool {
	return map[string]bool{"qobuz": true, "deezer": true, "apple": true, "jiosaavn": true}
}

func attemptIDs(attempts []sourceDownloadAttempt) []string {
	ids := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		ids = append(ids, attempt.id)
	}
	return ids
}

func containsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

// A legacy saved order (or a stale bridge caller) favoring retired services
// must not reintroduce them into automatic selection, even when stored
// benchmarks claim the retired services are fast and validated.
func TestAutoRoutingExcludesRetiredServices(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(appDataDirEnv, dir)
	now := time.Now()
	rows := []SourceBenchmark{
		{ID: "service-tidal", Service: "tidal", Quality: "16", State: "validated", Attempts: 9, Successes: 9, MedianMS: 1, CheckedAt: now},
		{ID: "service-amazon", Service: "amazon", Quality: "16", State: "validated", Attempts: 9, Successes: 9, MedianMS: 2, CheckedAt: now},
	}
	body, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source-performance.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	legacy := []string{"tidal", "qobuz", "amazon", "deezer", "apple", "jiosaavn"}
	for _, quality := range []string{"16", "24", "atmos", "lossy"} {
		got := RankedDownloadServices(legacy, quality)
		if len(got) == 0 {
			t.Fatalf("%s: auto routing returned no services", quality)
		}
		for _, service := range got {
			if !supportedAutoServiceSet()[service] {
				t.Fatalf("%s: retired service %q reintroduced in %v", quality, service, got)
			}
		}
		if containsID(got, "tidal") || containsID(got, "amazon") {
			t.Fatalf("%s: legacy order leaked into auto routing: %v", quality, got)
		}
		if quality != "lossy" && got[len(got)-1] != "jiosaavn" {
			t.Fatalf("%s: AAC route must stay last: %v", quality, got)
		}
	}
}

// An order containing only retired services falls back to the vetted set
// instead of failing or attempting the retired routes.
func TestAutoRoutingDefaultsWithoutRetiredServices(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	for _, order := range [][]string{{"tidal", "amazon"}, {"amazon-tidal"}, {""}, {}} {
		got := RankedDownloadServices(order, "16")
		if len(got) != len(defaultAutoServices) {
			t.Fatalf("order %v: got %v, want the full vetted set", order, got)
		}
		seen := map[string]bool{}
		for _, service := range got {
			seen[service] = true
		}
		for _, service := range defaultAutoServices {
			if !seen[service] {
				t.Fatalf("order %v: vetted service %q missing from %v", order, service, got)
			}
		}
	}
}

// The download registry advertises only verified built-in routes. Explicitly
// enabled custom servers are still listed; retired built-ins are not.
func TestDownloadSourcesRegistryListsOnlyVerifiedAutoRoutes(t *testing.T) {
	isolateAppData(t)
	var builtIn []string
	for _, source := range DownloadSources() {
		if source.Role != "download" {
			t.Fatalf("non-download role advertised: %+v", source)
		}
		builtIn = append(builtIn, source.ID)
	}
	want := map[string]bool{"antra-qobuz": true, "antra-deezer": true, "antra-apple": true, "jiosaavn-official": true}
	if len(builtIn) != len(want) {
		t.Fatalf("clean install registry = %v, want %v", builtIn, want)
	}
	for _, id := range builtIn {
		if !want[id] {
			t.Fatalf("clean install registry contains %q, want only %v", id, want)
		}
	}
	for _, retired := range retiredBuiltInRouteIDs {
		if containsID(builtIn, retired) {
			t.Fatalf("retired route %q still advertised", retired)
		}
	}
	sources, err := ListCommunitySources()
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, CommunitySource{
		ID: "custom-qobuz-node", Name: "Custom Qobuz Node",
		Service: "qobuz", Protocol: "qobuz-dl",
		BaseURL: "https://custom-node.example.org", Enabled: true,
	})
	if err := SaveCommunitySources(sources); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, source := range DownloadSources() {
		ids[source.ID] = true
	}
	if !ids["custom-qobuz-node"] || !ids["antra-qobuz"] {
		t.Fatalf("explicit custom server dropped from registry: %v", ids)
	}
	for _, retired := range retiredBuiltInRouteIDs {
		if ids[retired] {
			t.Fatalf("retired route %q reintroduced alongside custom servers", retired)
		}
	}
}

// Normal Qobuz routing attempts the verified Antra route (and explicitly
// enabled custom servers) without appending retired community/Zarz mirrors.
func TestQobuzAutoAttemptsExcludeRetiredRoutes(t *testing.T) {
	isolateAppData(t)
	dest := filepath.Join(t.TempDir(), "audio.flac")
	ids := attemptIDs(qobuzAutoAttempts(&QobuzDownloader{}, 30369895, "6", dest, SourceTrack{ID: "30369895"}))
	if !containsID(ids, "antra-qobuz") {
		t.Fatalf("verified qobuz route missing: %v", ids)
	}
	for _, retired := range []string{"community-qobuz", "zarz-qobuz", "community-tidal", "zarz-tidal", "community-amazon", "zarz-amazon"} {
		if containsID(ids, retired) {
			t.Fatalf("retired route %q attempted by normal qobuz routing: %v", retired, ids)
		}
	}
	sources, err := ListCommunitySources()
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, CommunitySource{
		ID: "custom-qobuz-node", Name: "Custom Qobuz Node",
		Service: "qobuz", Protocol: "qobuz-dl",
		BaseURL: "https://custom-node.example.org", Enabled: true,
	})
	if err := SaveCommunitySources(sources); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dropCircuitIDs("custom-qobuz-node") })
	ids = attemptIDs(qobuzAutoAttempts(&QobuzDownloader{}, 30369895, "6", dest, SourceTrack{ID: "30369895"}))
	if !containsID(ids, "antra-qobuz") || !containsID(ids, "custom-qobuz-node") {
		t.Fatalf("explicit custom qobuz server not attempted: %v", ids)
	}
	for _, retired := range []string{"community-qobuz", "zarz-qobuz"} {
		if containsID(ids, retired) {
			t.Fatalf("retired route %q attempted alongside custom servers: %v", retired, ids)
		}
	}
}

// Tidal routing keeps its explicit built-in route and custom servers but no
// longer appends retired community/Zarz mirrors.
func TestTidalAutoAttemptsExcludeRetiredRoutes(t *testing.T) {
	isolateAppData(t)
	dest := filepath.Join(t.TempDir(), "audio.flac")
	ids := attemptIDs(tidalAutoAttempts(NewTidalDownloader(""), 55130631, "LOSSLESS", dest, SourceTrack{ID: "55130631"}))
	if !containsID(ids, "antra-tidal") {
		t.Fatalf("explicit tidal route missing: %v", ids)
	}
	for _, retired := range []string{"community-tidal", "zarz-tidal"} {
		if containsID(ids, retired) {
			t.Fatalf("retired route %q attempted by tidal routing: %v", retired, ids)
		}
	}
}
