//go:build windows

package backend

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/wailsapp/go-webview2/pkg/edge"
)

// In-app verification window: hosts the upstream Cloudflare Turnstile challenge
// page in a native WebView2 window owned by Auralis, with cosmetic branding so
// the flow feels first-party. The verification itself is untouched — the real
// remote page loads from the real domain, so Turnstile behaves normally.
//
// Threading model: Win32 windows are thread-affine. The caller's goroutine is
// locked to an OS thread and owns the window for its whole lifetime: create,
// pump messages, tear down. This slots directly into the existing flow, since
// completeZarzChallenge / runCommunityVerification already block while waiting
// for the grant.

//go:embed assets/verify-branding.css
var verifyBrandingCSS string

const verifyWindowTitle = "Auralis — Verification"

type verifySession struct {
	mu      sync.Mutex
	active  bool
	closing bool
}

var verifySessionState verifySession

// OpenVerificationWindow shows the challenge URL in the embedded browser
// window and pumps its message loop until the window is closed (either by the
// user, or automatically once the grant has been delivered).
func OpenVerificationWindow(target string) error {
	if err := validateVerificationChallengeURL(target); err != nil {
		return err
	}

	verifySessionState.mu.Lock()
	if verifySessionState.active {
		verifySessionState.mu.Unlock()
		return fmt.Errorf("verification window is already open")
	}
	verifySessionState.active = true
	verifySessionState.closing = false
	verifySessionState.mu.Unlock()

	defer func() {
		verifySessionState.mu.Lock()
		verifySessionState.active = false
		verifySessionState.mu.Unlock()
	}()

	// Webview2 controllers and the host window must live on one thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hwnd := createVerifyHostWindow(verifyWindowTitle)
	if hwnd == 0 {
		return fmt.Errorf("could not create the verification window")
	}
	// Ensure the window never outlives this call, whatever happens.
	defer destroyVerifyHostWindow(hwnd)

	chromium := edge.NewChromium()
	chromium.DataPath = verifyProfileDir()
	chromium.SetBackgroundColour(0x0a, 0x0a, 0x0a, 0xff)
	chromium.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		if strings.HasPrefix(strings.TrimSpace(message), "auralis-verify:close") {
			CloseVerificationWindow()
		}
	}
	if !chromium.Embed(uintptr(hwnd)) {
		return fmt.Errorf("failed to initialise the embedded browser")
	}
	defer chromium.ShuttingDown()

	chromium.Init(verifyBootstrapJS())
	chromium.Navigate(target)
	if err := chromium.Show(); err != nil {
		return err
	}
	focusVerifyHostWindow(hwnd)

	pumpVerifyHostMessages(hwnd)
	return nil
}

// CloseVerificationWindow asks the embedded window to close. Safe to call from
// any goroutine: it merely posts a message, and the owning thread performs the
// actual teardown. No-op when nothing is open.
func CloseVerificationWindow() {
	postVerifyHostClose()
}

func verifyProfileDir() string {
	dir, err := EnsureAppDir()
	if err != nil {
		return ""
	}
	return dir + `\verify_profile`
}

// verifyBootstrapJS runs on every document before page scripts. Applies the
// branding stylesheet and keeps it applied against late DOM changes.
func verifyBootstrapJS() string {
	css := strings.ReplaceAll(verifyBrandingCSS, "\\", `\\`)
	css = strings.ReplaceAll(css, "`", "\\`")
	css = strings.ReplaceAll(css, "\n", `\n`)
	return `(function () {
	if (window.__auralisBranding) return;
	window.__auralisBranding = true;
	var css = ` + "`" + css + "`" + `;
	function apply() {
		if (document.getElementById("auralis-branding")) return;
		var el = document.createElement("style");
		el.id = "auralis-branding";
		el.textContent = css;
		(document.head || document.documentElement).appendChild(el);
	}
	apply();
	document.addEventListener("DOMContentLoaded", apply);
	new MutationObserver(function () { apply(); }).observe(document.documentElement, { childList: true, subtree: true });
})();`
}
