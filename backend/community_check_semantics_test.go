package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func isolateCheckSemanticsEnv(t *testing.T) {
	t.Helper()
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
}

func saveCheckSemanticsSource(t *testing.T, source CommunitySource) {
	t.Helper()
	if err := SaveCommunitySources([]CommunitySource{source}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		communityCircuit.Lock()
		delete(communityCircuit.until, source.ID)
		communityCircuit.Unlock()
		ClearCommunitySourceCheck(source.ID)
	})
}

func lucidaCheckFixturePage(info string) string {
	return `<html><script>,{"type":"data","data":{` + info + `},"uses":{"url":1}}];</script></html>`
}

func TestExpectSourceJSONDistinguishesChallengeFromBroken(t *testing.T) {
	isolateCheckSemanticsEnv(t)
	var access *communityAccessError
	if err := expectSourceJSON([]byte{}); err == nil || errors.As(err, &access) {
		t.Fatalf("empty response classified as success/auth: %v", err)
	}
	if err := expectSourceJSON([]byte("   \n\t ")); err == nil || errors.As(err, &access) {
		t.Fatalf("whitespace response classified as success/auth: %v", err)
	}
	if err := expectSourceJSON([]byte("<html><body>not a source payload</body></html>")); err == nil || errors.As(err, &access) {
		t.Fatalf("generic HTML classified as success/auth: %v", err)
	}
	if err := expectSourceJSON([]byte("<!doctype html><html><head><title>Redirecting...</title></head><body>Loading</body></html>")); err == nil || errors.As(err, &access) {
		t.Fatalf("ordinary redirect page classified as success/auth: %v", err)
	}
	challenge := []byte("<!doctype html><html><title>Just a moment...</title><body>cf-challenge</body></html>")
	if err := expectSourceJSON(challenge); !errors.As(err, &access) {
		t.Fatalf("challenge page not classified as authentication: %v", err)
	}
	if err := expectSourceJSON([]byte(`{"url":"https://media.example/audio.flac"}`)); err != nil {
		t.Fatalf("valid JSON rejected: %v", err)
	}
}

func TestCheckCommunitySourceBrokenEndpointIsGenericFailure(t *testing.T) {
	isolateCheckSemanticsEnv(t)
	genericHTML := "<html><body>not a source payload</body></html>"
	challengeHTML := "<!doctype html><html><title>Just a moment...</title><body>cf-challenge</body></html>"

	t.Run("DABGenericHTML", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, genericHTML)
		}))
		defer server.Close()
		source := CommunitySource{ID: "sem-dab-generic", Name: "DAB Generic", Service: "qobuz", Protocol: "dab", BaseURL: server.URL, Enabled: true, CredentialType: "cookie"}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State == "authentication_required" {
			t.Fatalf("generic DAB HTML presented as verification: %+v", row)
		}
		if row.State != "failed" {
			t.Fatalf("expected failed, got %+v", row)
		}
		if row.AudioVerified {
			t.Fatal("probe must not verify audio")
		}
		if row.Action == "verify" && row.State == "authentication_required" {
			t.Fatal("generic failure must not request browser verification")
		}
	})

	t.Run("DABEmpty", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
		}))
		defer server.Close()
		source := CommunitySource{ID: "sem-dab-empty", Name: "DAB Empty", Service: "qobuz", Protocol: "dab", BaseURL: server.URL, Enabled: true, CredentialType: "cookie"}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State == "authentication_required" {
			t.Fatalf("empty DAB response presented as verification: %+v", row)
		}
		if row.State != "failed" || row.AudioVerified {
			t.Fatalf("expected failed without audio verification, got %+v", row)
		}
	})

	t.Run("QobuzGenericHTML", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, genericHTML)
		}))
		defer server.Close()
		source := CommunitySource{ID: "sem-qobuz-generic", Name: "Qobuz Generic", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: server.URL, Enabled: true}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State == "authentication_required" || row.State == "configuration_required" {
			t.Fatalf("generic Qobuz HTML presented as auth/config: %+v", row)
		}
		if row.State != "failed" || row.AudioVerified {
			t.Fatalf("expected failed without audio verification, got %+v", row)
		}
	})

	t.Run("DABChallengeStaysAuthentication", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, challengeHTML)
		}))
		defer server.Close()
		source := CommunitySource{ID: "sem-dab-challenge", Name: "DAB Challenge", Service: "qobuz", Protocol: "dab", BaseURL: server.URL, Enabled: true, CredentialType: "cookie"}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State != "authentication_required" || row.Action != "verify" || row.AudioVerified {
			t.Fatalf("challenge must stay browser authentication without audio: %+v", row)
		}
	})
}

func TestProbeLucidaRejectsWrongIdentity(t *testing.T) {
	isolateCheckSemanticsEnv(t)
	const fixtureID = "30369895"
	const canonical = "https://open.qobuz.com/track/" + fixtureID

	serve := func(info string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, lucidaCheckFixturePage(info))
		}))
	}

	t.Run("Matching", func(t *testing.T) {
		server := serve(`info:{type:"track",url:"` + canonical + `"}`)
		defer server.Close()
		source := CommunitySource{BaseURL: server.URL, Service: "qobuz", Protocol: "lucida"}
		if err := probeLucidaMetadata(context.Background(), source, SourceTrack{ID: fixtureID}); err != nil {
			t.Fatalf("matching identity rejected: %v", err)
		}
	})

	t.Run("SongKindMatching", func(t *testing.T) {
		server := serve(`info:{type:"song",url:"` + canonical + `"}`)
		defer server.Close()
		source := CommunitySource{BaseURL: server.URL, Service: "qobuz", Protocol: "lucida"}
		if err := probeLucidaMetadata(context.Background(), source, SourceTrack{ID: fixtureID}); err != nil {
			t.Fatalf("song identity rejected: %v", err)
		}
	})

	t.Run("Wrong", func(t *testing.T) {
		server := serve(`info:{type:"track",url:"https://open.qobuz.com/track/99999999"}`)
		defer server.Close()
		source := CommunitySource{BaseURL: server.URL, Service: "qobuz", Protocol: "lucida"}
		err := probeLucidaMetadata(context.Background(), source, SourceTrack{ID: fixtureID})
		if err == nil || !strings.Contains(err.Error(), "different track URL") {
			t.Fatalf("wrong identity not rejected as mismatch: %v", err)
		}
		var access *communityAccessError
		if errors.As(err, &access) {
			t.Fatal("wrong identity must be a generic mismatch, not authentication")
		}
	})

	t.Run("Missing", func(t *testing.T) {
		server := serve(`info:{type:"track"}`)
		defer server.Close()
		source := CommunitySource{BaseURL: server.URL, Service: "qobuz", Protocol: "lucida"}
		err := probeLucidaMetadata(context.Background(), source, SourceTrack{ID: fixtureID})
		if err == nil {
			t.Fatal("missing identity accepted")
		}
		var access *communityAccessError
		if errors.As(err, &access) {
			t.Fatal("missing identity must fail closed as generic mismatch, not authentication")
		}
	})

	t.Run("CheckIntegration", func(t *testing.T) {
		matching := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, lucidaCheckFixturePage(`info:{type:"track",url:"`+canonical+`"}`))
		}))
		defer matching.Close()
		okSource := CommunitySource{ID: "sem-lucida-ok", Name: "Lucida OK", Service: "qobuz", Protocol: "lucida", BaseURL: matching.URL, Enabled: true, CredentialType: "cookie"}
		saveCheckSemanticsSource(t, okSource)
		row, err := CheckCommunitySource(okSource.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State != "available" || row.AudioVerified {
			t.Fatalf("matching probe must be available without audio verification: %+v", row)
		}

		wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, lucidaCheckFixturePage(`info:{type:"track",url:"https://open.qobuz.com/track/00000000"}`))
		}))
		defer wrong.Close()
		badSource := CommunitySource{ID: "sem-lucida-wrong", Name: "Lucida Wrong", Service: "qobuz", Protocol: "lucida", BaseURL: wrong.URL, Enabled: true, CredentialType: "cookie"}
		saveCheckSemanticsSource(t, badSource)
		bad, err := CheckCommunitySource(badSource.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if bad.State == "authentication_required" || bad.State == "available" || bad.AudioVerified {
			t.Fatalf("wrong identity must be a generic failure: %+v", bad)
		}
	})
}

func TestCheckCommunitySourceCredentialActionSemantics(t *testing.T) {
	isolateCheckSemanticsEnv(t)

	t.Run("MalformedLocalCredentialIsConfiguration", func(t *testing.T) {
		t.Setenv("AURALIS_CHECK_SEM_BAD_KEY", "invalid\r\ncredential")
		source := CommunitySource{ID: "sem-invalid-key", Name: "Invalid key", Service: "qobuz", Protocol: "qobuz-rest", BaseURL: "http://127.0.0.1:9", CredentialEnv: "AURALIS_CHECK_SEM_BAD_KEY", CredentialType: "api_key"}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil || row.State != "configuration_required" || row.Action != "configure" || row.AudioVerified {
			t.Fatalf("invalid local credential: result=%+v err=%v", row, err)
		}
	})

	t.Run("APIKeyRejectedIsConfiguration", func(t *testing.T) {
		const secret = "check-semantics-api-secret"
		t.Setenv("AURALIS_CHECK_SEM_REST_KEY", secret)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()
		source := CommunitySource{ID: "sem-rest-401", Name: "REST 401", Service: "qobuz", Protocol: "qobuz-rest", BaseURL: server.URL, Enabled: true, CredentialEnv: "AURALIS_CHECK_SEM_REST_KEY", CredentialType: "api_key"}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State != "configuration_required" || row.Action != "configure" || row.AudioVerified {
			t.Fatalf("api_key 401 must be configuration_required/configure without audio: %+v", row)
		}
		if strings.Contains(row.Message, secret) {
			t.Fatal("configuration message exposed a secret")
		}
		if !strings.Contains(strings.ToLower(row.Message), "environment") && !strings.Contains(strings.ToLower(row.Message), "api key") {
			t.Fatalf("configuration message is not a useful instruction: %q", row.Message)
		}
	})

	t.Run("BearerRejectedIsConfiguration", func(t *testing.T) {
		const secret = "check-semantics-bearer-secret"
		t.Setenv("AURALIS_CHECK_SEM_BEARER", secret)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()
		source := CommunitySource{ID: "sem-bearer-403", Name: "Bearer 403", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: server.URL, Enabled: true, CredentialEnv: "AURALIS_CHECK_SEM_BEARER", CredentialType: "bearer"}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State != "configuration_required" || row.Action != "configure" || row.AudioVerified {
			t.Fatalf("bearer 403 must be configuration_required/configure without audio: %+v", row)
		}
		if strings.Contains(row.Message, secret) {
			t.Fatal("configuration message exposed a secret")
		}
	})

	t.Run("SubsonicMissingIsConfiguration", func(t *testing.T) {
		env := "AURALIS_CHECK_SEM_SUBSONIC_MISSING"
		t.Setenv(env, "")
		source := CommunitySource{ID: "sem-subsonic-missing", Name: "Subsonic Missing", Service: "qobuz", Protocol: "subsonic", BaseURL: "http://127.0.0.1:9", Enabled: true, CredentialEnv: env, CredentialType: "subsonic"}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State != "configuration_required" || row.Action != "configure" || row.AudioVerified {
			t.Fatalf("missing subsonic credential must be configuration_required/configure: %+v", row)
		}
	})

	t.Run("BrowserRejectedIsAuthentication", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()
		source := CommunitySource{ID: "sem-dab-403", Name: "DAB 403", Service: "qobuz", Protocol: "dab", BaseURL: server.URL, Enabled: true, CredentialType: "cookie"}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State != "authentication_required" || row.Action != "verify" || row.AudioVerified {
			t.Fatalf("cookie 403 must be authentication_required/verify without audio: %+v", row)
		}
	})

	t.Run("NetworkFailureStaysFailed", func(t *testing.T) {
		t.Setenv("AURALIS_CHECK_SEM_NET_KEY", "network-secret-value")
		source := CommunitySource{ID: "sem-net-failed", Name: "Net Failed", Service: "qobuz", Protocol: "qobuz-rest", BaseURL: "http://127.0.0.1:9", Enabled: true, CredentialEnv: "AURALIS_CHECK_SEM_NET_KEY", CredentialType: "api_key"}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State == "configuration_required" || row.State == "authentication_required" {
			t.Fatalf("network failure must not be classified as credential/auth: %+v", row)
		}
		if row.State != "failed" || row.AudioVerified {
			t.Fatalf("expected failed without audio verification, got %+v", row)
		}
	})

	t.Run("ProbeSuccessLeavesAudioUnverified", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/search":
				fmt.Fprint(w, `{"tracks":[{"id":"dab-id","title":"Come Together","artist":"The Beatles","isrc":"GBAYE0601690"}]}`)
			case "/api/stream":
				fmt.Fprint(w, `{"url":"https://media.example/audio.flac"}`)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()
		source := CommunitySource{ID: "sem-dab-ok", Name: "DAB OK", Service: "qobuz", Protocol: "dab", BaseURL: server.URL, Enabled: true, CredentialType: "cookie"}
		saveCheckSemanticsSource(t, source)
		row, err := CheckCommunitySource(source.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.State != "available" || row.AudioVerified {
			t.Fatalf("probe success must be available without audio verification: %+v", row)
		}
	})
}
