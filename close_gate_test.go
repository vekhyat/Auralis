package main

import "testing"

func TestCloseWaitsForSaveAndCanRetryAfterFailure(t *testing.T) {
	gate := &closeGate{}
	if gate.request() != allowClose {
		t.Fatal("startup without frontend should be closable")
	}
	gate.ready()
	if gate.request() != flushBeforeClose {
		t.Fatal("ready frontend did not get a save request")
	}
	if gate.request() != waitForCloseFlush {
		t.Fatal("duplicate close emitted another save")
	}
	if gate.complete(false) {
		t.Fatal("failed save approved close")
	}
	if gate.request() != flushBeforeClose {
		t.Fatal("failed save could not be retried")
	}
	if !gate.complete(true) {
		t.Fatal("confirmed save did not approve close")
	}
	if gate.request() != allowClose {
		t.Fatal("saved queue still prevents close")
	}
}

func TestCloseCannotBeApprovedBeforeARequest(t *testing.T) {
	gate := &closeGate{}
	if gate.complete(true) {
		t.Fatal("unsolicited close was approved")
	}
	gate.ready()
	if gate.complete(true) {
		t.Fatal("ready is not a close request")
	}
	if gate.request() != flushBeforeClose {
		t.Fatal("unexpected ready state")
	}
}
