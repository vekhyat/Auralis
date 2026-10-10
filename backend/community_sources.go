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

// defaultCommunitySources lists only hosts that answered a live check. Each one
// sits behind a browser check, which runs in the app the first time a download
// reaches it.
func defaultCommunitySources() []CommunitySource {
	return []CommunitySource{
		{ID: "hifi-monochrome-api.samidy.com", Name: "Hi-Fi / monochrome-api.samidy.com", Service: "tidal", Protocol: "hifi", BaseURL: "https://monochrome-api.samidy.com", Enabled: true},
		{ID: "lucida-qobuz", Name: "Lucida / Qobuz", Service: "qobuz", Protocol: "lucida", BaseURL: "https://lucida.to", Enabled: true, CredentialEnv: "AURALIS_LUCIDA_COOKIE", CredentialType: "cookie"},
		{ID: "lucida-amazon", Name: "Lucida / Amazon Music", Service: "amazon", Protocol: "lucida", BaseURL: "https://lucida.to", Enabled: true, CredentialEnv: "AURALIS_LUCIDA_COOKIE", CredentialType: "cookie"},
	}
}

// retiredCommunitySources were built in until their hosts stopped resolving or
// answering, mapped to the URL they shipped with. Saved settings can still name
// them; a row that still points there is dropped on load so a dead host never
// returns as a custom source. The server templates shipped without a URL, so a
// user's own server saved under one of those IDs is kept.
var retiredCommunitySources = map[string]string{
	"hifi-eu-central.monochrome.tf": "https://eu-central.monochrome.tf",
	"hifi-us-west.monochrome.tf":    "https://us-west.monochrome.tf",
	"hifi-arran.monochrome.tf":      "https://arran.monochrome.tf",
	"hifi-api.monochrome.tf":        "https://api.monochrome.tf",
	"hifi-triton.squid.wtf":         "https://triton.squid.wtf",
	"hifi-wolf.qqdl.site":           "https://wolf.qqdl.site",
	"hifi-maus.qqdl.site":           "https://maus.qqdl.site",
	"hifi-vogel.qqdl.site":          "https://vogel.qqdl.site",
	"hifi-katze.qqdl.site":          "https://katze.qqdl.site",
	"hifi-hund.qqdl.site":           "https://hund.qqdl.site",
	"hifi-tidal.kinoplus.online":    "https://tidal.kinoplus.online",
	"hifi-hifi.p1nkhamster.xyz":     "https://hifi.p1nkhamster.xyz",
	"qobuz-squid":                   "https://qobuz.squid.wtf",
	"dab-xyz":                       "https://dabmusic.xyz",
	"dab-yeet":                      "https://dab.yeet.su",
	"qobuz-rest":                    "",
	"qobuz-dl":                      "",
	"bryan-qobuz":                   "",
	"octo-fiesta":                   "",
}

func retiredCommunitySource(row CommunitySource) bool {
	dead, ok := retiredCommunitySources[strings.TrimSpace(row.ID)]
	return ok && strings.TrimRight(strings.TrimSpace(row.BaseURL), "/") == dead
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
// profiles. Callers must already hold communitySourcesMu.
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
		if retiredCommunitySource(row) {
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
	for _, row := range rows {
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
