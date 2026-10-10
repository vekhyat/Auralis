package backend

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
)

// VerificationPresentation is the snapshot the main window renders. It carries
// the run id and source hostname only — never the target URL, grant, or secret.
type VerificationPresentation struct {
	ID     uint64 `json:"id"`
	Active bool   `json:"active"`
	Title  string `json:"title"`
	Host   string `json:"host"`
	Ready  bool   `json:"ready"`
	Error  string `json:"error,omitempty"`
}

const verificationGenericTitle = "Verify"

const (
	wsPopupStyle        = uintptr(0x80000000)
	wsChildStyle        = uintptr(0x40000000)
	wsVisibleStyle      = uintptr(0x10000000)
	wsClipSiblingsStyle = uintptr(0x04000000)
	wsClipChildrenStyle = uintptr(0x02000000)
	wsCaptionStyle      = uintptr(0x00C00000)
	wsSysMenuStyle      = uintptr(0x00080000)
	wsThickFrameStyle   = uintptr(0x00040000)
	wsMinimizeBoxStyle  = uintptr(0x00020000)
	wsMaximizeBoxStyle  = uintptr(0x00010000)
	wsExAppWindowStyle  = uintptr(0x00040000)
)

var errVerificationNotActive = errors.New("verification is not active")

type inAppViewport struct {
	id            uint64
	left, top     int
	width, height int
	hide          bool
	valid         bool
}

var (
	presentationMu       sync.Mutex
	presentationCurrent  VerificationPresentation
	presentationHandler  func(VerificationPresentation)
	presentationQueue    chan VerificationPresentation
	presentationPumpOnce sync.Once

	inAppViewportMu    sync.Mutex
	inAppViewportState inAppViewport
)

// SetVerificationPresentationHandler registers the listener the shell uses to
// mirror verification into the main window. The handler runs off the browser
// goroutine. It must return; GetVerificationPresentation stays current even
// when delivery is queued. Pass nil to clear it.
func SetVerificationPresentationHandler(handler func(VerificationPresentation)) {
	ensurePresentationPump()
	presentationMu.Lock()
	presentationHandler = handler
	presentationMu.Unlock()
}

// GetVerificationPresentation returns the latest safe snapshot.
func GetVerificationPresentation() VerificationPresentation {
	presentationMu.Lock()
	defer presentationMu.Unlock()
	return presentationCurrent
}

func ensurePresentationPump() {
	presentationPumpOnce.Do(func() {
		presentationQueue = make(chan VerificationPresentation, 8)
		go func() {
			for snap := range presentationQueue {
				presentationMu.Lock()
				handler := presentationHandler
				presentationMu.Unlock()
				if handler == nil {
					continue
				}
				func() {
					defer func() { _ = recover() }()
					handler(snap)
				}()
			}
		}()
	})
}

func publishVerificationPresentation(next VerificationPresentation) {
	ensurePresentationPump()
	presentationMu.Lock()
	if next.ID < presentationCurrent.ID || (next.ID == presentationCurrent.ID && !presentationCurrent.Active && next.Active) {
		presentationMu.Unlock()
		return
	}
	presentationCurrent = next
	presentationMu.Unlock()
	select {
	case presentationQueue <- next:
	default:
	}
}

func publishVerificationFailure(id uint64, raw string, err error) {
	publishVerificationPresentation(VerificationPresentation{
		ID:     id,
		Active: false,
		Title:  verificationGenericTitle,
		Host:   verificationPresentationHost(raw),
		Ready:  false,
		Error:  verificationPresentationError(err, raw),
	})
}

func verificationPresentationHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func verificationPresentationError(err error, secret string) string {
	if err == nil || errors.Is(err, errVerificationClosed) || errors.Is(err, errVerificationDismissed) || errors.Is(err, ErrDownloadCancelled) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if secret != "" {
		msg = strings.ReplaceAll(msg, secret, "")
	}
	lower := strings.ToLower(msg)
	if strings.Contains(msg, "://") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "grant") || strings.Contains(lower, "password") {
		return "verification failed"
	}
	if msg == "" {
		return "verification failed"
	}
	return msg
}

// SetVerificationViewport positions the embedded browser in physical client
// pixels of the Auralis window. The shell multiplies the CSS rect by
// devicePixelRatio. A zero width or height hides the native view.
func SetVerificationViewport(id uint64, left, top, width, height int) error {
	if id == 0 {
		return errVerificationNotActive
	}
	attempt := adoptVerificationAttempt()
	if attempt == nil || attempt.id != id {
		return errVerificationNotActive
	}
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	hide := width == 0 || height == 0
	inAppViewportMu.Lock()
	inAppViewportState = inAppViewport{
		id: id, left: left, top: top, width: width, height: height, hide: hide, valid: true,
	}
	inAppViewportMu.Unlock()
	applyInAppVerificationViewport()
	return nil
}

func resetInAppViewport(id uint64) {
	inAppViewportMu.Lock()
	inAppViewportState = inAppViewport{id: id, hide: true, valid: true}
	inAppViewportMu.Unlock()
}

func viewportForRun(id uint64) (left, top, width, height int, show bool) {
	inAppViewportMu.Lock()
	defer inAppViewportMu.Unlock()
	view := inAppViewportState
	if !view.valid || view.id != id {
		return 0, 0, 0, 0, false
	}
	return view.left, view.top, view.width, view.height, !view.hide && view.width > 0 && view.height > 0
}

// CancelInAppVerification cancels only the run identified by id.
func CancelInAppVerification(id uint64) error {
	if id == 0 {
		return errVerificationNotActive
	}
	verificationRunState.mu.Lock()
	attempt := verificationRunState.current
	if attempt == nil || attempt.id != id {
		verificationRunState.mu.Unlock()
		return errVerificationNotActive
	}
	cancel := attempt.cancel
	verificationRunState.mu.Unlock()
	cancel()
	terminateVerificationSession(id)
	return nil
}

// verificationMayShowTopLevel is always false. The Edge process stays hidden
// until its window is a child of the Auralis window.
func verificationMayShowTopLevel() bool {
	return false
}

func verificationRevealAllowed(parentAttached bool, width, height int) bool {
	return parentAttached && width > 0 && height > 0
}

// verificationRect is a window rectangle in physical screen pixels.
type verificationRect struct {
	left, top, right, bottom int
}

// verificationFrameInsets is the browser chrome Edge keeps drawing around the
// page after it becomes a child window: its title bar, caption buttons, and
// resize border. The embed is shifted by these insets and clipped to the page,
// so the main window shows only the verification content.
type verificationFrameInsets struct {
	left, top, right, bottom int
}

// grow extends every side by n, used to push the page's own edge past the clip.
func (i verificationFrameInsets) grow(n int) verificationFrameInsets {
	return verificationFrameInsets{i.left + n, i.top + n, i.right + n, i.bottom + n}
}

// Edge's chrome measures about 7px per side and 30px on top at 100% scale.
// The caps allow 400% scaling with headroom; anything larger means the wrong
// widget was measured and showing the window could expose the title bar.
const (
	verificationMaxSideInset = 64
	verificationMaxTopInset  = 192
)

// verificationFrameInsetsFrom derives the chrome around the page from Edge's
// window rect and its page widget rect. ok=false keeps the embed hidden.
func verificationFrameInsetsFrom(window, page verificationRect) (verificationFrameInsets, bool) {
	if page.right <= page.left || page.bottom <= page.top {
		return verificationFrameInsets{}, false
	}
	insets := verificationFrameInsets{
		left:   page.left - window.left,
		top:    page.top - window.top,
		right:  window.right - page.right,
		bottom: window.bottom - page.bottom,
	}
	if insets.left < 0 || insets.top < 0 || insets.right < 0 || insets.bottom < 0 {
		return verificationFrameInsets{}, false
	}
	if insets.left > verificationMaxSideInset || insets.right > verificationMaxSideInset ||
		insets.bottom > verificationMaxSideInset || insets.top > verificationMaxTopInset {
		return verificationFrameInsets{}, false
	}
	return insets, true
}

func verificationChildStyle(style uintptr) uintptr {
	const popupChrome = wsPopupStyle | wsCaptionStyle | wsSysMenuStyle | wsThickFrameStyle | wsMinimizeBoxStyle | wsMaximizeBoxStyle
	style &^= popupChrome
	style |= wsChildStyle | wsClipSiblingsStyle | wsClipChildrenStyle
	return style
}

func verificationChildExStyle(style uintptr) uintptr {
	const drop = uintptr(0x00000001 | 0x00000080 | 0x00000100 | 0x00000200 | 0x00020000 | 0x08000000 | wsExAppWindowStyle)
	return style &^ drop
}

func verificationHostIsLoopback(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func verificationURLUsable(parsed *url.URL) error {
	if parsed == nil || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("verification URL must be https")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return nil
	case "http":
		if verificationHostIsLoopback(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf("verification URL must be https")
	default:
		return fmt.Errorf("verification URL must be https")
	}
}

func parseVerificationHTTPURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("verification URL must be https")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("verification URL must not include credentials")
	}
	if err := verificationURLUsable(parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func verificationOrigin(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		switch scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		default:
			return ""
		}
	}
	return scheme + "://" + host + ":" + port
}

func communitySourceOrigin(source CommunitySource) string {
	parsed, err := url.Parse(strings.TrimSpace(source.BaseURL))
	if err != nil || parsed.User != nil {
		return ""
	}
	if err := verificationURLUsable(parsed); err != nil {
		return ""
	}
	return verificationOrigin(parsed)
}

func verificationOriginMatchesCommunitySource(raw string) bool {
	parsed, err := parseVerificationHTTPURL(raw)
	if err != nil {
		return false
	}
	origin := verificationOrigin(parsed)
	if origin == "" {
		return false
	}
	sources, err := ListCommunitySources()
	if err != nil {
		return false
	}
	for _, source := range sources {
		if communitySourceOrigin(source) == origin {
			return true
		}
	}
	return false
}

func verificationServicesForOrigin(raw string) []string {
	parsed, err := parseVerificationHTTPURL(raw)
	if err != nil {
		return nil
	}
	origin := verificationOrigin(parsed)
	sources, err := ListCommunitySources()
	if err != nil {
		return nil
	}
	var services []string
	seen := map[string]struct{}{}
	for _, source := range sources {
		if communitySourceOrigin(source) != origin {
			continue
		}
		service := strings.ToLower(strings.TrimSpace(source.Service))
		if service == "" {
			continue
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		services = append(services, service)
	}
	return services
}

func verificationProviderDomains(service string) []string {
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "tidal":
		return []string{"tidal.com"}
	case "qobuz":
		return []string{"qobuz.com"}
	case "amazon":
		return []string{
			"amazon.com", "amazon.co.uk", "amazon.de", "amazon.fr", "amazon.co.jp",
			"amazon.ca", "amazon.com.au", "amazon.it", "amazon.es", "amazon.in",
			"amazonmusic.com",
		}
	case "deezer":
		return []string{"deezer.com"}
	case "apple":
		return []string{"apple.com", "icloud.com"}
	default:
		return nil
	}
}

func verificationHostInDomains(host string, domains []string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	for _, domain := range domains {
		domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
		if domain == "" {
			continue
		}
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// validateVerificationTargetURL accepts a known challenge URL or the exact
// origin of a configured community source. HTTP is limited to loopback, and
// userinfo is rejected. Legacy validateVerificationChallengeURL is unchanged.
func validateVerificationTargetURL(raw string) error {
	parsed, err := parseVerificationHTTPURL(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme == "https" && validateVerificationChallengeURL(raw) == nil {
		return nil
	}
	if verificationOriginMatchesCommunitySource(raw) {
		return nil
	}
	return fmt.Errorf("verification URL host is not allowed")
}

// validateVerificationNavigation allows the original target, the same origin,
// or an https host under a provider domain for that source. Other http URLs
// and unrelated hosts are rejected.
func validateVerificationNavigation(origin, next string) error {
	if _, err := parseVerificationHTTPURL(next); err != nil {
		return err
	}
	if err := validateVerificationTargetURL(next); err == nil {
		return nil
	}
	if err := validateVerificationTargetURL(origin); err != nil {
		return fmt.Errorf("verification URL host is not allowed")
	}
	originURL, err := parseVerificationHTTPURL(origin)
	if err != nil {
		return fmt.Errorf("verification URL host is not allowed")
	}
	nextURL, err := parseVerificationHTTPURL(next)
	if err != nil {
		return err
	}
	if verificationOrigin(originURL) == verificationOrigin(nextURL) {
		return nil
	}
	if nextURL.Scheme != "https" {
		return fmt.Errorf("verification URL must be https")
	}
	for _, service := range verificationServicesForOrigin(origin) {
		if verificationHostInDomains(nextURL.Hostname(), verificationProviderDomains(service)) {
			return nil
		}
	}
	return fmt.Errorf("verification URL host is not allowed")
}
