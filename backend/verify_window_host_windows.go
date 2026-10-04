//go:build windows

package backend

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 chrome for the isolated Edge --app verification window. The previous
// in-process WebView2 host is intentionally gone: go-webview2 called os.Exit
// on controller errors and took Auralis down with it.

const (
	wsCaption        = 0x00C00000
	wsSysMenu        = 0x00080000
	wsThickFrame     = 0x00040000
	wsMinimizeBox    = 0x00020000
	wsMaximizeBox    = 0x00010000
	wsExAppWindow    = 0x00040000
	wsExToolWindow   = 0x00000080
	wsExNoActivate   = 0x08000000

	swHide    = 0
	swShow    = 5
	swRestore = 9

	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	gwOwner         = 4

	spiGetWorkArea = 0x0030

	dwmwaBorderColor  = 34
	dwmwaCaptionColor = 35
	dwmwaTextColor    = 36

	wmClose   = 0x0010
	wmDestroy = 0x0002
)

type winRect struct {
	left, top, right, bottom int32
}

var (
	verifyChromeMu   sync.Mutex
	verifyChromeProc uintptr
	verifyChromeJob  windows.Handle
	verifyChromeShow bool
	verifyChromeW    int
	verifyChromeH    int

	verifyGwlStyle   = int32(-16)
	verifyGwlExStyle = int32(-20)

	modUser32            = windows.NewLazySystemDLL("user32.dll")
	procShowWindow       = modUser32.NewProc("ShowWindow")
	procSetForegroundWin = modUser32.NewProc("SetForegroundWindow")
	procPostMessageW     = modUser32.NewProc("PostMessageW")
	procIsWindow         = modUser32.NewProc("IsWindow")
	procGetWindowLongPtr = modUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr = modUser32.NewProc("SetWindowLongPtrW")
	procSetWindowPos     = modUser32.NewProc("SetWindowPos")
	procSetWindowTextW   = modUser32.NewProc("SetWindowTextW")
	procSystemParamsInfo = modUser32.NewProc("SystemParametersInfoW")
	procGetClassNameW    = modUser32.NewProc("GetClassNameW")
	procGetParent        = modUser32.NewProc("GetParent")
	procGetWindow        = modUser32.NewProc("GetWindow")

	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	modDwmapi              = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttr   = modDwmapi.NewProc("DwmSetWindowAttribute")
)

func restyleVerificationJobWindows(job windows.Handle, visible bool, width, height int) {
	if job == 0 {
		return
	}
	if width <= 0 {
		width = verificationPopupWidth
	}
	if height <= 0 {
		height = verificationPopupHeight
	}
	if verifyChromeProc == 0 {
		verifyChromeProc = windows.NewCallback(enumVerificationChromeWindow)
	}
	verifyChromeMu.Lock()
	verifyChromeJob = job
	verifyChromeShow = visible
	verifyChromeW = width
	verifyChromeH = height
	verifyChromeMu.Unlock()
	_ = windows.EnumWindows(verifyChromeProc, nil)
	verifyChromeMu.Lock()
	verifyChromeJob = 0
	verifyChromeMu.Unlock()
}

func enumVerificationChromeWindow(hwnd uintptr, _ uintptr) uintptr {
	verifyChromeMu.Lock()
	job := verifyChromeJob
	visible := verifyChromeShow
	width := verifyChromeW
	height := verifyChromeH
	verifyChromeMu.Unlock()
	if job == 0 || !hwndBelongsToJob(hwnd, job) || !isVerificationEdgeAppWindow(hwnd) {
		return 1
	}
	applyVerificationWindowChrome(windows.HWND(hwnd), visible, width, height)
	return 1
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

func applyVerificationWindowChrome(hwnd windows.HWND, visible bool, width, height int) {
	alive, _, _ := procIsWindow.Call(uintptr(hwnd))
	if alive == 0 {
		return
	}

	styleIndex := verifyGwlStyle
	exIndex := verifyGwlExStyle
	style, _, _ := procGetWindowLongPtr.Call(uintptr(hwnd), uintptr(styleIndex))
	style &^= uintptr(wsThickFrame | wsMinimizeBox | wsMaximizeBox)
	style |= uintptr(wsCaption | wsSysMenu)
	_, _, _ = procSetWindowLongPtr.Call(uintptr(hwnd), uintptr(styleIndex), style)

	ex, _, _ := procGetWindowLongPtr.Call(uintptr(hwnd), uintptr(exIndex))
	ex |= uintptr(wsExToolWindow)
	ex &^= uintptr(wsExAppWindow)
	if visible {
		ex &^= uintptr(wsExNoActivate)
	} else {
		ex |= uintptr(wsExNoActivate)
	}
	_, _, _ = procSetWindowLongPtr.Call(uintptr(hwnd), uintptr(exIndex), ex)

	titlePtr, err := syscall.UTF16PtrFromString("Auralis")
	if err == nil {
		_, _, _ = procSetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(titlePtr)))
	}
	applyDawnCaptionColors(hwnd)

	flags := uintptr(swpFrameChanged)
	x, y := int32(verificationOffscreenX), int32(verificationOffscreenY)
	if visible {
		x, y = centeredVerificationOrigin(width, height)
	} else {
		flags |= swpNoActivate
	}
	_, _, _ = procSetWindowPos.Call(
		uintptr(hwnd),
		0,
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		flags,
	)
	if visible {
		_, _, _ = procShowWindow.Call(uintptr(hwnd), swRestore)
		_, _, _ = procSetForegroundWin.Call(uintptr(hwnd))
	}
}

func centeredVerificationOrigin(width, height int) (int32, int32) {
	var work winRect
	_, _, _ = procSystemParamsInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&work)), 0)
	areaW := work.right - work.left
	areaH := work.bottom - work.top
	if areaW <= 0 || areaH <= 0 {
		return 80, 80
	}
	x := work.left + (areaW-int32(width))/2
	y := work.top + (areaH-int32(height))/2
	if x < work.left {
		x = work.left
	}
	if y < work.top {
		y = work.top
	}
	return x, y
}

func applyDawnCaptionColors(hwnd windows.HWND) {
	if procDwmSetWindowAttr.Find() != nil {
		return
	}
	caption := uint32(0x00F4F2F1) // #f1f2f4 paper
	text := uint32(0x00332B26)    // #262b33 ink
	border := uint32(0x00DCD7D4)
	hwndVal := uintptr(hwnd)
	_, _, _ = procDwmSetWindowAttr.Call(hwndVal, dwmwaCaptionColor, uintptr(unsafe.Pointer(&caption)), 4)
	_, _, _ = procDwmSetWindowAttr.Call(hwndVal, dwmwaTextColor, uintptr(unsafe.Pointer(&text)), 4)
	_, _, _ = procDwmSetWindowAttr.Call(hwndVal, dwmwaBorderColor, uintptr(unsafe.Pointer(&border)), 4)
}
