package backend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func parseSessionExpiry(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
		switch {
		case unix > 1e12:
			return time.UnixMilli(unix).UTC(), true
		case unix > 1e9:
			return time.Unix(unix, 0).UTC(), true
		}
	}
	return time.Time{}, false
}

func jsonFlexibleString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(string(raw))
}

func sessionCredentialsValid(sessionID, sessionSecret, expiresAt string, skew time.Duration) bool {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(sessionSecret) == "" {
		return false
	}
	parsed, ok := parseSessionExpiry(expiresAt)
	if !ok {
		return true
	}
	return time.Until(parsed) > skew
}

func isGatewaySessionFailure(status int, body []byte) bool {
	if status != http.StatusUnauthorized && status != http.StatusPreconditionRequired {
		return false
	}

	var parsed struct {
		Error   string `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Origin  string `json:"origin"`
	}
	_ = json.Unmarshal(body, &parsed)

	combined := strings.ToUpper(strings.Join([]string{
		parsed.Code,
		parsed.Error,
		parsed.Message,
		string(body),
	}, " "))

	markers := []string{
		"SESSION_INVALID",
		"SESSION_EXPIRED",
		"SESSION_MISSING",
		"INVALID_SESSION",
		"NO_SESSION",
		"VERIFY_REQUIRED",
		"VERIFICATION_REQUIRED",
		"CHALLENGE_REQUIRED",
		"GRANT_REQUIRED",
		"CAPTCHA_REQUIRED",
		"TURNSTILE_REQUIRED",
		"UNVERIFIED",
	}
	for _, marker := range markers {
		if strings.Contains(combined, marker) {
			return true
		}
	}

	origin := strings.ToLower(strings.TrimSpace(parsed.Origin))
	if origin == "gateway" || origin == "auth" || origin == "session" {
		return true
	}

	// Empty 428 is the historical SpotBye/Zarz "complete verification" signal.
	if status == http.StatusPreconditionRequired && len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	return false
}
