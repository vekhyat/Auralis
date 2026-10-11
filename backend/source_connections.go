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

// API checks validate a fixture response. They prove API access, never audio
// quality or full-track delivery.
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

// SourceConnections lists the built-in download and resource rows. Identity
// is by exact built-in ID: a user-configured source never appears here,
// whatever custom ID it uses, and never inherits a built-in check. No row is
// verifiable: the community/Zarz session flow is retired, so CanVerify is
// always false and verification stops with an explanatory error.
func SourceConnections() []SourceConnection {
	var rows []SourceConnection
	for _, source := range builtInDownloadSources() {
		rows = append(rows, SourceConnection{ID: source.ID, Name: source.Name, Service: source.Service, Role: source.Role, State: "unchecked"})
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

// CheckSourceConnection runs the built-in API check for an exact built-in
// ID. A user-configured source never reaches a built-in check: unknown IDs
// fail the lookup above before any session or network work.
func CheckSourceConnection(id string) (SourceConnection, error) {
	row, err := sourceConnection(id)
	if err != nil {
		return row, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ok := false
	switch id {
	case "antra-qobuz", "antra-deezer", "antra-apple":
		var hit antraSearchHit
		hit, err = antraSearchByISRC(row.Service, "GBAYE0601690")
		ok = err == nil && hit.TrackID != "" && sourceCheckRecordingMatches(hit.ISRC, hit.Title, hit.Artist)
	case "jiosaavn-official":
		var track string
		track, _, err = jioSaavnOfficialSearch("Come Together The Beatles", jioSaavnWant{Title: "Come Together", Artist: "The Beatles"})
		ok = err == nil && track != ""
	default:
		ok, err = checkResourceConnection(ctx, id)
	}
	row.State, row.Message = "failed", "The source did not pass its API check. Try again later."
	if ok {
		row.State, row.Message = "available", "API access is available; full-track audio has not been verified by this check."
	}
	if isConnectionAccessError(err) {
		row.State, row.Message = "authentication_required", "This source requires access credentials or verification."
	}
	return saveSourceConnection(row), nil
}

// No connection row is verifiable: the community/Zarz session flow is
// retired, so every row reports CanVerify == false and verification stops
// here before any browser or network work.
func VerifySourceConnection(id string) (SourceConnection, error) {
	row, err := sourceConnection(id)
	if err != nil {
		return row, err
	}
	return row, fmt.Errorf("this source uses an API check or configured credentials")
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
	case "resource-tidal":
		raw = tidalPublicSearchPath("GBAYE0601690", 1)
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
			TrackName  string `json:"trackName"`
			ArtistName string `json:"artistName"`
		}
		if json.Unmarshal(body, &rows) != nil {
			return false
		}
		for _, row := range rows {
			if sourceCheckRecordingMatches("", row.TrackName, row.ArtistName) {
				return true
			}
		}
		return false
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
		var rows []struct {
			ISRC   string   `json:"isrc"`
			ISRCs  []string `json:"isrcs"`
			Title  string   `json:"title"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
			ArtistCredit []struct {
				Name string `json:"name"`
			} `json:"artist-credit"`
		}
		if json.Unmarshal(object[key], &rows) != nil {
			return false
		}
		for _, row := range rows {
			if len(row.ISRCs) > 0 {
				for _, isrc := range row.ISRCs {
					if sourceCheckRecordingMatches(isrc, "", "") {
						return true
					}
				}
				continue
			}
			artist := row.Artist.Name
			if len(row.ArtistCredit) == 1 {
				artist = row.ArtistCredit[0].Name
			}
			if sourceCheckRecordingMatches(row.ISRC, row.Title, artist) {
				return true
			}
		}
		return false
	}
	var trackID int64
	if json.Unmarshal(object["id"], &trackID) != nil {
		return false
	}
	return id == "resource-deezer" && trackID == 116348128
}

// An explicit recording identifier must match; metadata is the fallback only
// when the API omits ISRC. A nonempty search result alone proves no identity.
func sourceCheckRecordingMatches(isrc, title, artist string) bool {
	if strings.TrimSpace(isrc) != "" {
		return strings.EqualFold(strings.TrimSpace(isrc), "GBAYE0601690")
	}
	return normalizedSourceTitle(title) == "come together" && strings.EqualFold(strings.TrimSpace(artist), "The Beatles")
}
