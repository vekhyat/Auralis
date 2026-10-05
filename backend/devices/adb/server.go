package adb

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/vekhyat/Auralis/backend"
)

// GetADBExecutable finds adb.exe or adb in the app's platform-tools directory or on PATH.
func GetADBExecutable() (string, error) {
	adbName := "adb"
	if runtime.GOOS == "windows" {
		adbName = "adb.exe"
	}

	// 1. Check local app directory first
	appDir, err := backend.GetAppDir()
	if err == nil {
		localPath := filepath.Join(appDir, "platform-tools", adbName)
		if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
			return localPath, nil
		}
	}

	// 2. Check PATH
	if p, err := exec.LookPath(adbName); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("adb executable not found")
}

// EnsureServerRunning checks whether an ADB server is reachable at addr (defaults
// to 127.0.0.1:5037). If not, it attempts to launch `adb start-server`.
func EnsureServerRunning(ctx context.Context, addr string) error {
	if addr == "" {
		addr = defaultADBAddr
	}

	// Check if already running
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return nil
	}

	exe, err := GetADBExecutable()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, exe, "start-server")
	setHideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start adb server: %w", err)
	}

	// Wait up to 3 seconds for the server to listen
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("adb server did not respond at %s after starting", addr)
}
