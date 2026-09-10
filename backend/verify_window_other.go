//go:build !windows

package backend

import "fmt"

// OpenVerificationWindow is only implemented for Windows, where the WebView2
// runtime is guaranteed by the Wails build. Other platforms use the system
// browser via the normal openBrowser handler.
func OpenVerificationWindow(target string) error {
	return fmt.Errorf("embedded verification window is not supported on this platform")
}

// CloseVerificationWindow is a no-op without the native window.
func CloseVerificationWindow() {}
