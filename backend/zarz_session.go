package backend

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	zarzBaseURL           = "https://api.zarz.moe/v2"
	zarzSchemeLabel       = "ZARZ-HMAC-V1"
	zarzHeaderPrefix      = "X-Zarz-"
	zarzPlatform          = "extension"
	zarzTimeWindowSeconds = 300
	zarzSessionSkew       = 5 * time.Minute
	zarzVerifyTimeout     = 5 * time.Minute
	zarzMaxProviderTries  = 4
)

type zarzSessionRecord struct {
	InstallID     string `json:"install_id"`
	AppVersion    string `json:"app_version,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	SessionSecret string `json:"session_secret,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

type zarzSessionStore struct {
	InstallID string                       `json:"install_id"`
	Sessions  map[string]zarzSessionRecord `json:"sessions"`
}

type zarzAPIError struct {
	Status            int
	Message           string
	Code              string
	Origin            string
	Retryable         bool
	RetryMode         string
	RetryAfterSeconds int
	Body              []byte
}

func (e *zarzAPIError) Error() string {
	if e == nil {
		return "zarz api error"
	}
	if strings.TrimSpace(e.Message) != "" {
		return fmt.Sprintf("zarz HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("zarz HTTP %d: %s", e.Status, zarzPreviewBody(e.Body, 180))
}

func (e *zarzAPIError) sameOperationRetry() bool {
	return e != nil &&
		e.Status == http.StatusServiceUnavailable &&
		e.Retryable &&
		e.RetryMode == "same_operation"
}

func (e *zarzAPIError) isRateLimited() bool {
	if e == nil {
		return false
	}
	if e.Status == http.StatusTooManyRequests {
		return true
	}
	combined := strings.ToLower(strings.TrimSpace(e.Message + " " + e.Code))
	return strings.Contains(combined, "server busy") ||
		strings.Contains(combined, "rate limit") ||
		strings.Contains(combined, "too many requests") ||
		strings.Contains(combined, "retry shortly")
}

func (e *zarzAPIError) shouldRetry() bool {
	return e.sameOperationRetry() || e.isRateLimited()
}

func parseZarzAPIError(status int, body []byte) *zarzAPIError {
	err := &zarzAPIError{Status: status, Body: body}
	var parsed struct {
		Error             string `json:"error"`
		Code              string `json:"code"`
		Origin            string `json:"origin"`
		Retryable         bool   `json:"retryable"`
		RetryMode         string `json:"retry_mode"`
		RetryAfterSeconds int    `json:"retry_after_seconds"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		err.Message = parsed.Error
		err.Code = parsed.Code
		err.Origin = parsed.Origin
		err.Retryable = parsed.Retryable
		err.RetryMode = parsed.RetryMode
		err.RetryAfterSeconds = parsed.RetryAfterSeconds
	}
	return err
}

func zarzAppVersionForProvider(provider string) string {
	switch provider {
	case "qbz":
		return "qobuz-web@1.1.0"
	case "amazeamazeamaze":
		return "amzn@2.2.0"
	case "dzr":
		return "deezer@1.3.0"
	default:
		return "tidal-web@1.1.0"
	}
}

func zarzUserAgentFor(appVersion string) string {
	return "SpotiFLAC-Mobile/" + appVersion
}

type zarzSessionExchange struct {
	SessionID     string `json:"session_id"`
	SessionSecret string `json:"session_secret"`
	ExpiresAt     string `json:"expires_at"`
	ChallengeURL  string `json:"challenge_url"`
	AuthURL       string `json:"auth_url"`
	ChallengeID   string `json:"challenge_id"`
	ServerNonce   string `json:"server_nonce"`
}

var (
	zarzSessionMu sync.Mutex
	zarzStoreMem  *zarzSessionStore
	zarzHTTP      = &http.Client{Timeout: 60 * time.Second}
)

func zarzSessionPath() (string, error) {
	dir, err := EnsureAppDir()
	if err != nil {
		return "", err
	}
	_ = os.Chmod(dir, 0700)
	return filepath.Join(dir, "zarz_session.json"), nil
}

func legacyZarzSessionPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".spotiflac", "zarz_session.json"), nil
}

func parseZarzStoreData(data []byte) *zarzSessionStore {
	store := &zarzSessionStore{Sessions: map[string]zarzSessionRecord{}}
	_ = json.Unmarshal(data, store)
	if store.Sessions == nil {
		store.Sessions = map[string]zarzSessionRecord{}
	}
	if len(store.Sessions) > 0 {
		return store
	}
	var legacy zarzSessionRecord
	if json.Unmarshal(data, &legacy) == nil && strings.TrimSpace(legacy.SessionID) != "" {
		if strings.TrimSpace(store.InstallID) == "" {
			store.InstallID = legacy.InstallID
		}
		legacy.AppVersion = "tidal-web@1.1.0"
		store.Sessions["tidal-web@1.1.0"] = legacy
	}
	return store
}

func zarzStoreHasValidSession(store *zarzSessionStore) bool {
	if store == nil {
		return false
	}
	for _, record := range store.Sessions {
		item := record
		if zarzSessionValid(&item) {
			return true
		}
	}
	return false
}

func importZarzStore(primary []byte, primaryOK bool, legacy []byte, legacyOK bool) (*zarzSessionStore, bool) {
	store := &zarzSessionStore{Sessions: map[string]zarzSessionRecord{}}
	if primaryOK {
		store = parseZarzStoreData(primary)
	}
	if zarzStoreHasValidSession(store) {
		return store, false
	}
	if !legacyOK {
		return store, false
	}
	legacyStore := parseZarzStoreData(legacy)
	if zarzStoreHasValidSession(legacyStore) {
		return legacyStore, true
	}
	if strings.TrimSpace(store.InstallID) == "" && strings.TrimSpace(legacyStore.InstallID) != "" {
		return legacyStore, true
	}
	return store, false
}

func loadZarzStore() (*zarzSessionStore, error) {
	if zarzStoreMem != nil && strings.TrimSpace(zarzStoreMem.InstallID) != "" {
		if zarzStoreMem.Sessions == nil {
			zarzStoreMem.Sessions = map[string]zarzSessionRecord{}
		}
		return zarzStoreMem, nil
	}
	path, err := zarzSessionPath()
	if err != nil {
		return nil, err
	}
	primary, primaryErr := os.ReadFile(path)
	var legacy []byte
	legacyOK := false
	if legacyPath, legacyPathErr := legacyZarzSessionPath(); legacyPathErr == nil {
		if data, readErr := os.ReadFile(legacyPath); readErr == nil {
			legacy = data
			legacyOK = true
		}
	}
	store, migrated := importZarzStore(primary, primaryErr == nil, legacy, legacyOK)
	if store.Sessions == nil {
		store.Sessions = map[string]zarzSessionRecord{}
	}
	created := false
	if strings.TrimSpace(store.InstallID) == "" {
		store.InstallID = zarzRandomHex(16)
		created = true
	}
	zarzStoreMem = store
	if migrated || created || primaryErr != nil {
		if err := saveZarzStore(store); err != nil {
			return nil, err
		}
		if migrated {
			fmt.Println("Imported Zarz session from existing SpotiFLAC data folder")
		}
	}
	return store, nil
}

func saveZarzStore(store *zarzSessionStore) error {
	path, err := zarzSessionPath()
	if err != nil {
		return err
	}
	if store.Sessions == nil {
		store.Sessions = map[string]zarzSessionRecord{}
	}
	zarzStoreMem = store
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return os.Chmod(path, 0600)
}

func zarzSessionValid(record *zarzSessionRecord) bool {
	if record == nil {
		return false
	}
	return sessionCredentialsValid(record.SessionID, record.SessionSecret, record.ExpiresAt, zarzSessionSkew)
}

func ensureZarzSession(appVersion string) (*zarzSessionRecord, error) {
	zarzSessionMu.Lock()
	defer zarzSessionMu.Unlock()
	store, err := loadZarzStore()
	if err != nil {
		return nil, err
	}
	record := store.Sessions[appVersion]
	record.InstallID = store.InstallID
	record.AppVersion = appVersion
	if zarzSessionValid(&record) {
		return &record, nil
	}
	if err := runZarzBootstrapLocked(&record, appVersion); err != nil {
		return nil, err
	}
	if !zarzSessionValid(&record) {
		return nil, fmt.Errorf("zarz session is not available")
	}
	store.Sessions[appVersion] = record
	if err := saveZarzStore(store); err != nil {
		return nil, err
	}
	if record.ExpiresAt != "" {
		fmt.Printf("Zarz session saved for %s until %s\n", appVersion, record.ExpiresAt)
	} else {
		fmt.Printf("Zarz session saved for %s\n", appVersion)
	}
	return &record, nil
}

func clearZarzSessionCredentials(appVersion string) {
	zarzSessionMu.Lock()
	defer zarzSessionMu.Unlock()
	store, err := loadZarzStore()
	if err != nil {
		return
	}
	if appVersion == "" {
		store.Sessions = map[string]zarzSessionRecord{}
	} else {
		delete(store.Sessions, appVersion)
	}
	_ = saveZarzStore(store)
}

func runZarzBootstrapLocked(record *zarzSessionRecord, appVersion string) error {
	bootstrap, err := url.Parse(zarzBaseURL + "/bootstrap")
	if err != nil {
		return err
	}
	query := bootstrap.Query()
	query.Set("install_id", record.InstallID)
	query.Set("app_version", appVersion)
	query.Set("platform", zarzPlatform)
	bootstrap.RawQuery = query.Encode()

	req, err := http.NewRequest(http.MethodGet, bootstrap.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", zarzUserAgentFor(appVersion))
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("zarz bootstrap failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("zarz bootstrap returned HTTP %d", resp.StatusCode)
	}

	boot, err := decodeZarzSessionExchange(body)
	if err != nil {
		return fmt.Errorf("invalid zarz bootstrap response: %w", err)
	}
	if boot.SessionID != "" && boot.SessionSecret != "" {
		record.SessionID = boot.SessionID
		record.SessionSecret = boot.SessionSecret
		record.ExpiresAt = boot.ExpiresAt
		record.AppVersion = appVersion
		return nil
	}

	challenge := strings.TrimSpace(boot.AuthURL)
	if challenge == "" {
		challenge = strings.TrimSpace(boot.ChallengeURL)
	}
	if challenge == "" && strings.TrimSpace(boot.ChallengeID) != "" {
		challenge = zarzBaseURL + "/challenge?id=" + url.QueryEscape(strings.TrimSpace(boot.ChallengeID))
	}
	if challenge == "" {
		return fmt.Errorf("zarz bootstrap did not return a session or challenge: %s", zarzPreviewBody(body, 240))
	}
	grant, err := completeZarzChallenge(record, challenge)
	if err != nil {
		return err
	}
	exchanged, err := exchangeZarzGrant(record, grant, appVersion)
	if err != nil {
		return err
	}
	record.SessionID = exchanged.SessionID
	record.SessionSecret = exchanged.SessionSecret
	record.ExpiresAt = exchanged.ExpiresAt
	record.AppVersion = appVersion
	return nil
}

func completeZarzChallenge(_ *zarzSessionRecord, challenge string) (string, error) {
	parsed, err := url.Parse(challenge)
	if err != nil || parsed.Scheme != "https" {
		return "", fmt.Errorf("zarz returned an invalid challenge URL")
	}
	if err := RegisterAuralisProtocol(); err != nil {
		fmt.Printf("Could not register auralis:// handler: %v\n", err)
	}

	callbackState := zarzRandomHex(16)
	query := parsed.Query()
	query.Set("cb", "spotiflac://session-grant?cb_version=v2grant&state="+callbackState)
	parsed.RawQuery = query.Encode()

	communityBrowserMu.RLock()
	openBrowser := communityBrowserOpen
	communityBrowserMu.RUnlock()
	if openBrowser == nil {
		return "", fmt.Errorf("browser integration is not ready for zarz verification")
	}

	stopPoll := make(chan struct{})
	defer close(stopPoll)
	go pollZarzChallengeGrant(query.Get("id"), stopPoll)

	fmt.Println("Zarz API requires a one-time verification in your browser...")
	openBrowser(parsed.String())

	grant, err := waitForZarzGrant(zarzVerifyTimeout)
	if err != nil {
		return "", err
	}
	fmt.Println("Zarz verification grant received")
	return grant, nil
}

func extractZarzGrant(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "grant=") {
		value, err := url.QueryUnescape(raw[6:])
		if err != nil {
			return strings.TrimSpace(raw[6:])
		}
		return strings.TrimSpace(value)
	}
	if parsed, err := url.Parse(raw); err == nil {
		if grant := strings.TrimSpace(parsed.Query().Get("grant")); grant != "" {
			return grant
		}
	}
	if idx := strings.Index(raw, "grant="); idx >= 0 {
		value := raw[idx+6:]
		if amp := strings.IndexAny(value, "&\n "); amp >= 0 {
			value = value[:amp]
		}
		decoded, err := url.QueryUnescape(value)
		if err != nil {
			return strings.TrimSpace(value)
		}
		return strings.TrimSpace(decoded)
	}
	if !strings.ContainsAny(raw, " \t\n") && len(raw) >= 16 {
		return raw
	}
	return ""
}

func exchangeZarzGrant(record *zarzSessionRecord, grant, appVersion string) (*zarzSessionExchange, error) {
	payload, _ := json.Marshal(map[string]string{
		"grant":       grant,
		"install_id":  record.InstallID,
		"app_version": appVersion,
		"platform":    zarzPlatform,
	})
	req, err := http.NewRequest(http.MethodPost, zarzBaseURL+"/session/exchange", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", zarzUserAgentFor(appVersion))
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("zarz session exchange returned HTTP %d", resp.StatusCode)
	}
	result, err := decodeZarzSessionExchange(body)
	if err != nil {
		return nil, err
	}
	if result.SessionID == "" || result.SessionSecret == "" {
		return nil, fmt.Errorf("zarz session exchange response is incomplete")
	}
	return result, nil
}

func decodeZarzSessionExchange(body []byte) (*zarzSessionExchange, error) {
	var raw struct {
		SessionID     string          `json:"session_id"`
		SessionSecret string          `json:"session_secret"`
		ExpiresAt     json.RawMessage `json:"expires_at"`
		ChallengeURL  string          `json:"challenge_url"`
		AuthURL       string          `json:"auth_url"`
		ChallengeID   string          `json:"challenge_id"`
		ServerNonce   string          `json:"server_nonce"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return &zarzSessionExchange{
		SessionID:     strings.TrimSpace(raw.SessionID),
		SessionSecret: strings.TrimSpace(raw.SessionSecret),
		ExpiresAt:     jsonFlexibleString(raw.ExpiresAt),
		ChallengeURL:  strings.TrimSpace(raw.ChallengeURL),
		AuthURL:       strings.TrimSpace(raw.AuthURL),
		ChallengeID:   strings.TrimSpace(raw.ChallengeID),
		ServerNonce:   strings.TrimSpace(raw.ServerNonce),
	}, nil
}

func zarzSignedJSON(appVersion, method, requestPath string, payload any, extraHeaders map[string]string) ([]byte, error) {
	body := []byte{}
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = encoded
	}

	record, err := ensureZarzSession(appVersion)
	if err != nil {
		return nil, err
	}

	status, respBody, err := doZarzSignedRequest(record, appVersion, method, requestPath, body, extraHeaders)
	if err != nil {
		return nil, err
	}
	if isGatewaySessionFailure(status, respBody) {
		fmt.Printf("Zarz session for %s was rejected; completing one-time verification again...\n", appVersion)
		clearZarzSessionCredentials(appVersion)
		record, err = ensureZarzSession(appVersion)
		if err != nil {
			return nil, err
		}
		status, respBody, err = doZarzSignedRequest(record, appVersion, method, requestPath, body, extraHeaders)
		if err != nil {
			return nil, err
		}
	}
	if status != http.StatusOK {
		return nil, parseZarzAPIError(status, respBody)
	}
	return respBody, nil
}

func doZarzSignedRequest(record *zarzSessionRecord, appVersion, method, requestPath string, body []byte, extraHeaders map[string]string) (int, []byte, error) {
	fullURL := strings.TrimRight(zarzBaseURL, "/") + "/" + strings.TrimLeft(requestPath, "/")
	parsed, err := url.Parse(fullURL)
	if err != nil {
		return 0, nil, err
	}
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	nonce := zarzRandomHex(12)
	bodySum := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(bodySum[:])
	parsedTs, _ := time.Parse("2006-01-02T15:04:05.000Z", ts)
	window := parsedTs.Unix() / zarzTimeWindowSeconds
	rollingInput := fmt.Sprintf("%d:%s", window, record.SessionID)
	rollingKey := base64.RawURLEncoding.EncodeToString(zarzHMAC([]byte(record.SessionSecret), []byte(rollingInput)))
	signingInput := strings.Join([]string{
		zarzSchemeLabel,
		method,
		parsed.EscapedPath(),
		"",
		bodyHash,
		ts,
		nonce,
		record.SessionID,
		appVersion,
		zarzPlatform,
	}, "\n")
	sig := base64.RawURLEncoding.EncodeToString(zarzHMAC([]byte(rollingKey), []byte(signingInput)))

	req, err := http.NewRequest(method, fullURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", zarzUserAgentFor(appVersion))
	req.Header.Set(zarzHeaderPrefix+"Session", record.SessionID)
	req.Header.Set(zarzHeaderPrefix+"Timestamp", ts)
	req.Header.Set(zarzHeaderPrefix+"Nonce", nonce)
	req.Header.Set(zarzHeaderPrefix+"Body-SHA256", bodyHash)
	req.Header.Set(zarzHeaderPrefix+"Signature", sig)
	req.Header.Set(zarzHeaderPrefix+"App-Version", appVersion)
	req.Header.Set(zarzHeaderPrefix+"Platform", zarzPlatform)
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}

	resp, err := zarzHTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}

func mintZarzTicket(provider, resourceType, id string) (string, error) {
	resource := strings.ToLower(provider + ":" + resourceType + ":" + id)
	sum := sha256.Sum256([]byte(resource))
	payload := map[string]string{
		"capability":    "download_ticket",
		"provider":      provider,
		"resource_hash": hex.EncodeToString(sum[:]),
	}
	body, err := zarzSignedJSON(zarzAppVersionForProvider(provider), http.MethodPost, "/tickets", payload, nil)
	if err != nil {
		return "", fmt.Errorf("zarz ticket request: %w", err)
	}
	var parsed struct {
		TicketID string `json:"ticket_id"`
		Ticket   string `json:"ticket"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("invalid zarz ticket response: %w", err)
	}
	ticket := strings.TrimSpace(parsed.TicketID)
	if ticket == "" {
		ticket = strings.TrimSpace(parsed.Ticket)
	}
	if ticket == "" {
		return "", fmt.Errorf("zarz ticket response missing ticket_id")
	}
	return ticket, nil
}

func zarzRetryDelay(err error) time.Duration {
	wait := 8 * time.Second
	var apiErr *zarzAPIError
	if errors.As(err, &apiErr) && apiErr.RetryAfterSeconds > 0 {
		wait = time.Duration(apiErr.RetryAfterSeconds) * time.Second
	}
	if wait > 20*time.Second {
		wait = 20 * time.Second
	}
	if wait < time.Second {
		wait = time.Second
	}
	return wait
}

func zarzHMAC(key, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil)
}

func zarzPreviewBody(body []byte, maxLen int) string {
	preview := strings.TrimSpace(string(body))
	if maxLen > 0 && len(preview) > maxLen {
		return preview[:maxLen] + "..."
	}
	return preview
}

func zarzRandomHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
