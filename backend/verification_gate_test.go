package backend

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A protected source fixture: the API answers only after the check passes.
func onDemandVerificationFixture(t *testing.T) (CommunitySource, *atomic.Bool) {
	t.Helper()
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Cleanup(resetSourceVerificationForTest)
	previous := communityAutoConfirmInterval
	communityAutoConfirmInterval = 20 * time.Millisecond
	t.Cleanup(func() { communityAutoConfirmInterval = previous })
	_, release := BeginDownloadCancellationScope()
	t.Cleanup(release)
	var accept atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !accept.Load() {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<!doctype html><html><title>Just a moment</title><body>cf-challenge</body></html>")
			return
		}
		switch r.URL.Path {
		case "/api/search":
			fmt.Fprint(w, `{"tracks":[{"id":"1","title":"Come Together","artist":"The Beatles","isrc":"GBAYE0601690"}]}`)
		default:
			fmt.Fprint(w, `{"url":"https://media.example/audio.flac"}`)
		}
	}))
	t.Cleanup(server.Close)
	source := CommunitySource{ID: "dab-on-demand", Name: "On demand", Service: "qobuz", Protocol: "dab", BaseURL: server.URL, Enabled: true, CredentialType: "cookie"}
	if err := SaveCommunitySources([]CommunitySource{source}); err != nil {
		t.Fatal(err)
	}
	sourceVerificationHook.capture = func(string) ([]storedCookie, error) {
		return []storedCookie{{Name: "session", Value: "fixture", Domain: "127.0.0.1", Path: "/", HostOnly: true}}, nil
	}
	return source, &accept
}

func TestDownloadVerificationConfirmsItselfAndAsksOnce(t *testing.T) {
	if !browserSessionPersistenceAvailable() {
		t.Skip("this test persists a Windows DPAPI session")
	}
	source, accept := onDemandVerificationFixture(t)
	var opens atomic.Int32
	sourceVerificationHook.open = func(string) <-chan error {
		opens.Add(1)
		return make(chan error)
	}
	sourceVerificationHook.close = func() {}
	refused := time.Now()
	done := make(chan error, 1)
	go func() { done <- verifyCommunitySourceForDownload(source, refused) }()
	waitForVerificationWindow(t, &opens)
	if SourceVerificationNeedsConfirmation() {
		t.Fatal("a download-time check asked the user to confirm")
	}
	accept.Store(true)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("check did not pass on its own: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("check did not close after the source accepted the session")
	}
	// A track refused before that check finished retries without asking.
	if err := verifyCommunitySourceForDownload(source, refused); err != nil || opens.Load() != 1 {
		t.Fatalf("second track err=%v opens=%d", err, opens.Load())
	}
}

func TestClosedDownloadVerificationIsNotAskedAgain(t *testing.T) {
	source, _ := onDemandVerificationFixture(t)
	var opens atomic.Int32
	window := make(chan error, 1)
	sourceVerificationHook.open = func(string) <-chan error {
		opens.Add(1)
		return window
	}
	sourceVerificationHook.close = func() {}
	done := make(chan error, 1)
	go func() { done <- verifyCommunitySourceForDownload(source, time.Now()) }()
	waitForVerificationWindow(t, &opens)
	window <- errVerificationClosed
	if err := <-done; err != errVerificationSkipped {
		t.Fatalf("closing the check returned %v", err)
	}
	if err := verifyCommunitySourceForDownload(source, time.Now()); err != errVerificationSkipped || opens.Load() != 1 {
		t.Fatalf("closed source asked again: err=%v opens=%d", err, opens.Load())
	}
}

func waitForVerificationWindow(t *testing.T, opens *atomic.Int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for opens.Load() == 0 || currentManual() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the check window was not opened")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
