package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

// newAuditOuterFailure converts an outer CheckCommunitySource error (lease
// busy, unknown source, temp-dir failure) into audit evidence. The message is
// redacted like any other check so URLs and environment secrets never reach
// the persisted report.
func newAuditOuterFailure(id string, err error, secrets ...string) CommunitySourceCheck {
	msg := "audit check failed"
	if err != nil {
		msg = err.Error()
	}
	return CommunitySourceCheck{
		ID:        id,
		State:     "failed",
		Message:   redactCommunityMessage(msg, secrets...),
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// auditOuterFailureForID redacts an outer error for a source known only by ID
// by looking up its configured credential for secret redaction. Lookup
// failures fall back to URL redaction so the error is never lost.
func auditOuterFailureForID(id string, err error) CommunitySourceCheck {
	if source, findErr := findCommunitySource(id); findErr == nil {
		return newAuditOuterFailure(id, err, communitySecretValues(source, nil)...)
	}
	return newAuditOuterFailure(id, err)
}

// replaceCommunityAuditRow swaps the stored row for updated.ID, appending when
// the ID is new. Callers sort before persisting.
func replaceCommunityAuditRow(rows []CommunitySourceCheck, updated CommunitySourceCheck) []CommunitySourceCheck {
	for i, row := range rows {
		if row.ID == updated.ID {
			rows[i] = updated
			return rows
		}
	}
	return append(rows, updated)
}

// refreshAvailableWithAudio runs full download/decode validation for every
// API-available row and returns the merged set. Non-available rows pass
// through untouched. Audio results (validated, failed,
// authentication_required, cancelled) always replace the earlier available
// status; outer errors become redacted failed evidence instead of being
// dropped.
func refreshAvailableWithAudio(rows []CommunitySourceCheck, check func(string, bool) (CommunitySourceCheck, error)) []CommunitySourceCheck {
	merged := make([]CommunitySourceCheck, len(rows))
	copy(merged, rows)
	for _, row := range rows {
		if row.State != "available" {
			continue
		}
		audio, err := check(row.ID, true)
		if err != nil {
			audio = auditOuterFailureForID(row.ID, err)
		}
		merged = replaceCommunityAuditRow(merged, audio)
	}
	return merged
}

// writeCommunityAdaptersReport sorts rows by ID and persists them with
// owner-only permissions. Sorting keeps the report deterministic across the
// concurrent probe phase.
func writeCommunityAdaptersReport(root string, rows []CommunitySourceCheck) error {
	sorted := make([]CommunitySourceCheck, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	body, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "community-adapters.json"), body, 0600)
}

// checkTestCatalogCandidate mirrors CheckCommunitySource but takes the audit
// candidate directly instead of looking it up in the production registry.
// Production defaults are empty, so the live audit seeds from the TEST-ONLY
// catalog helper; this keeps the audit reproducible without shipping any
// fixture catalog in the app. No fresh production defaults are used.
func checkTestCatalogCandidate(source CommunitySource, download bool) (result CommunitySourceCheck, resultErr error) {
	if source.BaseURL == "" {
		return CommunitySourceCheck{ID: source.ID, State: "not_configured", Message: "Requires a configured server", CheckedAt: time.Now().UTC().Format(time.RFC3339)}, nil
	}
	if download {
		if err := TryBeginTrackedDownload(); err != nil {
			return result, err
		}
		defer EndTrackedDownload()
	}
	started := time.Now()
	result = CommunitySourceCheck{ID: source.ID, State: "failed", CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	defer func() {
		result.LatencyMS = float64(time.Since(started)) / float64(time.Millisecond)
		saveCommunityCheck(result)
	}()
	track, quality := sourceCheckFixture(source)
	var err error
	if !download {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err = probeCommunitySourceAPI(ctx, source)
		if err == nil {
			result.State = "available"
			result.Message = "API responded; audio download has not been verified"
		}
	} else {
		_, finish := BeginDownloadCancellationScope()
		defer finish()
		timer := time.AfterFunc(3*time.Minute, ForceStopActiveDownloads)
		defer timer.Stop()
		dir, makeErr := os.MkdirTemp("", "auralis-community-sample-")
		if makeErr != nil {
			return result, makeErr
		}
		defer os.RemoveAll(dir)
		path, downloadErr := downloadCommunitySource(source, track, map[string]string{"tidal": "LOSSLESS", "qobuz": "6", "amazon": "16", "deezer": "16", "apple": "16"}[source.Service], filepath.Join(dir, "sample.flac"))
		err = downloadErr
		if err == nil {
			err = validateCommunitySample(path, track.Duration, quality)
		}
		if err == nil {
			result.State = "validated"
			result.AudioVerified = true
			result.Message = "Full-track audio and decoding passed"
		}
		recordSourceOutcome(source.ID, source.Service, quality, time.Since(started), err == nil, err)
	}
	if err != nil {
		result.Message = redactCommunityMessage(err.Error(), communitySecretValues(source, nil)...)
		var access *communityAccessError
		switch {
		case IsDownloadCancelledError(err):
			result.State = "cancelled"
		case errors.As(err, &access):
			action, instruction := communityVerificationCapability(source)
			result.Action = action
			if action == "configure" {
				result.State = "configuration_required"
				result.Message = redactCommunityMessage(instruction, communitySecretValues(source, nil)...)
			} else {
				result.State = "authentication_required"
			}
		case errors.Is(err, errInvalidCommunityCredential):
			action, instruction := communityVerificationCapability(source)
			if action == "configure" {
				result.State = "configuration_required"
				result.Action = action
				result.Message = redactCommunityMessage(instruction, communitySecretValues(source, nil)...)
			}
		}
		if !IsDownloadCancelledError(err) {
			communityCircuit.Lock()
			communityCircuit.until[source.ID] = time.Now().Add(10 * time.Minute)
			communityCircuit.Unlock()
		}
	} else {
		communityCircuit.Lock()
		delete(communityCircuit.until, source.ID)
		communityCircuit.Unlock()
	}
	return result, nil
}

func TestLiveCommunitySourceAdapters(t *testing.T) {
	if os.Getenv("AURALIS_LIVE_SOURCE_CHECK") != "1" {
		t.Skip("live community adapters are opt-in")
	}
	root := os.Getenv("AURALIS_LIVE_RESULTS_DIR")
	if !filepath.IsAbs(root) {
		t.Fatal("isolated results directory required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(appDataDirEnv, filepath.Join(root, "app-data"))
	t.Setenv("USERPROFILE", filepath.Join(root, "profile"))
	// Production defaults are intentionally empty. Seed the audit from the
	// TEST-ONLY catalog helper so the historical candidates stay
	// reproducible without shipping any fixture catalog in the app.
	sources := testCommunityCandidateCatalog()
	byID := map[string]CommunitySource{}
	for _, source := range sources {
		byID[source.ID] = source
	}
	// Audit every hosted candidate even when disabled. Enabled only gates
	// automatic download attempts; only sources without a server URL are
	// reported as not_configured.
	var rows []CommunitySourceCheck
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, source := range sources {
		if source.BaseURL == "" {
			mu.Lock()
			rows = append(rows, CommunitySourceCheck{ID: source.ID, State: "not_configured", Message: "Requires a configured server"})
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(source CommunitySource) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			row, err := checkTestCatalogCandidate(source, false)
			if err != nil {
				row = newAuditOuterFailure(source.ID, err, communitySecretValues(source, nil)...)
			}
			mu.Lock()
			rows = append(rows, row)
			mu.Unlock()
		}(source)
	}
	wg.Wait()
	if err := writeCommunityAdaptersReport(root, rows); err != nil {
		t.Fatal(err)
	}
	// Full-track audio and decoding for each API-available source. The final
	// persisted report must carry these results, not the earlier API-only
	// status. Failures stay as audit evidence: a passing test never means
	// all providers work.
	checkByID := func(id string, download bool) (CommunitySourceCheck, error) {
		source, ok := byID[id]
		if !ok {
			return CommunitySourceCheck{}, errors.New("unknown audit candidate")
		}
		row, err := checkTestCatalogCandidate(source, download)
		if err != nil {
			return newAuditOuterFailure(id, err, communitySecretValues(source, nil)...), nil
		}
		return row, nil
	}
	merged := refreshAvailableWithAudio(rows, checkByID)
	for _, row := range merged {
		if row.State == "validated" || row.AudioVerified {
			body, _ := json.Marshal(row)
			t.Log(string(body))
			continue
		}
		// Log failures too so the audit trail is visible without reading the file.
		for _, probe := range rows {
			if probe.ID == row.ID && probe.State == "available" && row.State != "available" {
				body, _ := json.Marshal(row)
				t.Log(string(body))
				break
			}
		}
	}
	if err := writeCommunityAdaptersReport(root, merged); err != nil {
		t.Fatal(err)
	}
}
