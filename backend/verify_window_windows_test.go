//go:build windows

package backend

import "testing"

func TestOpenVerificationWindowRejectsNonHTTPS(t *testing.T) {
	if err := OpenVerificationWindow("http://api.zarz.moe/challenge"); err == nil {
		t.Fatal("http challenge URLs must be rejected before launching Edge")
	}
	if err := OpenVerificationWindow("https://evil.example/turnstile"); err == nil {
		t.Fatal("unknown hosts must be rejected before launching Edge")
	}
}
