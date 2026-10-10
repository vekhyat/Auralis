package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCommunityVerificationActionSeparatesBrowserAndConfiguration(t *testing.T) {
	t.Cleanup(resetSourceVerificationForTest)
	t.Setenv(appDataDirEnv, t.TempDir())
	rows := []CommunitySource{
		{ID: "rest-local", Name: "REST", Service: "qobuz", Protocol: "qobuz-rest", BaseURL: "http://127.0.0.1:9", Enabled: true, CredentialEnv: "AURALIS_TEST_REST_KEY", CredentialType: "api_key"},
		{ID: "sub-local", Name: "Sub", Service: "qobuz", Protocol: "subsonic", BaseURL: "http://127.0.0.1:9", Enabled: true, CredentialEnv: "AURALIS_TEST_SUBSONIC_AUTH", CredentialType: "subsonic"},
		{ID: "bearer-local", Name: "Bearer", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: "http://127.0.0.1:9", Enabled: true, CredentialEnv: "AURALIS_TEST_BEARER", CredentialType: "bearer"},
		{ID: "cookie-local", Name: "Cookie", Service: "qobuz", Protocol: "dab", BaseURL: "https://dab.example.test", Enabled: true, CredentialType: "cookie"},
	}
	if err := SaveCommunitySources(rows); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"rest-local", "sub-local", "bearer-local"} {
		action, err := CommunitySourceVerificationAction(id)
		if err != nil || action.Action != "configure" {
			t.Fatalf("%s action %+v %v", id, action, err)
		}
		opened := false
		sourceVerificationHook.open = func(string) <-chan error {
			opened = true
			return closedVerificationError(nil)
		}
		result, err := VerifyCommunitySource(id)
		sourceVerificationHook = sourceVerificationHooks{}
		if err != nil || opened || result.State != "configuration_required" || result.AudioVerified || result.Action != "configure" {
			t.Fatalf("%s verify %+v opened=%v err=%v", id, result, opened, err)
		}
	}
	action, err := CommunitySourceVerificationAction("cookie-local")
	if err != nil || action.Action != "verify" {
		t.Fatalf("cookie action %+v %v", action, err)
	}
	hifi, err := CommunitySourceVerificationAction("hifi-monochrome-api.samidy.com")
	if err != nil || hifi.Action != "verify" {
		t.Fatalf("hifi action %+v %v", hifi, err)
	}
}

func TestManualVerificationDoesNotCloseOrStoreBeforeAPIAccepts(t *testing.T) {
	if !browserSessionPersistenceAvailable() {
		t.Skip("this lifecycle test persists a Windows DPAPI session")
	}
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Cleanup(resetSourceVerificationForTest)
	secret := "secret-cookie-value"
	var accept atomic.Bool
	var closed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !accept.Load() {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<!doctype html><html><title>Just a moment</title><body>cf-challenge</body></html>")
			return
		}
		if !strings.Contains(r.Header.Get("Cookie"), secret) {
			t.Errorf("API request missing browser cookie")
		}
		switch r.URL.Path {
		case "/api/search":
			fmt.Fprint(w, `{"tracks":[{"id":"1","title":"Come Together","artist":"The Beatles","isrc":"GBAYE0601690"}]}`)
		case "/api/stream":
			fmt.Fprint(w, `{"url":"https://media.example/audio.flac"}`)
		default:
			fmt.Fprint(w, "ok")
		}
	}))
	defer server.Close()
	source := CommunitySource{ID: "dab-manual", Name: "Manual", Service: "qobuz", Protocol: "dab", BaseURL: server.URL, Enabled: true, CredentialType: "cookie"}
	if err := SaveCommunitySources([]CommunitySource{source}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	sourceVerificationHook.open = func(target string) <-chan error {
		if !strings.Contains(target, strings.TrimPrefix(server.URL, "http://")) {
			t.Errorf("opened unexpected target %s", target)
		}
		close(started)
		return make(chan error)
	}
	sourceVerificationHook.close = func() { closed.Add(1) }
	sourceVerificationHook.capture = func(origin string) ([]storedCookie, error) {
		if !strings.Contains(origin, strings.TrimPrefix(server.URL, "http://")) {
			t.Errorf("captured %s", origin)
		}
		return []storedCookie{{Name: "session", Value: secret, Domain: "127.0.0.1", Path: "/", HostOnly: true}}, nil
	}
	done := make(chan CommunitySourceCheck, 1)
	go func() {
		result, err := VerifyCommunitySource(source.ID)
		if err != nil {
			t.Errorf("verify: %v", err)
		}
		done <- result
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("verification window was not requested")
	}
	waitForVerificationConfirmation(t)
	accepted, err := ConfirmSourceVerification()
	if accepted || err == nil || closed.Load() != 0 || strings.Contains(err.Error(), secret) {
		t.Fatalf("early confirm accepted=%v closed=%d err=%v", accepted, closed.Load(), err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "cookie") && strings.Contains(err.Error(), "session=") {
		t.Fatalf("confirm error exposed a cookie: %v", err)
	}
	if !SourceVerificationNeedsConfirmation() {
		t.Fatal("rejected API closed the manual session")
	}
	for _, row := range GetCommunitySourceChecks() {
		if row.ID == source.ID && (row.State == "available" || row.AudioVerified) {
			t.Fatalf("stored check was verified early: %+v", row)
		}
	}
	body := readSessionFile(t)
	if strings.Contains(string(body), secret) {
		t.Fatal("cookie was stored before the API accepted it")
	}

	accept.Store(true)
	accepted, err = ConfirmSourceVerification()
	if !accepted || err != nil {
		t.Fatalf("confirm after API success: %v %v", accepted, err)
	}
	if closed.Load() == 0 {
		t.Fatal("verification window stayed open after the API accepted the session")
	}
	result := <-done
	if result.State != "available" || result.AudioVerified || strings.Contains(result.Message, secret) {
		t.Fatalf("result %+v", result)
	}
	if SourceVerificationNeedsConfirmation() {
		t.Fatal("confirmation still required after success")
	}
	again, againErr := ConfirmSourceVerification()
	if again || againErr != nil {
		t.Fatalf("idle confirm = %v %v", again, againErr)
	}
	body = readSessionFile(t)
	if len(body) == 0 || strings.Contains(string(body), secret) {
		t.Fatal("browser session was missing or stored in plaintext")
	}
}

func TestManualVerificationCancelDoesNotMarkVerified(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Cleanup(resetSourceVerificationForTest)
	var closed atomic.Int32
	release := make(chan error, 1)
	sourceVerificationHook.open = func(string) <-chan error { return release }
	sourceVerificationHook.close = func() {
		closed.Add(1)
		select {
		case release <- errVerificationClosed:
		default:
		}
	}
	done := make(chan CommunitySourceCheck, 1)
	go func() {
		result, err := VerifyCommunitySource("lucida-qobuz")
		if err != nil {
			t.Errorf("verify: %v", err)
		}
		done <- result
	}()
	waitForVerificationConfirmation(t)
	select {
	case release <- errVerificationClosed:
	default:
		t.Fatal("verification window did not accept cancel")
	}
	select {
	case result := <-done:
		if result.State != "cancelled" || result.AudioVerified || strings.Contains(result.Message, "API responded") {
			t.Fatalf("cancel result %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not finish verification")
	}
	if closed.Load() == 0 {
		t.Fatal("cancel left the verification window open")
	}
	accepted, err := ConfirmSourceVerification()
	if accepted || err != nil {
		t.Fatalf("confirm after cancel = %v %v", accepted, err)
	}
	for _, row := range GetCommunitySourceChecks() {
		if row.ID == "lucida-qobuz" && (row.AudioVerified || row.State == "available" || row.State == "validated") {
			t.Fatalf("cancel stored a verified check: %+v", row)
		}
	}
}

func TestManualVerificationFollowsDownloadContextCancel(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Cleanup(resetSourceVerificationForTest)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var closed atomic.Int32
	sourceVerificationHook.context = func() context.Context { return ctx }
	sourceVerificationHook.open = func(string) <-chan error { return make(chan error) }
	sourceVerificationHook.close = func() { closed.Add(1) }
	done := make(chan CommunitySourceCheck, 1)
	go func() {
		result, err := VerifyCommunitySource("lucida-qobuz")
		if err != nil {
			t.Errorf("verify: %v", err)
		}
		done <- result
	}()
	waitForVerificationConfirmation(t)
	cancel()
	select {
	case result := <-done:
		if result.State != "cancelled" || result.AudioVerified {
			t.Fatalf("context cancel result %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("download context did not cancel verification")
	}
	if closed.Load() == 0 {
		t.Fatal("context cancel left the verification window open")
	}
}

func TestManualVerificationTimeoutDoesNotMarkVerified(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Cleanup(resetSourceVerificationForTest)
	var closed atomic.Int32
	sourceVerificationHook.timeout = 200 * time.Millisecond
	sourceVerificationHook.open = func(string) <-chan error { return make(chan error) }
	sourceVerificationHook.close = func() { closed.Add(1) }
	result, err := VerifyCommunitySource("lucida-qobuz")
	if err != nil || result.State == "available" || result.AudioVerified || closed.Load() == 0 {
		t.Fatalf("timeout result %+v closed=%d err=%v", result, closed.Load(), err)
	}
}

func TestCommunitySourceChangeKeepsUnrelatedEvidence(t *testing.T) {
	if !browserSessionPersistenceAvailable() {
		t.Skip("this test persists Windows DPAPI sessions")
	}
	dir := t.TempDir()
	t.Setenv(appDataDirEnv, dir)
	t.Cleanup(func() {
		communityCircuit.Lock()
		delete(communityCircuit.until, "hifi-monochrome-api.samidy.com")
		delete(communityCircuit.until, "keep-custom")
		delete(communityCircuit.until, "change-custom")
		communityCircuit.Unlock()
		resetSourceVerificationForTest()
	})
	keep := CommunitySource{ID: "keep-custom", Name: "Keep", Service: "qobuz", Protocol: "dab", BaseURL: "https://keep.example.test", Enabled: true, CredentialType: "cookie"}
	changed := CommunitySource{ID: "change-custom", Name: "Change", Service: "qobuz", Protocol: "dab", BaseURL: "https://old.example.test", Enabled: true, CredentialType: "cookie"}
	if err := SaveCommunitySources([]CommunitySource{keep, changed}); err != nil {
		t.Fatal(err)
	}
	secretKeep := "keep-cookie-secret"
	secretChange := "change-cookie-secret"
	if err := saveCommunityBrowserSession(keep, []storedCookie{{Name: "session", Value: secretKeep, Domain: "keep.example.test", Path: "/", HostOnly: true, Secure: true}}); err != nil {
		t.Fatal(err)
	}
	if err := saveCommunityBrowserSession(changed, []storedCookie{{Name: "session", Value: secretChange, Domain: "old.example.test", Path: "/", HostOnly: true, Secure: true}}); err != nil {
		t.Fatal(err)
	}
	saveCommunityCheck(CommunitySourceCheck{ID: keep.ID, State: "available"})
	saveCommunityCheck(CommunitySourceCheck{ID: changed.ID, State: "validated", AudioVerified: true})
	saveCommunityCheck(CommunitySourceCheck{ID: "hifi-monochrome-api.samidy.com", State: "available"})
	recordSourceOutcome(keep.ID, "qobuz", "16", time.Millisecond, true, nil)
	recordSourceOutcome(changed.ID, "qobuz", "16", time.Millisecond, true, nil)
	recordSourceOutcome("hifi-monochrome-api.samidy.com", "tidal", "16", time.Millisecond, true, nil)
	pause := time.Now().Add(time.Hour)
	communityCircuit.Lock()
	communityCircuit.until[keep.ID] = pause
	communityCircuit.until[changed.ID] = pause
	communityCircuit.until["hifi-monochrome-api.samidy.com"] = pause
	communityCircuit.Unlock()

	changed.BaseURL = "https://new.example.test"
	keep.Name = "Renamed"
	if err := SaveCommunitySources([]CommunitySource{keep, changed}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]CommunitySourceCheck{}
	for _, row := range GetCommunitySourceChecks() {
		ids[row.ID] = row
	}
	if _, ok := ids[keep.ID]; !ok {
		t.Fatal("unrelated source check was cleared")
	}
	if _, ok := ids["hifi-monochrome-api.samidy.com"]; !ok {
		t.Fatal("untouched built-in check was cleared")
	}
	if _, ok := ids[changed.ID]; ok {
		t.Fatal("changed source kept a stale check")
	}
	benchmarks := map[string]bool{}
	for _, row := range SourceBenchmarks() {
		benchmarks[row.ID] = true
	}
	if !benchmarks[keep.ID] || !benchmarks["hifi-monochrome-api.samidy.com"] || benchmarks[changed.ID] {
		t.Fatalf("benchmarks = %+v", benchmarks)
	}
	communityCircuit.Lock()
	_, keepPaused := communityCircuit.until[keep.ID]
	_, hifiPaused := communityCircuit.until["hifi-monochrome-api.samidy.com"]
	_, changedPaused := communityCircuit.until[changed.ID]
	communityCircuit.Unlock()
	if !keepPaused || !hifiPaused || changedPaused {
		t.Fatalf("circuits keep=%v hifi=%v changed=%v", keepPaused, hifiPaused, changedPaused)
	}
	kept, err := browserCookiesForSource(keep)
	if err != nil || len(kept) != 1 || kept[0].Value != secretKeep {
		t.Fatalf("unrelated session = %+v %v", kept, err)
	}
	dropped, err := browserCookiesForSource(changed)
	if err != nil || len(dropped) != 0 {
		t.Fatalf("changed session survived: %+v %v", dropped, err)
	}
	body := readSessionFile(t)
	if strings.Contains(string(body), secretKeep) || strings.Contains(string(body), secretChange) {
		t.Fatal("session file stored cookie values in plaintext")
	}
}

func TestBrowserSessionCookiesStayOnTheirOrigin(t *testing.T) {
	if !browserSessionPersistenceAvailable() {
		t.Skip("this test persists Windows DPAPI sessions")
	}
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Cleanup(resetSourceVerificationForTest)
	wide := storedCookie{Name: "wide", Value: "wide-secret-value", Domain: ".lucida.to", Path: "/", Secure: true}
	hostOnly := storedCookie{Name: "host", Value: "host-secret-value", Domain: "lucida.to", Path: "/", HostOnly: true, Secure: true}
	other := storedCookie{Name: "other", Value: "other-secret-value", Domain: "other-source.example", Path: "/", HostOnly: true, Secure: true}
	source := CommunitySource{ID: "lucida-qobuz", Name: "Lucida", Service: "qobuz", Protocol: "lucida", BaseURL: "https://lucida.to", Enabled: true, CredentialEnv: "AURALIS_TEST_LUCIDA_COOKIE", CredentialType: "cookie"}
	if err := saveCommunityBrowserSession(source, []storedCookie{wide, hostOnly, other}); err != nil {
		t.Fatal(err)
	}
	worker, err := lucidaWorkerSource(source, "eu")
	if err != nil || worker.CredentialEnv != "" || worker.CredentialType != "" || worker.BaseURL != "https://eu.lucida.to" || worker.ID != source.ID {
		t.Fatalf("worker %+v %v", worker, err)
	}
	header, err := cookieHeaderForURL([]storedCookie{wide, hostOnly, other}, worker.BaseURL+"/api/fetch/request/job")
	if err != nil || strings.Contains(header, "host-secret-value") || strings.Contains(header, "other-secret-value") || !strings.Contains(header, "wide-secret-value") {
		t.Fatalf("worker header %q %v", header, err)
	}
	t.Setenv("AURALIS_TEST_LUCIDA_COOKIE", "env-secret-value")
	envHeader, err := communityCookieHeader(source, source.BaseURL+"/api/load")
	if err != nil || envHeader != "env-secret-value" {
		t.Fatalf("env override %q %v", envHeader, err)
	}
	leaked, err := communityCookieHeader(source, "https://cdn.example/audio")
	if err != nil || leaked != "" {
		t.Fatalf("env cookie left its origin: %q %v", leaked, err)
	}
	jarOnly := source
	jarOnly.ID = "lucida-jar"
	jarOnly.CredentialEnv = ""
	if err := saveCommunityBrowserSession(jarOnly, []storedCookie{wide, hostOnly, other}); err != nil {
		t.Fatal(err)
	}
	jarHeader, err := communityCookieHeader(jarOnly, "https://lucida.to/api/load")
	if err != nil || !strings.Contains(jarHeader, "wide-secret-value") || !strings.Contains(jarHeader, "host-secret-value") || strings.Contains(jarHeader, "other-secret-value") {
		t.Fatalf("jar header %q %v", jarHeader, err)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://cdn.example/audio", nil)
	keyed := CommunitySource{BaseURL: "https://api.example.test", CredentialEnv: "AURALIS_TEST_PIPELINE_KEY", CredentialType: "api_key"}
	t.Setenv("AURALIS_TEST_PIPELINE_KEY", "server-key-secret")
	if err := applyCommunityRequestCredentials(req, keyed); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-API-Key") != "" || req.Header.Get("Cookie") != "" {
		t.Fatal("API key was attached to another origin")
	}
	same, _ := http.NewRequest(http.MethodGet, keyed.BaseURL+"/download", nil)
	if err := applyCommunityRequestCredentials(same, keyed); err != nil || same.Header.Get("X-API-Key") != "server-key-secret" {
		t.Fatal(err)
	}
	message := redactCommunityMessage("failed "+wide.Value+" at https://lucida.to/private?sig=1", wide.Value)
	if strings.Contains(message, wide.Value) || strings.Contains(message, "https://") {
		t.Fatal(message)
	}
}

func TestCommunityMediaRedirectDropsCookiesOffOrigin(t *testing.T) {
	if !browserSessionPersistenceAvailable() {
		t.Skip("this test persists a Windows DPAPI session")
	}
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Cleanup(resetSourceVerificationForTest)
	secret := "media-secret-value"
	var mediaCookie string
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "audio/flac")
		fmt.Fprint(w, "fLaC")
	}))
	defer media.Close()
	var originCookie string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originCookie = r.Header.Get("Cookie")
		port := media.Listener.Addr().(*net.TCPAddr).Port
		http.Redirect(w, r, fmt.Sprintf("http://localhost:%d/audio", port), http.StatusFound)
	}))
	defer origin.Close()
	source := CommunitySource{ID: "media-cookie", Name: "Media", Service: "qobuz", Protocol: "dab", BaseURL: origin.URL, CredentialType: "cookie"}
	if err := saveCommunityBrowserSession(source, []storedCookie{{Name: "session", Value: secret, Domain: "127.0.0.1", Path: "/", HostOnly: true}}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "audio.flac")
	if _, err := downloadCommunityMedia(source, origin.URL+"/audio", dest); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(originCookie, secret) || mediaCookie != "" {
		t.Fatalf("origin %q media %q", originCookie, mediaCookie)
	}
}

func TestHiFiAndLucidaChallengePagesAreAuthentication(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	challenge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!doctype html><html><title>Just a moment...</title><body>cf-challenge</body></html>")
	}))
	defer challenge.Close()
	source := CommunitySource{BaseURL: challenge.URL, Service: "qobuz", Protocol: "lucida"}
	var access *communityAccessError
	if _, err := resolveHiFiSource(context.Background(), CommunitySource{BaseURL: challenge.URL}, "123", "LOSSLESS"); !errors.As(err, &access) {
		t.Fatalf("hifi challenge: %v", err)
	}
	if _, err := resolveLucidaSource(context.Background(), source, SourceTrack{ID: "123"}, "6"); !errors.As(err, &access) {
		t.Fatalf("lucida challenge: %v", err)
	}
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body>not a source payload</body></html>")
	}))
	defer plain.Close()
	if _, err := resolveHiFiSource(context.Background(), CommunitySource{BaseURL: plain.URL}, "123", "LOSSLESS"); errors.As(err, &access) {
		t.Fatal("ordinary Hi-Fi HTML was treated as authentication")
	}
	if _, err := resolveLucidaSource(context.Background(), CommunitySource{BaseURL: plain.URL, Service: "qobuz", Protocol: "lucida"}, SourceTrack{ID: "123"}, "6"); errors.As(err, &access) {
		t.Fatal("ordinary Lucida HTML was treated as authentication")
	}
}

func TestVerificationTargetURLIsExactConfiguredOrigin(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	if err := validateVerificationChallengeURL("https://api.zarz.moe/challenge"); err != nil {
		t.Fatal(err)
	}
	if err := validateVerificationChallengeURL("https://lucida.to/"); err == nil {
		t.Fatal("legacy allowlist accepted a community origin")
	}
	if err := validateVerificationTargetURL("https://lucida.to/login"); err != nil {
		t.Fatal(err)
	}
	if err := validateVerificationTargetURL("https://api.zarz.moe/challenge"); err != nil {
		t.Fatal(err)
	}
	for _, blocked := range []string{
		"https://www.lucida.to/",
		"https://evil.lucida.to/",
		"https://lucida.to.evil.test/",
		"https://user:secret@lucida.to/",
		"http://lucida.to/",
		"javascript:alert(1)",
	} {
		if err := validateVerificationTargetURL(blocked); err == nil {
			t.Fatalf("accepted %s", blocked)
		}
	}
}

func TestCaptureRequestsOnlyTheSelectedOrigin(t *testing.T) {
	profile := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(filepath.Join(profile, "DevToolsActivePort"), []byte(strconv.Itoa(port)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got strings.Builder
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{"type":"page","url":"https://lucida.to/","webSocketDebuggerUrl":"ws://127.0.0.1:%d/devtools/page/verification"}]`, port)
	})
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://127.0.0.1:%d/devtools/browser"}`, port)
	})
	mux.HandleFunc("/devtools/page/verification", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			mu.Lock()
			got.Write(msg)
			mu.Unlock()
			var req struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			_ = json.Unmarshal(msg, &req)
			result := map[string]any{}
			if req.Method == "Network.getCookies" {
				result["cookies"] = []map[string]any{
					{"name": "session", "value": "origin-secret-value", "domain": "lucida.to", "path": "/", "expires": -1, "secure": true, "httpOnly": true},
					{"name": "other", "value": "foreign-secret-value", "domain": "evil.example", "path": "/", "expires": -1, "secure": true},
				}
			}
			_ = conn.WriteJSON(map[string]any{"id": req.ID, "result": result})
			if req.Method == "Network.getCookies" {
				return
			}
		}
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	cookies, err := captureCommunityCookiesFromProfile(profile, "https://lucida.to/")
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	request := got.String()
	mu.Unlock()
	if !strings.Contains(request, "Network.getCookies") || strings.Contains(request, "getAllCookies") || !strings.Contains(request, "https://lucida.to/") {
		t.Fatalf("cookie request was not limited to the origin: %s", request)
	}
	if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "origin-secret-value" {
		t.Fatalf("cookies %+v", cookies)
	}
	foreign := filepath.Join(t.TempDir(), "Google", "Chrome", "User Data", "Default")
	if _, err := captureCommunityCookiesFromProfile(foreign, "https://lucida.to/"); err == nil {
		t.Fatal("foreign browser profile was read")
	}
}

func TestPlaintextBrowserSessionIsRejected(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Cleanup(resetSourceVerificationForTest)
	path, err := browserSessionPath()
	if err != nil {
		t.Fatal(err)
	}
	secret := "plaintext-cookie-secret"
	if err := os.WriteFile(path, []byte(`{"sources":{"dab-xyz":{"cookies":[{"value":"`+secret+`"}]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	resetSourceVerificationForTest()
	t.Setenv(appDataDirEnv, filepath.Dir(path))
	_, err = loadBrowserSessions()
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("plaintext session error = %v", err)
	}
}

func waitForVerificationConfirmation(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if SourceVerificationNeedsConfirmation() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("community verification did not become confirmable")
}

func closedVerificationError(err error) <-chan error {
	ch := make(chan error, 1)
	ch <- err
	return ch
}

func readSessionFile(t *testing.T) []byte {
	t.Helper()
	path, err := browserSessionPath()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return body
}
