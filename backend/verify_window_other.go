//go:build !windows

package backend

// OpenVerificationWindow is only implemented for Windows. Other platforms
// open the system browser from presentVerificationChallenge. Windows never
// uses that fallback.
func OpenVerificationWindow(target string) error {
	attempt := adoptVerificationAttempt()
	if attempt == nil {
		var err error
		attempt, err = armVerificationRun()
		if err != nil {
			return err
		}
		defer attempt.complete()
	}
	if err := validateVerificationTargetURL(target); err != nil {
		publishVerificationFailure(attempt.id, target, err)
		return err
	}
	publishVerificationFailure(attempt.id, target, errVerificationUnsupported)
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
