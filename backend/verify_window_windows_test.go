//go:build windows

package backend

import (
	"strings"
	"testing"
	"time"
)

func TestVerificationBrowserArgsAvoidStartupNavigation(t *testing.T) {
	args := verificationBrowserArgs(`C:\Program Files\Microsoft\Edge\Application\msedge.exe`, `C:\profile`, `C:\ext`)
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "--app=about:blank") {
		t.Fatalf("browser should start blank and navigate after the network stack is up: %s", joined)
	}
	if strings.Contains(joined, "api.zarz.moe") {
		t.Fatal("challenge URL must not be on the command line")
	}
	if !strings.Contains(joined, "--window-size=380,300") {
		t.Fatalf("compact popup size missing: %s", joined)
	}
	if !strings.Contains(joined, "--window-position=-32000,-32000") {
		t.Fatalf("background grace must start off-screen: %s", joined)
	}
	if strings.Contains(joined, "500,640") {
		t.Fatal("full-browser 500x640 window must not be used")
	}
	if strings.Count(joined, "--disable-features=") != 1 {
		t.Fatalf("expected one disable-features flag, got %s", joined)
	}
	for _, needle := range []string{
		"DisableLoadExtensionCommandLineSwitch",
		"LocalNetworkAccessChecks",
		"--remote-debugging-port=0",
		"--load-extension=C:\\ext",
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("missing %s in %s", needle, joined)
		}
	}
}

func TestVerificationStyleIndexesUseRuntimeSignedValues(t *testing.T) {
	styleIndex := verifyGwlStyle
	exIndex := verifyGwlExStyle
	if uintptr(styleIndex) == 0 || uintptr(exIndex) == 0 {
		t.Fatal("GWL indexes must convert at runtime so -16/-20 do not overflow uintptr constants")
	}
}

func TestOpenVerificationWindowRejectsNonHTTPS(t *testing.T) {
	if err := OpenVerificationWindow("http://api.zarz.moe/challenge"); err == nil {
		t.Fatal("http challenge URLs must be rejected before launching Edge")
	}
	if err := OpenVerificationWindow("https://evil.example/turnstile"); err == nil {
		t.Fatal("unknown hosts must be rejected before launching Edge")
	}
}

func TestOpenVerificationWindowHonorsCancelledContext(t *testing.T) {
	isolateVerificationHome(t)
	_, release := BeginDownloadCancellationScope()
	t.Cleanup(release)
	ForceStopActiveDownloads()
	start := time.Now()
	err := OpenVerificationWindow("https://api.zarz.moe/challenge?id=1")
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancelled verification must not launch Edge")
	}
	if !IsDownloadCancelledError(err) {
		t.Fatalf("cancelled open = %v", err)
	}
}
