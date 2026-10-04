package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Connection checks prove API access, never audio quality or full-track delivery.
type SourceConnection struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Service   string `json:"service"`
	Role      string `json:"role"`
	State     string `json:"state"`
	Message   string `json:"message"`
	CanVerify bool   `json:"can_verify"`
	CheckedAt string `json:"checked_at,omitempty"`
}

var sourceConnectionChecks = struct {
	sync.Mutex
	rows map[string]SourceConnection
}{rows: make(map[string]SourceConnection)}

func SourceConnections() []SourceConnection {
	var rows []SourceConnection
	for _, source := range DownloadSources() {
		if !strings.HasPrefix(source.ID, "antra-") && !strings.HasPrefix(source.ID, "community-") && !strings.HasPrefix(source.ID, "zarz-") && !strings.HasPrefix(source.ID, "jiosaavn-") {
			continue
		}
		rows = append(rows, SourceConnection{ID: source.ID, Name: source.Name, Service: source.Service, Role: source.Role, State: "unchecked", CanVerify: strings.HasPrefix(source.ID, "community-") || strings.HasPrefix(source.ID, "zarz-")})
	}
	for _, source := range ResourceSources() {
		rows = append(rows, SourceConnection{ID: source.ID, Name: source.Name, Service: source.Service, Role: source.Role, State: "unchecked"})
	}
	sourceConnectionChecks.Lock()
	defer sourceConnectionChecks.Unlock()
	for i := range rows {
		if check, ok := sourceConnectionChecks.rows[rows[i].ID]; ok {
			rows[i] = check
		}
	}
	return rows
}

func sourceConnection(id string) (SourceConnection, error) {
	for _, row := range SourceConnections() {
		if row.ID == id {
			return row, nil
		}
	}
	return SourceConnection{}, fmt.Errorf("unknown source")
}

func saveSourceConnection(row SourceConnection) SourceConnection {
	row.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	sourceConnectionChecks.Lock()
	sourceConnectionChecks.rows[row.ID] = row
	sourceConnectionChecks.Unlock()
	return row
}

// Checking a legacy connection never initiates a challenge. Verification is an
// explicit action, or is requested by its existing download/session flow.
func CheckSourceConnection(id string) (SourceConnection, error) {
	row, err := sourceConnection(id)
	if err != nil {
		return row, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ok := false
	switch {
	case strings.HasPrefix(id, "community-"):
		var session *communitySessionRecord
		if !communitySessionMu.TryLock() {
			row.State, row.Message = "pending", "Source verification is already in progress."
			return saveSourceConnection(row), nil
		}
		session, err = loadCommunitySession()
		ok = err == nil && communitySessionValid(session)
		communitySessionMu.Unlock()
	case strings.HasPrefix(id, "zarz-"):
		var store *zarzSessionStore
		if !zarzSessionMu.TryLock() {
			row.State, row.Message = "pending", "Source verification is already in progress."
			return saveSourceConnection(row), nil
		}
		store, err = loadZarzStore()
		if err == nil && store != nil {
			session := store.Sessions[sourceConnectionAppVersion(row.Service)]
			ok = zarzSessionValid(&session)
		}
		zarzSessionMu.Unlock()
	case strings.HasPrefix(id, "antra-"):
		var hit antraSearchHit
		hit, err = antraSearchByISRC(row.Service, "GBAYE0601690")
		ok = err == nil && hit.TrackID != ""
	case id == "jiosaavn-official":
		var track string
		track, _, err = jioSaavnOfficialSearch("Come Together The Beatles", jioSaavnWant{Title: "Come Together", Artist: "The Beatles"})
		ok = err == nil && track != ""
	case id == "jiosaavn-community":
		var track string
		track, _, err = jioSaavnCommunitySearch("Come Together The Beatles", jioSaavnWant{Title: "Come Together", Artist: "The Beatles"})
		ok = err == nil && track != ""
	default:
		ok, err = checkResourceConnection(ctx, id)
	}
	row.State, row.Message = "failed", "The source did not pass its API check. Try again later."
	if row.CanVerify && !ok {
		row.State, row.Message = "authentication_required", "Complete verification in Auralis to connect this source."
	}
	if ok {
		row.State, row.Message = "available", "API access is available; full-track audio has not been verified by this check."
	}
	if isConnectionAccessError(err) {
		row.State, row.Message = "authentication_required", "This source requires access credentials or verification."
	}
	return saveSourceConnection(row), nil
}

func sourceConnectionAppVersion(service string) string {
	provider := map[string]string{"tidal": "tidal", "qobuz": "qbz", "amazon": "amazeamazeamaze"}[service]
	return zarzAppVersionForProvider(provider)
}

func VerifySourceConnection(id string) (SourceConnection, error) {
	row, err := sourceConnection(id)
	if err != nil {
		return row, err
	}
	if !row.CanVerify {
		return row, fmt.Errorf("this source uses an API check or configured credentials")
	}
	if err = TryBeginTrackedDownload(); err != nil {
		return row, err
	}
	defer EndTrackedDownload()
	_, finish := BeginDownloadCancellationScope()
	defer finish()
	if strings.HasPrefix(id, "community-") {
		_, err = ensureCommunitySession()
	} else {
		_, err = ensureZarzSession(sourceConnectionAppVersion(row.Service))
	}
	if err != nil {
		row.State, row.Message = "authentication_required", "Verification did not finish. Complete the source's check and try again."
		if IsDownloadCancelledError(err) {
			row.State, row.Message = "cancelled", "Verification was cancelled."
		}
		return saveSourceConnection(row), nil
	}
	row.State, row.Message = "available", "The source session is connected; full-track audio has not been checked."
	return saveSourceConnection(row), nil
}

func isConnectionAccessError(err error) bool {
	if access, ok := err.(*communityAccessError); ok {
		return access.Status == 401 || access.Status == 403 || access.Status == 428
	}
	return false
}

func checkResourceConnection(ctx context.Context, id string) (bool, error) {
	client := NewSongLinkClientWithContext(ctx)
	if id == "resource-spotify" {
		isrc, err := client.GetISRCDirect("2EqlS6tkEnglzr7tkKAAYD")
		return isrc == "GBAYE0601690", err
	}
	if id == "resource-songstats" {
		links := resolvedTrackLinks{ISRC: "GBAYE0601690"}
		return client.resolveLinksViaSongstats(&links)
	}
	if id == "resource-songlink" {
		links := resolvedTrackLinks{ISRC: "GBAYE0601690"}
		return client.resolveLinksViaDeezerSongLink(&links, "2EqlS6tkEnglzr7tkKAAYD", "")
	}
	raw := ""
	switch id {
	case "resource-lrclib":
		raw = "https://lrclib.net/api/search?artist_name=The%20Beatles&track_name=Come%20Together"
	case "resource-musicbrainz":
		raw = musicBrainzAPIBase + "/recording/?query=isrc:GBAYE0601690&fmt=json&limit=1"
	case "resource-deezer":
		raw = "https://api.deezer.com/track/116348128"
	case "resource-qobuz":
		raw = qobuzAPIBaseURL + "/track/get?track_id=30369895&app_id=" + qobuzZarzCatalogAppID
	case "resource-tidal":
		raw = tidalPublicSearchPath("GBAYE0601690", 1)
	case "samidy-catalog":
		raw = "https://monochrome-api.samidy.com/info/?id=55130631"
	default:
		return false, fmt.Errorf("unknown resource")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", DefaultDownloaderUserAgent)
	req.Header.Set("Accept", "application/json")
	if id == "resource-tidal" {
		req.Header.Set("X-Tidal-Token", tidalPublicToken)
	}
	resp, err := NewSignedHTTPClient(12 * time.Second).Do(req)
	if err != nil {
		return false, fmt.Errorf("resource connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 428 {
		return false, &communityAccessError{Status: resp.StatusCode}
	}
	if resp.StatusCode != 200 {
		return false, fmt.Errorf("resource HTTP %d", resp.StatusCode)
	}
	body, err := readBoundedBody(resp.Body, 2<<20)
	if err != nil {
		return false, err
	}
	return validateResourceResponse(id, body), nil
}

func validateResourceResponse(id string, body []byte) bool {
	if id == "resource-lrclib" {
		var rows []struct {
			TrackName string `json:"trackName"`
		}
		return json.Unmarshal(body, &rows) == nil && len(rows) > 0 && rows[0].TrackName != ""
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil {
		return false
	}
	if id == "resource-tidal" || id == "resource-musicbrainz" {
		key := "items"
		if id == "resource-musicbrainz" {
			key = "recordings"
		}
		var rows []json.RawMessage
		return json.Unmarshal(object[key], &rows) == nil && len(rows) > 0
	}
	if id == "samidy-catalog" {
		var data struct {
			ID int64 `json:"id"`
		}
		return json.Unmarshal(object["data"], &data) == nil && data.ID == 55130631
	}
	var trackID int64
	if json.Unmarshal(object["id"], &trackID) != nil {
		return false
	}
	return id == "resource-deezer" && trackID == 116348128 || id == "resource-qobuz" && trackID == 30369895
}
