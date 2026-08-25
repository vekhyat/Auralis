package backend

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	zarzGrantMu      sync.Mutex
	zarzGrantWaiters []chan string
	zarzPendingGrant string
	zarzRedeliverRe  = regexp.MustCompile(`var redeliverGrant = ("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')`)
)

func HandleProtocolArgs(args []string) {
	for _, arg := range args {
		arg = strings.Trim(strings.TrimSpace(arg), `"'`)
		if grant := extractZarzGrant(arg); grant != "" {
			fmt.Println("Received Zarz grant from protocol handler")
			DeliverZarzGrant(grant)
			return
		}
	}
}

func DeliverZarzGrant(grant string) {
	grant = extractZarzGrant(grant)
	if grant == "" {
		return
	}

	zarzGrantMu.Lock()
	defer zarzGrantMu.Unlock()
	if len(zarzGrantWaiters) == 0 {
		zarzPendingGrant = grant
		return
	}
	for _, waiter := range zarzGrantWaiters {
		select {
		case waiter <- grant:
		default:
		}
	}
	zarzGrantWaiters = nil
	zarzPendingGrant = ""

	communityBrowserMu.RLock()
	foreground := communityWindowForeground
	communityBrowserMu.RUnlock()
	if foreground != nil {
		foreground()
	}
}

func waitForZarzGrant(timeout time.Duration) (string, error) {
	ch := make(chan string, 1)
	zarzGrantMu.Lock()
	if zarzPendingGrant != "" {
		grant := zarzPendingGrant
		zarzPendingGrant = ""
		zarzGrantMu.Unlock()
		return grant, nil
	}
	zarzGrantWaiters = append(zarzGrantWaiters, ch)
	zarzGrantMu.Unlock()

	select {
	case grant := <-ch:
		return grant, nil
	case <-time.After(timeout):
		zarzGrantMu.Lock()
		filtered := zarzGrantWaiters[:0]
		for _, waiter := range zarzGrantWaiters {
			if waiter != ch {
				filtered = append(filtered, waiter)
			}
		}
		zarzGrantWaiters = filtered
		zarzGrantMu.Unlock()
		return "", fmt.Errorf("zarz verification timed out")
	}
}

func pollZarzChallengeGrant(challengeID string, stop <-chan struct{}) {
	challengeID = strings.TrimSpace(challengeID)
	if challengeID == "" {
		return
	}
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: 8 * time.Second}
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			grant := fetchZarzRedeliveredGrant(client, challengeID)
			if grant != "" {
				fmt.Println("Received Zarz grant from challenge poll")
				DeliverZarzGrant(grant)
				return
			}
		}
	}
}

func fetchZarzRedeliveredGrant(client *http.Client, challengeID string) string {
	req, err := http.NewRequest(http.MethodGet, zarzBaseURL+"/challenge?id="+url.QueryEscape(challengeID), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", zarzUserAgentFor("tidal-web@1.1.0"))
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return ""
	}
	match := zarzRedeliverRe.FindSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	quoted := string(match[1])
	unquoted, err := strconv.Unquote(quoted)
	if err != nil {
		return strings.Trim(quoted, `"'`)
	}
	return strings.TrimSpace(unquoted)
}
