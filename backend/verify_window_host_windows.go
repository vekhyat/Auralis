//go:build windows

package backend

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 plumbing for the in-app verification window: a small overlapped window
// that parents the WebView2 controller, created and pumped on the calling
// (OS-locked) thread.

const (
	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	wsCaption          = 0x00C00000
	wsSysMenu          = 0x00080000
	wsMinimizeBox      = 0x00020000

	swShow = 5

	wmDestroy = 0x0002
	wmClose   = 0x0010

	classNameAuralisVerify = "AuralisVerifyWindow"
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type msg struct {
	hwnd     windows.HWND
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       struct{ x, y int32 }
	lPrivate uintptr
}

var (
	verifyHostMu     sync.Mutex
	verifyHostProc   uintptr
	verifyHostClass  bool
	verifyHostHwnd   windows.HWND

	modUser32            = windows.NewLazySystemDLL("user32.dll")
	procGetModuleHandleW = modUser32.NewProc("GetModuleHandleW")
	procRegisterClassExW = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow    = modUser32.NewProc("DestroyWindow")
	procShowWindow       = modUser32.NewProc("ShowWindow")
	procSetForegroundWin = modUser32.NewProc("SetForegroundWindow")
	procDefWindowProcW   = modUser32.NewProc("DefWindowProcW")
	procGetMessageW      = modUser32.NewProc("GetMessageW")
	procTranslateMessage = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")
	procPostMessageW     = modUser32.NewProc("PostMessageW")
	procIsWindow         = modUser32.NewProc("IsWindow")

	modKernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procKernelModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
)

func createVerifyHostWindow(title string) windows.HWND {
	verifyHostMu.Lock()
	defer verifyHostMu.Unlock()

	if verifyHostProc == 0 {
		verifyHostProc = windows.NewCallback(verifyHostWndProc)
	}
	if !verifyHostClass {
		classNamePtr, err := syscall.UTF16PtrFromString(classNameAuralisVerify)
		if err != nil {
			return 0
		}
		instance, _, _ := procKernelModuleHandleW.Call(0)
		var wc wndClassEx
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		wc.lpfnWndProc = verifyHostProc
		wc.hInstance = windows.Handle(instance)
		wc.lpszClassName = classNamePtr
		// ERROR_CLASS_ALREADY_EXISTS simply means a previous window round used it.
		_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		verifyHostClass = true
	}

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	classNamePtr, err := syscall.UTF16PtrFromString(classNameAuralisVerify)
	if err != nil {
		return 0
	}

	const wsExToolWindow = 0x00000080 // keeps the helper window off the taskbar
	style := uint32(wsOverlappedWindow | wsCaption | wsSysMenu | wsMinimizeBox | wsVisible)
	instance, _, _ := procKernelModuleHandleW.Call(0)
	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(wsExToolWindow),
		uintptr(unsafe.Pointer(classNamePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(style),
		^uintptr(0) >> 1, // CW_USEDEFAULT x
		^uintptr(0) >> 1, // CW_USEDEFAULT y
		500,              // width — comfortably fits the Turnstile widget
		640,              // height
		0, 0,
		instance,
		0,
	)
	if hwnd == 0 {
		return 0
	}
	verifyHostHwnd = windows.HWND(hwnd)
	return verifyHostHwnd
}

func focusVerifyHostWindow(hwnd windows.HWND) {
	procShowWindow.Call(uintptr(hwnd), swShow)
	procSetForegroundWin.Call(uintptr(hwnd))
}

func destroyVerifyHostWindow(hwnd windows.HWND) {
	verifyHostMu.Lock()
	verifyHostHwnd = 0
	verifyHostMu.Unlock()
	if hwnd != 0 {
		_, _, _ = procDestroyWindow.Call(uintptr(hwnd))
	}
}

func postVerifyHostClose() {
	verifyHostMu.Lock()
	hwnd := verifyHostHwnd
	verifyHostMu.Unlock()
	if hwnd != 0 {
		_, _, _ = procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
	}
}

// pumpVerifyHostMessages runs until the window receives WM_CLOSE/WM_DESTROY.
// Must be called on the thread that created the window.
func pumpVerifyHostMessages(hwnd windows.HWND) {
	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&m)),
			uintptr(hwnd),
			0, 0,
		)
		// GetMessageW returns -1 on error and 0 on WM_QUIT.
		if ret == 0 || ^ret == 0 { // 0 or -1 → stop pumping
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		if m.message == wmDestroy && (hwnd == 0 || m.hwnd == hwnd) {
			return
		}
	}
}

func verifyHostWndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmDestroy:
		postQuitMessage()
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func postQuitMessage() {
	modUser32.NewProc("PostQuitMessage").Call(0)
}
