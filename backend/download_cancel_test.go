package backend

import (
	"testing"
	"time"
)

func TestBeginDownloadCancellationScopeDoesNotReuseCancelledContext(t *testing.T) {
	resetDownloadLifecycleForTest()
	t.Cleanup(resetDownloadLifecycleForTest)

	ctxA, finishA := BeginDownloadCancellationScope()
	ForceStopActiveDownloads()
	if err := ctxA.Err(); err == nil {
		t.Fatal("force stop should cancel the in-flight scope")
	}

	ctxB, finishB := BeginDownloadCancellationScope()
	defer finishB()
	if err := ctxB.Err(); err != nil {
		t.Fatalf("new download reused a cancelled context: %v", err)
	}
	if ctxA == ctxB {
		t.Fatal("new scope must not share the cancelled context")
	}

	finishA()
	select {
	case <-ctxB.Done():
		t.Fatal("finishing the old scope cancelled the new one")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestDownloadScopeFinishDoesNotCancelSibling(t *testing.T) {
	resetDownloadLifecycleForTest()
	t.Cleanup(resetDownloadLifecycleForTest)

	ctxA, finishA := BeginDownloadCancellationScope()
	ctxB, finishB := BeginDownloadCancellationScope()
	defer finishB()

	finishA()
	if ctxA.Err() == nil {
		t.Fatal("finished scope should cancel its own context")
	}
	select {
	case <-ctxB.Done():
		t.Fatal("finishing one download cancelled the other")
	default:
	}
	if err := ActiveDownloadContext().Err(); err != nil {
		t.Fatalf("provider context cancelled while another download is active: %v", err)
	}
}

func TestStopAcceptingDownloadsCancelsAndRejectsNewWork(t *testing.T) {
	resetDownloadLifecycleForTest()
	t.Cleanup(resetDownloadLifecycleForTest)

	if err := TryBeginTrackedDownload(); err != nil {
		t.Fatal(err)
	}
	ctx, finish := BeginDownloadCancellationScope()
	started := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer EndTrackedDownload()
		defer finish()
		close(started)
		<-ctx.Done()
	}()
	<-started

	drained := make(chan struct{})
	go func() {
		StopAcceptingDownloadsAndDrain()
		close(drained)
	}()

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight download was not cancelled")
	}
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish draining")
	}
	if err := TryBeginTrackedDownload(); err == nil {
		EndTrackedDownload()
		t.Fatal("new download accepted after shutdown")
	}
}
