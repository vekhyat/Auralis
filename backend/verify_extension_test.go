package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteVerifyExtension(t *testing.T) {
	dir, err := writeVerifyExtension()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(manifest)
	if !strings.Contains(body, "https://api.zarz.moe/*") {
		t.Fatalf("manifest missing zarz match: %s", body)
	}
	if !strings.Contains(body, "content.js") {
		t.Fatal("manifest missing content.js")
	}
	js, err := os.ReadFile(filepath.Join(dir, "content.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), "SpotiFLAC") {
		t.Fatal("content script must scrub SpotiFLAC branding")
	}
	if !strings.Contains(string(js), "turnstile") {
		t.Fatal("content script must keep the Turnstile widget")
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
