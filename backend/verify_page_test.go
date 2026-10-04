package backend

import "testing"

func TestVerificationPageIsNetworkError(t *testing.T) {
	if !verificationPageIsNetworkError("chrome-error://chromewebdata/", "") {
		t.Fatal("chrome error page should reload")
	}
	if !verificationPageIsNetworkError("https://api.zarz.moe/challenge", "Your connection was interrupted. A network change was detected.") {
		t.Fatal("network-change interstitial should reload even when the URL is the challenge")
	}
	if verificationPageIsNetworkError("https://api.zarz.moe/challenge?id=1", "Complete the check to continue") {
		t.Fatal("challenge page should not reload")
	}
}

func TestVerificationPageIsReady(t *testing.T) {
	if verificationPageIsReady("about:blank", "") {
		t.Fatal("about:blank is not the challenge")
	}
	if verificationPageIsReady("https://api.zarz.moe/challenge", "A network change was detected") {
		t.Fatal("error text is not ready")
	}
	if !verificationPageIsReady("https://api.zarz.moe/challenge?id=1", "Verify") {
		t.Fatal("zarz challenge should count as ready")
	}
	if !verificationPageIsReady("http://127.0.0.1:1234/session-grant?grant=abc", "Verified") {
		t.Fatal("loopback grant callback should count as ready")
	}
	if verificationPageIsReady("https://evil.example/challenge", "Verify") {
		t.Fatal("unknown hosts are not the verification page")
	}
}
