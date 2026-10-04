//go:build !windows

package backend

func browserSessionPersistenceAvailable() bool { return false }

func platformCaptureCommunityCookies(string) ([]storedCookie, error) {
	return nil, errInAppVerificationUnavailable
}

func platformCaptureCommunityUserAgent() (string, error) { return "", errInAppVerificationUnavailable }

func communityVerificationBrowserReady() bool { return false }

func protectBrowserSession([]byte) ([]byte, error) {
	return nil, errBrowserSessionPersistenceUnavailable
}

func unprotectBrowserSession([]byte) ([]byte, error) {
	return nil, errBrowserSessionPersistenceUnavailable
}
