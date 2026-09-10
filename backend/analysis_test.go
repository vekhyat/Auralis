package backend

import "testing"

func TestRejectLongAnalysis(t *testing.T) {
	if err := rejectLongAnalysis(60); err != nil {
		t.Fatalf("60s should be allowed: %v", err)
	}
	if err := rejectLongAnalysis(8*60 + 1); err == nil {
		t.Fatal("expected error above 8 minutes")
	}
}
