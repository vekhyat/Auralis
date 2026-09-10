package backend

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func normalizeVerificationHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimPrefix(host, "www.")
}

func allowedVerificationHost(host string) bool {
	host = normalizeVerificationHost(host)
	switch host {
	case "api.zarz.moe", "challenges.cloudflare.com", "127.0.0.1", "localhost", "::1":
		return true
	}
	if verify := strings.TrimSpace(GetCommunityVerifyURL()); verify != "" {
		if parsed, err := url.Parse(verify); err == nil {
			if normalizeVerificationHost(parsed.Host) == host {
				return true
			}
		}
	}
	return false
}

func validateVerificationChallengeURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("verification URL must be https")
	}
	if !allowedVerificationHost(parsed.Host) {
		return fmt.Errorf("verification URL host is not allowed")
	}
	return nil
}
