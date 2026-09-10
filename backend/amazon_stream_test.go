package backend

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestIsTransientAmazonStreamError(t *testing.T) {
	if isTransientAmazonStreamError(nil) {
		t.Fatal("nil should not be transient")
	}
	if !isTransientAmazonStreamError(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded should be retried")
	}
	if !isTransientAmazonStreamError(errors.New("Get https://cdn.example: context deadline exceeded (Client.Timeout exceeded while awaiting headers)")) {
		t.Fatal("client timeout errors should be retried")
	}
	timeoutErr := &net.DNSError{Err: "timeout", IsTimeout: true}
	if !isTransientAmazonStreamError(timeoutErr) {
		t.Fatal("net timeout should be retried")
	}
	if isTransientAmazonStreamError(errors.New("Amazon API returned status 404")) {
		t.Fatal("HTTP 404 should not be retried as a stream timeout")
	}
}

func TestAmazonStreamClientTimeout(t *testing.T) {
	client := amazonStreamHTTPClient()
	if client.Timeout != 5*time.Minute {
		t.Fatalf("stream timeout = %s, want 5m", client.Timeout)
	}
}
