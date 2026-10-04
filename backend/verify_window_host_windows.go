//go:build windows

package backend

import (
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 hosting for the isolated Edge verification process. The browser stays
// a separate process (an in-process WebView2 host called os.Exit). Its window
// is hidden until SetParent makes it a WS_CHILD of the current process's
// Auralis window, then it is positioned in the shell-provided viewport.
// It is never shown as a standalone top-level window.

const (
	wsExToolWindow = 0x00000080
	wsExNoActivate = 0x08000000

	swHide = 0

	swpNoSize       = 0x0001
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	swpShowWindow   = 0x0040
	swpHideWindow   = 0x0080
	gwOwner         = 4

	wmClose = 0x0010

	auralisMainWindowClass = "wailsWindow"
	auralisMainWindowTitle = "Auralis"
)

var (
	verifyChromeMu     sync.Mutex
	verifyChromeProc   uintptr
	verifyChromeJob    windows.Handle
	verifyChromeParent windows.HWND
	verifyChromeShow   bool
	verifyChromeX      int
	verifyChromeY      int
	verifyChromeW      int
	verifyChromeH      int
	verifyChromeFound  windows.HWND
	verifyEmbeddedHWND windows.HWND

	verifyGwlStyle   = int32(-16)
	verifyGwlExStyle = int32(-20)

	modUser32            = windows.NewLazySystemDLL("user32.dll")
	procShowWindow       = modUser32.NewProc("ShowWindow")
	procPostMessageW     = modUser32.NewProc("PostMessageW")
	procIsWindow         = modUser32.NewProc("IsWindow")
	procGetWindowLongPtr = modUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr = modUser32.NewProc("SetWindowLongPtrW")
	procSetWindowPos     = modUser32.NewProc("SetWindowPos")
	procGetClassNameW    = modUser32.NewProc("GetClassNameW")
	procGetWindowTextW   = modUser32.NewProc("GetWindowTextW")
	procGetParent        = modUser32.NewProc("GetParent")
	procGetWindow        = modUser32.NewProc("GetWindow")
	procSetParent        = modUser32.NewProc("SetParent")

	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	auralisWindowOnce  sync.Once
	auralisWindowProc  uintptr
	auralisWindowMu    sync.Mutex
	auralisWindowFound windows.HWND
	auralisWindowScore int
	auralisWindowPID   uint32
)

func restyleVerificationJobWindows(job windows.Handle, parent windows.HWND, left, top, width, height int, show bool) windows.HWND {
	if job == 0 {
		return 0
	}
	if verifyChromeProc == 0 {
		verifyChromeProc = windows.NewCallback(enumVerificationChromeWindow)
	}
	verifyChromeMu.Lock()
	verifyChromeJob = job
	verifyChromeParent = parent
	verifyChromeShow = show && parent != 0 && verificationRevealAllowed(true, width, height)
	verifyChromeX = left
	verifyChromeY = top
	verifyChromeW = width
	verifyChromeH = height
	verifyChromeFound = 0
	verifyChromeMu.Unlock()
	_ = windows.EnumWindows(verifyChromeProc, nil)
	verifyChromeMu.Lock()
	found := verifyChromeFound
	verifyChromeJob = 0
	verifyChromeMu.Unlock()
	return found
}

func enumVerificationChromeWindow(hwnd uintptr, _ uintptr) uintptr {
	verifyChromeMu.Lock()
	job := verifyChromeJob
	parent := verifyChromeParent
	show := verifyChromeShow
	left := verifyChromeX
	top := verifyChromeY
	width := verifyChromeW
	height := verifyChromeH
	verifyChromeMu.Unlock()
	if job == 0 || !hwndBelongsToJob(hwnd, job) || !isVerificationAppWindowClass(verificationWindowClassName(hwnd)) {
		return 1
	}
	window := windows.HWND(hwnd)
	primary := latchedVerificationWindow()
	owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
	currentParent, _, _ := procGetParent.Call(hwnd)
	adopt := window == primary && primary != 0
	if !adopt && primary == 0 && owner == 0 && (currentParent == 0 || (parent != 0 && currentParent == uintptr(parent))) {
		adopt = true
	}
	if !adopt {
		suppressVerificationPopup(window)
		return 1
	}
	if parent == 0 || !verificationWindowAlive(parent) {
		hideVerificationWindow(window)
		latchVerificationWindow(window)
		return 1
	}
	if placeVerificationWindow(window, parent, left, top, width, height, show) {
		latchVerificationWindow(window)
		verifyChromeMu.Lock()
		verifyChromeFound = window
		verifyChromeMu.Unlock()
		return 1
	}
	hideVerificationWindow(window)
	latchVerificationWindow(window)
	return 1
}

func latchedVerificationWindow() windows.HWND {
	verifyChromeMu.Lock()
	hwnd := verifyEmbeddedHWND
	verifyChromeMu.Unlock()
	if hwnd != 0 && !verificationWindowAlive(hwnd) {
		verifyChromeMu.Lock()
		if verifyEmbeddedHWND == hwnd {
			verifyEmbeddedHWND = 0
		}
		verifyChromeMu.Unlock()
		return 0
	}
	return hwnd
}

func latchVerificationWindow(hwnd windows.HWND) {
	verifyChromeMu.Lock()
	verifyEmbeddedHWND = hwnd
	verifyChromeMu.Unlock()
}

func resetVerificationEmbedding() {
	verifyChromeMu.Lock()
	verifyEmbeddedHWND = 0
	verifyChromeMu.Unlock()
}

func isVerificationEdgeAppWindow(hwnd uintptr) bool {
	parent, _, _ := procGetParent.Call(hwnd)
	if parent != 0 {
		return false
	}
	owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
	if owner != 0 {
		return false
	}
	return isVerificationAppWindowClass(verificationWindowClassName(hwnd))
}

func verificationWindowClassName(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || n > uintptr(len(buf)) {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func verificationWindowTitle(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || n > uintptr(len(buf)) {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func hwndBelongsToJob(hwnd uintptr, job windows.Handle) bool {
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return false
	}
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(proc)
	var inJob int32
	r, _, _ := procIsProcessInJob.Call(uintptr(proc), uintptr(job), uintptr(unsafe.Pointer(&inJob)))
	return r != 0 && inJob != 0
}

func verificationWindowAlive(hwnd windows.HWND) bool {
	if hwnd == 0 {
		return false
	}
	alive, _, _ := procIsWindow.Call(uintptr(hwnd))
	return alive != 0
}

func windowLong(hwnd windows.HWND, index int32) uintptr {
	value, _, _ := procGetWindowLongPtr.Call(uintptr(hwnd), uintptr(index))
	return value
}

func setWindowLong(hwnd windows.HWND, index int32, value uintptr) {
	_, _, _ = procSetWindowLongPtr.Call(uintptr(hwnd), uintptr(index), value)
}

// placeVerificationWindow parents hwnd into the Auralis window. A missing
// parent or a zero viewport only hides the browser; it never reveals a
// top-level window and never restyles the parent.
func placeVerificationWindow(hwnd, parent windows.HWND, left, top, width, height int, show bool) bool {
	if !verificationWindowAlive(hwnd) {
		return false
	}
	if parent == 0 || !verificationWindowAlive(parent) || !verificationRevealAllowed(true, width, height) {
		show = false
	}
	if parent == 0 || !verificationWindowAlive(parent) {
		hideVerificationWindow(hwnd)
		return false
	}
	style := verificationChildStyle(windowLong(hwnd, verifyGwlStyle))
	if show {
		style |= wsVisibleStyle
	} else {
		style &^= wsVisibleStyle
	}
	setWindowLong(hwnd, verifyGwlStyle, style)
	setWindowLong(hwnd, verifyGwlExStyle, verificationChildExStyle(windowLong(hwnd, verifyGwlExStyle)))
	_, _, _ = procSetParent.Call(uintptr(hwnd), uintptr(parent))
	flags := uintptr(swpFrameChanged | swpNoActivate)
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	if show {
		flags |= swpShowWindow
	} else {
		flags |= swpHideWindow
		_, _, _ = procShowWindow.Call(uintptr(hwnd), uintptr(swHide))
	}
	_, _, _ = procSetWindowPos.Call(
		uintptr(hwnd),
		0,
		uintptr(int32(left)),
		uintptr(int32(top)),
		uintptr(int32(width)),
		uintptr(int32(height)),
		flags,
	)
	got, _, _ := procGetParent.Call(uintptr(hwnd))
	return got == uintptr(parent)
}

func hideVerificationWindow(hwnd windows.HWND) {
	if !verificationWindowAlive(hwnd) {
		return
	}
	ex := windowLong(hwnd, verifyGwlExStyle)
	ex |= uintptr(wsExToolWindow | wsExNoActivate)
	ex &^= wsExAppWindowStyle
	setWindowLong(hwnd, verifyGwlExStyle, ex)
	_, _, _ = procShowWindow.Call(uintptr(hwnd), uintptr(swHide))
	x := int32(verificationOffscreenX)
	y := int32(verificationOffscreenY)
	_, _, _ = procSetWindowPos.Call(
		uintptr(hwnd),
		0,
		uintptr(x),
		uintptr(y),
		0,
		0,
		uintptr(swpNoActivate|swpNoZOrder|swpNoSize|swpHideWindow),
	)
}

func suppressVerificationPopup(hwnd windows.HWND) {
	hideVerificationWindow(hwnd)
	_, _, _ = procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
}

func findAuralisMainWindow() windows.HWND {
	auralisWindowOnce.Do(func() {
		auralisWindowProc = windows.NewCallback(enumAuralisMainWindow)
	})
	auralisWindowMu.Lock()
	auralisWindowFound = 0
	auralisWindowScore = -1
	auralisWindowPID = uint32(os.Getpid())
	auralisWindowMu.Unlock()
	_ = windows.EnumWindows(auralisWindowProc, nil)
	auralisWindowMu.Lock()
	found := auralisWindowFound
	auralisWindowMu.Unlock()
	return found
}

func enumAuralisMainWindow(hwnd uintptr, _ uintptr) uintptr {
	auralisWindowMu.Lock()
	want := auralisWindowPID
	auralisWindowMu.Unlock()
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || pid != want {
		return 1
	}
	parent, _, _ := procGetParent.Call(hwnd)
	if parent != 0 {
		return 1
	}
	owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
	if owner != 0 {
		return 1
	}
	if verificationWindowClassName(hwnd) != auralisMainWindowClass {
		return 1
	}
	if windowLong(windows.HWND(hwnd), verifyGwlStyle)&wsChildStyle != 0 {
		return 1
	}
	score := 1
	if verificationWindowVisible(hwnd) {
		score += 2
	}
	if verificationWindowTitle(hwnd) == auralisMainWindowTitle {
		score += 4
	}
	auralisWindowMu.Lock()
	if score > auralisWindowScore {
		auralisWindowScore = score
		auralisWindowFound = windows.HWND(hwnd)
	}
	auralisWindowMu.Unlock()
	return 1
}

func verificationWindowVisible(hwnd uintptr) bool {
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	return visible != 0
}
