package backend

import (
	"net/http"
	"testing"
	"time"
)

func TestIsGatewaySessionFailure(t *testing.T) {
	if !isGatewaySessionFailure(http.StatusPreconditionRequired, nil) {
		t.Fatal("empty 428 should require verification")
	}
	if !isGatewaySessionFailure(http.StatusUnauthorized, []byte(`{"code":"SESSION_INVALID"}`)) {
		t.Fatal("SESSION_INVALID should require verification")
	}
	if !isGatewaySessionFailure(http.StatusUnauthorized, []byte(`{"code":"VERIFY_REQUIRED","origin":"gateway"}`)) {
		t.Fatal("VERIFY_REQUIRED should require verification")
	}
	if isGatewaySessionFailure(http.StatusUnauthorized, []byte(`{"error":"track unavailable"}`)) {
		t.Fatal("generic provider 401 should keep the saved session")
	}
	if isGatewaySessionFailure(http.StatusForbidden, []byte(`{"code":"SESSION_INVALID"}`)) {
		t.Fatal("403 should not be treated as a session failure")
	}
}

func TestSessionCredentialsValid(t *testing.T) {
	if sessionCredentialsValid("", "secret", "", time.Minute) {
		t.Fatal("missing session id should be invalid")
	}
	if !sessionCredentialsValid("sess", "secret", "", time.Minute) {
		t.Fatal("credentials without expiry should stay valid")
	}
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if !sessionCredentialsValid("sess", "secret", future, 5*time.Minute) {
		t.Fatal("future RFC3339 expiry should be valid")
	}
	past := time.Now().UTC().Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")
	if sessionCredentialsValid("sess", "secret", past, 5*time.Minute) {
		t.Fatal("expired session should be invalid")
	}
}
