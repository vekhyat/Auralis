package backend

import (
	"net/url"
	"strings"
	"testing"
)

func TestAllowedVerificationHost(t *testing.T) {
	if !allowedVerificationHost("api.zarz.moe") {
		t.Fatal("api.zarz.moe must be allowed")
	}
	if !allowedVerificationHost("API.ZARZ.MOE") {
		t.Fatal("host matching is case-insensitive")
	}
	if !allowedVerificationHost("challenges.cloudflare.com") {
		t.Fatal("the Cloudflare widget host must be allowed")
	}
	if allowedVerificationHost("api.zarz.moe.evil.example") {
		t.Fatal("suffix lookalikes must be rejected")
	}
	if allowedVerificationHost("phish.example") {
		t.Fatal("unknown hosts must be rejected")
	}
	verify := strings.TrimSpace(GetCommunityVerifyURL())
	if verify == "" {
		t.Fatal("community verify URL must decrypt")
	}
	parsed, err := url.Parse(verify)
	if err != nil {
		t.Fatal(err)
	}
	if !allowedVerificationHost(parsed.Host) {
		t.Fatalf("SpotBye verify host %q must be allowed", parsed.Host)
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
	verify := strings.TrimSpace(GetCommunityVerifyURL())
	if verify == "" {
		t.Fatal("community verify URL must decrypt")
	}
	if err := validateVerificationChallengeURL(verify + "/challenge?id=1"); err != nil {
		t.Fatalf("SpotBye challenge must be allowed: %v", err)
	}
}
