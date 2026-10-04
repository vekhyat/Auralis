package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testHiFiPayload(id int, raw, asset, encryption string) string {
	manifest, _ := json.Marshal(map[string]any{"mimeType": "audio/flac", "encryptionType": encryption, "urls": []string{raw}})
	return fmt.Sprintf(`{"data":{"trackId":%d,"assetPresentation":%q,"manifest":%q}}`, id, asset, base64.StdEncoding.EncodeToString(manifest))
}

func TestCommunityQobuzRESTUsesKeyAndMapsQuality(t *testing.T) {
	t.Setenv("AURALIS_TEST_REST_KEY", "private-api-key")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download-url/123" || r.URL.Query().Get("quality") != "hi24" || r.Header.Get("X-API-Key") != "private-api-key" {
			t.Errorf("unexpected request path, quality, or credential")
		}
		fmt.Fprintf(w, `{"track_id":"123","bit_depth":24,"url":%q}`, server.URL+"/audio")
	}))
	defer server.Close()
	source := CommunitySource{Protocol: "qobuz-rest", BaseURL: server.URL, CredentialEnv: "AURALIS_TEST_REST_KEY", CredentialType: "api_key"}
	raw, err := resolveQobuzSource(context.Background(), source, "123", "7")
	if err != nil || raw != server.URL+"/audio" {
		t.Fatalf("%q %v", raw, err)
	}
}

func TestCommunityQobuzDLShapesAndFailedResponses(t *testing.T) {
	for _, fixture := range []struct {
		body string
		pass bool
	}{
		{`{"success":true,"data":{"url":"https://media.example/audio.flac"}}`, true},
		{`{"url":"https://media.example/audio.flac"}`, true},
		{`{"success":false,"data":{"url":"https://media.example/audio.flac"}}`, false},
		{`{"url":"file:///private/audio.flac"}`, false},
		{`{"track_id":"124","url":"https://media.example/audio.flac"}`, false},
	} {
		t.Run(fixture.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/download-music" || r.URL.Query().Get("quality") != "6" {
					t.Error("wrong Qobuz-DL route")
				}
				fmt.Fprint(w, fixture.body)
			}))
			defer server.Close()
			_, err := resolveQobuzSource(context.Background(), CommunitySource{Protocol: "qobuz-dl", BaseURL: server.URL}, "123", "6")
			if (err == nil) != fixture.pass {
				t.Fatal(err)
			}
		})
	}
}

func TestCommunityHiFiRejectsPreviewWrongTrackAndProtectedManifest(t *testing.T) {
	for _, fixture := range []struct {
		asset, encryption string
		id                int
	}{{"PREVIEW", "NONE", 123}, {"FULL", "AES", 123}, {"FULL", "NONE", 124}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, testHiFiPayload(fixture.id, "https://media.example/audio.flac", fixture.asset, fixture.encryption))
		}))
		_, err := resolveHiFiSource(context.Background(), CommunitySource{BaseURL: server.URL}, "123", "LOSSLESS")
		server.Close()
		if err == nil {
			t.Fatal("invalid playback response accepted")
		}
	}
}

func TestCommunityHiFiPollsQueueAndReturnsManifest(t *testing.T) {
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/track/":
			w.WriteHeader(202)
			fmt.Fprint(w, `{"statusUrl":"/playback/requests/job","cancelUrl":"/playback/requests/job"}`)
		case "/playback/requests/job":
			polls.Add(1)
			fmt.Fprint(w, testHiFiPayload(123, "https://media.example/audio.flac", "FULL", "NONE"))
		default:
			t.Errorf("unexpected route: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	raw, err := resolveHiFiSource(context.Background(), CommunitySource{BaseURL: server.URL}, "123", "LOSSLESS")
	if err != nil || !strings.HasPrefix(raw, "MANIFEST:") || polls.Load() != 1 {
		t.Fatalf("%q %v polls=%d", raw, err, polls.Load())
	}
}

func TestCommunityHiFiCancelsQueuedRequestWithoutLosingAuth(t *testing.T) {
	t.Setenv("AURALIS_TEST_HIFI_KEY", "queue-key")
	var deleted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if r.Header.Get("X-API-Key") != "queue-key" {
				t.Error("queue cancellation lost credential")
			}
			deleted.Store(true)
			w.WriteHeader(204)
			return
		}
		w.WriteHeader(202)
		fmt.Fprint(w, `{"statusUrl":"/playback/requests/job","cancelUrl":"/playback/requests/job"}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := resolveHiFiSource(ctx, CommunitySource{BaseURL: server.URL, CredentialEnv: "AURALIS_TEST_HIFI_KEY", CredentialType: "api_key"}, "123", "LOSSLESS")
	if err == nil || !deleted.Load() {
		t.Fatalf("err=%v deleted=%v", err, deleted.Load())
	}
	if _, err := sameSourcePath(server.URL, "https://other.example/playback/requests/job"); err == nil {
		t.Fatal("cross-origin job URL accepted")
	}
}

func TestCommunityDABMatchesRecordingBeforeResolvingItsOwnID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search":
			fmt.Fprint(w, `{"tracks":[{"id":"wrong","title":"Come Together","artist":"Cover Artist"},{"id":"dab-id","title":"Come Together","artist":"The Beatles","isrc":"GBAYE0601690"}]}`)
		case "/api/stream":
			if r.URL.Query().Get("trackId") != "dab-id" || r.URL.Query().Get("quality") != "6" {
				t.Error("DAB used another provider's track ID or wrong quality")
			}
			fmt.Fprint(w, `{"url":"https://media.example/audio.flac"}`)
		}
	}))
	defer server.Close()
	_, err := resolveDABSource(context.Background(), CommunitySource{BaseURL: server.URL}, SourceTrack{ID: "qobuz-id", Title: "Come Together", Artist: "The Beatles", ISRC: "GBAYE0601690"}, "6")
	if err != nil {
		t.Fatal(err)
	}
}

func TestCommunityLucidaParsesPageAndRunsHandoff(t *testing.T) {
	t.Setenv("AURALIS_TEST_LUCIDA_COOKIE", "session=authorized")
	token := base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString([]byte("csrf-value"))))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			if r.URL.Query().Get("country") != "US" {
				t.Error("missing Qobuz country")
			}
			fmt.Fprintf(w, `<script>,{"type":"data","data":{info:{type:"track",url:"https://open.qobuz.com/track/123"},token:%q,expiry:123},"uses":{"url":1}}];</script>`, token)
		case "/api/load":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			tokens, _ := body["token"].(map[string]any)
			if tokens["primary"] != "csrf-value" || r.Header.Get("Cookie") != "session=authorized" || r.URL.Query().Get("url") != "/api/fetch/stream/v2" {
				t.Error("invalid Lucida request")
			}
			fmt.Fprint(w, `{"handoff":"job-id","server":"local"}`)
		case "/api/fetch/request/job-id":
			fmt.Fprint(w, `{"status":"completed"}`)
		default:
			t.Error("unexpected Lucida request")
		}
	}))
	defer server.Close()
	raw, err := resolveLucidaSource(context.Background(), CommunitySource{BaseURL: server.URL, Service: "qobuz", CredentialEnv: "AURALIS_TEST_LUCIDA_COOKIE", CredentialType: "cookie"}, SourceTrack{ID: "123"}, "6")
	if err != nil || raw != server.URL+"/api/fetch/request/job-id/download" {
		t.Fatalf("%q %v", raw, err)
	}
}

func TestCommunityQobuzRouteDownloadsThroughNormalPipelineWithoutLeakingKey(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Setenv("AURALIS_TEST_PIPELINE_KEY", "server-key")
	var calls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/download-url/123" {
			if r.Header.Get("X-API-Key") != "server-key" {
				t.Error("missing key")
			}
			fmt.Fprintf(w, `{"url":%q}`, server.URL+"/audio")
			return
		}
		if r.URL.Path == "/audio" {
			if r.Header.Get("X-API-Key") != "" || r.Header.Get("Cookie") != "" {
				t.Error("server credentials leaked into media request")
			}
			w.Header().Set("Content-Type", "audio/flac")
			fmt.Fprint(w, "fLaCmock-audio")
			return
		}
		t.Error("unexpected route")
	}))
	defer server.Close()
	rows := defaultCommunitySources()
	for i := range rows {
		rows[i].Enabled = false
	}
	rows = append(rows, CommunitySource{ID: "test-rest", Name: "Test REST", Service: "qobuz", Protocol: "qobuz-rest", BaseURL: server.URL, Enabled: true, CredentialEnv: "AURALIS_TEST_PIPELINE_KEY", CredentialType: "api_key"})
	if err := SaveCommunitySources(rows); err != nil {
		t.Fatal(err)
	}
	recordSourceOutcome("test-rest", "qobuz", "16", time.Millisecond, true, nil)
	SetAudioDurationReaderForTest(func(string) (float64, error) { return 260, nil })
	defer SetAudioDurationReaderForTest(nil)
	communityCodecReader = func(string) (string, error) { return "flac", nil }
	defer func() { communityCodecReader = readCommunityCodec }()
	dest := filepath.Join(t.TempDir(), "audio.flac")
	q := &QobuzDownloader{client: &http.Client{Timeout: time.Second}}
	path, err := q.downloadRankedQobuz(123, "6", dest, 260, false)
	if err != nil || path != dest || calls.Load() != 2 {
		t.Fatalf("%q %v calls=%d", path, err, calls.Load())
	}
	body, _ := os.ReadFile(path)
	if string(body) != "fLaCmock-audio" {
		t.Fatal("media was not downloaded")
	}
}

// A server that silently transcodes must not be recorded as a lossless success.
func TestCommunityRouteRejectsTranscodedAudio(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/download-music" {
			fmt.Fprintf(w, `{"url":%q}`, "http://"+r.Host+"/audio")
			return
		}
		fmt.Fprint(w, "ID3mock-mp3")
	}))
	defer server.Close()
	if err := SaveCommunitySources([]CommunitySource{{ID: "test-dl", Name: "Test DL", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: server.URL, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	communityCodecReader = func(string) (string, error) { return "mp3", nil }
	defer func() { communityCodecReader = readCommunityCodec }()
	var attempt *sourceDownloadAttempt
	for _, candidate := range communitySourceAttempts("qobuz", "6", filepath.Join(t.TempDir(), "audio.flac"), SourceTrack{ID: "123"}) {
		if candidate.id == "test-dl" {
			attempt = &candidate
		}
	}
	if attempt == nil {
		t.Fatal("configured source was not offered")
	}
	if _, err := attempt.download(); err == nil || !strings.Contains(err.Error(), "mp3") {
		t.Fatalf("transcoded audio accepted: %v", err)
	}
	for _, candidate := range communitySourceAttempts("qobuz", "6", "", SourceTrack{ID: "123"}) {
		if candidate.id == "test-dl" {
			t.Fatal("failed source was not paused by the circuit breaker")
		}
	}
	communityCircuit.Lock()
	delete(communityCircuit.until, "test-dl")
	communityCircuit.Unlock()
}

func TestCommunitySourceSettingsValidateAndPreserveCorruption(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	source := CommunitySource{ID: "rest", Name: "REST", Service: "qobuz", Protocol: "qobuz-rest", BaseURL: "http://127.0.0.1:8000", Enabled: true}
	if err := SaveCommunitySources([]CommunitySource{source}); err != nil {
		t.Fatal(err)
	}
	rows, err := ListCommunitySources()
	if err != nil || len(rows) == 0 {
		t.Fatal(err)
	}
	source.BaseURL = "https://username:password@example.com"
	if err := SaveCommunitySources([]CommunitySource{source}); err == nil {
		t.Fatal("embedded credentials accepted")
	}
	path, _ := communitySourcesPath()
	broken := []byte("broken settings")
	_ = os.WriteFile(path, broken, 0600)
	if _, err := ListCommunitySources(); err == nil {
		t.Fatal("corruption silently ignored")
	}
	body, _ := os.ReadFile(path)
	if string(body) != string(broken) {
		t.Fatal("corrupt file was overwritten")
	}
}

func TestCommunitySubsonicMatchesAndAuthenticatesWithoutSendingPassword(t *testing.T) {
	t.Setenv("AURALIS_TEST_SUBSONIC_AUTH", "listener:private-password")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/search3" || r.URL.Query().Get("u") != "listener" || len(r.URL.Query().Get("t")) != 32 || r.URL.Query().Get("p") != "" || strings.Contains(r.URL.RawQuery, "private-password") {
			t.Error("invalid Subsonic authentication")
		}
		fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[{"id":"wrong","title":"Come Together","artist":"The Beatles","duration":30},{"id":"match","title":"Come Together","artist":"The Beatles","duration":260,"isrc":"GBAYE0601690"}]}}}`)
	}))
	defer server.Close()
	raw, err := resolveSubsonicSource(context.Background(), CommunitySource{BaseURL: server.URL, CredentialEnv: "AURALIS_TEST_SUBSONIC_AUTH", CredentialType: "subsonic"}, SourceTrack{Title: "Come Together", Artist: "The Beatles", Duration: 260, ISRC: "GBAYE0601690"}, "16")
	if err != nil || !strings.Contains(raw, "id=match") || strings.Contains(raw, "private-password") {
		t.Fatalf("Subsonic resolution failed: %v", err)
	}
}

// A browser verification page is an access problem, not a malformed payload, so
// it must surface as authentication_required rather than a JSON parse failure.
func TestCommunityHTMLInterstitialIsReportedAsAuthenticationRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!doctype html><html><head><title>Redirecting...</title></head><body>Loading</body></html>")
	}))
	defer server.Close()
	source := CommunitySource{BaseURL: server.URL}
	_, err := resolveDABSource(context.Background(), source, SourceTrack{Title: "Come Together", Artist: "The Beatles"}, "16")
	var access *communityAccessError
	if !errors.As(err, &access) {
		t.Fatalf("HTML interstitial reported as %v", err)
	}
	if _, err := resolveQobuzSource(context.Background(), source, "123", "16"); !errors.As(err, &access) {
		t.Fatalf("Qobuz HTML interstitial reported as %v", err)
	}
}

// Atmos has no representation in these protocols, so a request must fail rather
// than silently return a 16-bit file that would pass a lossy check.
func TestCommunityAdaptersRefuseAtmosInsteadOfDowngrading(t *testing.T) {
	source := CommunitySource{BaseURL: "https://example.invalid", Service: "qobuz"}
	if _, err := resolveQobuzSource(context.Background(), source, "123", "ATMOS"); err == nil {
		t.Fatal("Qobuz adapter accepted an Atmos request")
	}
	if _, err := resolveDABSource(context.Background(), source, SourceTrack{Title: "a", Artist: "b"}, "ATMOS"); err == nil {
		t.Fatal("DAB adapter accepted an Atmos request")
	}
	if _, err := resolveSubsonicSource(context.Background(), source, SourceTrack{Title: "a", Artist: "b"}, "ATMOS"); err == nil {
		t.Fatal("Subsonic adapter accepted an Atmos request")
	}
	if _, err := resolveLucidaSource(context.Background(), source, SourceTrack{ID: "1"}, "ATMOS"); err == nil {
		t.Fatal("Lucida adapter accepted an Atmos request")
	}
}

// Repointing a source at a new server must drop the old server's verification
// and timings instead of presenting them as evidence for the replacement.
func TestCommunitySourceChangeDropsStaleChecksAndPerformance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(appDataDirEnv, dir)
	source := CommunitySource{ID: "qobuz-rest", Name: "REST", Service: "qobuz", Protocol: "qobuz-rest", BaseURL: "http://127.0.0.1:8000", Enabled: true}
	if err := SaveCommunitySources([]CommunitySource{source}); err != nil {
		t.Fatal(err)
	}
	saveCommunityCheck(CommunitySourceCheck{ID: "qobuz-rest", State: "validated", AudioVerified: true})
	recordSourceOutcome("qobuz-rest", "qobuz", "16", 900*time.Millisecond, true, nil)

	if len(GetCommunitySourceChecks()) != 1 {
		t.Fatal("check was not stored")
	}
	found := false
	for _, row := range SourceBenchmarks() {
		if row.ID == "qobuz-rest" {
			found = true
		}
	}
	if !found {
		t.Fatal("measurement was not stored")
	}

	// An unrelated save must keep the existing evidence.
	unchanged := source
	unchanged.Name = "Renamed"
	if err := SaveCommunitySources([]CommunitySource{unchanged}); err != nil {
		t.Fatal(err)
	}
	if len(GetCommunitySourceChecks()) != 1 {
		t.Fatal("a cosmetic change discarded verification")
	}

	// Changing the server URL must discard it.
	changed := unchanged
	changed.BaseURL = "http://127.0.0.1:9999"
	if err := SaveCommunitySources([]CommunitySource{changed}); err != nil {
		t.Fatal(err)
	}
	if rows := GetCommunitySourceChecks(); len(rows) != 0 {
		t.Fatalf("stale check survived a server change: %+v", rows)
	}
	for _, row := range SourceBenchmarks() {
		if row.ID == "qobuz-rest" {
			t.Fatal("stale measurement survived a server change")
		}
	}
}
