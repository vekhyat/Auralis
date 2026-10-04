//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

const verificationAttachTimeout = 15 * time.Second

const verificationPopupBlockSource = `(function(){
  if (window.__auralisNoPopup) return;
  window.__auralisNoPopup = true;
  try { window.open = function(){ return null; }; } catch (e) {}
  document.addEventListener("click", function(ev){
    var node = ev.target;
    while (node && node.tagName !== "A") node = node.parentElement;
    if (node && String(node.target || "").toLowerCase() === "_blank") ev.preventDefault();
  }, true);
})();`

var verificationHostAttached atomic.Bool

func resetVerificationHostAttachment() {
	verificationHostAttached.Store(false)
}

func markVerificationHostAttached() {
	verificationHostAttached.Store(true)
}

func verificationHostReady() bool {
	return verificationHostAttached.Load()
}

func rememberVerificationEmbed(runID uint64, child, parent windows.HWND) {
	verifySessionState.mu.Lock()
	defer verifySessionState.mu.Unlock()
	if verifySessionState.runID != runID {
		return
	}
	verifySessionState.child = child
	verifySessionState.parent = parent
}

func applyInAppVerificationViewport() {
	inAppViewportMu.Lock()
	view := inAppViewportState
	inAppViewportMu.Unlock()
	if !view.valid {
		return
	}
	verifySessionState.mu.Lock()
	if verifySessionState.runID != view.id {
		verifySessionState.mu.Unlock()
		return
	}
	child := verifySessionState.child
	parent := verifySessionState.parent
	verifySessionState.mu.Unlock()
	if child == 0 || parent == 0 {
		return
	}
	show := verificationRevealAllowed(true, view.width, view.height) && !view.hide
	_ = placeVerificationWindow(child, parent, view.left, view.top, view.width, view.height, show)
}

func terminateVerificationSession(id uint64) {
	if id == 0 {
		return
	}
	verifySessionState.mu.Lock()
	if verifySessionState.runID != id {
		verifySessionState.mu.Unlock()
		return
	}
	job := verifySessionState.job
	process := verifySessionState.process
	browserWS := verifySessionState.browserWS
	child := verifySessionState.child
	verifySessionState.mu.Unlock()
	releaseVerificationChild(child)
	closeVerifyDebugger(browserWS)
	terminateVerifyBrowser(job, process)
}

func releaseVerificationChild(hwnd windows.HWND) {
	if hwnd == 0 {
		return
	}
	_, _, _ = procShowWindow.Call(uintptr(hwnd), uintptr(swHide))
}

func driveInAppVerificationPage(ctx context.Context, profileDir, target string) (string, error) {
	var browserWS string
	var err error
	if validateVerificationChallengeURL(target) == nil {
		browserWS, err = driveVerificationPage(ctx, profileDir, target)
	} else {
		browserWS, err = driveConfiguredVerificationPage(ctx, profileDir, target)
	}
	if err == nil {
		blockVerificationPopups(ctx, profileDir)
	}
	return browserWS, err
}

func driveConfiguredVerificationPage(ctx context.Context, profileDir, target string) (string, error) {
	port, browserWS, err := waitForStableDevTools(ctx, profileDir)
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(30 * time.Second)
	navigated := false
	reloads := 0
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return browserWS, errVerificationClosed
		}
		wsURL, pageErr := pageWebSocket(port)
		if pageErr != nil {
			if next, nextWS, refreshErr := refreshDevToolsEndpoint(profileDir); refreshErr == nil {
				port = next
				if nextWS != "" {
					browserWS = nextWS
				}
			}
			if sleepErr := sleepContext(ctx, 250*time.Millisecond); sleepErr != nil {
				return browserWS, sleepErr
			}
			continue
		}
		client, dialErr := dialCDP(wsURL)
		if dialErr != nil {
			if sleepErr := sleepContext(ctx, 250*time.Millisecond); sleepErr != nil {
				return browserWS, sleepErr
			}
			continue
		}
		installVerificationPopupBlock(ctx, client)
		snapshot, snapErr := readPageSnapshot(ctx, client)
		if snapErr != nil {
			client.close()
			if ctx.Err() != nil {
				return browserWS, errVerificationClosed
			}
			if sleepErr := sleepContext(ctx, 250*time.Millisecond); sleepErr != nil {
				return browserWS, sleepErr
			}
			continue
		}
		if !navigated {
			if navErr := navigateVerificationPage(ctx, client, target, false); navErr != nil && ctx.Err() != nil {
				client.close()
				return browserWS, errVerificationClosed
			}
			navigated = true
			client.close()
			if sleepErr := sleepContext(ctx, 200*time.Millisecond); sleepErr != nil {
				return browserWS, sleepErr
			}
			continue
		}
		if verificationPageIsNetworkError(snapshot.Href, snapshot.Text) {
			reloads++
			if reloads > 4 {
				client.close()
				return browserWS, fmt.Errorf("verification page still shows a network error")
			}
			_ = navigateVerificationPage(ctx, client, target, true)
			client.close()
			continue
		}
		if verificationURLExempt(snapshot.Href) {
			client.close()
			if sleepErr := sleepContext(ctx, 200*time.Millisecond); sleepErr != nil {
				return browserWS, sleepErr
			}
			continue
		}
		if err := validateVerificationNavigation(target, snapshot.Href); err != nil {
			client.close()
			return browserWS, fmt.Errorf("verification left the allowed site")
		}
		client.close()
		return browserWS, nil
	}
	if ctx.Err() != nil {
		return browserWS, errVerificationClosed
	}
	return browserWS, fmt.Errorf("verification page did not become ready")
}

func installVerificationPopupBlock(ctx context.Context, client *cdpClient) {
	if client == nil {
		return
	}
	_, _ = client.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{
		"source": verificationPopupBlockSource,
	})
	_, _ = client.call(ctx, "Runtime.evaluate", map[string]any{
		"expression": verificationPopupBlockSource,
	})
}

func blockVerificationPopups(ctx context.Context, profileDir string) {
	port, err := readDevToolsPort(profileDir)
	if err != nil {
		return
	}
	wsURL, err := pageWebSocket(port)
	if err != nil {
		return
	}
	client, err := dialCDP(wsURL)
	if err != nil {
		return
	}
	defer client.close()
	installVerificationPopupBlock(ctx, client)
}

func verificationURLExempt(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" || raw == "about:blank" {
		return true
	}
	for _, prefix := range []string{"about:", "chrome:", "chrome-error:", "edge:", "edge-error:", "devtools:"} {
		if strings.HasPrefix(raw, prefix) {
			return true
		}
	}
	return false
}

func verificationNavigationViolation(profileDir, origin string) error {
	port, err := readDevToolsPort(profileDir)
	if err != nil {
		return nil
	}
	body, err := devToolsGET(port, "/json/list")
	if err != nil {
		return nil
	}
	var targets []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if json.Unmarshal(body, &targets) != nil {
		return nil
	}
	for _, target := range targets {
		if !strings.EqualFold(target.Type, "page") || verificationURLExempt(target.URL) {
			continue
		}
		if err := validateVerificationNavigation(origin, target.URL); err != nil {
			return fmt.Errorf("verification left the allowed site")
		}
	}
	return nil
}
