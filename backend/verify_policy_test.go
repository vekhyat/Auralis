package backend

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func isolateVerificationHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(appDataDirEnv, home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("TEMP", t.TempDir())
	t.Setenv("TMP", t.TempDir())
}

func TestVerificationBackgroundGraceIsPassiveWindow(t *testing.T) {
	if verificationBackgroundGraceDefault < 8*time.Second || verificationBackgroundGraceDefault > 12*time.Second {
		t.Fatalf("grace %s is outside the 8-12s passive window", verificationBackgroundGraceDefault)
	}
}

func TestVerificationShouldReveal(t *testing.T) {
	grace := 10 * time.Second
	if verificationShouldReveal(5*time.Second, grace, true) {
		t.Fatal("should stay in the background before the grace elapses")
	}
	if !verificationShouldReveal(10*time.Second, grace, true) {
		t.Fatal("should reveal once the grace elapses and verification is still pending")
	}
	if verificationShouldReveal(30*time.Second, grace, false) {
		t.Fatal("must not reveal after the grant arrives")
	}
	if !verificationShouldReveal(0, 0, true) {
		t.Fatal("zero grace should reveal immediately while pending")
	}
}

func TestClampVerificationPopupSize(t *testing.T) {
	w, h := clampVerificationPopupSize(300, 65)
	if w < verificationPopupMinWidth || h < verificationPopupMinHeight {
		t.Fatalf("compact widget should still meet the popup minimum, got %dx%d", w, h)
	}
	w, h = clampVerificationPopupSize(2000, 2000)
	if w > verificationPopupMaxWidth || h > verificationPopupMaxHeight {
		t.Fatalf("popup must not grow into a full browser, got %dx%d", w, h)
	}
	if w < 360 || h < 260 {
		t.Fatalf("clamped size %dx%d dropped below the widget floor", w, h)
	}
}

func TestVerificationWindowOutcome(t *testing.T) {
	if err := verificationWindowOutcome(false, false, true); err != nil {
		t.Fatalf("grant received: %v", err)
	}
	if err := verificationWindowOutcome(true, false, false); !errors.Is(err, errVerificationClosed) {
		t.Fatalf("cancel: %v", err)
	}
	if err := verificationWindowOutcome(false, true, false); !errors.Is(err, errVerificationDismissed) {
		t.Fatalf("dismiss: %v", err)
	}
	if err := verificationWindowOutcome(true, true, true); err != nil {
		t.Fatalf("grant wins over close: %v", err)
	}
}

func TestFormatVerificationFailure(t *testing.T) {
	if err := formatVerificationFailure(ErrDownloadCancelled); !errors.Is(err, ErrDownloadCancelled) {
		t.Fatalf("cancel must stay a cancel: %v", err)
	}
	err := formatVerificationFailure(errVerificationDismissed)
	if err == nil || !strings.Contains(err.Error(), "complete the check") {
		t.Fatalf("dismiss should tell the user to complete the check: %v", err)
	}
	if !IsDownloadCancelledError(err) {
		t.Fatalf("dismiss must stop fallback verification prompts: %v", err)
	}
	err = formatVerificationFailure(errors.New("Microsoft Edge was not found"))
	if err == nil || !strings.Contains(err.Error(), "install Microsoft Edge") {
		t.Fatalf("missing Edge should be actionable: %v", err)
	}
}

func TestIsVerificationAppWindowClass(t *testing.T) {
	if !isVerificationAppWindowClass("Chrome_WidgetWin_1") {
		t.Fatal("Edge app windows use Chrome_WidgetWin_1")
	}
	if isVerificationAppWindowClass("Chrome_WidgetWin_0") {
		t.Fatal("helper/GPU windows must not be restyled")
	}
	if isVerificationAppWindowClass("Chrome_SystemMessageWindow") {
		t.Fatal("message windows must not be restyled")
	}
	if isVerificationAppWindowClass("") {
		t.Fatal("empty class is not the app window")
	}
}

func TestSelectVerificationGrantDismissUnblocks(t *testing.T) {
	isolateVerificationHome(t)
	prev := verificationDismissSettle
	verificationDismissSettle = 15 * time.Millisecond
	t.Cleanup(func() { verificationDismissSettle = prev })

	windowErr := make(chan error, 1)
	windowErr <- errVerificationDismissed
	start := time.Now()
	_, err := selectVerificationGrant(make(chan string), windowErr, 30*time.Second)
	if time.Since(start) > time.Second {
		t.Fatal("closing the window must not wait for the long verification timeout")
	}
	if err == nil || !strings.Contains(err.Error(), "closed before it finished") {
		t.Fatalf("dismiss error = %v", err)
	}
	if !IsDownloadCancelledError(err) {
		t.Fatalf("dismiss must be a download cancel: %v", err)
	}
}

func TestSelectVerificationGrantPrefersGrantWhenWindowAlsoEnds(t *testing.T) {
	isolateVerificationHome(t)
	prev := verificationDismissSettle
	verificationDismissSettle = 20 * time.Millisecond
	t.Cleanup(func() { verificationDismissSettle = prev })

	grant := make(chan string, 1)
	windowErr := make(chan error, 1)
	grant <- "token-value"
	windowErr <- errVerificationDismissed
	got, err := selectVerificationGrant(grant, windowErr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != "token-value" {
		t.Fatalf("grant = %q", got)
	}
}

func TestSelectVerificationGrantTimesOutWithActionableError(t *testing.T) {
	isolateVerificationHome(t)
	start := time.Now()
	_, err := selectVerificationGrant(make(chan string), nil, 25*time.Millisecond)
	if time.Since(start) > time.Second {
		t.Fatal("short timeout took too long")
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestWatchVerificationWindowOnlyOnEmbeddedPlatform(t *testing.T) {
	ch := make(chan error, 1)
	watched := watchVerificationWindow(ch)
	if embeddedVerificationSupported() {
		if watched != ch {
			t.Fatal("Windows should watch the isolated Edge window for close/cancel")
		}
		return
	}
	if watched != nil {
		t.Fatal("non-Windows must not treat an immediate window error as a dismiss")
	}
}

func TestStartVerificationWindowHonorsCancelledDownload(t *testing.T) {
	isolateVerificationHome(t)
	_, release := BeginDownloadCancellationScope()
	t.Cleanup(release)
	ForceStopActiveDownloads()
	if _, err := armVerificationRun(); !IsDownloadCancelledError(err) {
		t.Fatalf("arm on cancelled download = %v", err)
	}
	start := time.Now()
	errCh := startVerificationWindow("https://api.zarz.moe/challenge?id=1")
	select {
	case err := <-errCh:
		if !IsDownloadCancelledError(err) {
			t.Fatalf("start on cancelled download = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled verification started a window goroutine")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancelled verification must unwind immediately")
	}
	closed := make(chan struct{})
	go func() {
		CloseVerificationWindow()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("CloseVerificationWindow blocked after cancelled start")
	}
}

func TestStartVerificationWindowInvalidHostUnwinds(t *testing.T) {
	isolateVerificationHome(t)
	start := time.Now()
	errCh := startVerificationWindow("https://evil.example/turnstile")
	var err error
	select {
	case err = <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("invalid host must fail without launching a browser")
	}
	if err == nil {
		t.Fatal("expected host rejection")
	}
	if strings.Contains(strings.ToLower(err.Error()), "edge") && strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Fatalf("invalid host launched far enough to need Edge: %v", err)
	}
	closed := make(chan struct{})
	go func() {
		CloseVerificationWindow()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("CloseVerificationWindow blocked after a pre-launch failure")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("startup failure took too long")
	}
}

func TestArmVerificationRunSerializesDuplicateCallers(t *testing.T) {
	isolateVerificationHome(t)
	first, err := armVerificationRun()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if attempt := adoptVerificationAttempt(); attempt != nil {
			attempt.complete()
		}
	})
	started := make(chan struct{})
	got := make(chan *verificationAttempt, 1)
	errCh := make(chan error, 1)
	go func() {
		close(started)
		second, armErr := armVerificationRun()
		if armErr != nil {
			errCh <- armErr
			return
		}
		got <- second
	}()
	<-started
	time.Sleep(30 * time.Millisecond)
	if adoptVerificationAttempt() != first {
		t.Fatal("a waiter replaced the armed run before it completed")
	}
	first.complete()
	select {
	case armErr := <-errCh:
		t.Fatalf("second arm failed: %v", armErr)
	case second := <-got:
		if second == first {
			t.Fatal("second arm reused the first run identity")
		}
		if adoptVerificationAttempt() != second {
			t.Fatal("second run should be current after the first completes")
		}
		second.complete()
	case <-time.After(time.Second):
		t.Fatal("waiter did not acquire after the first run completed")
	}
}
