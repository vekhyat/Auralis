package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	communityManualVerificationTimeout = 5 * time.Minute
	communityVerificationProbeTimeout  = 45 * time.Second
	communityVerificationCaptureBudget = 15 * time.Second
)

var (
	errBrowserSessionPersistenceUnavailable = errors.New("browser session persistence is unavailable on this platform")
	errInAppVerificationUnavailable         = errors.New("in-app source verification is unavailable on this platform")
)

// CommunityVerificationAction tells the UI whether Verify should open the
// in-app browser or ask for an environment credential.
type CommunityVerificationAction struct {
	ID      string `json:"id"`
	Action  string `json:"action"`
	Message string `json:"message"`
}

type storedCookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Expires  int64  `json:"expires"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
	HostOnly bool   `json:"hostOnly"`
}

type persistedSourceSession struct {
	Scope     string         `json:"scope"`
	Origin    string         `json:"origin"`
	Cookies   []storedCookie `json:"cookies"`
	UserAgent string         `json:"user_agent,omitempty"`
}

type persistedBrowserSessions struct {
	Sources map[string]persistedSourceSession `json:"sources"`
}

type cdpCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"httpOnly"`
	HostOnly bool    `json:"hostOnly"`
	Session  bool    `json:"session"`
}

type sourceVerificationHooks struct {
	open    func(string) <-chan error
	close   func()
	capture func(string) ([]storedCookie, error)
	context func() context.Context
	timeout time.Duration
}

type manualSourceVerification struct {
	id          string
	source      CommunitySource
	scope       string
	done        chan struct{}
	cancel      chan struct{}
	finishOnce  sync.Once
	cancelOnce  sync.Once
	mu          sync.Mutex
	finished    bool
	runID       uint64
	result      CommunitySourceCheck
	probeCancel context.CancelFunc
}

var (
	sourceVerificationHook sourceVerificationHooks
	manualMu               sync.Mutex
	manualCurrent          *manualSourceVerification
	pendingBrowser         struct {
		sync.Mutex
		id        string
		cookies   []storedCookie
		active    bool
		userAgent string
	}
	browserSessionCache struct {
		sync.Mutex
		path    string
		ok      bool
		records map[string]persistedSourceSession
	}
)

func CommunitySourceVerificationAction(id string) (CommunityVerificationAction, error) {
	source, err := findCommunitySource(id)
	if err != nil {
		return CommunityVerificationAction{}, err
	}
	if _, err := validateCommunitySource(source); err != nil {
		return CommunityVerificationAction{}, err
	}
	action, message := communityVerificationCapability(source)
	return CommunityVerificationAction{ID: source.ID, Action: action, Message: message}, nil
}

func communityVerificationCapability(source CommunitySource) (string, string) {
	switch source.CredentialType {
	case "api_key", "bearer", "subsonic":
		return "configure", "This source needs an API key, bearer token, or Subsonic account in the environment. Browser verification does not sign it in."
	case "cookie":
		return "verify", "Open the source, complete its login or browser check, then confirm. The source stays unverified until its API answers."
	}
	switch source.Protocol {
	case "hifi", "dab", "lucida":
		return "verify", "Open the source, complete its login or browser check, then confirm. The source stays unverified until its API answers."
	default:
		return "configure", "This source needs a configured server credential. Browser verification does not sign it in."
	}
}

// VerifyCommunitySource opens the isolated verification browser for a cookie
// or hosted source and waits until confirm, cancel, or timeout. API-key,
// bearer, and Subsonic sources do not open a browser and are not marked
// verified. AudioVerified stays false: this checks the API only.
func VerifyCommunitySource(id string) (result CommunitySourceCheck, resultErr error) {
	source, err := findCommunitySource(id)
	if err != nil {
		return result, err
	}
	if source, err = validateCommunitySource(source); err != nil {
		return result, err
	}
	action, message := communityVerificationCapability(source)
	if action != "verify" {
		return CommunitySourceCheck{
			ID: source.ID, State: "configuration_required", Message: message,
			CheckedAt: time.Now().UTC().Format(time.RFC3339), Action: "configure",
		}, nil
	}
	if !embeddedVerificationSupported() && sourceVerificationHook.open == nil {
		return result, errInAppVerificationUnavailable
	}
	if currentManual() != nil {
		return result, fmt.Errorf("a source verification window is already open")
	}
	if sourceVerificationHook.open == nil {
		if attempt := adoptVerificationAttempt(); attempt != nil {
			return result, fmt.Errorf("a verification window is already open")
		}
	}
	if err := TryBeginTrackedDownload(); err != nil {
		return result, err
	}
	defer EndTrackedDownload()

	target, err := communityVerificationURL(source)
	if err != nil {
		return result, err
	}
	if err := validateCommunityVerificationTarget(target); err != nil {
		return result, err
	}
	session, err := beginManualSession(source)
	if err != nil {
		return result, err
	}
	defer clearManualSession(session)

	started := time.Now()
	host := communityHost(source.BaseURL)
	defer func() { session.present(false, false, "") }()

	var previousRun uint64
	if sourceVerificationHook.open == nil {
		if attempt := adoptVerificationAttempt(); attempt != nil {
			previousRun = attempt.id
		}
	}
	readyStop := make(chan struct{})
	defer close(readyStop)
	windowErr := openCommunityVerification(session, target)
	go watchCommunityVerification(session, host, previousRun, readyStop)

	timer := time.NewTimer(communityManualTimeout())
	defer timer.Stop()
	select {
	case <-session.done:
		result = session.snapshot()
	case <-session.cancel:
		stopCommunityVerification(session, true)
		if !session.isFinished() {
			session.finish(cancelledVerificationCheck(source))
		}
		result = session.snapshot()
	case err := <-windowErr:
		if !session.isFinished() {
			// The window already ended. Closing again must not cancel a newer run.
			stopCommunityVerification(session, false)
			session.finish(windowFailureCheck(source, err))
		}
		result = session.snapshot()
	case <-sourceVerificationContext().Done():
		stopCommunityVerification(session, true)
		if !session.isFinished() {
			session.finish(cancelledVerificationCheck(source))
		}
		result = session.snapshot()
	case <-timer.C:
		stopCommunityVerification(session, true)
		if !session.isFinished() {
			session.finish(failedVerificationCheck(source, "Verification timed out before the source API accepted the session."))
		}
		result = session.snapshot()
	}
	result.LatencyMS = float64(time.Since(started)) / float64(time.Millisecond)
	if result.ID == "" {
		result.ID = source.ID
	}
	if result.State == "available" || result.State == "failed" {
		saveCommunityCheck(result)
	}
	return result, nil
}

// ConfirmSourceVerification reads cookies for the active community source and
// accepts the session only when that source's API answers. With no community
// session it returns false, nil so an older grant flow can finish by itself.
// A rejected API leaves the verification window open.
func ConfirmSourceVerification() (bool, error) {
	session := currentManual()
	if session == nil || session.isFinished() {
		return false, nil
	}
	fresh, err := findCommunitySource(session.id)
	if err != nil || communitySourceScope(fresh) != session.scope {
		session.finish(failedVerificationCheck(session.source, "Source configuration changed. Start verification again."))
		closeCommunityVerification(session)
		return false, fmt.Errorf("source configuration changed; start verification again")
	}
	origin, err := communityVerificationURL(fresh)
	if err != nil {
		return false, err
	}
	cookies, err := captureCommunityVerificationCookies(communityCookieRequestURL(origin))
	if err != nil {
		return false, redactVerificationError(err, fresh, nil)
	}
	cookies = sanitizeStoredCookies(origin, cookies)
	userAgent := ""
	if sourceVerificationHook.capture == nil {
		userAgent, err = platformCaptureCommunityUserAgent()
		if err != nil {
			return false, fmt.Errorf("could not read the verification browser identity")
		}
	}
	stopPending := usePendingBrowserIdentity(fresh.ID, cookies, userAgent)
	defer stopPending()

	ctx, cancel := context.WithTimeout(sourceVerificationContext(), communityVerificationProbeTimeout)
	session.setProbeCancel(cancel)
	defer cancel()
	probeErr := probeCommunitySourceAPI(ctx, fresh)
	if session.isFinished() {
		return false, nil
	}
	if probeErr != nil {
		return false, verificationRejectedError(fresh, cookies, probeErr)
	}
	if len(cookies) > 0 {
		if err := saveCommunityBrowserIdentity(fresh, cookies, userAgent); err != nil {
			return false, redactVerificationError(err, fresh, cookies)
		}
	}
	result := CommunitySourceCheck{
		ID: fresh.ID, State: "available", Action: "verify", AudioVerified: false,
		Message:   "API responded; audio download has not been verified",
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if !session.markSuccess(result) {
		invalidateCommunityBrowserSession(fresh.ID)
		return false, fmt.Errorf("verification was cancelled before the source API accepted the session")
	}
	closeCommunityVerification(session)
	session.present(false, false, "")
	return true, nil
}

// SourceVerificationNeedsConfirmation reports an open community browser session.
// Grant verification does not use it.
func SourceVerificationNeedsConfirmation() bool {
	session := currentManual()
	return session != nil && !session.isFinished()
}

func communityManualTimeout() time.Duration {
	if sourceVerificationHook.timeout > 0 {
		return sourceVerificationHook.timeout
	}
	return communityManualVerificationTimeout
}

func sourceVerificationContext() context.Context {
	if sourceVerificationHook.context != nil {
		if ctx := sourceVerificationHook.context(); ctx != nil {
			return ctx
		}
	}
	if ctx := ActiveDownloadContext(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func openCommunityVerification(session *manualSourceVerification, target string) <-chan error {
	if sourceVerificationHook.open != nil {
		return sourceVerificationHook.open(target)
	}
	// presentVerificationChallenge now accepts validateVerificationTargetURL, so
	// this uses startVerificationWindow. validateVerificationChallengeURL stays
	// strict. A different run is left alone until it ends or this session stops.
	errCh := make(chan error, 1)
	go func() {
		if attempt := adoptVerificationAttempt(); attempt != nil && attempt.id != session.runIdentifier() {
			select {
			case <-attempt.done:
			case <-session.done:
				errCh <- errVerificationClosed
				return
			case <-sourceVerificationContext().Done():
				errCh <- ErrDownloadCancelled
				return
			}
			if session.isFinished() {
				errCh <- errVerificationClosed
				return
			}
		}
		inner := startVerificationWindow(target)
		errCh <- <-inner
	}()
	return errCh
}

func stopCommunityVerification(session *manualSourceVerification, cancelRun bool) {
	if sourceVerificationHook.close != nil {
		sourceVerificationHook.close()
		return
	}
	if session == nil {
		return
	}
	id := session.runIdentifier()
	if id == 0 {
		return
	}
	if cancelRun {
		_ = CancelInAppVerification(id)
	}
	attempt := adoptVerificationAttempt()
	if attempt != nil && attempt.id == id {
		CloseVerificationWindow()
	}
}

func closeCommunityVerification(session *manualSourceVerification) {
	if sourceVerificationHook.close != nil {
		sourceVerificationHook.close()
		return
	}
	if session == nil {
		return
	}
	id := session.runIdentifier()
	if id == 0 {
		return
	}
	attempt := adoptVerificationAttempt()
	if attempt == nil || attempt.id != id {
		return
	}
	CloseVerificationWindow()
}

func captureCommunityVerificationCookies(origin string) ([]storedCookie, error) {
	if sourceVerificationHook.capture != nil {
		return sourceVerificationHook.capture(origin)
	}
	return platformCaptureCommunityCookies(origin)
}

func validateCommunityVerificationTarget(raw string) error {
	if sourceVerificationHookValidate != nil {
		return sourceVerificationHookValidate(raw)
	}
	return validateVerificationTargetURL(raw)
}

// sourceVerificationHookValidate is set only by tests. Production uses the
// native worker's exact-origin check.
var sourceVerificationHookValidate func(string) error

func beginManualSession(source CommunitySource) (*manualSourceVerification, error) {
	manualMu.Lock()
	defer manualMu.Unlock()
	if manualCurrent != nil && !manualCurrent.isFinished() {
		return nil, fmt.Errorf("a source verification window is already open")
	}
	session := &manualSourceVerification{
		id:     source.ID,
		source: source,
		scope:  communitySourceScope(source),
		done:   make(chan struct{}),
		cancel: make(chan struct{}),
	}
	manualCurrent = session
	return session, nil
}

func currentManual() *manualSourceVerification {
	manualMu.Lock()
	defer manualMu.Unlock()
	return manualCurrent
}

func clearManualSession(session *manualSourceVerification) {
	if session == nil {
		return
	}
	manualMu.Lock()
	if manualCurrent == session {
		manualCurrent = nil
	}
	manualMu.Unlock()
}

func signalManualVerificationCancel(id string) {
	session := currentManual()
	if session == nil || session.isFinished() {
		return
	}
	if id != "" && id != session.id {
		return
	}
	session.requestCancel()
}

func (m *manualSourceVerification) requestCancel() {
	if m == nil {
		return
	}
	m.cancelOnce.Do(func() { close(m.cancel) })
}

func (m *manualSourceVerification) setProbeCancel(cancel context.CancelFunc) {
	m.mu.Lock()
	m.probeCancel = cancel
	m.mu.Unlock()
}

func (m *manualSourceVerification) markSuccess(result CommunitySourceCheck) bool {
	accepted := false
	m.finishOnce.Do(func() {
		accepted = true
		m.mu.Lock()
		m.finished = true
		m.result = result
		cancel := m.probeCancel
		m.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		saveCommunityCheck(result)
		close(m.done)
	})
	return accepted
}

func (m *manualSourceVerification) finish(result CommunitySourceCheck) {
	m.finishOnce.Do(func() {
		m.mu.Lock()
		m.finished = true
		m.result = result
		cancel := m.probeCancel
		m.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		close(m.done)
	})
}

func (m *manualSourceVerification) isFinished() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finished
}

func (m *manualSourceVerification) setRunID(id uint64) {
	if m == nil || id == 0 {
		return
	}
	m.mu.Lock()
	if m.runID == 0 {
		m.runID = id
	}
	m.mu.Unlock()
}

func (m *manualSourceVerification) runIdentifier() uint64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runID
}

func (m *manualSourceVerification) snapshot() CommunitySourceCheck {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.result
}

func (m *manualSourceVerification) present(active, ready bool, errText string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	finished := m.finished
	id := m.runID
	title := strings.TrimSpace(m.source.Name)
	host := communityHost(m.source.BaseURL)
	m.mu.Unlock()
	if id == 0 || (finished && active) {
		return
	}
	if title == "" {
		title = verificationGenericTitle
	}
	publishVerificationPresentation(VerificationPresentation{
		ID: id, Active: active, Title: title, Host: host, Ready: ready, Error: errText,
	})
}

func watchCommunityVerification(session *manualSourceVerification, host string, previousRun uint64, stop <-chan struct{}) {
	if sourceVerificationHook.open != nil {
		return
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	for {
		select {
		case <-session.done:
			return
		case <-stop:
			return
		case <-ticker.C:
			if session.runIdentifier() == 0 {
				snap := GetVerificationPresentation()
				if snap.Active && snap.ID != 0 && snap.ID != previousRun && strings.EqualFold(snap.Host, host) {
					session.setRunID(snap.ID)
				}
			}
			if !ready && GetVerificationPresentation().Ready {
				ready = true
			}
			if session.runIdentifier() != 0 {
				session.present(true, ready, "")
				if ready {
					return
				}
			}
		}
	}
}

func cancelledVerificationCheck(source CommunitySource) CommunitySourceCheck {
	return CommunitySourceCheck{
		ID: source.ID, State: "cancelled", Action: "verify", AudioVerified: false,
		Message:   "Verification was cancelled before the source API accepted the session.",
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func failedVerificationCheck(source CommunitySource, message string) CommunitySourceCheck {
	if strings.TrimSpace(message) == "" {
		message = "Verification failed before the source API accepted the session."
	}
	return CommunitySourceCheck{
		ID: source.ID, State: "failed", Action: "verify", AudioVerified: false,
		Message: redactCommunityMessage(message), CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func windowFailureCheck(source CommunitySource, err error) CommunitySourceCheck {
	if err == nil || errors.Is(err, ErrDownloadCancelled) || errors.Is(err, errVerificationClosed) || errors.Is(err, errVerificationDismissed) {
		return cancelledVerificationCheck(source)
	}
	return failedVerificationCheck(source, err.Error())
}

func verificationRejectedError(source CommunitySource, cookies []storedCookie, err error) error {
	if IsDownloadCancelledError(err) {
		return fmt.Errorf("verification was cancelled before the source API accepted the session")
	}
	var access *communityAccessError
	if errors.As(err, &access) {
		return fmt.Errorf("the site has not accepted this session yet; finish the login or browser check in the verification window, then confirm again")
	}
	return fmt.Errorf("the source API is not usable yet: %s", redactCommunityMessage(err.Error(), communitySecretValues(source, cookies)...))
}

func redactVerificationError(err error, source CommunitySource, cookies []storedCookie) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", redactCommunityMessage(err.Error(), communitySecretValues(source, cookies)...))
}

func communitySourceScope(source CommunitySource) string {
	return strings.Join([]string{
		source.Service,
		source.Protocol,
		strings.TrimRight(strings.TrimSpace(source.BaseURL), "/"),
		source.CredentialEnv,
		source.CredentialType,
	}, "\x00")
}

func communityVerificationURL(source CommunitySource) (string, error) {
	origin, err := canonicalCommunityOrigin(source.BaseURL)
	if err != nil {
		return "", fmt.Errorf("configure this server URL first")
	}
	parsed, _ := url.Parse(origin)
	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", fmt.Errorf("use HTTPS, or HTTP for a local server")
		}
	}
	return origin, nil
}

func communityCookieRequestURL(origin string) string {
	return strings.TrimRight(strings.TrimSpace(origin), "/") + "/"
}

func communityHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func canonicalCommunityOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return "", fmt.Errorf("invalid source URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("invalid source URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		if !((scheme == "https" && port == "443") || (scheme == "http" && port == "80")) {
			host = net.JoinHostPort(host, port)
		}
	}
	return scheme + "://" + host, nil
}

func sameCommunityOrigin(base, raw string) bool {
	left, leftErr := canonicalCommunityOrigin(base)
	right, rightErr := canonicalCommunityOrigin(raw)
	return leftErr == nil && rightErr == nil && left == right
}

func communityUsesBrowserJar(source CommunitySource) bool {
	switch source.CredentialType {
	case "api_key", "bearer", "subsonic":
		return false
	case "cookie":
		return true
	}
	switch source.Protocol {
	case "hifi", "dab", "lucida":
		return true
	default:
		return false
	}
}

func applyCommunityRequestCredentials(req *http.Request, source CommunitySource) error {
	if req == nil || req.URL == nil {
		return fmt.Errorf("invalid source request")
	}
	credential := ""
	if source.CredentialEnv != "" {
		credential = os.Getenv(source.CredentialEnv)
		if strings.ContainsAny(credential, "\r\n") {
			return fmt.Errorf("credential contains invalid characters")
		}
	}
	if credential != "" && sameCommunityOrigin(source.BaseURL, req.URL.String()) {
		switch source.CredentialType {
		case "api_key":
			req.Header.Set("X-API-Key", credential)
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+credential)
		case "cookie":
			req.Header.Set("Cookie", credential)
		}
	}
	if credential != "" && source.CredentialType == "cookie" {
		return nil
	}
	header, err := browserJarCookieHeader(source, req.URL.String())
	if err != nil {
		return err
	}
	if header != "" && req.Header.Get("Cookie") == "" {
		req.Header.Set("Cookie", header)
	}
	if req.Header.Get("Cookie") != "" {
		if userAgent := browserUserAgentForSource(source); userAgent != "" {
			req.Header.Set("User-Agent", userAgent)
		}
	}
	return nil
}

func communityCookieHeader(source CommunitySource, rawURL string) (string, error) {
	credential := ""
	if source.CredentialEnv != "" {
		credential = os.Getenv(source.CredentialEnv)
		if strings.ContainsAny(credential, "\r\n") {
			return "", fmt.Errorf("credential contains invalid characters")
		}
	}
	if credential != "" && source.CredentialType == "cookie" {
		if sameCommunityOrigin(source.BaseURL, rawURL) {
			return credential, nil
		}
		return "", nil
	}
	return browserJarCookieHeader(source, rawURL)
}

func browserJarCookieHeader(source CommunitySource, rawURL string) (string, error) {
	if !communityUsesBrowserJar(source) {
		return "", nil
	}
	cookies, err := browserCookiesForSource(source)
	if err != nil {
		return "", err
	}
	return cookieHeaderForURL(cookies, rawURL)
}

func communitySecretValues(source CommunitySource, cookies []storedCookie) []string {
	var values []string
	if source.CredentialEnv != "" {
		if value := strings.TrimSpace(os.Getenv(source.CredentialEnv)); len(value) >= 6 {
			values = append(values, value)
		}
	}
	for _, cookie := range cookies {
		if len(cookie.Value) >= 6 {
			values = append(values, cookie.Value)
		}
	}
	return values
}

func redactCommunityMessage(message string, secrets ...string) string {
	message = redactSourceURL.ReplaceAllString(message, "<redacted-url>")
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) < 6 || strings.ContainsAny(secret, "\r\n") {
			continue
		}
		message = strings.ReplaceAll(message, secret, "<redacted>")
	}
	return message
}

func cookieHeaderForURL(cookies []storedCookie, rawURL string) (string, error) {
	var pairs []string
	for _, cookie := range cookies {
		if !cookieApplies(cookie, rawURL) {
			continue
		}
		pairs = append(pairs, cookie.Name+"="+cookie.Value)
	}
	sort.Strings(pairs)
	header := strings.Join(pairs, "; ")
	if len(header) > 8192 {
		return "", fmt.Errorf("browser session for this source is too large")
	}
	return header, nil
}

func cookieApplies(cookie storedCookie, rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	if !cookieFresh(cookie, time.Now()) {
		return false
	}
	if cookie.Secure && scheme != "https" && !communityLoopback(parsed.Hostname()) {
		return false
	}
	if !cookieDomainMatches(cookie, parsed.Hostname()) {
		return false
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return cookiePathMatches(cookie.Path, path)
}

func cookieFresh(cookie storedCookie, now time.Time) bool {
	if cookie.Expires <= 0 {
		return true
	}
	return now.Before(time.Unix(cookie.Expires, 0))
}

func cookieDomainMatches(cookie storedCookie, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	domain := strings.ToLower(strings.TrimSpace(cookie.Domain))
	hostOnly := cookie.HostOnly || !strings.HasPrefix(domain, ".")
	domain = strings.TrimPrefix(domain, ".")
	if host == "" || domain == "" || strings.Contains(domain, "/") {
		return false
	}
	if host == domain {
		return true
	}
	if hostOnly || strings.Contains(host, ":") {
		return false
	}
	return strings.HasSuffix(host, "."+domain)
}

func cookiePathMatches(cookiePath, requestPath string) bool {
	if cookiePath == "" {
		cookiePath = "/"
	}
	if requestPath == "" {
		requestPath = "/"
	}
	if cookiePath == "/" {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	if len(requestPath) == len(cookiePath) || strings.HasSuffix(cookiePath, "/") {
		return true
	}
	return requestPath[len(cookiePath)] == '/'
}

func communityLoopback(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sanitizeStoredCookies(origin string, cookies []storedCookie) []storedCookie {
	requestURL := communityCookieRequestURL(origin)
	kept := make([]storedCookie, 0, len(cookies))
	for _, cookie := range cookies {
		cookie.Name = strings.TrimSpace(cookie.Name)
		if cookie.Name == "" || strings.ContainsAny(cookie.Name, "\r\n;= ,") || strings.ContainsAny(cookie.Value, "\r\n;") {
			continue
		}
		if len(cookie.Name) > 256 || len(cookie.Value) > 4096 {
			continue
		}
		if cookie.Path == "" {
			cookie.Path = "/"
		}
		if cookie.Expires < 0 {
			cookie.Expires = 0
		}
		if !cookieApplies(cookie, requestURL) {
			continue
		}
		kept = append(kept, cookie)
		if len(kept) == 64 {
			break
		}
	}
	return kept
}

func usePendingBrowserCookies(id string, cookies []storedCookie) func() {
	return usePendingBrowserIdentity(id, cookies, "")
}

func usePendingBrowserIdentity(id string, cookies []storedCookie, userAgent string) func() {
	pendingBrowser.Lock()
	pendingBrowser.id = id
	pendingBrowser.cookies = append([]storedCookie(nil), cookies...)
	pendingBrowser.active = true
	pendingBrowser.userAgent = safeBrowserUserAgent(userAgent)
	pendingBrowser.Unlock()
	return func() {
		pendingBrowser.Lock()
		if pendingBrowser.id == id {
			pendingBrowser.active = false
			pendingBrowser.cookies = nil
			pendingBrowser.id = ""
			pendingBrowser.userAgent = ""
		}
		pendingBrowser.Unlock()
	}
}

func browserCookiesForSource(source CommunitySource) ([]storedCookie, error) {
	pendingBrowser.Lock()
	if pendingBrowser.active && pendingBrowser.id == source.ID {
		cookies := append([]storedCookie(nil), pendingBrowser.cookies...)
		pendingBrowser.Unlock()
		return cookies, nil
	}
	pendingBrowser.Unlock()

	records, err := loadBrowserSessions()
	if err != nil {
		if errors.Is(err, errBrowserSessionPersistenceUnavailable) {
			return nil, err
		}
		return nil, err
	}
	row, ok := records[source.ID]
	if !ok || row.Scope != communitySourceScope(source) {
		return nil, nil
	}
	origin, err := canonicalCommunityOrigin(source.BaseURL)
	if err != nil || row.Origin != origin {
		return nil, nil
	}
	return sanitizeStoredCookies(origin, row.Cookies), nil
}

func saveCommunityBrowserSession(source CommunitySource, cookies []storedCookie) error {
	return saveCommunityBrowserIdentity(source, cookies, "")
}

func saveCommunityBrowserIdentity(source CommunitySource, cookies []storedCookie, userAgent string) error {
	origin, err := canonicalCommunityOrigin(source.BaseURL)
	if err != nil {
		return err
	}
	cookies = sanitizeStoredCookies(origin, cookies)
	if len(cookies) == 0 {
		return nil
	}
	records, err := loadBrowserSessions()
	if err != nil {
		return err
	}
	records[source.ID] = persistedSourceSession{
		Scope: communitySourceScope(source), Origin: origin, Cookies: cookies, UserAgent: safeBrowserUserAgent(userAgent),
	}
	return saveBrowserSessions(records)
}

func invalidateCommunityBrowserSession(id string) {
	if strings.TrimSpace(id) == "" {
		return
	}
	records, err := loadBrowserSessions()
	if err != nil || records == nil {
		return
	}
	if _, ok := records[id]; !ok {
		return
	}
	delete(records, id)
	_ = saveBrowserSessions(records)
}

func browserSessionPath() (string, error) {
	dir, err := EnsureAppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "community-source-sessions.bin"), nil
}

func loadBrowserSessions() (map[string]persistedSourceSession, error) {
	path, err := browserSessionPath()
	if err != nil {
		return nil, err
	}
	browserSessionCache.Lock()
	if browserSessionCache.ok && browserSessionCache.path == path {
		records := cloneBrowserSessions(browserSessionCache.records)
		browserSessionCache.Unlock()
		return records, nil
	}
	browserSessionCache.Unlock()

	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]persistedSourceSession{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read the protected browser session")
	}
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("protected browser session is too large")
	}
	if !browserSessionPersistenceAvailable() {
		return nil, errBrowserSessionPersistenceUnavailable
	}
	plain, err := unprotectBrowserSession(body)
	if err != nil {
		return nil, err
	}
	var stored persistedBrowserSessions
	if json.Unmarshal(plain, &stored) != nil {
		return nil, fmt.Errorf("protected browser session could not be read")
	}
	if stored.Sources == nil {
		stored.Sources = map[string]persistedSourceSession{}
	}
	rememberBrowserSessions(path, stored.Sources)
	return cloneBrowserSessions(stored.Sources), nil
}

func saveBrowserSessions(records map[string]persistedSourceSession) error {
	path, err := browserSessionPath()
	if err != nil {
		return err
	}
	if records == nil {
		records = map[string]persistedSourceSession{}
	}
	if len(records) == 0 {
		rememberBrowserSessions(path, map[string]persistedSourceSession{})
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("could not remove the protected browser session")
		}
		return nil
	}
	if !browserSessionPersistenceAvailable() {
		return errBrowserSessionPersistenceUnavailable
	}
	plain, err := json.Marshal(persistedBrowserSessions{Sources: records})
	if err != nil {
		return fmt.Errorf("could not store the browser session")
	}
	protected, err := protectBrowserSession(plain)
	if err != nil {
		return err
	}
	if bytesContainPlainSecret(protected, plain) {
		return fmt.Errorf("could not protect the browser session")
	}
	if err := WriteFileAtomic(path, protected, 0600); err != nil {
		return fmt.Errorf("could not store the protected browser session")
	}
	if err := restrictPrivateFile(path); err != nil {
		return fmt.Errorf("could not protect the browser session file")
	}
	rememberBrowserSessions(path, records)
	return nil
}

func bytesContainPlainSecret(protected, plain []byte) bool {
	if len(plain) == 0 || len(protected) == 0 {
		return false
	}
	return strings.Contains(string(protected), string(plain))
}

func rememberBrowserSessions(path string, records map[string]persistedSourceSession) {
	browserSessionCache.Lock()
	browserSessionCache.path = path
	browserSessionCache.records = cloneBrowserSessions(records)
	browserSessionCache.ok = true
	browserSessionCache.Unlock()
}

func cloneBrowserSessions(records map[string]persistedSourceSession) map[string]persistedSourceSession {
	out := make(map[string]persistedSourceSession, len(records))
	for id, row := range records {
		row.Cookies = append([]storedCookie(nil), row.Cookies...)
		out[id] = row
	}
	return out
}

func resetSourceVerificationForTest() {
	sourceVerificationHook = sourceVerificationHooks{}
	sourceVerificationHookValidate = nil
	manualMu.Lock()
	manualCurrent = nil
	manualMu.Unlock()
	pendingBrowser.Lock()
	pendingBrowser.active = false
	pendingBrowser.cookies = nil
	pendingBrowser.id = ""
	pendingBrowser.Unlock()
	browserSessionCache.Lock()
	browserSessionCache.ok = false
	browserSessionCache.path = ""
	browserSessionCache.records = nil
	browserSessionCache.Unlock()
}

func refuseForeignBrowserProfile(profileDir string) error {
	cleaned := strings.ToLower(filepath.ToSlash(filepath.Clean(profileDir)))
	if strings.TrimSpace(cleaned) == "" || cleaned == "." {
		return fmt.Errorf("refusing to read another browser profile")
	}
	for _, marker := range []string{"google/chrome", "edge/user data", "chromium"} {
		if strings.Contains(cleaned, marker) && !strings.Contains(cleaned, "verify_profile") {
			return fmt.Errorf("refusing to read another browser profile")
		}
	}
	return nil
}

func captureCommunityCookiesFromProfile(profileDir, origin string) ([]storedCookie, error) {
	if err := refuseForeignBrowserProfile(profileDir); err != nil {
		return nil, err
	}
	requestURL := communityCookieRequestURL(origin)
	if _, err := canonicalCommunityOrigin(requestURL); err != nil {
		return nil, fmt.Errorf("verification browser is not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), communityVerificationCaptureBudget)
	defer cancel()
	port, err := readDevToolsPort(profileDir)
	if err != nil {
		return nil, fmt.Errorf("verification browser is not ready")
	}
	pageWS, err := pageWebSocket(port)
	if err != nil {
		return nil, fmt.Errorf("verification browser is not ready")
	}
	client, err := dialCDP(pageWS)
	if err != nil {
		return nil, fmt.Errorf("verification browser is not ready")
	}
	defer client.close()
	_, _ = client.call(ctx, "Network.enable", map[string]any{})
	raw, err := client.call(ctx, "Network.getCookies", map[string]any{"urls": []string{requestURL}})
	if err != nil {
		return nil, fmt.Errorf("could not read the verification browser session")
	}
	var payload struct {
		Cookies []cdpCookie `json:"cookies"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil, fmt.Errorf("could not read the verification browser session")
	}
	return sanitizeStoredCookies(requestURL, cdpCookiesToStored(payload.Cookies)), nil
}

func verificationBrowserWebSocket(profileDir string) (string, error) {
	port, browserWS, err := refreshDevToolsEndpoint(profileDir)
	if err == nil && browserWS != "" {
		return browserWS, nil
	}
	if port == 0 {
		var portErr error
		port, portErr = readDevToolsPort(profileDir)
		if portErr != nil {
			return "", portErr
		}
	}
	body, err := devToolsGET(port, "/json/version")
	if err != nil {
		return "", err
	}
	var version devToolsVersion
	if json.Unmarshal(body, &version) != nil || strings.TrimSpace(version.WebSocketDebuggerURL) == "" {
		return "", fmt.Errorf("verification browser is not ready")
	}
	return version.WebSocketDebuggerURL, nil
}

func cdpCookiesToStored(rows []cdpCookie) []storedCookie {
	out := make([]storedCookie, 0, len(rows))
	for _, row := range rows {
		expires := int64(0)
		if !row.Session && row.Expires > 0 {
			expires = int64(row.Expires)
		}
		out = append(out, storedCookie{
			Name: row.Name, Value: row.Value, Domain: row.Domain, Path: row.Path,
			Expires: expires, Secure: row.Secure, HTTPOnly: row.HTTPOnly,
			HostOnly: row.HostOnly || !strings.HasPrefix(row.Domain, "."),
		})
	}
	return out
}
