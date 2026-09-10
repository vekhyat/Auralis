package backend

import (
	"testing"
	"time"
)

func TestBeginDownloadCancellationScopeDoesNotReuseCancelledContext(t *testing.T) {
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
