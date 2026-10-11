package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// dropCircuitIDs removes only the listed circuit pauses. It must not reset the
// whole map, so unrelated sources keep their backoff state.
func dropCircuitIDs(ids ...string) {
	if len(ids) == 0 {
		return
	}
	communityCircuit.Lock()
	for _, id := range ids {
		delete(communityCircuit.until, id)
	}
	communityCircuit.Unlock()
}

func TestCleanInstallCommunitySourcesDefaultsDisabled(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	if got := defaultCommunitySources(); len(got) != 0 {
		t.Fatalf("production defaults = %d rows (expected empty, no shipped presets)", len(got))
	}
	sources, err := ListCommunitySources()
	if err != nil {
		t.Fatalf("ListCommunitySources failed: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("clean install registry = %d rows (expected empty)", len(sources))
	}
	if checks := GetCommunitySourceChecks(); len(checks) != 0 {
		t.Fatalf("clean install has %d stored checks (expected 0)", len(checks))
	}

	track := SourceTrack{ID: "track1", Title: "Track", Artist: "Artist"}
	dest := filepath.Join(t.TempDir(), "dummy.flac")
	for _, tc := range []struct{ service, quality string }{
		{"tidal", "lossless"},
		{"qobuz", "6"},
		{"amazon", "lossless"},
	} {
		if attempts := communitySourceAttempts(tc.service, tc.quality, dest, track); len(attempts) != 0 {
			t.Fatalf("clean install produced %d %s attempts (expected 0)", len(attempts), tc.service)
		}
	}
}

// Explicitly saved custom routes must be offered without requiring prior
// audio verification. Uses synthetic fixtures only; no shipped public preset.
func TestCommunitySourcesSavedOptInRetainsRoute(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Cleanup(func() { dropCircuitIDs("custom-qobuz-a", "custom-qobuz-b") })

	rows := []CommunitySource{
		{ID: "custom-qobuz-a", Name: "Custom Qobuz A", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: "https://custom-node-a.example.org", Enabled: true},
		{ID: "custom-qobuz-b", Name: "Custom Qobuz B", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: "https://custom-node-b.example.org", Enabled: true},
	}
	if err := SaveCommunitySources(rows); err != nil {
		t.Fatalf("SaveCommunitySources failed: %v", err)
	}

	reloaded, err := ListCommunitySources()
	if err != nil {
		t.Fatalf("ListCommunitySources after save failed: %v", err)
	}
	enabled := map[string]bool{}
	for _, s := range reloaded {
		if s.Enabled {
			enabled[s.ID] = true
		}
	}
	if !enabled["custom-qobuz-a"] || !enabled["custom-qobuz-b"] {
		t.Fatalf("saved opt-ins not retained: %v", enabled)
	}

	dropCircuitIDs("custom-qobuz-a", "custom-qobuz-b")
	attempts := communitySourceAttempts("qobuz", "6", filepath.Join(t.TempDir(), "dummy.flac"), SourceTrack{ID: "track1", Title: "Track", Artist: "Artist"})
	got := map[string]bool{}
	for _, a := range attempts {
		got[a.id] = true
	}
	if !got["custom-qobuz-a"] || !got["custom-qobuz-b"] {
		t.Fatalf("opt-ins not offered without verification gate: %v", got)
	}
}

// Saving an empty list (settings reset to defaults) returns to an empty
// registry with no automatic attempts.
func TestCommunitySourcesSettingsResetRevertsToDisabled(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	custom := []CommunitySource{
		{ID: "reset-custom-tidal", Name: "Custom Tidal", Service: "tidal", Protocol: "hifi", BaseURL: "https://tidal-custom.example.test", Enabled: true},
		{ID: "reset-custom-qobuz", Name: "Custom Qobuz", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: "https://qobuz-custom.example.test", Enabled: true},
	}
	if err := SaveCommunitySources(custom); err != nil {
		t.Fatalf("SaveCommunitySources failed: %v", err)
	}
	if err := SaveCommunitySources([]CommunitySource{}); err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	reset, err := ListCommunitySources()
	if err != nil {
		t.Fatalf("ListCommunitySources after reset failed: %v", err)
	}
	if len(reset) != 0 {
		t.Fatalf("registry after reset = %d rows (expected empty)", len(reset))
	}
	track := SourceTrack{ID: "track1", Title: "Track", Artist: "Artist"}
	dest := filepath.Join(t.TempDir(), "dummy.flac")
	if n := len(communitySourceAttempts("tidal", "lossless", dest, track)); n != 0 {
		t.Fatalf("tidal attempts after reset: %d", n)
	}
	if n := len(communitySourceAttempts("qobuz", "6", dest, track)); n != 0 {
		t.Fatalf("qobuz attempts after reset: %d", n)
	}
}

// Toggling Enabled keeps checks/circuit; changing the server drops stale evidence.
func TestCommunitySourcesEvidencePreservedOnToggle(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	const id = "policy-toggle-custom"
	t.Cleanup(func() { dropCircuitIDs(id) })

	fixture := CommunitySource{ID: id, Name: "Toggle Fixture", Service: "tidal", Protocol: "hifi", BaseURL: "https://hifi-toggle.example.test", Enabled: true}
	if err := SaveCommunitySources([]CommunitySource{fixture}); err != nil {
		t.Fatalf("SaveCommunitySources failed: %v", err)
	}
	saveCommunityCheck(CommunitySourceCheck{ID: id, State: "validated", AudioVerified: true, CheckedAt: time.Now().UTC().Format(time.RFC3339)})
	pauseUntil := time.Now().Add(10 * time.Minute)
	communityCircuit.Lock()
	communityCircuit.until[id] = pauseUntil
	communityCircuit.Unlock()

	sources, err := ListCommunitySources()
	if err != nil {
		t.Fatalf("ListCommunitySources failed: %v", err)
	}
	for i := range sources {
		if sources[i].ID == id {
			sources[i].Enabled = !sources[i].Enabled
		}
	}
	if err := SaveCommunitySources(sources); err != nil {
		t.Fatalf("SaveCommunitySources failed: %v", err)
	}

	kept := false
	for _, c := range GetCommunitySourceChecks() {
		if c.ID == id && c.AudioVerified {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("check evidence for %s lost on toggle", id)
	}
	communityCircuit.Lock()
	paused := communityCircuit.until[id]
	communityCircuit.Unlock()
	if !paused.Equal(pauseUntil) {
		t.Fatalf("circuit pause not preserved: got %v want %v", paused, pauseUntil)
	}

	sources, _ = ListCommunitySources()
	for i := range sources {
		if sources[i].ID == id {
			sources[i].BaseURL = "https://changed-server.example.test"
		}
	}
	if err := SaveCommunitySources(sources); err != nil {
		t.Fatalf("SaveCommunitySources with changed URL failed: %v", err)
	}
	for _, c := range GetCommunitySourceChecks() {
		if c.ID == id {
			t.Fatalf("stale check survived server change: %+v", c)
		}
	}
	communityCircuit.Lock()
	_, stillPaused := communityCircuit.until[id]
	communityCircuit.Unlock()
	if stillPaused {
		t.Fatalf("circuit pause survived server change for %s", id)
	}
}

// Persisted copies of the retired public presets must not resurrect, even with
// Enabled:true or URL variations (trailing slash, case, path). Custom local
// servers must survive. No network calls.
func TestRetiredPublicPresetsExcludedFromPersistedSettings(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Cleanup(func() { dropCircuitIDs("custom-local-node", "stale-alias", "dab-xyz") })

	stale := []CommunitySource{
		{ID: "dab-xyz", Name: "DAB Music", Service: "qobuz", Protocol: "dab", BaseURL: "https://dabmusic.xyz", Enabled: true, CredentialEnv: "AURALIS_DAB_COOKIE", CredentialType: "cookie"},
		{ID: "stale-alias", Name: "Stale Alias", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: "https://QOBUZ.SQUID.WTF/api/download-music?x=1", Enabled: true},
		{ID: "stale-slash", Name: "Stale Slash", Service: "tidal", Protocol: "hifi", BaseURL: "https://monochrome-api.samidy.com/", Enabled: true},
		{ID: "qobuz-rest", Name: "Qobuz REST server", Service: "qobuz", Protocol: "qobuz-rest", CredentialEnv: "AURALIS_QOBUZ_REST_KEY", CredentialType: "api_key"},
		{ID: "custom-local-node", Name: "Custom Local", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: "http://127.0.0.1:9", Enabled: true},
	}
	path, err := communitySourcesPath()
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.MarshalIndent(stale, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, body, 0600); err != nil {
		t.Fatal(err)
	}

	rows, err := ListCommunitySources()
	if err != nil {
		t.Fatalf("ListCommunitySources failed: %v", err)
	}
	byID := map[string]CommunitySource{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	for _, retired := range []string{"dab-xyz", "stale-alias", "stale-slash", "qobuz-rest"} {
		if _, ok := byID[retired]; ok {
			t.Fatalf("retired preset %s resurrected from persisted settings", retired)
		}
	}
	kept, ok := byID["custom-local-node"]
	if !ok || !kept.Enabled {
		t.Fatalf("custom local configuration lost: %v", byID)
	}

	dropCircuitIDs("custom-local-node", "stale-alias", "dab-xyz")
	attempts := communitySourceAttempts("qobuz", "6", filepath.Join(t.TempDir(), "dummy.flac"), SourceTrack{ID: "track1", Title: "Track", Artist: "Artist"})
	for _, a := range attempts {
		if a.id == "dab-xyz" || a.id == "stale-alias" {
			t.Fatalf("retired preset offered an attempt: %s", a.id)
		}
	}
	foundCustom := false
	for _, a := range attempts {
		if a.id == "custom-local-node" {
			foundCustom = true
		}
	}
	if !foundCustom {
		t.Fatal("custom local route was not offered")
	}

	// Saving through the public API must also purge retired rows from disk.
	if err := SaveCommunitySources(stale); err != nil {
		t.Fatalf("SaveCommunitySources failed: %v", err)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var written []CommunitySource
	if err := json.Unmarshal(persisted, &written); err != nil {
		t.Fatal(err)
	}
	for _, row := range written {
		if row.ID == "dab-xyz" || row.ID == "stale-alias" || row.ID == "stale-slash" || row.ID == "qobuz-rest" {
			t.Fatalf("retired preset persisted to disk: %s", row.ID)
		}
	}
}

// A legacy profile ID deliberately repointed at a custom/local server is a
// custom configuration and must be preserved.
func TestRepointedLegacyIDPreservedAsCustom(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Cleanup(func() { dropCircuitIDs("dab-xyz", "qobuz-rest") })

	rows := []CommunitySource{
		{ID: "dab-xyz", Name: "Repointed DAB", Service: "qobuz", Protocol: "dab", BaseURL: "http://127.0.0.1:9", Enabled: true, CredentialType: "cookie"},
		{ID: "qobuz-rest", Name: "Repointed REST", Service: "qobuz", Protocol: "qobuz-rest", BaseURL: "http://127.0.0.1:8000", Enabled: true},
	}
	if err := SaveCommunitySources(rows); err != nil {
		t.Fatalf("SaveCommunitySources failed: %v", err)
	}
	reloaded, err := ListCommunitySources()
	if err != nil {
		t.Fatalf("ListCommunitySources failed: %v", err)
	}
	byID := map[string]CommunitySource{}
	for _, row := range reloaded {
		byID[row.ID] = row
	}
	if got, ok := byID["dab-xyz"]; !ok || got.BaseURL != "http://127.0.0.1:9" || !got.Enabled {
		t.Fatalf("repointed dab-xyz not preserved: %v", byID)
	}
	if got, ok := byID["qobuz-rest"]; !ok || got.BaseURL != "http://127.0.0.1:8000" || !got.Enabled {
		t.Fatalf("repointed qobuz-rest not preserved: %v", byID)
	}

	dropCircuitIDs("dab-xyz", "qobuz-rest")
	attempts := communitySourceAttempts("qobuz", "6", filepath.Join(t.TempDir(), "dummy.flac"), SourceTrack{ID: "track1", Title: "Track", Artist: "Artist"})
	got := map[string]bool{}
	for _, a := range attempts {
		got[a.id] = true
	}
	if !got["dab-xyz"] || !got["qobuz-rest"] {
		t.Fatalf("repointed custom routes not offered: %v", got)
	}
}

// After a settings reset the registry stays empty and no service attempts.
func TestEmptyRegistryProducesNoAttemptsAfterReset(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	if err := SaveCommunitySources([]CommunitySource{
		{ID: "reset-tidal-custom", Name: "Tidal Custom", Service: "tidal", Protocol: "hifi", BaseURL: "https://tidal-reset.example.test", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := SaveCommunitySources([]CommunitySource{}); err != nil {
		t.Fatal(err)
	}
	rows, err := ListCommunitySources()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("registry after reset = %d rows (expected empty)", len(rows))
	}
	track := SourceTrack{ID: "track1", Title: "Track", Artist: "Artist"}
	dest := filepath.Join(t.TempDir(), "dummy.flac")
	for _, service := range []string{"tidal", "qobuz", "amazon", "deezer", "apple"} {
		if n := len(communitySourceAttempts(service, "16", dest, track)); n != 0 {
			t.Fatalf("%s attempts after reset: %d (expected 0)", service, n)
		}
	}
}
