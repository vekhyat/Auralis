package backend

import "testing"

func TestAllowedVerificationHost(t *testing.T) {
	if !allowedVerificationHost("api.zarz.moe") {
		t.Fatal("api.zarz.moe must be allowed")
	}
	if !allowedVerificationHost("API.ZARZ.MOE") {
		t.Fatal("host matching is case-insensitive")
	}
	if allowedVerificationHost("api.zarz.moe.evil.example") {
		t.Fatal("suffix lookalikes must be rejected")
	}
	if allowedVerificationHost("phish.example") {
		t.Fatal("unknown hosts must be rejected")
	}
}

func TestValidateVerificationChallengeURL(t *testing.T) {
	if err := validateVerificationChallengeURL("https://api.zarz.moe/challenge?id=1"); err != nil {
		t.Fatal(err)
	}
	if err := validateVerificationChallengeURL("http://api.zarz.moe/challenge"); err == nil {
		t.Fatal("http must be rejected")
	}
	if err := validateVerificationChallengeURL("https://evil.example/turnstile"); err == nil {
		t.Fatal("unknown https host must be rejected")
	}
}
