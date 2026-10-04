package backend

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVerificationPresentationDoesNotReviveClosedOrOlderRun(t *testing.T) {
	presentationMu.Lock()
	before := presentationCurrent
	presentationCurrent = VerificationPresentation{}
	presentationMu.Unlock()
	t.Cleanup(func() { presentationMu.Lock(); presentationCurrent = before; presentationMu.Unlock() })
	publishVerificationPresentation(VerificationPresentation{ID: 1, Active: true})
	publishVerificationPresentation(VerificationPresentation{ID: 1, Active: false})
	publishVerificationPresentation(VerificationPresentation{ID: 1, Active: true, Ready: true})
	if GetVerificationPresentation().Active {
		t.Fatal("a delayed ready event revived a closed run")
	}
	publishVerificationPresentation(VerificationPresentation{ID: 2, Active: true})
	publishVerificationPresentation(VerificationPresentation{ID: 1, Active: false})
	if p := GetVerificationPresentation(); p.ID != 2 || !p.Active {
		t.Fatal("an older completion hid the current run")
	}
}

func isolateVerificationAppDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(appDataDirEnv, dir)
	return dir
}

func writeCommunitySourceFixture(t *testing.T, dir string, raw string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "community-sources.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestVerificationDoesNotUseTopLevelWindow(t *testing.T) {
	if verificationMayShowTopLevel() {
		t.Fatal("verification must not show a standalone top-level window")
	}
	if verificationRevealAllowed(false, 800, 600) {
		t.Fatal("a viewport must not reveal the browser before it is parented")
	}
	if verificationRevealAllowed(true, 0, 400) || verificationRevealAllowed(true, 400, 0) {
		t.Fatal("a zero viewport must hide the native view")
	}
	if !verificationRevealAllowed(true, 32, 32) {
		t.Fatal("a parented non-zero viewport may show the child view")
	}
}

func TestVerificationChildStyleStripsPopupChrome(t *testing.T) {
	style := verificationChildStyle(wsPopupStyle | wsCaptionStyle | wsSysMenuStyle | wsThickFrameStyle | wsMinimizeBoxStyle | wsMaximizeBoxStyle | wsVisibleStyle)
	if style&wsPopupStyle != 0 || style&wsCaptionStyle != 0 || style&wsSysMenuStyle != 0 || style&wsThickFrameStyle != 0 {
		t.Fatalf("child style kept top-level chrome: %#x", style)
	}
	if style&wsChildStyle == 0 || style&wsClipSiblingsStyle == 0 || style&wsClipChildrenStyle == 0 {
		t.Fatalf("child style missing WS_CHILD clipping: %#x", style)
	}
	ex := verificationChildExStyle(wsExAppWindowStyle | 0x00000080 | 0x08000000)
	if ex&wsExAppWindowStyle != 0 {
		t.Fatal("child exstyle kept WS_EX_APPWINDOW")
	}
}

func TestValidateVerificationTargetURL(t *testing.T) {
	dir := isolateVerificationAppDir(t)
	writeCommunitySourceFixture(t, dir, `[
		{"id":"local-subsonic","name":"Local Subsonic","service":"qobuz","protocol":"subsonic","base_url":"http://127.0.0.1:4533","enabled":true}
	]`)

	if err := validateVerificationChallengeURL("https://dabmusic.xyz/login"); err == nil {
		t.Fatal("legacy challenge validation must still reject a community source host")
	}
	if err := validateVerificationTargetURL("https://api.zarz.moe/challenge?id=1"); err != nil {
		t.Fatal(err)
	}
	if err := validateVerificationTargetURL("https://dabmusic.xyz/login"); err != nil {
		t.Fatal(err)
	}
	if err := validateVerificationTargetURL("http://127.0.0.1:4533/app"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"http://api.zarz.moe/challenge",
		"http://dabmusic.xyz/login",
		"http://127.0.0.1:4534/app",
		"http://example.com",
		"https://evil.example/turnstile",
		"https://dabmusic.xyz.evil.com/login",
		"https://user:secret@dabmusic.xyz/login?token=abc",
		"javascript:alert(1)",
	} {
		if err := validateVerificationTargetURL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	err := validateVerificationTargetURL("https://user:secret@dabmusic.xyz/login?token=abc")
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "token") {
		t.Fatalf("credential rejection leaked the URL: %v", err)
	}
}

func TestPresentVerificationAllowsConfiguredOrigin(t *testing.T) {
	isolateVerificationAppDir(t)
	_, release := BeginDownloadCancellationScope()
	t.Cleanup(release)
	ForceStopActiveDownloads()
	if embeddedVerificationSupported() {
		err := presentVerificationChallenge("https://dabmusic.xyz/login")
		if !IsDownloadCancelledError(err) {
			t.Fatalf("configured origin should pass validation, got %v", err)
		}
	}
	err := presentVerificationChallenge("https://evil.example/login")
	if err == nil || IsDownloadCancelledError(err) {
		t.Fatalf("unknown origin must be rejected before a browser opens, got %v", err)
	}
}

func TestValidateVerificationNavigation(t *testing.T) {
	isolateVerificationAppDir(t)
	origin := "https://dabmusic.xyz/start"
	tidal := "https://monochrome-api.samidy.com/session"
	if err := validateVerificationNavigation(origin, "https://dabmusic.xyz/login?next=1"); err != nil {
		t.Fatal(err)
	}
	if err := validateVerificationNavigation(origin, "https://www.qobuz.com/login"); err != nil {
		t.Fatal(err)
	}
	if err := validateVerificationNavigation(origin, "https://api.zarz.moe/challenge?id=1"); err != nil {
		t.Fatal(err)
	}
	if err := validateVerificationNavigation(tidal, "https://login.tidal.com/authorize"); err != nil {
		t.Fatal(err)
	}
	for _, next := range []string{
		"https://login.dabmusic.xyz/auth",
		"https://login.tidal.com/authorize",
		"http://dabmusic.xyz/login",
		"http://www.qobuz.com/login",
		"https://evil.example/phish",
		"https://qobuz.com.evil.example/login",
		"https://user:secret@dabmusic.xyz/login",
	} {
		if err := validateVerificationNavigation(origin, next); err == nil {
			t.Fatalf("transition to %s was allowed", next)
		}
	}
	if err := validateVerificationNavigation("https://evil.example/start", "https://evil.example/next"); err == nil {
		t.Fatal("an unapproved origin must not allow same-host navigation")
	}
}

func TestVerificationPresentationOmitsSecrets(t *testing.T) {
	events := make(chan VerificationPresentation, 4)
	SetVerificationPresentationHandler(func(snap VerificationPresentation) {
		events <- snap
	})
	t.Cleanup(func() { SetVerificationPresentationHandler(nil) })

	raw := "https://dabmusic.xyz/login?token=super-secret#grant"
	publishVerificationPresentation(VerificationPresentation{
		ID: atomic.LoadUint64(&verificationRunSeq) + 1, Active: true, Title: verificationGenericTitle, Host: verificationPresentationHost(raw), Ready: false,
	})
	assertPresentationSafe(t, GetVerificationPresentation(), raw)
	select {
	case snap := <-events:
		assertPresentationSafe(t, snap, raw)
		if snap.Host != "dabmusic.xyz" || snap.Title != verificationGenericTitle || !snap.Active || snap.Ready {
			t.Fatalf("snapshot = %+v", snap)
		}
	case <-time.After(time.Second):
		t.Fatal("presentation handler was not called")
	}

	if err := OpenVerificationWindow("https://user:secret@dabmusic.xyz/login?token=abc"); err == nil {
		t.Fatal("credential URL must be rejected before a browser starts")
	}
	failed := GetVerificationPresentation()
	assertPresentationSafe(t, failed, "secret")
	assertPresentationSafe(t, failed, "token")
	if failed.Active || failed.Error == "" {
		t.Fatalf("failure snapshot = %+v", failed)
	}
}

func assertPresentationSafe(t *testing.T, snap VerificationPresentation, secret string) {
	t.Helper()
	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "://") || strings.Contains(text, secret) || strings.Contains(strings.ToLower(text), "token") || strings.Contains(text, "grant") {
		t.Fatalf("presentation leaked private data: %s", text)
	}
}

func TestCancelInAppVerificationIsScoped(t *testing.T) {
	attempt, err := armVerificationRun()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { attempt.complete() })
	if err := CancelInAppVerification(attempt.id + 1); err == nil || !errors.Is(err, errVerificationNotActive) {
		t.Fatalf("foreign cancel = %v", err)
	}
	if attempt.ctx.Err() != nil {
		t.Fatal("foreign id cancelled the current run")
	}
	if err := CancelInAppVerification(0); err == nil {
		t.Fatal("zero id cancelled verification")
	}
	if err := CancelInAppVerification(attempt.id); err != nil {
		t.Fatal(err)
	}
	if attempt.ctx.Err() == nil {
		t.Fatal("matching id should cancel the run")
	}
}

func TestSetVerificationViewportIsScoped(t *testing.T) {
	attempt, err := armVerificationRun()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		attempt.complete()
		resetInAppViewport(0)
	})
	if err := SetVerificationViewport(attempt.id+1, 1, 2, 3, 4); err == nil {
		t.Fatal("foreign viewport was accepted")
	}
	if err := SetVerificationViewport(attempt.id, 8, 12, 0, 40); err != nil {
		t.Fatal(err)
	}
	_, _, width, height, show := viewportForRun(attempt.id)
	if show || width != 0 || height != 40 {
		t.Fatalf("zero width should hide, got %dx%d show=%v", width, height, show)
	}
	if err := SetVerificationViewport(attempt.id, 8, 12, 30, 40); err != nil {
		t.Fatal(err)
	}
	left, top, width, height, show := viewportForRun(attempt.id)
	if !show || left != 8 || top != 12 || width != 30 || height != 40 {
		t.Fatalf("viewport = %d,%d %dx%d show=%v", left, top, width, height, show)
	}
}
