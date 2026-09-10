//go:build windows

package backend

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Verification is shown in a dedicated Microsoft Edge --app window, not a
// second in-process WebView2. go-webview2's Chromium.Embed calls os.Exit(1)
// on environment/controller errors, which killed Auralis the moment a
// download needed Turnstile. A child Edge process cannot take the app down.

type verifySession struct {
	mu      sync.Mutex
	active  bool
	cmd     *exec.Cmd
	done    chan struct{}
	closing bool
}

var verifySessionState verifySession

func OpenVerificationWindow(target string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("verification window panic: %v", recovered)
		}
	}()

	if err := validateVerificationChallengeURL(target); err != nil {
		return err
	}

	edgePath := microsoftEdgePath()
	if edgePath == "" {
		return fmt.Errorf("Microsoft Edge was not found")
	}

	verifySessionState.mu.Lock()
	if verifySessionState.active {
		verifySessionState.mu.Unlock()
		return fmt.Errorf("verification window is already open")
	}
	profileDir := verifyProfileDir()
	args := []string{
		"--app=" + target,
		"--window-size=500,640",
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
	}
	if extensionDir := mustWriteVerifyExtension(); extensionDir != "" {
		args = append(args, verifyExtensionLoadArgs(extensionDir)...)
	}
	cmd := exec.Command(edgePath, args...)
	if err := cmd.Start(); err != nil {
		verifySessionState.mu.Unlock()
		return fmt.Errorf("failed to open verification window: %w", err)
	}
	done := make(chan struct{})
	verifySessionState.active = true
	verifySessionState.closing = false
	verifySessionState.cmd = cmd
	verifySessionState.done = done
	verifySessionState.mu.Unlock()

	go func() {
		_ = cmd.Wait()
		close(done)
		verifySessionState.mu.Lock()
		if verifySessionState.cmd == cmd {
			verifySessionState.cmd = nil
			verifySessionState.active = false
			verifySessionState.done = nil
		}
		verifySessionState.mu.Unlock()
	}()

	<-done
	return nil
}

func CloseVerificationWindow() {
	verifySessionState.mu.Lock()
	cmd := verifySessionState.cmd
	done := verifySessionState.done
	verifySessionState.closing = true
	verifySessionState.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if done != nil {
		<-done
	}
}

func verifyProfileDir() string {
	dir, err := EnsureAppDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "auralis-verify-profile")
	}
	return filepath.Join(dir, "verify_profile")
}

func microsoftEdgePath() string {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Microsoft\Edge\Application\msedge.exe`),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate
		}
	}
	if found, err := exec.LookPath("msedge"); err == nil {
		return found
	}
	return ""
}
