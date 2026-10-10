//go:build windows

package backend

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// End-to-end local fixture: the real isolated Edge page receives an HttpOnly
// cookie, the confirm path captures it on the page CDP target, and the source
// API accepts that identity. It never contacts a real verification service.
func TestNativeConfiguredVerificationConfirmsSession(t *testing.T) {
	if os.Getenv("AURALIS_VERIFY_NATIVE") == "" {
		t.Skip("native Edge fixture is opt-in")
	}
	t.Setenv(appDataDirEnv, t.TempDir())
	t.Cleanup(resetSourceVerificationForTest)
	pumpedAuralisTestWindow(t)
	var server *httptest.Server
	var pageServed atomic.Bool
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.SetCookie(w, &http.Cookie{Name: "local-session", Value: "synthetic", Path: "/", HttpOnly: true})
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<!doctype html><title>Local verification fixture</title><p>Local session is ready.</p>")
			pageServed.Store(true)
			return
		}
		cookie, err := r.Cookie("local-session")
		if err != nil || cookie.Value != "synthetic" {
			http.Error(w, "session required", 401)
			return
		}
		if !strings.Contains(r.UserAgent(), "Edg/") {
			t.Error("API did not use the captured browser user agent")
		}
		fmt.Fprint(w, testHiFiPayload(55130631, server.URL+"/audio", "FULL", "NONE"))
	}))
	defer server.Close()
	source := CommunitySource{ID: "local-native-verification", Name: "Local fixture", Service: "tidal", Protocol: "hifi", BaseURL: server.URL, Enabled: true}
	if err := SaveCommunitySources([]CommunitySource{source}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		result, err := VerifyCommunitySource(source.ID)
		if err == nil && (result.State != "available" || result.AudioVerified) {
			err = fmt.Errorf("API verification result was not available")
		}
		done <- err
	}()
	t.Cleanup(CloseVerificationWindow)
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		p := GetVerificationPresentation()
		if p.Active {
			_ = SetVerificationViewport(p.ID, 12, 16, 640, 400)
		}
		if p.Ready && pageServed.Load() {
			ok, err := ConfirmSourceVerification()
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatal("the local fixture session was not accepted")
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("verification did not finish after confirmation")
			}
			if GetVerificationPresentation().Active {
				t.Fatal("verification remained active after confirmation")
			}
			return
		}
		select {
		case err := <-done:
			t.Fatalf("verification ended early: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the local verification page did not become ready (host ready=%t, page served=%t)", GetVerificationPresentation().Ready, pageServed.Load())
}

// pumpedAuralisTestWindow stands in for the Wails window. Like the real one it
// pumps messages on its own thread, which a reparented Edge window relies on:
// moves and redraws of a cross-process child wait for the parent to respond.
func pumpedAuralisTestWindow(t *testing.T) windows.HWND {
	t.Helper()
	const wsOverlappedWindow = uintptr(0x00CF0000)
	readyParent := make(chan windows.HWND)
	stopParent := make(chan struct{})
	doneParent := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(doneParent)
		parent := createTestWindow(t, auralisMainWindowClass, auralisMainWindowTitle, wsOverlappedWindow, 0)
		readyParent <- parent
		peek := modUser32.NewProc("PeekMessageW")
		translate := modUser32.NewProc("TranslateMessage")
		dispatch := modUser32.NewProc("DispatchMessageW")
		var msg struct {
			Hwnd           uintptr
			Message        uint32
			WParam, LParam uintptr
			Time           uint32
			X, Y           int32
			Private        uint32
		}
		for {
			for {
				ok, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1)
				if ok == 0 {
					break
				}
				translate.Call(uintptr(unsafe.Pointer(&msg)))
				dispatch.Call(uintptr(unsafe.Pointer(&msg)))
			}
			select {
			case <-stopParent:
				procDestroyWindow.Call(uintptr(parent))
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	parent := <-readyParent
	t.Cleanup(func() { close(stopParent); <-doneParent })
	return parent
}

const errorClassAlreadyExists = 1410

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

var (
	testUser32           = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassExW = testUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = testUser32.NewProc("CreateWindowExW")
	procDestroyWindow    = testUser32.NewProc("DestroyWindow")
	procDefWindowProcW   = testUser32.NewProc("DefWindowProcW")
)

func currentTestModule() uintptr {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	handle, _, _ := kernel.NewProc("GetModuleHandleW").Call(0)
	return handle
}

func registerTestWindowClass(t *testing.T, class string) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(class)
	if err != nil {
		t.Fatal(err)
	}
	cls := wndClassExW{
		LpfnWndProc:   procDefWindowProcW.Addr(),
		HInstance:     currentTestModule(),
		LpszClassName: name,
	}
	cls.CbSize = uint32(unsafe.Sizeof(cls))
	atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&cls)))
	if atom == 0 {
		if errno, ok := callErr.(windows.Errno); !ok || errno != errorClassAlreadyExists {
			t.Fatalf("register %s: %v", class, callErr)
		}
	}
}

func createTestWindow(t *testing.T, class, title string, style, exStyle uintptr) windows.HWND {
	t.Helper()
	registerTestWindowClass(t, class)
	className, err := windows.UTF16PtrFromString(class)
	if err != nil {
		t.Fatal(err)
	}
	titleName, err := windows.UTF16PtrFromString(title)
	if err != nil {
		t.Fatal(err)
	}
	x := int32(verificationOffscreenX)
	y := int32(verificationOffscreenY)
	hwnd, _, callErr := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titleName)),
		style,
		uintptr(x),
		uintptr(y),
		400,
		300,
		0,
		0,
		currentTestModule(),
		0,
	)
	if hwnd == 0 {
		t.Fatalf("create %s: %v", class, callErr)
	}
	t.Cleanup(func() {
		_, _, _ = procDestroyWindow.Call(hwnd)
	})
	return windows.HWND(hwnd)
}

func TestPlaceVerificationWindowStaysChild(t *testing.T) {
	const (
		wsOverlappedWindow = uintptr(0x00CF0000)
		wsPopup            = uintptr(0x80000000)
		wsCaption          = uintptr(0x00C00000)
		wsVisible          = uintptr(0x10000000)
		wsExAppWindow      = uintptr(0x00040000)
	)
	parent := createTestWindow(t, auralisMainWindowClass, auralisMainWindowTitle, wsOverlappedWindow, 0)
	popup := createTestWindow(t, verificationAppWindowClass, "Edge", wsPopup|wsCaption, wsExAppWindow)
	parentStyle := windowLong(parent, verifyGwlStyle)
	parentTitle := verificationWindowTitle(uintptr(parent))

	if placeVerificationWindow(popup, 0, 10, 20, 200, 100, true) {
		t.Fatal("browser attached without a parent")
	}
	if verificationWindowVisible(uintptr(popup)) {
		t.Fatal("verification window was visible without a parent")
	}
	if windowLong(parent, verifyGwlStyle) != parentStyle || verificationWindowTitle(uintptr(parent)) != parentTitle {
		t.Fatal("hiding the browser changed the Auralis window")
	}

	if !placeVerificationWindow(popup, parent, 30, 40, 180, 120, true) {
		t.Fatal("parented browser did not attach")
	}
	gotParent, _, _ := procGetParent.Call(uintptr(popup))
	if gotParent != uintptr(parent) {
		t.Fatalf("parent = %#x, want the Auralis window %#x", gotParent, uintptr(parent))
	}
	style := windowLong(popup, verifyGwlStyle)
	if style&wsChildStyle == 0 || style&wsPopupStyle != 0 || style&wsCaptionStyle != 0 {
		t.Fatalf("embedded style = %#x", style)
	}
	if windowLong(parent, verifyGwlStyle) != parentStyle || verificationWindowTitle(uintptr(parent)) != parentTitle {
		t.Fatal("embedding changed the Auralis titlebar or style")
	}

	if !placeVerificationWindow(popup, parent, 0, 0, 0, 0, true) {
		t.Fatal("zero viewport detached the child")
	}
	if verificationWindowVisible(uintptr(popup)) {
		t.Fatal("zero viewport left the native view visible")
	}
	gotParent, _, _ = procGetParent.Call(uintptr(popup))
	if gotParent != uintptr(parent) {
		t.Fatal("hiding the viewport unparented the browser")
	}
}

func TestFindAuralisMainWindowIgnoresBrowserWindows(t *testing.T) {
	const wsOverlappedWindow = uintptr(0x00CF0000)
	const wsPopup = uintptr(0x80000000)
	parent := createTestWindow(t, auralisMainWindowClass, auralisMainWindowTitle, wsOverlappedWindow, 0)
	_ = createTestWindow(t, verificationAppWindowClass, "Edge popup", wsPopup, 0)
	_ = createTestWindow(t, "AuralisDecoyWindow", auralisMainWindowTitle, wsOverlappedWindow, 0)
	found := findAuralisMainWindow()
	if found != parent {
		t.Fatalf("main window = %#x, want %#x", uintptr(found), uintptr(parent))
	}
}

func TestSuppressVerificationPopupDoesNotShowIt(t *testing.T) {
	popup := createTestWindow(t, verificationAppWindowClass, "popup", uintptr(0x80000000|0x00C00000)|wsVisibleStyle, wsExAppWindowStyle)
	suppressVerificationPopup(popup)
	if verificationWindowVisible(uintptr(popup)) {
		t.Fatal("suppressed popup stayed visible")
	}
	if verificationMayShowTopLevel() {
		t.Fatal("popup suppression must not fall back to a top-level window")
	}
}

// assertVerificationShowsOnlyPage checks that Edge's page covers the requested
// viewport and that its window region excludes the title bar and page border.
func assertVerificationShowsOnlyPage(t *testing.T, job windows.Handle, child, parent windows.HWND, left, top, width, height int) {
	t.Helper()
	// The test parent is never shown, so IsWindowVisible stays false; the
	// child's own WS_VISIBLE bit records whether placement revealed it.
	deadline := time.Now().Add(10 * time.Second)
	for windowLong(child, verifyGwlStyle)&wsVisibleStyle == 0 {
		if time.Now().After(deadline) {
			insets, ok := measureVerificationFrame(child)
			t.Fatalf("Edge page never measured a stable frame (last insets %+v ok=%v)", insets, ok)
		}
		// The app's appearance loop path, after Edge stopped being top-level.
		restyleVerificationJobWindows(job, parent, left, top, width, height, true)
		time.Sleep(200 * time.Millisecond)
	}
	page, ok := verificationPageRect(child)
	if !ok {
		t.Fatal("Edge page widget disappeared")
	}
	var origin struct{ X, Y int32 }
	clientToScreen := testUser32.NewProc("ClientToScreen")
	clientToScreen.Call(uintptr(parent), uintptr(unsafe.Pointer(&origin)))
	// The page overhangs the viewport by the margin so Edge's rounded page
	// border stays outside the clip.
	m := verificationContentMargin(parent)
	got := verificationRect{page.left - int(origin.X), page.top - int(origin.Y), page.right - int(origin.X), page.bottom - int(origin.Y)}
	if want := (verificationRect{left - m, top - m, left + width + m, top + height + m}); got != want {
		t.Fatalf("Edge page at %+v in the Auralis window, want %+v", got, want)
	}
	var window windows.Rect
	if !verificationWindowRect(child, &window) {
		t.Fatal("Edge window rect unavailable")
	}
	var box windows.Rect
	getRgnBox := testUser32.NewProc("GetWindowRgnBox")
	if kind, _, _ := getRgnBox.Call(uintptr(child), uintptr(unsafe.Pointer(&box))); kind != 2 { // SIMPLEREGION
		t.Fatalf("Edge window region kind = %d, want a rectangle", kind)
	}
	clip := verificationRect{int(box.Left) + int(window.Left), int(box.Top) + int(window.Top), int(box.Right) + int(window.Left), int(box.Bottom) + int(window.Top)}
	if want := (verificationRect{page.left + m, page.top + m, page.right - m, page.bottom - m}); clip != want {
		t.Fatalf("Edge clip %+v, want the page %+v inset by %d", clip, page, m)
	}
}

func TestNativeVerificationEdgeEmbedsInParent(t *testing.T) {
	if os.Getenv("AURALIS_VERIFY_NATIVE") == "" {
		t.Skip("set AURALIS_VERIFY_NATIVE=1 to launch a hidden Edge process")
	}
	edge := microsoftEdgePath()
	if edge == "" {
		t.Skip("Microsoft Edge is not installed")
	}
	parent := pumpedAuralisTestWindow(t)
	parentStyle := windowLong(parent, verifyGwlStyle)
	parentTitle := verificationWindowTitle(uintptr(parent))
	profile := filepath.Join(t.TempDir(), "edge-profile")
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	process, thread, err := startVerificationBrowser(verificationBrowserArgs(edge, profile, ""))
	if err != nil {
		t.Fatal(err)
	}
	job, err := assignVerificationJob(process)
	if err != nil {
		windows.CloseHandle(thread)
		terminateVerifyBrowser(0, process)
		closeVerificationHandles(0, process)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		terminateVerifyBrowser(job, process)
		closeVerificationHandles(job, process)
	})
	if _, err := windows.ResumeThread(thread); err != nil {
		windows.CloseHandle(thread)
		t.Fatal(err)
	}
	windows.CloseHandle(thread)

	deadline := time.Now().Add(25 * time.Second)
	var child windows.HWND
	for time.Now().Before(deadline) {
		child = restyleVerificationJobWindows(job, parent, 12, 16, 220, 140, true)
		if child != 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if child == 0 {
		t.Fatal("Edge window did not attach to the Auralis window")
	}
	gotParent, _, _ := procGetParent.Call(uintptr(child))
	if gotParent != uintptr(parent) {
		t.Fatalf("Edge parent = %#x", gotParent)
	}
	style := windowLong(child, verifyGwlStyle)
	if style&wsChildStyle == 0 || style&wsPopupStyle != 0 {
		t.Fatalf("Edge stayed top-level: %#x", style)
	}
	if windowLong(parent, verifyGwlStyle) != parentStyle || verificationWindowTitle(uintptr(parent)) != parentTitle {
		t.Fatal("Edge attach changed the Auralis window")
	}
	assertVerificationShowsOnlyPage(t, job, child, parent, 12, 16, 220, 140)
	if !placeVerificationWindow(child, parent, 0, 0, 0, 0, true) {
		t.Fatal("zero viewport detached Edge")
	}
	if verificationWindowVisible(uintptr(child)) {
		t.Fatal("zero viewport left Edge visible")
	}
}
