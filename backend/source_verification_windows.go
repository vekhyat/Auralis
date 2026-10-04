//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func browserSessionPersistenceAvailable() bool { return true }

func platformCaptureCommunityCookies(origin string) ([]storedCookie, error) {
	return captureCommunityCookiesFromProfile(verifyProfileDir(), origin)
}

func platformCaptureCommunityUserAgent() (string, error) {
	ws, err := verificationBrowserWebSocket(verifyProfileDir())
	if err != nil {
		return "", err
	}
	client, err := dialCDP(ws)
	if err != nil {
		return "", err
	}
	defer client.close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	raw, err := client.call(ctx, "Browser.getVersion", nil)
	if err != nil {
		return "", err
	}
	var result struct {
		UserAgent string `json:"userAgent"`
	}
	if json.Unmarshal(raw, &result) != nil || safeBrowserUserAgent(result.UserAgent) == "" {
		return "", fmt.Errorf("browser identity is unavailable")
	}
	return safeBrowserUserAgent(result.UserAgent), nil
}

func communityVerificationBrowserReady() bool {
	_, err := readDevToolsPort(verifyProfileDir())
	return err == nil
}

func protectBrowserSession(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, fmt.Errorf("could not protect the browser session")
	}
	in := windows.DataBlob{Size: uint32(len(plain)), Data: &plain[0]}
	var out windows.DataBlob
	desc, err := windows.UTF16PtrFromString("Auralis community source session")
	if err != nil {
		return nil, fmt.Errorf("could not protect the browser session")
	}
	if err := windows.CryptProtectData(&in, desc, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("could not protect the browser session")
	}
	defer func() {
		_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	}()
	if out.Data == nil || out.Size == 0 {
		return nil, fmt.Errorf("could not protect the browser session")
	}
	protected := make([]byte, out.Size)
	copy(protected, unsafe.Slice(out.Data, out.Size))
	runtime.KeepAlive(plain)
	runtime.KeepAlive(desc)
	return protected, nil
}

func unprotectBrowserSession(protected []byte) ([]byte, error) {
	if len(protected) == 0 {
		return nil, fmt.Errorf("could not read the protected browser session")
	}
	in := windows.DataBlob{Size: uint32(len(protected)), Data: &protected[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("could not read the protected browser session")
	}
	defer func() {
		_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	}()
	if out.Data == nil || out.Size == 0 {
		return nil, fmt.Errorf("could not read the protected browser session")
	}
	plain := make([]byte, out.Size)
	copy(plain, unsafe.Slice(out.Data, out.Size))
	runtime.KeepAlive(protected)
	return plain, nil
}
