package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestZarzRetryAfterHeaderSeconds(t *testing.T) {
	header := http.Header{}
	if got := zarzRetryAfterHeaderSeconds(header); got != 0 {
		t.Fatalf("empty header = %d", got)
	}
	header.Set("Retry-After", "12")
	if got := zarzRetryAfterHeaderSeconds(header); got != 12 {
		t.Fatalf("numeric header = %d", got)
	}
	header.Set("Retry-After", "-5")
	if got := zarzRetryAfterHeaderSeconds(header); got != 0 {
		t.Fatalf("negative header = %d", got)
	}
	header.Set("Retry-After", time.Now().UTC().Add(30*time.Second).Format(http.TimeFormat))
	got := zarzRetryAfterHeaderSeconds(header)
	if got < 25 || got > 30 {
		t.Fatalf("date header = %d", got)
	}
}

func TestParseZarzAPIErrorPrefersBodyRetryAfter(t *testing.T) {
	err := parseZarzAPIErrorWithRetryAfter(http.StatusTooManyRequests, []byte(`{"error":"busy","retry_after_seconds":9}`), 4)
	if !err.isRateLimited() {
		t.Fatal("expected rate limited error")
	}
	if err.RetryAfterSeconds != 9 {
		t.Fatalf("retry after = %d", err.RetryAfterSeconds)
	}
	fallback := parseZarzAPIErrorWithRetryAfter(http.StatusTooManyRequests, []byte(`{"error":"busy"}`), 7)
	if fallback.RetryAfterSeconds != 7 {
		t.Fatalf("header fallback retry after = %d", fallback.RetryAfterSeconds)
	}
}

func TestDoZarzSignedRequestCapturesRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Zarz-Signature") == "" {
			t.Error("missing signature header")
		}
		w.Header().Set("Retry-After", "6")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"server busy"}`))
	}))
	defer server.Close()

	previousBase, previousClient := zarzBaseURL, zarzHTTP
	zarzBaseURL = server.URL
	zarzHTTP = server.Client()
	defer func() {
		zarzBaseURL = previousBase
		zarzHTTP = previousClient
	}()

	record := &zarzSessionRecord{
		SessionID:     "sess-test",
		SessionSecret: "secret-test",
	}
	result, err := doZarzSignedRequest(record, "tidal-web@1.1.0", http.MethodPost, "/tickets", []byte("{}"), nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if result.Status != http.StatusTooManyRequests {
		t.Fatalf("status = %d", result.Status)
	}
	if result.RetryAfterSeconds != 6 {
		t.Fatalf("retry after = %d", result.RetryAfterSeconds)
	}
}

func TestRefreshZarzSessionLockedAppliesNewCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			InstallID string `json:"install_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.InstallID != "install-1" {
			t.Errorf("install id = %q", payload.InstallID)
		}
		_, _ = w.Write([]byte(`{"session_id":"sess-new","expires_at":"2026-09-01T00:00:00.000Z"}`))
	}))
	defer server.Close()

	previousBase := zarzBaseURL
	zarzBaseURL = server.URL
	defer func() { zarzBaseURL = previousBase }()

	record := &zarzSessionRecord{
		InstallID:     "install-1",
		SessionID:     "sess-old",
		SessionSecret: "secret-old",
		ExpiresAt:     time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339),
	}
	next, err := refreshZarzSessionLocked(record, "tidal-web@1.1.0")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if next.SessionID != "sess-new" {
		t.Fatalf("session id = %q", next.SessionID)
	}
	if next.SessionSecret != "secret-old" {
		t.Fatalf("session secret should be preserved, got %q", next.SessionSecret)
	}
	if next.ExpiresAt != "2026-09-01T00:00:00.000Z" {
		t.Fatalf("expires at = %q", next.ExpiresAt)
	}
}

func TestEnsureZarzSessionRefreshesWhenDue(t *testing.T) {
	restoreFile := snapshotZarzSessionFile(t)
	defer restoreFile()

	expiresSoon := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
	expiresLong := time.Now().UTC().Add(20 * time.Hour).Format(time.RFC3339)

	store := &zarzSessionStore{
		InstallID: "install-due",
		Sessions: map[string]zarzSessionRecord{
			"tidal-web@1.1.0": {
				InstallID:     "install-due",
				AppVersion:    "tidal-web@1.1.0",
				SessionID:     "sess-due",
				SessionSecret: "secret-due",
				ExpiresAt:     expiresSoon,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/refresh" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"session_id":"sess-fresh","session_secret":"secret-fresh","expires_at":"` + expiresLong + `"}`))
	}))
	defer server.Close()

	previousBase, previousStore := zarzBaseURL, zarzStoreMem
	zarzBaseURL = server.URL
	zarzStoreMem = store
	defer func() {
		zarzBaseURL = previousBase
		zarzStoreMem = previousStore
	}()

	record, err := ensureZarzSession("tidal-web@1.1.0")
	if err != nil {
		t.Fatalf("ensure failed: %v", err)
	}
	if record.SessionID != "sess-fresh" || record.SessionSecret != "secret-fresh" {
		t.Fatalf("refreshed credentials not applied: %+v", record)
	}
	saved := zarzStoreMem.Sessions["tidal-web@1.1.0"]
	if saved.SessionID != "sess-fresh" {
		t.Fatalf("refreshed session not persisted: %+v", saved)
	}
}

// snapshotZarzSessionFile preserves the developer's real zarz_session.json so
// tests that exercise saveZarzStore cannot corrupt live credentials.
func snapshotZarzSessionFile(t *testing.T) func() {
	t.Helper()
	path, err := zarzSessionPath()
	if err != nil {
		t.Fatalf("resolve session path: %v", err)
	}
	original, readErr := os.ReadFile(path)
	return func() {
		if readErr != nil {
			_ = os.Remove(path)
			return
		}
		_ = os.WriteFile(path, original, 0o600)
	}
}

func TestEnsureZarzSessionKeepsValidSessionWhenNotDue(t *testing.T) {
	expiresLong := time.Now().UTC().Add(20 * time.Hour).Format(time.RFC3339)
	store := &zarzSessionStore{
		InstallID: "install-ok",
		Sessions: map[string]zarzSessionRecord{
			"tidal-web@1.1.0": {
				InstallID:     "install-ok",
				AppVersion:    "tidal-web@1.1.0",
				SessionID:     "sess-ok",
				SessionSecret: "secret-ok",
				ExpiresAt:     expiresLong,
			},
		},
	}

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	previousBase, previousStore := zarzBaseURL, zarzStoreMem
	zarzBaseURL = server.URL
	zarzStoreMem = store
	defer func() {
		zarzBaseURL = previousBase
		zarzStoreMem = previousStore
	}()

	record, err := ensureZarzSession("tidal-web@1.1.0")
	if err != nil {
		t.Fatalf("ensure failed: %v", err)
	}
	if record.SessionID != "sess-ok" {
		t.Fatalf("session id changed to %q", record.SessionID)
	}
	if calls != 0 {
		t.Fatalf("refresh endpoint called %d times for a healthy session", calls)
	}
}

func TestFetchZarzAtmosManifestWrapsBodyAsBase64(t *testing.T) {
	previous := allowGatewayFetch
	allowGatewayFetch = func(raw string) error {
		if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
			return nil
		}
		return fmt.Errorf("fetch URL must be http(s)")
	}
	t.Cleanup(func() { allowGatewayFetch = previous })

	manifestXML := []byte(`<MPD xmlns="urn:mpeg:dash:schema:mpd:2011"></MPD>`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "" {
			t.Error("missing Accept header")
		}
		_, _ = w.Write(manifestXML)
	}))
	defer server.Close()

	encoded, err := fetchZarzAtmosManifest(server.URL)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
	if decodeErr != nil {
		t.Fatalf("invalid base64: %v", decodeErr)
	}
	if string(decoded) != string(manifestXML) {
		t.Fatal("roundtrip mismatch")
	}

	if _, err := fetchZarzAtmosManifest("ftp://example.invalid/manifest.mpd"); err == nil {
		t.Fatal("expected non-http URI to fail")
	}
}

func TestZarzRefreshDue(t *testing.T) {
	record := &zarzSessionRecord{ExpiresAt: time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)}
	if zarzRefreshDue(record) {
		t.Fatal("session with 2h left should not need refresh")
	}
	record.ExpiresAt = time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
	if !zarzRefreshDue(record) {
		t.Fatal("session with 30m left should need refresh")
	}
	record.ExpiresAt = ""
	if zarzRefreshDue(record) {
		t.Fatal("unknown expiry should never schedule a refresh")
	}
}
