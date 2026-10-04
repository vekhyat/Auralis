package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteVerifyExtension(t *testing.T) {
	isolateVerificationHome(t)
	dir, err := writeVerifyExtension()
	if err != nil {
		t.Fatal(err)
	}
	appDir := os.Getenv(appDataDirEnv)
	if appDir == "" || !strings.HasPrefix(dir, appDir) {
		t.Fatalf("extension written outside isolated app home: %s", dir)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(manifest)
	if !strings.Contains(body, "https://api.zarz.moe/*") {
		t.Fatalf("manifest missing zarz match: %s", body)
	}
	if !strings.Contains(body, "https://verify.spotbye.qzz.io/*") {
		t.Fatalf("manifest missing SpotBye match: %s", body)
	}
	if !strings.Contains(body, "content.js") {
		t.Fatal("manifest missing content.js")
	}
	js, err := os.ReadFile(filepath.Join(dir, "content.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(js)
	if !strings.Contains(script, "SpotiFLAC") {
		t.Fatal("content script must scrub SpotiFLAC branding")
	}
	if !strings.Contains(script, "turnstile") {
		t.Fatal("content script must keep the Turnstile widget")
	}
	if !strings.Contains(script, "auralis-favicon") {
		t.Fatal("content script must replace the SpotiFLAC favicon")
	}
	if !strings.Contains(script, "keepForCaptcha") {
		t.Fatal("content script must keep widget ancestors")
	}
	if !strings.Contains(script, "revealWidgetAncestors") {
		t.Fatal("content script must restore late-inserted widget ancestors")
	}
	if !strings.Contains(script, "#f1f2f4") {
		t.Fatal("content script must use dawn catalog paper")
	}
	if !strings.Contains(script, "Segoe UI") {
		t.Fatal("content script must use native Segoe UI")
	}
	style := script
	if i := strings.Index(script, "const STYLE"); i >= 0 {
		style = script[i:]
		if j := strings.Index(style, "const TEXT_RE"); j > 0 {
			style = style[:j]
		}
	}
	if strings.Contains(style, "nav, header, footer") {
		t.Fatal("CSS must not hide structural chrome; keepForCaptcha JS has to win")
	}
	if strings.Contains(style, "[class*=\"spotiflac\"") || strings.Contains(style, "[class*='spotiflac'") {
		t.Fatal("CSS must not hide wrappers that may contain the widget")
	}
	if !strings.Contains(script, "__auralisVerifyBrandingInstalled") {
		t.Fatal("content script must install once across CDP and extension injection")
	}
	if !strings.Contains(script, "textContent !== STYLE") {
		t.Fatal("style injection must not rewrite identical CSS")
	}
	if !strings.Contains(script, "clearHiddenDisplay") {
		t.Fatal("hidden ancestors must be restored when they acquire the widget")
	}
	if strings.Contains(script, "characterData: true") {
		t.Fatal("observer must not watch characterData or style rewrites loop")
	}
	if !strings.Contains(script, "subtree: true") {
		t.Fatal("observer must watch subtree for late-inserted widgets")
	}
}

func TestVerifyExtensionLoadArgs(t *testing.T) {
	args := verifyExtensionLoadArgs(`C:\ext`)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--load-extension=") {
		t.Fatalf("missing load-extension: %v", args)
	}
	if !strings.Contains(joined, "DisableLoadExtensionCommandLineSwitch") {
		t.Fatalf("missing chrome 137+ extension switch: %v", args)
	}
}
