package taste

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestPKCEVerifier(t *testing.T) {
	v1, err := pkceVerifier()
	if err != nil {
		t.Fatalf("pkceVerifier failed: %v", err)
	}
	if len(v1) < 43 || len(v1) > 128 {
		t.Fatalf("expected verifier length between 43 and 128, got %d (%s)", len(v1), v1)
	}
	// RFC 7636 unreserved characters [A-Z] / [a-z] / [0-9] / "-" / "." / "_" / "~"
	for _, r := range v1 {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_", r) {
			t.Fatalf("unexpected character %q in verifier", r)
		}
	}

	v2, err := pkceVerifier()
	if err != nil {
		t.Fatalf("second pkceVerifier failed: %v", err)
	}
	if v1 == v2 {
		t.Fatalf("expected two verifiers to be distinct")
	}
}

func TestPKCEChallenge(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := pkceChallenge(verifier)
	if challenge == "" {
		t.Fatalf("challenge should not be empty")
	}
	if strings.Contains(challenge, "=") {
		t.Fatalf("challenge should be unpadded URL-safe base64: %s", challenge)
	}

	sum := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != expected {
		t.Fatalf("expected challenge %s, got %s", expected, challenge)
	}
}

func TestRandomStateAndValidState(t *testing.T) {
	s1, err := randomState()
	if err != nil {
		t.Fatalf("randomState failed: %v", err)
	}
	if len(s1) < 16 {
		t.Fatalf("state too short: %s", s1)
	}
	s2, err := randomState()
	if err != nil {
		t.Fatalf("second randomState failed: %v", err)
	}
	if s1 == s2 {
		t.Fatalf("expected two states to be distinct")
	}

	if !validState(s1, s1) {
		t.Fatalf("expected validState to return true for matching states")
	}
	if validState(s1, s2) {
		t.Fatalf("expected validState to return false for different states")
	}
	if validState(s1, "") || validState("", s1) || validState("", "") {
		t.Fatalf("expected validState to return false for empty states")
	}
}
