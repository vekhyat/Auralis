package backend

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAntraConnectionRequiresMatchingRecording(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	for _, tc := range []struct {
		body      string
		available bool
	}{
		{`{"track_id":"123","isrc":"WRONG"}`, false},
		{`{"track_id":"123"}`, false},
		{`{"track_id":"123","isrc":"GBAYE0601690"}`, true},
		{`{"track_id":"123","title":"Come Together","artist":"The Beatles"}`, true},
	} {
		t.Run(tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			manifest := antraEndpointManifest{}
			manifest.Mirrors.Qobuz = server.URL
			defer seedAntraManifest(manifest)()
			row, err := CheckSourceConnection("antra-qobuz")
			if err != nil || (row.State == "available") != tc.available {
				t.Fatalf("wrong connection evidence: state=%s err=%v", row.State, err)
			}
		})
	}
}

func TestResourceChecksRejectUnrelatedAndErrorPayloads(t *testing.T) {
	for _, test := range []struct {
		id, body string
		want     bool
	}{
		{"resource-deezer", `{"id":116348128}`, true},
		{"resource-deezer", `{"id":1}`, false},
		{"resource-tidal", `{"items":[]}`, false},
		{"resource-lrclib", `<html>verify</html>`, false},
		{"resource-tidal", `{"items":[{}]}`, false},
		{"resource-tidal", `{"items":[{"id":123,"isrc":"WRONG"}]}`, false},
		{"resource-tidal", `{"items":[{"id":55130631,"isrc":"GBAYE0601690"}]}`, true},
		{"resource-tidal", `{"items":[{"title":"Come Together","artist":{"name":"The Beatles"}}]}`, true},
		{"resource-musicbrainz", `{"recordings":[{}]}`, false},
		{"resource-musicbrainz", `{"recordings":[{"title":"Another Song","artist-credit":[{"name":"The Beatles"}]}]}`, false},
		{"resource-musicbrainz", `{"recordings":[{"title":"Come Together","artist-credit":[{"name":"The Beatles"}]}]}`, true},
		{"resource-musicbrainz", `{"recordings":[{"isrcs":["GBAYE0601690"]}]}`, true},
		{"resource-lrclib", `[{"trackName":"Another Song","artistName":"The Beatles"}]`, false},
		{"resource-lrclib", `[{"trackName":"Come Together","artistName":"Someone Else"}]`, false},
		{"resource-lrclib", `[{"trackName":"Come Together (Remastered 2009)","artistName":"The Beatles"}]`, true},
	} {
		if got := validateResourceResponse(test.id, []byte(test.body)); got != test.want {
			t.Errorf("%s: got %v want %v", test.id, got, test.want)
		}
	}
}

// Configured community sources carry user-chosen IDs, so they must never
// inherit a built-in's check, session, or verification behaviour, whatever
// their ID looks like. Even with live community/Zarz sessions saved, tricky
// IDs (community-/zarz- prefixes, the retired jiosaavn-community route, an
// antra- prefix, and a built-in collision) are never verifiable, fail
// connection lookup before any browser or network work, and never shadow a
// built-in row. Synthetic hosts only; none is a retired public preset.
func TestConfiguredSourcesNeverInheritBuiltInConnectionBehaviour(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	communitySessionMu.Lock()
	previousCommunity := communitySessionMem
	communitySessionMem = nil
	communitySessionMu.Unlock()
	zarzSessionMu.Lock()
	previousZarz := zarzStoreMem
	zarzStoreMem = nil
	zarzSessionMu.Unlock()
	sourceConnectionChecks.Lock()
	previousChecks := sourceConnectionChecks.rows
	sourceConnectionChecks.rows = make(map[string]SourceConnection)
	sourceConnectionChecks.Unlock()
	t.Cleanup(func() {
		communitySessionMu.Lock()
		communitySessionMem = previousCommunity
		communitySessionMu.Unlock()
		zarzSessionMu.Lock()
		zarzStoreMem = previousZarz
		zarzSessionMu.Unlock()
		sourceConnectionChecks.Lock()
		sourceConnectionChecks.rows = previousChecks
		sourceConnectionChecks.Unlock()
	})
	configured := []CommunitySource{
		{ID: "community-fixture-tidal", Name: "Community Fixture", Service: "tidal", Protocol: "hifi", BaseURL: "https://community-fixture.example.test", Enabled: true},
		{ID: "zarz-fixture-tidal", Name: "Zarz Fixture", Service: "tidal", Protocol: "hifi", BaseURL: "https://zarz-fixture.example.test", Enabled: true},
		{ID: "jiosaavn-community", Name: "Custom Jio", Service: "tidal", Protocol: "hifi", BaseURL: "https://jio-custom.example.test", Enabled: true},
		{ID: "antra-fixture", Name: "Custom Antra", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: "https://antra-custom.example.test", Enabled: true},
		{ID: "antra-qobuz", Name: "Collision Qobuz", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: "https://collision-custom.example.test", Enabled: true},
	}
	if err := SaveCommunitySources(configured); err != nil {
		t.Fatal(err)
	}
	// Live sessions must not leak into connection state for custom IDs.
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	if err := saveCommunitySession(&communitySessionRecord{InstallID: "test-install", SessionID: "test-session", SessionSecret: "test-secret", ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	if err := saveZarzStore(&zarzSessionStore{InstallID: "test-install", Sessions: map[string]zarzSessionRecord{zarzAppVersionForProvider("tidal"): {SessionID: "test-session", SessionSecret: "test-secret", ExpiresAt: expires}}}); err != nil {
		t.Fatal(err)
	}

	// The colliding configured row must not shadow or duplicate the built-in.
	count := 0
	for _, source := range DownloadSources() {
		if source.ID == "antra-qobuz" {
			count++
			if source.Service != "qobuz" || source.Name != "Antra / qobuz" || source.URL != "" {
				t.Fatalf("built-in antra-qobuz shadowed by custom row: %+v", source)
			}
		}
		if strings.Contains(source.URL, "collision-custom.example.test") {
			t.Fatalf("colliding custom row leaked into DownloadSources: %+v", source)
		}
	}
	if count != 1 {
		t.Fatalf("antra-qobuz appears %d times in DownloadSources (want exactly once)", count)
	}

	listed := map[string]SourceConnection{}
	for _, row := range SourceConnections() {
		if _, dup := listed[row.ID]; dup {
			t.Fatalf("duplicate connection row %s", row.ID)
		}
		listed[row.ID] = row
	}
	for _, id := range []string{"community-fixture-tidal", "zarz-fixture-tidal", "jiosaavn-community", "antra-fixture"} {
		if row, ok := listed[id]; ok {
			t.Fatalf("configured source %s listed as connection: %+v", id, row)
		}
		// Lookup fails before any session, browser, or network work, so the
		// ID can never dispatch to the community/Zarz session, Antra, or
		// saavn.dev paths.
		if _, err := CheckSourceConnection(id); err == nil || !strings.Contains(err.Error(), "unknown source") {
			t.Fatalf("CheckSourceConnection(%s) = %v (want unknown source)", id, err)
		}
		if _, err := VerifySourceConnection(id); err == nil {
			t.Fatalf("VerifySourceConnection(%s) succeeded (want error)", id)
		}
	}
	// The collision ID resolves to the built-in row, which is not verifiable.
	builtIn, ok := listed["antra-qobuz"]
	if !ok || builtIn.CanVerify || builtIn.Service != "qobuz" {
		t.Fatalf("built-in antra-qobuz row = %+v (present=%v)", builtIn, ok)
	}
	if _, err := VerifySourceConnection("antra-qobuz"); err == nil {
		t.Fatal("built-in antra-qobuz verified (want API-check error)")
	}
	// A failed lookup stores no connection state for custom IDs.
	sourceConnectionChecks.Lock()
	stored := make([]string, 0, len(sourceConnectionChecks.rows))
	for id := range sourceConnectionChecks.rows {
		stored = append(stored, id)
	}
	sourceConnectionChecks.Unlock()
	for _, id := range stored {
		if _, ok := listed[id]; !ok {
			t.Fatalf("connection state stored for unlisted id %s", id)
		}
	}

	// A saved session must not resurrect the retired built-in routes.
	for _, id := range []string{"community-tidal", "zarz-tidal", "community-qobuz", "zarz-amazon"} {
		if row, ok := listed[id]; ok {
			t.Errorf("retired route %s is listed: %+v", id, row)
		}
		if row, err := CheckSourceConnection(id); err == nil {
			t.Errorf("retired route %s was checked: %+v", id, row)
		}
		if row, err := VerifySourceConnection(id); err == nil {
			t.Errorf("retired route %s was verified: %+v", id, row)
		}
	}
}

// The four built-in download sources are listed for API checks and are never
// verifiable: the community/Zarz session flow is retired.
func TestBuiltInDownloadSourcesAreNotVerifiable(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	sourceConnectionChecks.Lock()
	previousChecks := sourceConnectionChecks.rows
	sourceConnectionChecks.rows = make(map[string]SourceConnection)
	sourceConnectionChecks.Unlock()
	t.Cleanup(func() {
		sourceConnectionChecks.Lock()
		sourceConnectionChecks.rows = previousChecks
		sourceConnectionChecks.Unlock()
	})
	listed := map[string]SourceConnection{}
	for _, row := range SourceConnections() {
		listed[row.ID] = row
	}
	for _, id := range []string{"antra-qobuz", "antra-deezer", "antra-apple", "jiosaavn-official"} {
		row, ok := listed[id]
		if !ok {
			t.Fatalf("built-in download source %s missing", id)
		}
		if row.CanVerify {
			t.Fatalf("built-in download source %s is verifiable", id)
		}
		if row.State != "unchecked" {
			t.Fatalf("built-in download source %s state = %s (want unchecked)", id, row.State)
		}
		if _, err := VerifySourceConnection(id); err == nil {
			t.Fatalf("VerifySourceConnection(%s) succeeded (want error)", id)
		}
	}
}

func TestSourceConnectionUnknownOrNonInteractiveDoesNotLaunchVerification(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	if _, err := VerifySourceConnection("unknown"); err == nil {
		t.Fatal("unknown source accepted")
	}
	if _, err := VerifySourceConnection("resource-lrclib"); err == nil {
		t.Fatal("resource must not launch interactive verification")
	}
	if _, err := CheckSourceConnection("unknown"); err == nil {
		t.Fatal("unknown source checked")
	}
}
