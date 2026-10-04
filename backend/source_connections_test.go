package backend

import "testing"

func TestResourceChecksRejectUnrelatedAndErrorPayloads(t *testing.T) {
	for _, test := range []struct {
		id, body string
		want     bool
	}{
		{"resource-deezer", `{"id":116348128}`, true},
		{"resource-deezer", `{"id":1}`, false},
		{"resource-qobuz", `{"error":"login"}`, false},
		{"resource-qobuz", `{"id":30369895}`, true},
		{"samidy-catalog", `{"data":{"id":55130631}}`, true},
		{"samidy-catalog", `{"data":{"id":1}}`, false},
		{"resource-tidal", `{"items":[]}`, false},
		{"resource-lrclib", `<html>verify</html>`, false},
	} {
		if got := validateResourceResponse(test.id, []byte(test.body)); got != test.want {
			t.Errorf("%s: got %v want %v", test.id, got, test.want)
		}
	}
}

func TestSourceConnectionUnknownOrNonInteractiveDoesNotLaunchVerification(t *testing.T) {
	t.Setenv(appDataDirEnv, t.TempDir())
	if _, err := VerifySourceConnection("unknown"); err == nil {
		t.Fatal("unknown source accepted")
	}
	if _, err := VerifySourceConnection("resource-lrclib"); err == nil {
		t.Fatal("resource must not launch interactive verification")
	}
	if _, err := CheckSourceConnection("unknown"); err == nil {
		t.Fatal("unknown source checked")
	}
}
