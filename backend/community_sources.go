package backend

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type CommunitySource struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Service        string `json:"service"`
	Protocol       string `json:"protocol"`
	BaseURL        string `json:"base_url"`
	Enabled        bool   `json:"enabled"`
	CredentialEnv  string `json:"credential_env,omitempty"`
	CredentialType string `json:"credential_type,omitempty"`
}

var communitySourcesMu sync.Mutex
var sourceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)
var sourceEnvPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// defaultCommunitySources returns the production default registry. There are
// no shipped public presets: unvalidated hosted profiles and empty server
// templates were removed, so a clean install starts with an empty collection
// and makes no automatic attempts. Users add their own servers explicitly.
// The historical audit candidates live only in a TEST-ONLY catalog helper
// (backend/community_source_candidates_test.go) and are never shipped.
func defaultCommunitySources() []CommunitySource {
	return nil
}

// retiredCommunitySourceHosts are the canonical hostnames of the removed
// public presets. Any persisted row pointing at one of these hosts is retired
// even when its ID was renamed, and hostname matching is canonical so a
// trailing slash, casing, or path cannot resurrect a failed public host.
var retiredCommunitySourceHosts = map[string]struct{}{
	"monochrome-api.samidy.com":   {},
	"eu-central.monochrome.tf":    {},
	"us-west.monochrome.tf":       {},
	"arran.monochrome.tf":         {},
	"api.monochrome.tf":           {},
	"triton.squid.wtf":            {},
	"wolf.qqdl.site":              {},
	"maus.qqdl.site":              {},
	"vogel.qqdl.site":             {},
	"katze.qqdl.site":             {},
	"hund.qqdl.site":              {},
	"tidal.kinoplus.online":       {},
	"hifi.p1nkhamster.xyz":        {},
	"qobuz.squid.wtf":             {},
	"dabmusic.xyz":                {},
	"dab.yeet.su":                 {},
	"lucida.to":                   {},
}

// retiredCommunitySourceIDs are every shipped profile ID that was removed
// (18 hosted presets plus 4 empty server templates). persisted rows with one
// of these IDs and an empty URL are stale templates and are retired. A stored
// row that reuses one of these IDs but points at a different (custom/local)
// host is preserved as a deliberate custom configuration.
var retiredCommunitySourceIDs = map[string]struct{}{
	"hifi-monochrome-api.samidy.com":   {},
	"hifi-eu-central.monochrome.tf":    {},
	"hifi-us-west.monochrome.tf":       {},
	"hifi-arran.monochrome.tf":         {},
	"hifi-api.monochrome.tf":           {},
	"hifi-triton.squid.wtf":            {},
	"hifi-wolf.qqdl.site":              {},
	"hifi-maus.qqdl.site":              {},
	"hifi-vogel.qqdl.site":             {},
	"hifi-katze.qqdl.site":             {},
	"hifi-hund.qqdl.site":              {},
	"hifi-tidal.kinoplus.online":       {},
	"hifi-hifi.p1nkhamster.xyz":        {},
	"qobuz-squid":                      {},
	"dab-xyz":                          {},
	"dab-yeet":                         {},
	"lucida-qobuz":                     {},
	"lucida-amazon":                    {},
	"qobuz-rest":                       {},
	"qobuz-dl":                         {},
	"bryan-qobuz":                      {},
	"octo-fiesta":                      {},
}

func canonicalCommunitySourceHost(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
}

// isRetiredCommunitySource reports whether a stored row is a removed public
// preset or empty template. Hostname matching is canonical so variations in
// case, trailing slash, or path cannot resurrect a known failed public host.
// Repointed rows (a retired ID with a non-retired custom/local host) return
// false so deliberate custom configurations survive. No network calls.
func isRetiredCommunitySource(source CommunitySource) bool {
	if host := canonicalCommunitySourceHost(source.BaseURL); host != "" {
		if _, ok := retiredCommunitySourceHosts[host]; ok {
			return true
		}
		return false
	}
	if _, ok := retiredCommunitySourceIDs[strings.TrimSpace(source.ID)]; ok {
		return true
	}
	return false
}

func validateCommunitySource(source CommunitySource) (CommunitySource, error) {
	source.ID, source.Name = strings.TrimSpace(source.ID), strings.TrimSpace(source.Name)
	source.Service, source.Protocol = strings.ToLower(strings.TrimSpace(source.Service)), strings.ToLower(strings.TrimSpace(source.Protocol))
	source.BaseURL = strings.TrimRight(strings.TrimSpace(source.BaseURL), "/")
	if !sourceIDPattern.MatchString(source.ID) || source.Name == "" || len(source.Name) > 120 {
		return source, fmt.Errorf("source ID and name are required")
	}
	valid := source.Protocol == "hifi" && source.Service == "tidal" || (source.Protocol == "qobuz-rest" || source.Protocol == "qobuz-dl" || source.Protocol == "dab") && source.Service == "qobuz" || source.Protocol == "lucida" && (source.Service == "qobuz" || source.Service == "amazon")
	if source.Protocol == "subsonic" {
		valid = source.Service == "tidal" || source.Service == "qobuz" || source.Service == "deezer" || source.Service == "apple" || source.Service == "amazon"
	}
	if !valid {
		return source, fmt.Errorf("unsupported source protocol or music service")
	}
	if source.BaseURL != "" {
		u, err := url.Parse(source.BaseURL)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return source, fmt.Errorf("use a server URL without credentials, query parameters, or fragments")
		}
		ip := net.ParseIP(u.Hostname())
		local := u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
		if u.Scheme != "https" && !(u.Scheme == "http" && local) {
			return source, fmt.Errorf("use HTTPS, or HTTP for a local server")
		}
	} else if source.Enabled {
		return source, fmt.Errorf("set a server URL before enabling this source")
	}
	if source.CredentialEnv != "" && !sourceEnvPattern.MatchString(source.CredentialEnv) {
		return source, fmt.Errorf("invalid credential environment variable name")
	}
	switch source.CredentialType {
	case "", "api_key", "bearer", "cookie", "subsonic":
	default:
		return source, fmt.Errorf("unsupported credential type")
	}
	return source, nil
}

func communitySourcesPath() (string, error) {
	dir, err := GetAppDir()
	return filepath.Join(dir, "community-sources.json"), err
}

func ListCommunitySources() ([]CommunitySource, error) {
	communitySourcesMu.Lock()
	defer communitySourcesMu.Unlock()
	return listCommunitySourcesUnlocked()
}

// listCommunitySourcesUnlocked merges stored overrides onto the built-in
// profiles. The production defaults are empty, so this normally returns only
// user-configured servers. Persisted copies of retired public presets are
// excluded (canonical hostname match) so a saved Enabled:true cannot
// resurrect them; repointed IDs with a custom/local host are preserved.
// Callers must already hold communitySourcesMu.
func listCommunitySourcesUnlocked() ([]CommunitySource, error) {
	rows := defaultCommunitySources()
	path, err := communitySourcesPath()
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return rows, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read community source settings")
	}
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("community source settings are too large")
	}
	var stored []CommunitySource
	if json.Unmarshal(body, &stored) != nil {
		return nil, fmt.Errorf("community source settings are invalid JSON; the file was preserved")
	}
	indices := map[string]int{}
	for i, row := range rows {
		indices[row.ID] = i
	}
	seen := map[string]bool{}
	for _, row := range stored {
		if isRetiredCommunitySource(row) {
			continue
		}
		clean, err := validateCommunitySource(row)
		if err != nil {
			return nil, err
		}
		if seen[clean.ID] {
			return nil, fmt.Errorf("duplicate community source ID")
		}
		seen[clean.ID] = true
		if i, ok := indices[clean.ID]; ok {
			rows[i] = clean
		} else {
			indices[clean.ID] = len(rows)
			rows = append(rows, clean)
		}
	}
	return rows, nil
}

func SaveCommunitySources(rows []CommunitySource) error {
	clean := make([]CommunitySource, 0, len(rows))
	seen := map[string]bool{}
	var retired []string
	for _, row := range rows {
		if isRetiredCommunitySource(row) {
			retired = append(retired, strings.TrimSpace(row.ID))
			continue
		}
		source, err := validateCommunitySource(row)
		if err != nil {
			return err
		}
		if seen[source.ID] {
			return fmt.Errorf("duplicate community source ID")
		}
		seen[source.ID] = true
		clean = append(clean, source)
	}
	communitySourcesMu.Lock()
	defer communitySourcesMu.Unlock()
	previous, previousErr := listCommunitySourcesUnlocked()
	path, err := communitySourcesPath()
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return err
	}
	if err := WriteFileAtomic(path, body, 0600); err != nil {
		return err
	}
	// Compare the effective merged profiles. The saved file can omit untouched
	// built-in rows; those rows are still configured and must keep their checks.
	if previousErr == nil {
		if next, nextErr := listCommunitySourcesUnlocked(); nextErr == nil {
			invalidateChangedSources(previous, next)
		}
	}
	for _, id := range retired {
		if strings.TrimSpace(id) != "" {
			dropCommunitySourceEvidence(id)
		}
	}
	return nil
}

func communitySourceSettingsChanged(old, next CommunitySource) bool {
	return old.BaseURL != next.BaseURL || old.Service != next.Service ||
		old.Protocol != next.Protocol || old.CredentialEnv != next.CredentialEnv ||
		old.CredentialType != next.CredentialType
}

func clearCommunityCircuit(id string) {
	if id == "" {
		return
	}
	communityCircuit.Lock()
	delete(communityCircuit.until, id)
	communityCircuit.Unlock()
}

// invalidateChangedSources discards verification, browser sessions, and timing
// evidence for any source whose server or credential configuration changed.
// Unrelated sources, including their circuit pauses, stay as they are.
func invalidateChangedSources(previous, next []CommunitySource) {
	before := map[string]CommunitySource{}
	for _, row := range previous {
		before[row.ID] = row
	}
	after := map[string]CommunitySource{}
	for _, row := range next {
		after[row.ID] = row
		old, existed := before[row.ID]
		if existed && !communitySourceSettingsChanged(old, row) {
			continue
		}
		dropCommunitySourceEvidence(row.ID)
	}
	for id := range before {
		if _, ok := after[id]; !ok {
			dropCommunitySourceEvidence(id)
		}
	}
}

func dropCommunitySourceEvidence(id string) {
	ClearCommunitySourceCheck(id)
	InvalidateSourceRecords(id)
	invalidateCommunityBrowserSession(id)
	clearCommunityCircuit(id)
}

var communityCircuit = struct {
	sync.Mutex
	until map[string]time.Time
}{until: map[string]time.Time{}}

func communitySourceAttempts(service, quality, dest string, track SourceTrack) []sourceDownloadAttempt {
	sources, err := ListCommunitySources()
	if err != nil {
		// Settings surfaces the same error; the download continues on built-in routes.
		fmt.Printf("[CommunitySources] Skipping configured sources: %v\n", err)
		return nil
	}
	var out []sourceDownloadAttempt
	for _, source := range sources {
		if !source.Enabled || source.BaseURL == "" || source.Service != service {
			continue
		}
		communityCircuit.Lock()
		until := communityCircuit.until[source.ID]
		communityCircuit.Unlock()
		if time.Now().Before(until) {
			continue
		}
		out = append(out, sourceDownloadAttempt{id: source.ID, download: func() (string, error) {
			started := time.Now()
			path, err := downloadCommunitySource(source, track, quality, dest)
			if communitySourceNeedsVerification(source, err) {
				// The first download that reaches a protected source opens
				// its check in the app; a passed check is retried at once.
				if verifyErr := verifyCommunitySourceForDownload(source, started); verifyErr == nil {
					path, err = downloadCommunitySource(source, track, quality, dest)
				} else if IsDownloadCancelledError(verifyErr) {
					err = verifyErr
				}
			}
			if err == nil {
				err = requireCommunityCodec(path, quality)
			}
			if err != nil && !IsDownloadCancelledError(err) {
				communityCircuit.Lock()
				communityCircuit.until[source.ID] = time.Now().Add(10 * time.Minute)
				communityCircuit.Unlock()
			}
			return path, err
		}})
	}
	return out
}
