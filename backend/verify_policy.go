package backend

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
)

const (
	verificationBackgroundGraceDefault = 10 * time.Second
	verificationPopupMinWidth          = 360
	verificationPopupMinHeight         = 260
	verificationPopupWidth             = 380
	verificationPopupHeight            = 300
	verificationPopupMaxWidth          = 480
	verificationPopupMaxHeight         = 420
	verificationOffscreenX             = -32000
	verificationOffscreenY             = -32000
	verificationAppWindowClass         = "Chrome_WidgetWin_1"
)

var (
	errVerificationDismissed = errors.New("verification was closed before it finished")
	errVerificationUnsupported = errors.New("embedded verification window is not supported on this platform")

	verificationBackgroundGrace = verificationBackgroundGraceDefault
	verificationDismissSettle   = 1500 * time.Millisecond
)

func embeddedVerificationSupported() bool {
	return runtime.GOOS == "windows"
}

func isVerificationAppWindowClass(className string) bool {
	return strings.TrimSpace(className) == verificationAppWindowClass
}

func verificationShouldReveal(elapsed, grace time.Duration, stillPending bool) bool {
	if !stillPending {
		return false
	}
	if grace <= 0 {
		return true
	}
	return elapsed >= grace
}

func clampVerificationPopupSize(contentW, contentH int) (int, int) {
	const chromeW = 20
	const chromeH = 48
	w := contentW + chromeW
	h := contentH + chromeH
	if w < verificationPopupMinWidth {
		w = verificationPopupMinWidth
	}
	if h < verificationPopupMinHeight {
		h = verificationPopupMinHeight
	}
	if w > verificationPopupMaxWidth {
		w = verificationPopupMaxWidth
	}
	if h > verificationPopupMaxHeight {
		h = verificationPopupMaxHeight
	}
	return w, h
}

func verificationWindowOutcome(cancelled, dismissed, grantReceived bool) error {
	if grantReceived {
		return nil
	}
	if cancelled {
		return errVerificationClosed
	}
	if dismissed {
		return errVerificationDismissed
	}
	return nil
}

func formatVerificationFailure(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrDownloadCancelled) {
		return ErrDownloadCancelled
	}
	if errors.Is(err, errVerificationClosed) {
		return ErrDownloadCancelled
	}
	if errors.Is(err, errVerificationDismissed) {
		return fmt.Errorf("%w: verification was closed before it finished; complete the check to continue the download", ErrDownloadCancelled)
	}
	if errors.Is(err, errVerificationUnsupported) {
		return fmt.Errorf("verification window is unavailable on this platform")
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return fmt.Errorf("verification failed; complete the check to continue the download")
	}
	if strings.Contains(strings.ToLower(msg), "edge was not found") {
		return fmt.Errorf("Microsoft Edge was not found; install Microsoft Edge to complete download verification")
	}
	return fmt.Errorf("verification failed: %w", err)
}

func waitBrieflyForGrant(grant <-chan string, settle time.Duration) string {
	if grant == nil {
		return ""
	}
	if settle < 0 {
		settle = 0
	}
	timer := time.NewTimer(settle)
	defer timer.Stop()
	select {
	case value := <-grant:
		return strings.TrimSpace(value)
	case <-timer.C:
		return ""
	}
}

func selectVerificationGrant(grant <-chan string, windowErr <-chan error, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case value := <-grant:
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("verification grant was empty")
		}
		return strings.TrimSpace(value), nil
	case err := <-windowErr:
		if value := waitBrieflyForGrant(grant, verificationDismissSettle); value != "" {
			return value, nil
		}
		if err == nil || errors.Is(err, errVerificationDismissed) {
			return "", formatVerificationFailure(errVerificationDismissed)
		}
		return "", formatVerificationFailure(err)
	case <-ActiveDownloadContext().Done():
		CloseVerificationWindow()
		return "", ErrDownloadCancelled
	case <-timer.C:
		CloseVerificationWindow()
		return "", fmt.Errorf("verification timed out; complete the check to continue the download")
	}
}
