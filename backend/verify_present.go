package backend

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

type verificationAttempt struct {
	id     uint64
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

type verificationRun struct {
	mu      sync.Mutex
	current *verificationAttempt
}

var (
	verificationRunState verificationRun
	verificationRunSeq   uint64
)

func captureVerificationOpContext() context.Context {
	ctx := ActiveDownloadContext()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (a *verificationAttempt) complete() {
	if a == nil {
		return
	}
	a.once.Do(func() {
		a.cancel()
		close(a.done)
	})
	verificationRunState.mu.Lock()
	if verificationRunState.current == a {
		verificationRunState.current = nil
	}
	verificationRunState.mu.Unlock()
}

func armVerificationRun() (*verificationAttempt, error) {
	opCtx := captureVerificationOpContext()
	if opCtx.Err() != nil {
		return nil, ErrDownloadCancelled
	}
	for {
		if opCtx.Err() != nil {
			return nil, ErrDownloadCancelled
		}
		verificationRunState.mu.Lock()
		if cur := verificationRunState.current; cur != nil {
			done := cur.done
			verificationRunState.mu.Unlock()
			select {
			case <-done:
			case <-opCtx.Done():
				return nil, ErrDownloadCancelled
			}
			continue
		}
		ctx, cancel := context.WithCancel(opCtx)
		attempt := &verificationAttempt{
			id:     atomic.AddUint64(&verificationRunSeq, 1),
			ctx:    ctx,
			cancel: cancel,
			done:   make(chan struct{}),
		}
		verificationRunState.current = attempt
		verificationRunState.mu.Unlock()
		return attempt, nil
	}
}

func adoptVerificationAttempt() *verificationAttempt {
	verificationRunState.mu.Lock()
	defer verificationRunState.mu.Unlock()
	return verificationRunState.current
}

func startVerificationWindow(target string) <-chan error {
	errCh := make(chan error, 1)
	if captureVerificationOpContext().Err() != nil {
		errCh <- ErrDownloadCancelled
		return errCh
	}
	if !embeddedVerificationSupported() {
		go func() {
			errCh <- presentVerificationChallenge(target)
		}()
		return errCh
	}
	attempt, err := armVerificationRun()
	if err != nil {
		errCh <- err
		return errCh
	}
	go func() {
		defer attempt.complete()
		defer func() {
			if recovered := recover(); recovered != nil {
				select {
				case errCh <- fmt.Errorf("verification window panic: %v", recovered):
				default:
				}
			}
		}()
		errCh <- presentVerificationChallenge(target)
	}()
	return errCh
}

func presentVerificationChallenge(target string) error {
	// Challenge pages and configured community-source origins share this
	// window. validateVerificationChallengeURL itself stays strict.
	if err := validateVerificationTargetURL(target); err != nil {
		return err
	}
	err := OpenVerificationWindow(target)
	if err == nil || errors.Is(err, errVerificationDismissed) || errors.Is(err, errVerificationClosed) || errors.Is(err, ErrDownloadCancelled) {
		return err
	}
	// Windows has no system-browser or standalone-popup fallback. A missing
	// Edge install or a failed attach stays an error the main window can show.
	if runtime.GOOS == "windows" || embeddedVerificationSupported() {
		return err
	}
	communityBrowserMu.RLock()
	open := communityBrowserOpen
	communityBrowserMu.RUnlock()
	if open == nil {
		return err
	}
	fmt.Printf("Embedded verification window unavailable (%v); opening the system browser\n", err)
	open(target)
	return nil
}

func watchVerificationWindow(errCh <-chan error) <-chan error {
	if !embeddedVerificationSupported() {
		return nil
	}
	return errCh
}

func drainVerificationWindow(errCh <-chan error) {
	if errCh == nil {
		return
	}
	select {
	case <-errCh:
	default:
	}
}
