package backend

import (
	"net/url"
	"strings"
)

// verificationPageIsNetworkError reports the Chromium interstitial Edge shows
// when its first navigation is aborted (ERR_NETWORK_CHANGED — "A network
// change was detected"), including the times the address bar still holds the
// challenge URL.
func verificationPageIsNetworkError(pageURL, bodyText string) bool {
	lowerURL := strings.ToLower(strings.TrimSpace(pageURL))
	if strings.HasPrefix(lowerURL, "chrome-error:") || strings.HasPrefix(lowerURL, "edge-error:") {
		return true
	}
	text := strings.ToLower(bodyText)
	return strings.Contains(text, "network change") ||
		strings.Contains(text, "err_network_changed") ||
		strings.Contains(text, "err_internet_disconnected") ||
		strings.Contains(text, "err_connection_reset") ||
		strings.Contains(text, "this site can't be reached") ||
		strings.Contains(text, "this site can’t be reached")
}

// verificationPageIsReady is true once the challenge (or the loopback grant
// callback) is actually showing, so a reload does not wipe a finished captcha.
func verificationPageIsReady(pageURL, bodyText string) bool {
	if strings.TrimSpace(pageURL) == "" || verificationPageIsNetworkError(pageURL, bodyText) {
		return false
	}
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return true
	}
	return allowedVerificationHost(host)
}
