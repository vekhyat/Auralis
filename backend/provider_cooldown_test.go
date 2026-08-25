package backend

import (
	"net/http"
	"testing"
)

func TestZarzRateLimitedError(t *testing.T) {
	err := &zarzAPIError{Status: http.StatusTooManyRequests, Message: "Server busy, retry shortly"}
	if !err.isRateLimited() || !err.shouldRetry() {
		t.Fatal("429 busy errors should be retried")
	}
	if !IsRateLimitedError(err) {
		t.Fatal("wrapped 429 should be detected as rate limited")
	}
}
