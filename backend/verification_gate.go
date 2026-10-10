package backend

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// errVerificationSkipped means the user already closed this source's check
// during this run. The download moves on to the next source without asking.
var errVerificationSkipped = errors.New("verification was skipped for this source")

// verificationDeclined remembers, per source, that the user closed its check.
// A source asks at most once per run; a completed check saves a session that
// later downloads reuse, so it is not asked again either.
var verificationDeclined = struct {
	sync.Mutex
	keys map[string]bool
}{keys: map[string]bool{}}

func verificationSkipped(key string) bool {
	verificationDeclined.Lock()
	defer verificationDeclined.Unlock()
	return verificationDeclined.keys[key]
}

// noteVerificationOutcome records a closed or failed check. Cancelling the
// whole download is not a decision about the source, so it is not recorded.
func noteVerificationOutcome(key string, err error) {
	if err == nil || IsDownloadCancelledError(err) || CheckDownloadCancelled() != nil {
		return
	}
	verificationDeclined.Lock()
	verificationDeclined.keys[key] = true
	verificationDeclined.Unlock()
}

func resetVerificationDeclinedForTest() {
	verificationDeclined.Lock()
	verificationDeclined.keys = map[string]bool{}
	verificationDeclined.Unlock()
	sourceVerifiedAt.Lock()
	sourceVerifiedAt.at = map[string]time.Time{}
	sourceVerifiedAt.Unlock()
}

// onDemandVerifyMu runs one download-time check at a time. Album tracks that
// reach the same source together wait here instead of each opening a window.
var onDemandVerifyMu sync.Mutex

// sourceVerifiedAt records when each source last passed its check, so a track
// that was refused before another track finished the check retries quietly.
var sourceVerifiedAt = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

// communitySourceNeedsVerification reports a refusal that the source's own
// browser check can fix. Sources that need an API key are not asked.
func communitySourceNeedsVerification(source CommunitySource, err error) bool {
	var access *communityAccessError
	if !errors.As(err, &access) {
		return false
	}
	action, _ := communityVerificationCapability(source)
	return action == "verify"
}

// verifyCommunitySourceForDownload runs a source's check inside the download
// that reached it. refusedAt is when the source refused that download; nil
// means the caller should try the source again.
func verifyCommunitySourceForDownload(source CommunitySource, refusedAt time.Time) error {
	key := "source:" + source.ID
	onDemandVerifyMu.Lock()
	defer onDemandVerifyMu.Unlock()
	sourceVerifiedAt.Lock()
	verified := sourceVerifiedAt.at[source.ID]
	sourceVerifiedAt.Unlock()
	if verified.After(refusedAt) {
		return nil
	}
	if verificationSkipped(key) {
		return errVerificationSkipped
	}
	if err := CheckDownloadCancelled(); err != nil {
		return err
	}
	// Another kind of check is open; this track moves on without asking.
	if err := communityVerificationPreflight(); err != nil {
		return err
	}
	result, err := runCommunitySourceVerification(source, true)
	if err == nil && result.State != "available" {
		err = fmt.Errorf("%s was not verified", source.Name)
		if result.State == "cancelled" {
			err = errVerificationSkipped
		}
	}
	noteVerificationOutcome(key, err)
	if err != nil {
		return err
	}
	sourceVerifiedAt.Lock()
	sourceVerifiedAt.at[source.ID] = time.Now()
	sourceVerifiedAt.Unlock()
	return nil
}
