package backend

import "strings"

func safeBrowserUserAgent(value string) string {
	if len(value) > 1024 || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return strings.TrimSpace(value)
}

func browserUserAgentForSource(source CommunitySource) string {
	pendingBrowser.Lock()
	if pendingBrowser.active && pendingBrowser.id == source.ID {
		value := pendingBrowser.userAgent
		pendingBrowser.Unlock()
		return safeBrowserUserAgent(value)
	}
	pendingBrowser.Unlock()
	rows, err := loadBrowserSessions()
	if err != nil {
		return ""
	}
	row, ok := rows[source.ID]
	origin, err := canonicalCommunityOrigin(source.BaseURL)
	if !ok || err != nil || row.Origin != origin || row.Scope != communitySourceScope(source) {
		return ""
	}
	return safeBrowserUserAgent(row.UserAgent)
}
