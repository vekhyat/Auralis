package backend

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	providerRateLimitFloor = 5 * time.Minute
	providerRateLimitCap   = 10 * time.Minute
)

type providerRateLimitError struct {
	provider  string
	remaining time.Duration
}

func (e *providerRateLimitError) Error() string {
	if e == nil {
		return "provider rate limited"
	}
	return fmt.Sprintf("%s temporarily skipped after rate limit (%s remaining)", e.provider, e.remaining.Round(time.Second))
}

var (
	providerRateLimitMu      sync.Mutex
	providerRateLimitedUntil = map[string]time.Time{}
)

func zarzPublicProviderName(provider string) string {
	switch provider {
	case "tid":
		return "tidal"
	case "qbz":
		return "qobuz"
	case "amazeamazeamaze":
		return "amazon"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func markProviderRateLimited(provider string, wait time.Duration) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return
	}
	if wait < providerRateLimitFloor {
		wait = providerRateLimitFloor
	}
	if wait > providerRateLimitCap {
		wait = providerRateLimitCap
	}
	until := time.Now().Add(wait)
	providerRateLimitMu.Lock()
	providerRateLimitedUntil[provider] = until
	providerRateLimitMu.Unlock()
	fmt.Printf("%s rate limited; skipping that provider for %s\n", provider, wait.Round(time.Second))
}

func skipIfProviderRateLimited(provider string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil
	}
	providerRateLimitMu.Lock()
	until := providerRateLimitedUntil[provider]
	providerRateLimitMu.Unlock()
	remaining := time.Until(until)
	if remaining <= 0 {
		return nil
	}
	return &providerRateLimitError{provider: provider, remaining: remaining}
}

func IsRateLimitedError(err error) bool {
	if err == nil {
		return false
	}
	var skipErr *providerRateLimitError
	if errors.As(err, &skipErr) {
		return true
	}
	var apiErr *zarzAPIError
	if errors.As(err, &apiErr) && apiErr.isRateLimited() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 429") ||
		strings.Contains(msg, "rate limited") ||
		strings.Contains(msg, "server busy") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "retry shortly") ||
		strings.Contains(msg, "temporarily skipped after rate limit")
}
