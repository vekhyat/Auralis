package backend

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrDownloadCancelled = errors.New("download cancelled")

// downloadState tracks two different lifetimes:
//   - each BeginDownloadCancellationScope call owns a child context
//   - provider HTTP still observes the shared parent via ActiveDownloadContext
//
// Finishing one download cancels only its child. Force-stop and shutdown cancel
// the parent, which cancels every child and the HTTP calls that still share it.
// The parent context is cleared when the last scope for that parent finishes so
// a later request cannot observe a cancelled context it does not own.
var downloadState = struct {
	sync.Mutex
	accepting bool
	wg        sync.WaitGroup
	parent    context.Context
	cancelAll context.CancelFunc
	scopes    map[context.Context]context.CancelFunc
	stopping  bool
}{
	accepting: true,
	scopes:    map[context.Context]context.CancelFunc{},
}

func BeginDownloadCancellationScope() (context.Context, func()) {
	downloadState.Lock()
	if !downloadState.accepting {
		downloadState.Unlock()
		return alreadyCancelledContext()
	}

	if downloadState.parent == nil || downloadState.parent.Err() != nil || downloadState.stopping {
		downloadState.parent, downloadState.cancelAll = context.WithCancel(context.Background())
		downloadState.stopping = false
	}

	child, childCancel := context.WithCancel(downloadState.parent)
	parent := downloadState.parent
	downloadState.scopes[child] = childCancel
	downloadState.Unlock()

	once := sync.Once{}
	return child, func() {
		once.Do(func() {
			releaseDownloadScope(child, childCancel, parent)
		})
	}
}

func releaseDownloadScope(child context.Context, childCancel context.CancelFunc, parent context.Context) {
	childCancel()

	downloadState.Lock()
	delete(downloadState.scopes, child)
	var cancelParent context.CancelFunc
	if downloadState.parent == parent && len(downloadState.scopes) == 0 {
		cancelParent = downloadState.cancelAll
		downloadState.parent = nil
		downloadState.cancelAll = nil
		downloadState.stopping = false
	}
	downloadState.Unlock()

	if cancelParent != nil {
		cancelParent()
	}
}

func ActiveDownloadContext() context.Context {
	downloadState.Lock()
	defer downloadState.Unlock()

	if downloadState.parent != nil {
		return downloadState.parent
	}
	if !downloadState.accepting || downloadState.stopping {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return context.Background()
}

func ForceStopActiveDownloads() {
	downloadState.Lock()
	cancel := downloadState.cancelAll
	if cancel != nil {
		downloadState.stopping = true
	}
	downloadState.Unlock()

	if cancel != nil {
		cancel()
	}

	CancelQueuedAndDownloadingItems()
	ResetDownloading()
}

func IsDownloadForceStopRequested() bool {
	downloadState.Lock()
	defer downloadState.Unlock()

	return downloadState.stopping
}

func CheckDownloadCancelled() error {
	if !downloadsAccepting() {
		return ErrDownloadCancelled
	}
	ctx := ActiveDownloadContext()
	select {
	case <-ctx.Done():
		return ErrDownloadCancelled
	default:
		return nil
	}
}

func SleepWithDownloadContext(delay time.Duration) error {
	if delay <= 0 {
		return CheckDownloadCancelled()
	}

	ctx := ActiveDownloadContext()
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ErrDownloadCancelled
	case <-timer.C:
		return nil
	}
}

func IsDownloadCancelledError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrDownloadCancelled) || errors.Is(err, context.Canceled)
}

func WrapDownloadCancelled(err error) error {
	if err == nil {
		return nil
	}
	if IsDownloadForceStopRequested() || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w", ErrDownloadCancelled)
	}
	return err
}

// TryBeginTrackedDownload records an in-flight DownloadTrack. Shutdown stops
// accepting new calls and waits for the ones already recorded.
func TryBeginTrackedDownload() error {
	downloadState.Lock()
	defer downloadState.Unlock()
	if !downloadState.accepting {
		return ErrDownloadCancelled
	}
	downloadState.wg.Add(1)
	return nil
}

func EndTrackedDownload() {
	downloadState.wg.Done()
}

// StopAcceptingDownloadsAndDrain rejects new downloads, cancels the active
// parent context, and waits until tracked DownloadTrack calls return.
func StopAcceptingDownloadsAndDrain() {
	downloadState.Lock()
	downloadState.accepting = false
	downloadState.Unlock()

	ForceStopActiveDownloads()
	downloadState.wg.Wait()
}

func downloadsAccepting() bool {
	downloadState.Lock()
	defer downloadState.Unlock()
	return downloadState.accepting
}

func alreadyCancelledContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx, func() {}
}

func resetDownloadLifecycleForTest() {
	downloadState.Lock()
	cancel := downloadState.cancelAll
	downloadState.parent = nil
	downloadState.cancelAll = nil
	downloadState.scopes = map[context.Context]context.CancelFunc{}
	downloadState.stopping = false
	downloadState.accepting = true
	downloadState.wg = sync.WaitGroup{}
	downloadState.Unlock()
	if cancel != nil {
		cancel()
	}
}
