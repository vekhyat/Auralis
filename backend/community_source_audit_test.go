package backend

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Audio outcomes must replace the earlier API-only available status, and
// outer errors must become redacted failed evidence instead of disappearing.
func TestCommunityAuditAudioOutcomeReplacesAvailableStatus(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	initial := []CommunitySourceCheck{
		{ID: "audit-good", State: "available", Message: "API responded; audio download has not been verified"},
		{ID: "audit-bad", State: "available", Message: "API responded; audio download has not been verified"},
		{ID: "audit-broken", State: "available", Message: "API responded; audio download has not been verified"},
		{ID: "audit-untouched", State: "failed", Message: "API failed"},
		{ID: "audit-unconfigured", State: "not_configured", Message: "Requires a configured server"},
	}
	calls := map[string]int{}
	stub := func(id string, download bool) (CommunitySourceCheck, error) {
		calls[id]++
		if !download {
			t.Fatalf("audio refresh must use full validation (download=true), got %s download=%v", id, download)
		}
		switch id {
		case "audit-good":
			return CommunitySourceCheck{ID: id, State: "validated", AudioVerified: true, Message: "Full-track audio and decoding passed"}, nil
		case "audit-bad":
			return CommunitySourceCheck{ID: id, State: "failed", Message: "full-track audio decoding failed"}, nil
		case "audit-broken":
			return CommunitySourceCheck{}, errors.New("boom at https://media.example/audio.flac?sig=abc")
		default:
			t.Fatalf("audio refresh called for non-available source %s", id)
			return CommunitySourceCheck{}, nil
		}
	}

	merged := refreshAvailableWithAudio(initial, stub)
	if len(merged) != len(initial) {
		t.Fatalf("merged %d rows, want %d (evidence must be replaced, not dropped or duplicated)", len(merged), len(initial))
	}
	for id, want := range map[string]int{"audit-good": 1, "audit-bad": 1, "audit-broken": 1} {
		if calls[id] != want {
			t.Fatalf("audio check calls for %s = %d, want %d", id, calls[id], want)
		}
	}
	if calls["audit-untouched"] != 0 || calls["audit-unconfigured"] != 0 {
		t.Fatalf("non-available sources must not be rechecked: %v", calls)
	}

	byID := map[string]CommunitySourceCheck{}
	for _, row := range merged {
		if _, dup := byID[row.ID]; dup {
			t.Fatalf("duplicate row for %s", row.ID)
		}
		byID[row.ID] = row
	}
	if got := byID["audit-good"]; got.State != "validated" || !got.AudioVerified {
		t.Fatalf("success did not replace available: %+v", got)
	}
	if got := byID["audit-bad"]; got.State != "failed" || got.AudioVerified || !strings.Contains(got.Message, "decoding failed") {
		t.Fatalf("audio failure did not replace available: %+v", got)
	}
	broken := byID["audit-broken"]
	if broken.State != "failed" || broken.ID != "audit-broken" || strings.TrimSpace(broken.CheckedAt) == "" {
		t.Fatalf("outer error lost or missing evidence: %+v", broken)
	}
	if strings.Contains(broken.Message, "https://media.example/") {
		t.Fatalf("outer error URL leaked: %q", broken.Message)
	}
	if !strings.Contains(broken.Message, "<redacted-url>") {
		t.Fatalf("outer error URL not redacted: %q", broken.Message)
	}
	if kept := byID["audit-untouched"]; kept.State != "failed" || kept.Message != "API failed" {
		t.Fatalf("non-available row changed: %+v", kept)
	}
	if kept := byID["audit-unconfigured"]; kept.State != "not_configured" {
		t.Fatalf("not_configured row changed: %+v", kept)
	}
	for _, row := range merged {
		if row.ID == "audit-good" || row.ID == "audit-bad" || row.ID == "audit-broken" {
			if row.State == "available" {
				t.Fatalf("stale API-only status survived audio validation: %+v", row)
			}
		}
	}
}

// Outer failures must redact environment secrets and signed media URLs.
func TestCommunityAuditOuterFailureRedactsSecretsAndURLs(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	const fixtureID = "audit-dab-fixture"
	if err := SaveCommunitySources([]CommunitySource{{ID: fixtureID, Name: "Audit DAB Fixture", Service: "qobuz", Protocol: "dab", BaseURL: "https://audit-dab.example.test", Enabled: true, CredentialEnv: "AURALIS_DAB_COOKIE", CredentialType: "cookie"}}); err != nil {
		t.Fatal(err)
	}
	const secret = "super-secret-cookie-value-123"
	t.Setenv("AURALIS_DAB_COOKIE", secret)
	raw := "request failed for " + secret + " at https://audit-dab.example.test/stream?sig=abc and https://signed.media/audio.flac?token=xyz"
	row := auditOuterFailureForID(fixtureID, errors.New(raw))
	if row.ID != fixtureID || row.State != "failed" {
		t.Fatalf("outer failure = %+v", row)
	}
	if strings.Contains(row.Message, secret) {
		t.Fatalf("secret leaked in audit message: %q", row.Message)
	}
	if strings.Contains(row.Message, "https://") {
		t.Fatalf("URL leaked in audit message: %q", row.Message)
	}
	if !strings.Contains(row.Message, "<redacted-url>") || !strings.Contains(row.Message, "<redacted>") {
		t.Fatalf("redaction missing: %q", row.Message)
	}
	if strings.TrimSpace(row.CheckedAt) == "" {
		t.Fatal("outer failure missing timestamp")
	}
}

// The persisted report must carry the final audio result, not the earlier
// API-only status, in deterministic sorted order.
func TestCommunityAuditReportPersistsFinalAudioStatus(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	root := t.TempDir()
	initial := []CommunitySourceCheck{
		{ID: "audit-zed", State: "available", Message: "API responded; audio download has not been verified"},
		{ID: "audit-alpha", State: "available", Message: "API responded; audio download has not been verified"},
		{ID: "audit-stale", State: "failed", Message: "API failed"},
	}
	stub := func(id string, download bool) (CommunitySourceCheck, error) {
		if id == "audit-zed" {
			return CommunitySourceCheck{ID: id, State: "validated", AudioVerified: true, Message: "Full-track audio and decoding passed"}, nil
		}
		return CommunitySourceCheck{ID: id, State: "failed", Message: "full-track audio decoding failed"}, nil
	}
	merged := refreshAvailableWithAudio(initial, stub)
	if err := writeCommunityAdaptersReport(root, merged); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "community-adapters.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted []CommunitySourceCheck
	if err := json.Unmarshal(body, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 3 {
		t.Fatalf("persisted %d rows: %s", len(persisted), body)
	}
	// Deterministic order regardless of probe completion order.
	for i := 1; i < len(persisted); i++ {
		if persisted[i-1].ID >= persisted[i].ID {
			t.Fatalf("report not sorted: %s", body)
		}
	}
	for _, row := range persisted {
		if row.State == "available" {
			t.Fatalf("API-only status persisted instead of audio result: %s", body)
		}
	}
	byID := map[string]CommunitySourceCheck{}
	for _, row := range persisted {
		byID[row.ID] = row
	}
	if byID["audit-zed"].State != "validated" || !byID["audit-zed"].AudioVerified {
		t.Fatalf("validated result missing: %s", body)
	}
	if byID["audit-alpha"].State != "failed" {
		t.Fatalf("audio failure missing: %s", body)
	}
	if string(body) == "" || !strings.Contains(string(body), "audit-zed") {
		t.Fatalf("report incomplete: %s", body)
	}
}
