package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHiFiMediaAndManifestUseVerifiedSession(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	const identity = "Verified-browser-agent"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=synthetic" || r.Header.Get("User-Agent") != identity {
			t.Error("media request did not use the verified browser identity")
			http.Error(w, "login", 401)
			return
		}
		w.Header().Set("Content-Type", "audio/flac")
		fmt.Fprint(w, "fLaC-synthetic-audio")
	}))
	defer server.Close()
	source := CommunitySource{ID: "session-media", Service: "tidal", Protocol: "hifi", BaseURL: server.URL, Enabled: true}
	stop := usePendingBrowserIdentity(source.ID, []storedCookie{{Name: "session", Value: "synthetic", Domain: "127.0.0.1", Path: "/", HostOnly: true}}, identity)
	defer stop()
	d := NewTidalDownloader(server.URL)
	d.communitySource = &source
	manifest, _ := json.Marshal(map[string]any{"mimeType": "audio/flac", "urls": []string{server.URL + "/media"}})
	for _, raw := range []string{server.URL + "/media", "MANIFEST:" + base64.StdEncoding.EncodeToString(manifest)} {
		dest := filepath.Join(t.TempDir(), "audio.flac")
		if err := d.DownloadFile(raw, dest, "LOSSLESS"); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(dest)
		if err != nil || string(body) != "fLaC-synthetic-audio" {
			t.Fatal("media bytes were not saved")
		}
	}
}

func TestHiFiRedirectDoesNotForwardEnvironmentCredentials(t *testing.T) {
	t.Setenv("AURALIS_TEST_MEDIA_COOKIE", "session=private")
	t.Setenv(appDataDirEnv, t.TempDir())
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("X-API-Key") != "" {
			t.Error("credentials crossed an origin")
		}
		fmt.Fprint(w, "fLaC-test")
	}))
	defer foreign.Close()
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, foreign.URL, http.StatusFound) }))
	defer sourceServer.Close()
	source := CommunitySource{ID: "redirect-test", Protocol: "hifi", BaseURL: sourceServer.URL, CredentialEnv: "AURALIS_TEST_MEDIA_COOKIE", CredentialType: "cookie"}
	d := NewTidalDownloader(sourceServer.URL)
	d.communitySource = &source
	if err := d.DownloadFile(sourceServer.URL, filepath.Join(t.TempDir(), "test.flac"), "LOSSLESS"); err != nil {
		t.Fatal(err)
	}
}
