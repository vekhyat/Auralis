package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var errVerificationClosed = errors.New("verification window closed")

type cdpReply struct {
	result json.RawMessage
	err    error
}

type cdpClient struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	next    int
	wait    map[int]chan cdpReply
}

func dialCDP(wsURL string) (*cdpClient, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return nil, err
	}
	client := &cdpClient{conn: conn, wait: map[int]chan cdpReply{}}
	go client.readLoop()
	return client, nil
}

func (c *cdpClient) close() {
	c.writeMu.Lock()
	_ = c.conn.Close()
	c.writeMu.Unlock()
}

func (c *cdpClient) readLoop() {
	defer func() {
		c.mu.Lock()
		for id, ch := range c.wait {
			ch <- cdpReply{err: fmt.Errorf("cdp connection closed")}
			delete(c.wait, id)
		}
		c.mu.Unlock()
	}()
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
			continue
		}
		reply := cdpReply{result: msg.Result}
		if msg.Error != nil && msg.Error.Message != "" {
			reply.err = errors.New(msg.Error.Message)
		}
		c.mu.Lock()
		ch := c.wait[msg.ID]
		delete(c.wait, msg.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- reply
		}
	}
}

func (c *cdpClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan cdpReply, 1)
	c.wait[id] = ch
	c.mu.Unlock()

	payload := map[string]any{"id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	c.writeMu.Lock()
	err := c.conn.WriteJSON(payload)
	c.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case reply := <-ch:
		return reply.result, reply.err
	case <-time.After(8 * time.Second):
		return nil, fmt.Errorf("cdp %s timed out", method)
	}
}

func closeVerifyDebugger(browserWS string) {
	browserWS = strings.TrimSpace(browserWS)
	if browserWS == "" {
		return
	}
	client, err := dialCDP(browserWS)
	if err != nil {
		return
	}
	defer client.close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = client.call(ctx, "Browser.close", nil)
}

type devToolsTarget struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type devToolsVersion struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func readDevToolsPort(profileDir string) (int, error) {
	data, err := os.ReadFile(filepath.Join(profileDir, "DevToolsActivePort"))
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")[0])
	port, err := strconv.Atoi(line)
	if err != nil || port <= 0 {
		return 0, fmt.Errorf("invalid devtools port %q", line)
	}
	return port, nil
}

func devToolsGET(port int, path string) ([]byte, error) {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("devtools %s returned HTTP %d", path, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func waitForStableDevTools(ctx context.Context, profileDir string) (int, string, error) {
	deadline := time.Now().Add(20 * time.Second)
	var port int
	var browserWS string
	stable := 0
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return 0, "", errVerificationClosed
		}
		next, err := readDevToolsPort(profileDir)
		if err != nil {
			stable = 0
			if sleepErr := sleepContext(ctx, 200*time.Millisecond); sleepErr != nil {
				return 0, "", sleepErr
			}
			continue
		}
		versionBody, err := devToolsGET(next, "/json/version")
		if err != nil {
			stable = 0
			if sleepErr := sleepContext(ctx, 200*time.Millisecond); sleepErr != nil {
				return 0, "", sleepErr
			}
			continue
		}
		var version devToolsVersion
		_ = json.Unmarshal(versionBody, &version)
		if next != port {
			port = next
			browserWS = version.WebSocketDebuggerURL
			stable = 0
		}
		stable++
		// Edge restarts once to load the branding extension and aborts the
		// first navigation with ERR_NETWORK_CHANGED. Wait until the same
		// debugging port stays up so we attach to the browser that remains.
		if stable >= 3 && browserWS != "" {
			return port, browserWS, nil
		}
		if sleepErr := sleepContext(ctx, 200*time.Millisecond); sleepErr != nil {
			return 0, "", sleepErr
		}
	}
	return 0, "", fmt.Errorf("verification browser did not become ready")
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errVerificationClosed
	case <-timer.C:
		return nil
	}
}

func refreshDevToolsEndpoint(profileDir string) (int, string, error) {
	port, err := readDevToolsPort(profileDir)
	if err != nil {
		return 0, "", err
	}
	body, err := devToolsGET(port, "/json/version")
	if err != nil {
		return port, "", nil
	}
	var version devToolsVersion
	if json.Unmarshal(body, &version) != nil {
		return port, "", nil
	}
	return port, version.WebSocketDebuggerURL, nil
}

func pageWebSocket(port int) (string, error) {
	body, err := devToolsGET(port, "/json/list")
	if err != nil {
		return "", err
	}
	var targets []devToolsTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return "", err
	}
	for _, target := range targets {
		if target.Type == "page" && target.WebSocketDebuggerURL != "" {
			return target.WebSocketDebuggerURL, nil
		}
	}
	return "", fmt.Errorf("verification page target not found")
}

type pageSnapshot struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

func readPageSnapshot(ctx context.Context, client *cdpClient) (pageSnapshot, error) {
	raw, err := client.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    `({href: location.href, text: ((document.body && document.body.innerText) || "").slice(0, 800)})`,
		"returnByValue": true,
	})
	if err != nil {
		return pageSnapshot{}, err
	}
	var envelope struct {
		Result struct {
			Value pageSnapshot `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return pageSnapshot{}, err
	}
	return envelope.Result.Value, nil
}

func setVerificationBrowserVisible(ctx context.Context, browserWS string, visible bool, width, height int) error {
	browserWS = strings.TrimSpace(browserWS)
	if browserWS == "" {
		return fmt.Errorf("missing verification debugger")
	}
	client, err := dialCDP(browserWS)
	if err != nil {
		return err
	}
	defer client.close()
	raw, err := client.call(ctx, "Browser.getWindowForTarget", map[string]any{})
	if err != nil {
		return err
	}
	var window struct {
		WindowID int `json:"windowId"`
	}
	if json.Unmarshal(raw, &window) != nil || window.WindowID == 0 {
		return fmt.Errorf("verification window id missing")
	}
	if width <= 0 {
		width = verificationPopupWidth
	}
	if height <= 0 {
		height = verificationPopupHeight
	}
	bounds := map[string]any{
		"width":       width,
		"height":      height,
		"windowState": "normal",
	}
	if !visible {
		bounds["left"] = verificationOffscreenX
		bounds["top"] = verificationOffscreenY
	}
	_, err = client.call(ctx, "Browser.setWindowBounds", map[string]any{
		"windowId": window.WindowID,
		"bounds":   bounds,
	})
	return err
}

func measureVerificationPopupSize(ctx context.Context, profileDir string) (int, int, error) {
	port, err := readDevToolsPort(profileDir)
	if err != nil {
		return 0, 0, err
	}
	wsURL, err := pageWebSocket(port)
	if err != nil {
		return 0, 0, err
	}
	client, err := dialCDP(wsURL)
	if err != nil {
		return 0, 0, err
	}
	defer client.close()
	raw, err := client.call(ctx, "Runtime.evaluate", map[string]any{
		"expression": `(() => {
			const widget = document.querySelector('iframe[src*="challenges.cloudflare.com"], .cf-turnstile, #cf-turnstile, [id*="turnstile"], [class*="turnstile"]');
			let w = 300, h = 80;
			if (widget) {
				const r = widget.getBoundingClientRect();
				w = Math.max(r.width, widget.scrollWidth || 0, 300);
				h = Math.max(r.height, widget.scrollHeight || 0, 65);
			}
			let extra = 0;
			const prompt = Array.from(document.querySelectorAll('h1,h2,p,label,.prompt,.subtitle')).find((el) => (el.innerText || '').trim().length > 0);
			if (prompt) {
				extra = Math.min(prompt.getBoundingClientRect().height || 0, 72);
			}
			return {w: Math.ceil(w), h: Math.ceil(h + extra + 16)};
		})()`,
		"returnByValue": true,
	})
	if err != nil {
		return 0, 0, err
	}
	var envelope struct {
		Result struct {
			Value struct {
				W float64 `json:"w"`
				H float64 `json:"h"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return 0, 0, err
	}
	width, height := clampVerificationPopupSize(int(envelope.Result.Value.W), int(envelope.Result.Value.H))
	return width, height, nil
}

func installVerificationBranding(ctx context.Context, client *cdpClient) {
	_, _ = client.call(ctx, "Page.enable", map[string]any{})
	_, _ = client.call(ctx, "Network.enable", map[string]any{})
	_, _ = client.call(ctx, "Network.setBlockedURLs", map[string]any{
		"urls": []string{"*favicon*", "*apple-touch-icon*", "*://*/favicon.ico"},
	})
	_, _ = client.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{
		"source": verifyExtensionContentJS,
	})
	// Apply immediately if a document is already open. The script is idempotent.
	_, _ = client.call(ctx, "Runtime.evaluate", map[string]any{
		"expression": verifyExtensionContentJS,
	})
}

func navigateVerificationPage(ctx context.Context, client *cdpClient, target string, reload bool) error {
	method := "Page.navigate"
	params := map[string]any{"url": target}
	if reload {
		method = "Page.reload"
		params = map[string]any{"ignoreCache": true}
	}
	raw, err := client.call(ctx, method, params)
	if err != nil {
		return err
	}
	var navigated struct {
		ErrorText string `json:"errorText"`
	}
	_ = json.Unmarshal(raw, &navigated)
	if navigated.ErrorText != "" && verificationPageIsNetworkError("", navigated.ErrorText) {
		return fmt.Errorf("%s", navigated.ErrorText)
	}
	return nil
}

func waitForVerificationSnapshot(ctx context.Context, client *cdpClient) (pageSnapshot, error) {
	deadline := time.Now().Add(8 * time.Second)
	var last pageSnapshot
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return pageSnapshot{}, errVerificationClosed
		}
		snapshot, err := readPageSnapshot(ctx, client)
		if err != nil {
			return pageSnapshot{}, err
		}
		last = snapshot
		if verificationPageIsReady(snapshot.Href, snapshot.Text) || verificationPageIsNetworkError(snapshot.Href, snapshot.Text) {
			return snapshot, nil
		}
		if sleepErr := sleepContext(ctx, 300*time.Millisecond); sleepErr != nil {
			return pageSnapshot{}, sleepErr
		}
	}
	return last, nil
}

// driveVerificationPage attaches to the Edge window after it has finished
// restarting, loads the challenge, and reloads when Edge shows the
// "network change" interstitial. It returns once the challenge is visible;
// the caller keeps the process alive until verification finishes.
func driveVerificationPage(ctx context.Context, profileDir, target string) (string, error) {
	port, browserWS, err := waitForStableDevTools(ctx, profileDir)
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(30 * time.Second)
	reloads := 0
	navigated := false
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return browserWS, errVerificationClosed
		}
		wsURL, err := pageWebSocket(port)
		if err != nil {
			if next, nextWS, refreshErr := refreshDevToolsEndpoint(profileDir); refreshErr == nil {
				port = next
				if nextWS != "" {
					browserWS = nextWS
				}
			}
			if sleepErr := sleepContext(ctx, 250*time.Millisecond); sleepErr != nil {
				return browserWS, sleepErr
			}
			continue
		}
		client, err := dialCDP(wsURL)
		if err != nil {
			if sleepErr := sleepContext(ctx, 250*time.Millisecond); sleepErr != nil {
				return browserWS, sleepErr
			}
			continue
		}
		installVerificationBranding(ctx, client)
		snapshot, snapErr := readPageSnapshot(ctx, client)
		if snapErr != nil {
			client.close()
			if ctx.Err() != nil {
				return browserWS, errVerificationClosed
			}
			if sleepErr := sleepContext(ctx, 250*time.Millisecond); sleepErr != nil {
				return browserWS, sleepErr
			}
			continue
		}
		if verificationPageIsReady(snapshot.Href, snapshot.Text) {
			client.close()
			return browserWS, nil
		}
		reload := navigated && verificationPageIsNetworkError(snapshot.Href, snapshot.Text)
		if reload {
			reloads++
			if reloads > 4 {
				client.close()
				return browserWS, fmt.Errorf("verification page still shows a network error")
			}
			fmt.Printf("Verification page reported a network change; reloading (%d/4)\n", reloads)
		}
		navErr := navigateVerificationPage(ctx, client, target, reload)
		navigated = true
		if navErr != nil && ctx.Err() != nil {
			client.close()
			return browserWS, errVerificationClosed
		}
		snapshot, snapErr = waitForVerificationSnapshot(ctx, client)
		client.close()
		if snapErr != nil {
			if errors.Is(snapErr, errVerificationClosed) || ctx.Err() != nil {
				return browserWS, errVerificationClosed
			}
			continue
		}
		if verificationPageIsReady(snapshot.Href, snapshot.Text) {
			return browserWS, nil
		}
		if !verificationPageIsNetworkError(snapshot.Href, snapshot.Text) && navErr == nil {
			// Page is still loading a non-error document. One more pass.
			if sleepErr := sleepContext(ctx, 300*time.Millisecond); sleepErr != nil {
				return browserWS, sleepErr
			}
			continue
		}
	}
	if ctx.Err() != nil {
		return browserWS, errVerificationClosed
	}
	return browserWS, fmt.Errorf("verification page did not become ready")
}
