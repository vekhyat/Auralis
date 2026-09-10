package backend

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	communitySessionSkew   = 5 * time.Minute
	communityVerifyTimeout = 5 * time.Minute
)

type communitySessionRecord struct {
	InstallID     string `json:"install_id"`
	SessionID     string `json:"session_id,omitempty"`
	SessionSecret string `json:"session_secret,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

type communitySessionExchange struct {
	SessionID     string `json:"session_id"`
	SessionSecret string `json:"session_secret"`
	ExpiresAt     string `json:"expires_at"`
}

var (
	communitySessionMu        sync.Mutex
	communitySessionMem       *communitySessionRecord
	communityBrowserMu        sync.RWMutex
	communityBrowserOpen      func(string)
	communityWindowForeground func()
)

func SetCommunityVerificationHandlers(openBrowser func(string), foreground func()) {
	communityBrowserMu.Lock()
	communityBrowserOpen = openBrowser
	communityWindowForeground = foreground
	communityBrowserMu.Unlock()
}

func communitySessionPath() (string, error) {
	dir, err := EnsureAppDir()
	if err != nil {
		return "", err
	}
	_ = os.Chmod(dir, 0700)
	return filepath.Join(dir, "community_session.json"), nil
}

func legacyCommunitySessionPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".spotiflac", "community_session.json"), nil
}

func parseCommunitySessionData(data []byte) *communitySessionRecord {
	record := &communitySessionRecord{}
	_ = json.Unmarshal(data, record)
	return record
}

func importCommunitySession(primary []byte, primaryOK bool, legacy []byte, legacyOK bool) (*communitySessionRecord, bool) {
	record := &communitySessionRecord{}
	if primaryOK {
		record = parseCommunitySessionData(primary)
	}
	if communitySessionValid(record) {
		return record, false
	}
	if !legacyOK {
		return record, false
	}
	legacyRecord := parseCommunitySessionData(legacy)
	if communitySessionValid(legacyRecord) {
		return legacyRecord, true
	}
	if strings.TrimSpace(record.InstallID) == "" && strings.TrimSpace(legacyRecord.InstallID) != "" {
		return legacyRecord, true
	}
	return record, false
}

func cloneCommunitySession(record *communitySessionRecord) *communitySessionRecord {
	if record == nil {
		return &communitySessionRecord{}
	}
	copy := *record
	return &copy
}

func loadCommunitySession() (*communitySessionRecord, error) {
	path, err := communitySessionPath()
	if err != nil {
		return nil, err
	}
	primary, primaryErr := os.ReadFile(path)
	var legacy []byte
	legacyOK := false
	if legacyPath, legacyPathErr := legacyCommunitySessionPath(); legacyPathErr == nil {
		if data, readErr := os.ReadFile(legacyPath); readErr == nil {
			legacy = data
			legacyOK = true
		}
	}
	record, migrated := importCommunitySession(primary, primaryErr == nil, legacy, legacyOK)
	if communitySessionMem != nil {
		if strings.TrimSpace(record.InstallID) == "" {
			record.InstallID = communitySessionMem.InstallID
		}
		if communitySessionMem.InstallID == record.InstallID && communitySessionMem.SessionID != "" && record.SessionID == "" {
			record.SessionID = communitySessionMem.SessionID
			record.SessionSecret = communitySessionMem.SessionSecret
			record.ExpiresAt = communitySessionMem.ExpiresAt
		}
	}
	created := false
	if strings.TrimSpace(record.InstallID) == "" {
		record.InstallID = communityRandomHex(16)
		created = true
	}
	if migrated || created || primaryErr != nil {
		if err := saveCommunitySession(record); err != nil {
			return nil, err
		}
		if migrated {
			fmt.Println("Imported community session from legacy .spotiflac profile")
		}
	} else {
		communitySessionMem = cloneCommunitySession(record)
	}
	return record, nil
}

func saveCommunitySession(record *communitySessionRecord) error {
	path, err := communitySessionPath()
	if err != nil {
		return err
	}
	communitySessionMem = cloneCommunitySession(record)
	data, err := json.MarshalIndent(record, "", "  ")
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
	return restrictPrivateFile(path)
}

func communitySessionValid(record *communitySessionRecord) bool {
	if record == nil {
		return false
	}
	return sessionCredentialsValid(record.SessionID, record.SessionSecret, record.ExpiresAt, communitySessionSkew)
}

func ensureCommunitySession() (*communitySessionRecord, error) {
	communitySessionMu.Lock()
	defer communitySessionMu.Unlock()
	record, err := loadCommunitySession()
	if err != nil {
		return nil, err
	}
	if communitySessionValid(record) {
		return record, nil
	}
	fmt.Println("Community (SpotBye) API requires a one-time verification in your browser...")
	grant, err := runCommunityVerification(record)
	if err != nil {
		return nil, err
	}
	exchanged, err := exchangeCommunityGrant(record, grant)
	if err != nil {
		return nil, err
	}
	record.SessionID = exchanged.SessionID
	record.SessionSecret = exchanged.SessionSecret
	record.ExpiresAt = exchanged.ExpiresAt
	if err := saveCommunitySession(record); err != nil {
		return nil, err
	}
	if record.ExpiresAt != "" {
		fmt.Printf("Community session saved until %s\n", record.ExpiresAt)
	} else {
		fmt.Println("Community session saved")
	}
	return record, nil
}

func clearCommunitySessionCredentials() {
	communitySessionMu.Lock()
	defer communitySessionMu.Unlock()
	record, err := loadCommunitySession()
	if err != nil {
		return
	}
	record.SessionID = ""
	record.SessionSecret = ""
	record.ExpiresAt = ""
	communitySessionMem = cloneCommunitySession(record)
	_ = saveCommunitySession(record)
}

func runCommunityVerification(record *communitySessionRecord) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to start verification callback: %w", err)
	}
	defer listener.Close()
	callbackState := communityRandomHex(16)
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/session-grant?state=%s", listener.Addr().(*net.TCPAddr).Port, callbackState)
	grantCh := make(chan string, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("/session-grant", func(w http.ResponseWriter, req *http.Request) {
		if !hmac.Equal([]byte(req.URL.Query().Get("state")), []byte(callbackState)) {
			http.Error(w, "Invalid verification callback state", http.StatusBadRequest)
			return
		}
		grant := strings.TrimSpace(req.URL.Query().Get("grant"))
		if grant == "" {
			http.Error(w, "Missing verification grant", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Verified</title><style>*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:20px;background:#000;background-image:radial-gradient(circle,rgba(255,255,255,.2) 1.5px,transparent 1.5px);background-size:30px 30px;color:#f5f5f5;font:14px/1.5 Inter,sans-serif}main{text-align:center}.icon{width:48px;height:48px;margin:0 auto 20px;display:grid;place-items:center;border-radius:50%;background:#fff;color:#000;font-size:22px}h1{margin:0 0 6px;font-size:24px;letter-spacing:-.035em}p{margin:0;color:#888}</style></head><body><main><div class="icon">&#10003;</div><h1>Verified</h1><p>Returning to Auralis...</p></main><script>setTimeout(()=>window.close(),700)</script></body></html>`)
		select {
		case grantCh <- grant:
		default:
		}
		communityBrowserMu.RLock()
		foreground := communityWindowForeground
		communityBrowserMu.RUnlock()
		if foreground != nil {
			foreground()
		}
	})
	server.Handler = mux
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	verifyBaseURL := GetCommunityVerifyURL()
	if verifyBaseURL == "" {
		return "", fmt.Errorf("verification endpoint is unavailable")
	}
	bootstrap, err := url.Parse(verifyBaseURL + "/bootstrap")
	if err != nil {
		return "", err
	}
	query := bootstrap.Query()
	query.Set("install_id", record.InstallID)
	query.Set("app_version", communityAppVersion())
	query.Set("platform", "desktop")
	bootstrap.RawQuery = query.Encode()
	resp, err := NewSignedHTTPClient(15 * time.Second).Get(bootstrap.String())
	if err != nil {
		return "", fmt.Errorf("verification bootstrap failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("verification bootstrap returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		ChallengeURL string `json:"challenge_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32*1024)).Decode(&result); err != nil {
		return "", err
	}
	challengeURL, err := url.Parse(result.ChallengeURL)
	if err != nil {
		return "", fmt.Errorf("verification service returned an invalid challenge URL")
	}
	if err := validateVerificationChallengeURL(challengeURL.String()); err != nil {
		return "", fmt.Errorf("verification service returned an invalid challenge URL")
	}
	challengeQuery := challengeURL.Query()
	challengeQuery.Set("cb", callbackURL)
	challengeURL.RawQuery = challengeQuery.Encode()
	communityBrowserMu.RLock()
	openBrowser := communityBrowserOpen
	communityBrowserMu.RUnlock()

	// Embedded window first; system browser only as a fallback. The local
	// HTTP listener receives the grant regardless of which surface shows the
	// challenge, since the callback URL is an absolute 127.0.0.1 address.
	windowDone := make(chan struct{})
	go func() {
		defer close(windowDone)
		if err := OpenVerificationWindow(challengeURL.String()); err != nil {
			fmt.Printf("Embedded verification window unavailable (%v); falling back to the system browser\n", err)
			if openBrowser == nil {
				return
			}
			openBrowser(challengeURL.String())
		}
	}()

	select {
	case grant := <-grantCh:
		CloseVerificationWindow()
		<-windowDone
		foregroundAuralisWindow()
		return grant, nil
	case <-time.After(communityVerifyTimeout):
		CloseVerificationWindow()
		<-windowDone
		return "", fmt.Errorf("verification timed out")
	}
}

func exchangeCommunityGrant(record *communitySessionRecord, grant string) (*communitySessionExchange, error) {
	payload, _ := json.Marshal(map[string]string{
		"grant": grant, "install_id": record.InstallID,
		"app_version": communityAppVersion(), "platform": "desktop",
	})
	verifyBaseURL := GetCommunityVerifyURL()
	if verifyBaseURL == "" {
		return nil, fmt.Errorf("verification endpoint is unavailable")
	}
	resp, err := NewSignedHTTPClient(15*time.Second).Post(verifyBaseURL+"/session/exchange", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("session exchange returned HTTP %d", resp.StatusCode)
	}
	var raw struct {
		SessionID     string          `json:"session_id"`
		SessionSecret string          `json:"session_secret"`
		ExpiresAt     json.RawMessage `json:"expires_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32*1024)).Decode(&raw); err != nil {
		return nil, err
	}
	result := communitySessionExchange{
		SessionID:     strings.TrimSpace(raw.SessionID),
		SessionSecret: strings.TrimSpace(raw.SessionSecret),
		ExpiresAt:     jsonFlexibleString(raw.ExpiresAt),
	}
	if result.SessionID == "" || result.SessionSecret == "" {
		return nil, fmt.Errorf("session exchange response is incomplete")
	}
	return &result, nil
}

func signCommunityRequest(req *http.Request, record *communitySessionRecord) error {
	body := []byte{}
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return err
		}
		req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	bodySum := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(bodySum[:])
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	nonce := communityRandomHex(12)
	parsedTimestamp, _ := time.Parse("2006-01-02T15:04:05.000Z", timestamp)
	window := parsedTimestamp.Unix() / 300
	rollingInput := fmt.Sprintf("%d:%s", window, record.SessionID)
	rollingKey := communityHMAC([]byte(record.SessionSecret), []byte(rollingInput))
	signingInput := strings.Join([]string{
		"SPOTIFLAC-HMAC-V1", req.Method, req.URL.EscapedPath(), "", bodyHash,
		timestamp, nonce, record.SessionID, communityAppVersion(), "desktop",
	}, "\n")
	signature := base64.RawURLEncoding.EncodeToString(communityHMAC(rollingKey, []byte(signingInput)))
	req.Header.Set("X-Sig-Session", record.SessionID)
	req.Header.Set("X-Sig-Timestamp", timestamp)
	req.Header.Set("X-Sig-Nonce", nonce)
	req.Header.Set("X-Sig-Body-SHA256", bodyHash)
	req.Header.Set("X-Sig-Signature", signature)
	req.Header.Set("X-Sig-App-Version", communityAppVersion())
	req.Header.Set("X-Sig-Platform", "desktop")
	return nil
}

func communityAppVersion() string {
	version := strings.TrimSpace(AppVersion)
	if version == "" || version == "Unknown" {
		return "unknown"
	}
	return version
}
func communityRandomHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
func communityHMAC(key, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil)
}
