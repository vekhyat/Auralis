package backend

import (
	"strings"
	"testing"
	"time"
)

func TestParseZarzGrantFromProtocolURL(t *testing.T) {
	parsed := parseZarzGrant("auralis://session-grant?cb_version=v2grant&state=abc123&grant=token-value")
	if parsed.Grant != "token-value" {
		t.Fatalf("grant = %q", parsed.Grant)
	}
	if parsed.State != "abc123" {
		t.Fatalf("state = %q", parsed.State)
	}
	if !parsed.FromURL {
		t.Fatal("protocol URL should be marked FromURL")
	}
}

func TestParseZarzGrantRejectsBareProtocolWithoutGrant(t *testing.T) {
	parsed := parseZarzGrant("auralis://session-grant?state=abc123def4567890")
	if parsed.Grant != "" {
		t.Fatalf("unexpected grant %q from a URL with no grant=", parsed.Grant)
	}
}

func TestParseZarzGrantAcceptsRawToken(t *testing.T) {
	parsed := parseZarzGrant("abcdefghijklmnopqrstuvwxyz")
	if parsed.Grant != "abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("grant = %q", parsed.Grant)
	}
	if parsed.FromURL {
		t.Fatal("raw poller tokens are not URLs")
	}
}

func TestDeliverZarzGrantDropsMismatchAndIdleJunk(t *testing.T) {
	zarzGrantMu.Lock()
	zarzExpectedState = "expected-state"
	zarzPendingGrant = ""
	zarzGrantWaiters = nil
	zarzGrantMu.Unlock()
	t.Cleanup(func() {
		zarzGrantMu.Lock()
		zarzExpectedState = ""
		zarzPendingGrant = ""
		zarzGrantWaiters = nil
		zarzGrantMu.Unlock()
	})

	DeliverZarzGrant("auralis://session-grant?state=other-state&grant=stolen")
	zarzGrantMu.Lock()
	pending := zarzPendingGrant
	zarzGrantMu.Unlock()
	if pending != "" {
		t.Fatalf("mismatched state was stored: %q", pending)
	}

	zarzGrantMu.Lock()
	zarzExpectedState = ""
	zarzGrantMu.Unlock()
	DeliverZarzGrant("abcdefghijklmnopqrstuvwxyz")
	zarzGrantMu.Lock()
	pending = zarzPendingGrant
	zarzGrantMu.Unlock()
	if pending != "" {
		t.Fatalf("idle protocol junk was stored: %q", pending)
	}
}

func TestWaitForZarzGrantDismissUnblocks(t *testing.T) {
	isolateVerificationHome(t)
	prev := verificationDismissSettle
	verificationDismissSettle = 15 * time.Millisecond
	t.Cleanup(func() {
		verificationDismissSettle = prev
		zarzGrantMu.Lock()
		zarzExpectedState = ""
		zarzPendingGrant = ""
		zarzGrantWaiters = nil
		zarzGrantMu.Unlock()
	})

	zarzGrantMu.Lock()
	zarzExpectedState = "expected-state"
	zarzPendingGrant = ""
	zarzGrantWaiters = nil
	zarzGrantMu.Unlock()

	windowErr := make(chan error, 1)
	windowErr <- errVerificationDismissed
	start := time.Now()
	_, err := waitForZarzGrant(30*time.Second, windowErr)
	if time.Since(start) > time.Second {
		t.Fatal("dismissed verification must not sit on the five-minute timeout")
	}
	if err == nil || !strings.Contains(err.Error(), "closed before it finished") {
		t.Fatalf("dismiss error = %v", err)
	}
	if !IsDownloadCancelledError(err) {
		t.Fatalf("dismiss must cancel the download: %v", err)
	}
}
