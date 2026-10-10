//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Uses the built Wails app and a local session fixture, never a provider login.
// The test build uses a temporary copy of the WebView2 loader that honors AURALIS_TEST_BROWSER_ARGS;
// the production loader deliberately clears inspector environment arguments.
// Opt-in because it starts a desktop process with an isolated profile.
func TestNativeProductionAppVerificationPanel(t *testing.T) {
	exe := os.Getenv("AURALIS_SMOKE_EXE")
	if exe == "" {
		t.Skip("set AURALIS_SMOKE_EXE to the built Windows app")
	}
	t.Setenv(appDataDirEnv, t.TempDir())
	var served atomic.Bool
	var accept atomic.Bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.SetCookie(w, &http.Cookie{Name: "app-smoke-session", Value: "synthetic", Path: "/", HttpOnly: true})
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<!doctype html><title>Local source fixture</title><p>Local source verification is ready.</p>")
			served.Store(true)
			return
		}
		cookie, err := r.Cookie("app-smoke-session")
		if err != nil || cookie.Value != "synthetic" || !accept.Load() {
			http.Error(w, "session required", 401)
			return
		}
		fmt.Fprint(w, testHiFiPayload(55130631, server.URL+"/audio", "FULL", "NONE"))
	}))
	defer server.Close()
	rows := []CommunitySource{{ID: "local-app-smoke", Name: "Local source fixture", Service: "tidal", Protocol: "hifi", BaseURL: server.URL, Enabled: true}}
	if err := SaveCommunitySources(rows); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "AURALIS_TEST_BROWSER_ARGS=--remote-debugging-address=127.0.0.1 --remote-debugging-port="+strconv.Itoa(port))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	var client *cdpClient
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		ws, err := pageWebSocket(port)
		if err == nil {
			client, err = dialCDP(ws)
			if err == nil {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if client == nil {
		t.Fatal("the production app did not expose its isolated test webview")
	}
	defer client.close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	evaluate := func(expression string, await bool) json.RawMessage {
		raw, err := client.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": await})
		if err != nil {
			t.Fatal(err)
		}
		var reply struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			} `json:"result"`
			Exception json.RawMessage `json:"exceptionDetails"`
		}
		if json.Unmarshal(raw, &reply) != nil || len(reply.Exception) > 0 {
			t.Fatal("production frontend evaluation failed")
		}
		return reply.Result.Value
	}
	for time.Now().Before(deadline) {
		var ready bool
		_ = json.Unmarshal(evaluate(`!!document.querySelector('.app-content') && typeof window.go?.main?.App?.VerifyCommunitySource === 'function'`, false), &ready)
		if ready {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	evaluate(`window.go.main.App.VerifyCommunitySource('local-app-smoke').then(r => {window.__localVerificationResult=r;}).catch(() => {window.__localVerificationError=true;}); true`, false)
	deadline = time.Now().Add(25 * time.Second)
	var panel struct {
		Active, Ready bool
		Width, Height float64
	}
	for time.Now().Before(deadline) {
		_ = json.Unmarshal(evaluate(`(async()=>{const p=await window.go.main.App.GetSourceVerification();const r=document.querySelector('[aria-label="Source verification page"]')?.getBoundingClientRect();return {Active:!!document.querySelector('#source-verification-title'),Ready:p.ready,Width:r?.width||0,Height:r?.height||0};})()`, true), &panel)
		if panel.Active && panel.Ready && served.Load() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !panel.Active || !panel.Ready || !served.Load() || panel.Width < 300 || panel.Height < 150 {
		t.Fatalf("production verification panel was not ready: %+v fixtureLoaded=%t", panel, served.Load())
	}
	var rejected bool
	_ = json.Unmarshal(evaluate(`window.go.main.App.ConfirmSourceVerification().then(()=>false).catch(()=>true)`, true), &rejected)
	var stillOpen bool
	_ = json.Unmarshal(evaluate(`!!document.querySelector('#source-verification-title')`, false), &stillOpen)
	if !rejected || !stillOpen {
		t.Fatal("a rejected API session closed the production verification panel")
	}
	accept.Store(true)
	var accepted bool
	_ = json.Unmarshal(evaluate(`window.go.main.App.ConfirmSourceVerification()`, true), &accepted)
	if !accepted {
		t.Fatal("production app did not accept the local session")
	}
	var closed bool
	_ = json.Unmarshal(evaluate(`!document.querySelector('#source-verification-title')`, false), &closed)
	if !closed {
		t.Fatal("production verification panel did not close")
	}
	t.Logf("Built Wails app: embedded panel %.0fx%.0f; rejected API kept panel open; local browser session accepted; panel closed.", panel.Width, panel.Height)
}
