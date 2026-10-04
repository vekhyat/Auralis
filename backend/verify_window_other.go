//go:build !windows

package backend

// OpenVerificationWindow is only implemented for Windows. Other platforms
// open the system browser from presentVerificationChallenge.
func OpenVerificationWindow(target string) error {
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
