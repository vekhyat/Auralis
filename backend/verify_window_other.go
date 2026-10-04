//go:build !windows

package backend

// OpenVerificationWindow is only implemented for Windows. Other platforms
// open the system browser from presentVerificationChallenge. Windows never
// uses that fallback.
func OpenVerificationWindow(target string) error {
	if err := validateVerificationTargetURL(target); err != nil {
		return err
	}
	return errVerificationUnsupported
}

// CloseVerificationWindow is a no-op without the native window.
func CloseVerificationWindow() {
	attempt := adoptVerificationAttempt()
	if attempt == nil {
		return
	}
	attempt.cancel()
	<-attempt.done
}
