//go:build windows

package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Verification is shown in a dedicated Microsoft Edge --app window, not a
// second in-process WebView2. go-webview2's Chromium.Embed calls os.Exit(1)
// on environment/controller errors, which killed Auralis the moment a
// download needed Turnstile. A child Edge process cannot take the app down.
//
// Edge's launcher exits as soon as it spawns the real browser, and loading
// the branding extension restarts that browser. The restart aborts the first
// navigation with ERR_NETWORK_CHANGED ("A network change was detected") and
// leaves the visible window in a process this function no longer owns, so
// closing the launcher PID did not close the window. The browser is started
// suspended, placed in a kill-on-close job, and only then resumed, so every
// child dies with the job. The challenge itself is opened over DevTools
// after that restart settles, and reloaded if the interstitial is still up.

type verifySession struct {
	mu        sync.Mutex
	active    bool
	cancel    context.CancelFunc
	done      chan struct{}
	job       windows.Handle
	process   windows.Handle
	browserWS string
}

var (
	verifySessionState verifySession
	verifyStartMu      sync.Mutex
)

type verificationAppearance struct {
	mu        sync.Mutex
	visible   bool
	width     int
	height    int
	browserWS string
}

func OpenVerificationWindow(target string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("verification window panic: %v", recovered)
		}
	}()

	attempt := adoptVerificationAttempt()
	owned := false
	if attempt == nil {
		var armErr error
		attempt, armErr = armVerificationRun()
		if armErr != nil {
			return armErr
		}
		owned = true
	}
	if owned {
		defer attempt.complete()
	}
	ctx := attempt.ctx
	if ctx.Err() != nil {
		return ErrDownloadCancelled
	}
	if err := validateVerificationChallengeURL(target); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ErrDownloadCancelled
	}
	edgePath := microsoftEdgePath()
	if edgePath == "" {
		return fmt.Errorf("Microsoft Edge was not found")
	}

	verifyStartMu.Lock()
	defer verifyStartMu.Unlock()
	if ctx.Err() != nil {
		return ErrDownloadCancelled
	}

	verifySessionState.mu.Lock()
	if verifySessionState.active && verifySessionState.process != 0 {
		verifySessionState.mu.Unlock()
		return fmt.Errorf("verification window is already open")
	}
	profileDir := verifyProfileDir()
	if mkErr := os.MkdirAll(profileDir, 0700); mkErr != nil {
		verifySessionState.mu.Unlock()
		return fmt.Errorf("failed to prepare verification profile: %w", mkErr)
	}
	clearVerificationFaviconCache(profileDir)
	verifySessionState.mu.Unlock()

	if ctx.Err() != nil {
		return ErrDownloadCancelled
	}

	process, thread, startErr := startVerificationBrowser(verificationBrowserArgs(edgePath, profileDir, mustWriteVerifyExtension()))
	if startErr != nil {
		return fmt.Errorf("failed to open verification window: %w", startErr)
	}
	if ctx.Err() != nil {
		windows.CloseHandle(thread)
		terminateVerifyBrowser(0, process)
		closeVerificationHandles(0, process)
		return ErrDownloadCancelled
	}
	job, jobErr := assignVerificationJob(process)
	if jobErr != nil {
		windows.CloseHandle(thread)
		terminateVerifyBrowser(0, process)
		closeVerificationHandles(0, process)
		return fmt.Errorf("failed to manage verification window processes: %w", jobErr)
	}
	if ctx.Err() != nil {
		windows.CloseHandle(thread)
		terminateVerifyBrowser(job, process)
		closeVerificationHandles(job, process)
		return ErrDownloadCancelled
	}
	if _, resumeErr := windows.ResumeThread(thread); resumeErr != nil {
		windows.CloseHandle(thread)
		terminateVerifyBrowser(job, process)
		closeVerificationHandles(job, process)
		return fmt.Errorf("failed to open verification window: %w", resumeErr)
	}
	windows.CloseHandle(thread)
	if ctx.Err() != nil {
		terminateVerifyBrowser(job, process)
		closeVerificationHandles(job, process)
		return ErrDownloadCancelled
	}

	verifySessionState.mu.Lock()
	verifySessionState.active = true
	verifySessionState.job = job
	verifySessionState.process = process
	verifySessionState.browserWS = ""
	verifySessionState.mu.Unlock()

	appearance := &verificationAppearance{
		width:  verificationPopupWidth,
		height: verificationPopupHeight,
	}
	chromeDone := make(chan struct{})
	iconDone := make(chan struct{})
	go func() {
		defer close(chromeDone)
		manageVerificationAppearance(ctx, job, appearance)
	}()
	go func() {
		defer close(iconDone)
		refreshVerificationWindowIcons(ctx, job)
	}()

	defer func() {
		attempt.cancel()
		<-chromeDone
		<-iconDone
		verifySessionState.mu.Lock()
		jobHandle := verifySessionState.job
		processHandle := verifySessionState.process
		verifySessionState.job = 0
		verifySessionState.process = 0
		verifySessionState.active = false
		verifySessionState.browserWS = ""
		verifySessionState.mu.Unlock()
		closeVerificationHandles(jobHandle, processHandle)
	}()

	browserWS, driveErr := driveVerificationPage(ctx, profileDir, target)
	verifySessionState.mu.Lock()
	verifySessionState.browserWS = browserWS
	verifySessionState.mu.Unlock()
	appearance.mu.Lock()
	appearance.browserWS = browserWS
	appearance.mu.Unlock()
	if driveErr != nil && ctx.Err() == nil && !errors.Is(driveErr, errVerificationClosed) {
		if verificationBrowserAlive(job, process) {
			terminateVerifyBrowser(job, process)
		}
		return driveErr
	}
	return waitForVerificationLifecycle(ctx, job, process, profileDir, appearance)
}

func CloseVerificationWindow() {
	attempt := adoptVerificationAttempt()
	if attempt != nil {
		attempt.cancel()
	}
	verifySessionState.mu.Lock()
	job := verifySessionState.job
	process := verifySessionState.process
	browserWS := verifySessionState.browserWS
	verifySessionState.mu.Unlock()
	closeVerifyDebugger(browserWS)
	terminateVerifyBrowser(job, process)
	if attempt != nil {
		<-attempt.done
	}
}

func verificationBrowserArgs(edgePath, profileDir, extensionDir string) []string {
	// One --disable-features flag. A second copy replaces the first on some
	// Edge builds, which dropped the loopback exception the grant callback needs.
	features := strings.Join([]string{
		"DisableLoadExtensionCommandLineSwitch",
		"LocalNetworkAccessChecks",
		"BlockInsecurePrivateNetworkRequests",
		"PrivateNetworkAccessSendPreflights",
		"PrivateNetworkAccessRespectPreflightResults",
	}, ",")
	args := []string{
		edgePath,
		"--app=about:blank",
		fmt.Sprintf("--window-size=%d,%d", verificationPopupWidth, verificationPopupHeight),
		fmt.Sprintf("--window-position=%d,%d", verificationOffscreenX, verificationOffscreenY),
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-features=" + features,
		"--remote-debugging-port=0",
		"--remote-allow-origins=*",
	}
	if strings.TrimSpace(extensionDir) != "" {
		args = append(args,
			"--load-extension="+extensionDir,
			"--disable-extensions-except="+extensionDir,
		)
	}
	return args
}

func clearVerificationFaviconCache(profileDir string) {
	base := filepath.Join(profileDir, "Default")
	for _, name := range []string{"Favicons", "Favicons-journal"} {
		_ = os.Remove(filepath.Join(base, name))
	}
}

func startVerificationBrowser(args []string) (windows.Handle, windows.Handle, error) {
	if len(args) == 0 {
		return 0, 0, fmt.Errorf("missing browser command")
	}
	exe, err := windows.UTF16PtrFromString(args[0])
	if err != nil {
		return 0, 0, err
	}
	command := windowsCommandLine(args)
	cmdUTF16, err := windows.UTF16FromString(command)
	if err != nil {
		return 0, 0, err
	}
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	const (
		startfUseShowWindow = 0x00000001
		startfUseSize       = 0x00000002
		startfUsePosition   = 0x00000004
	)
	si.Flags = startfUseShowWindow | startfUsePosition | startfUseSize
	si.ShowWindow = swHide
	offX := int32(verificationOffscreenX)
	offY := int32(verificationOffscreenY)
	si.X = uint32(offX)
	si.Y = uint32(offY)
	si.XSize = uint32(verificationPopupWidth)
	si.YSize = uint32(verificationPopupHeight)
	var pi windows.ProcessInformation
	err = windows.CreateProcess(
		exe,
		&cmdUTF16[0],
		nil,
		nil,
		false,
		windows.CREATE_SUSPENDED|windows.CREATE_NEW_PROCESS_GROUP,
		nil,
		nil,
		&si,
		&pi,
	)
	if err != nil {
		return 0, 0, err
	}
	return pi.Process, pi.Thread, nil
}

func windowsCommandLine(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = windows.EscapeArg(arg)
	}
	return strings.Join(quoted, " ")
}

func createVerifyJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	ret, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if ret == 0 {
		windows.CloseHandle(job)
		if err == nil {
			err = fmt.Errorf("SetInformationJobObject failed")
		}
		return 0, err
	}
	return job, nil
}

func assignVerificationJob(process windows.Handle) (windows.Handle, error) {
	job, err := createVerifyJob()
	if err != nil {
		return 0, err
	}
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func terminateVerifyBrowser(job, process windows.Handle) {
	if job != 0 {
		_ = windows.TerminateJobObject(job, 1)
	}
	if process != 0 {
		_ = windows.TerminateProcess(process, 1)
	}
}

func closeVerificationHandles(job, process windows.Handle) {
	if job != 0 {
		windows.CloseHandle(job)
	}
	if process != 0 {
		windows.CloseHandle(process)
	}
}

type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func jobActiveProcesses(job windows.Handle) int {
	if job == 0 {
		return 0
	}
	var info jobAccounting
	err := windows.QueryInformationJobObject(
		job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	)
	if err != nil {
		return -1
	}
	return int(info.ActiveProcesses)
}

func verificationBrowserAlive(job, process windows.Handle) bool {
	if job != 0 {
		return jobActiveProcesses(job) != 0
	}
	if process == 0 {
		return false
	}
	wait, err := windows.WaitForSingleObject(process, 0)
	return err == nil && wait != windows.WAIT_OBJECT_0
}

func waitForVerifyJob(ctx context.Context, job windows.Handle) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if jobActiveProcesses(job) == 0 {
				return
			}
		}
	}
}

func manageVerificationAppearance(ctx context.Context, job windows.Handle, state *verificationAppearance) {
	if job == 0 || state == nil {
		return
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	revealPasses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if jobActiveProcesses(job) == 0 {
				return
			}
			state.mu.Lock()
			visible := state.visible
			width := state.width
			height := state.height
			browserWS := state.browserWS
			state.mu.Unlock()
			if visible {
				if revealPasses >= 8 {
					continue
				}
				revealPasses++
			} else {
				revealPasses = 0
			}
			restyleVerificationJobWindows(job, visible, width, height)
			if browserWS != "" {
				cctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
				_ = setVerificationBrowserVisible(cctx, browserWS, visible, width, height)
				cancel()
			}
		}
	}
}

func waitForVerificationLifecycle(ctx context.Context, job, process windows.Handle, profileDir string, appearance *verificationAppearance) error {
	grace := verificationBackgroundGrace
	if grace <= 0 {
		grace = verificationBackgroundGraceDefault
	}
	started := time.Now()
	graceTimer := time.NewTimer(grace)
	defer graceTimer.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	revealed := false

	for {
		alive := verificationBrowserAlive(job, process)
		pending := alive && ctx.Err() == nil
		if verificationShouldReveal(time.Since(started), grace, pending) && !revealed {
			width, height := verificationPopupWidth, verificationPopupHeight
			if w, h, err := measureVerificationPopupSize(ctx, profileDir); err == nil && w > 0 && h > 0 {
				width, height = w, h
			}
			if appearance != nil {
				appearance.mu.Lock()
				appearance.visible = true
				appearance.width = width
				appearance.height = height
				appearance.mu.Unlock()
			}
			restyleVerificationJobWindows(job, true, width, height)
			revealed = true
		}
		select {
		case <-ctx.Done():
			return verificationWindowOutcome(true, false, false)
		case <-graceTimer.C:
			continue
		case <-ticker.C:
			if !verificationBrowserAlive(job, process) {
				if ctx.Err() != nil {
					return verificationWindowOutcome(true, false, false)
				}
				return verificationWindowOutcome(false, true, false)
			}
		}
	}
}

func waitForVerifyProcess(ctx context.Context, process windows.Handle) {
	if process == 0 {
		return
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !verificationBrowserAlive(0, process) {
				return
			}
		}
	}
}

const wmSetIcon = 0x0080

var (
	verifyIconOnce  sync.Once
	verifyIconProc  uintptr
	verifyIconMu    sync.Mutex
	verifyIconJob   windows.Handle
	verifyIconBig   windows.Handle
	verifyIconSmall windows.Handle

	procIsWindowVisible          = modUser32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId = modUser32.NewProc("GetWindowThreadProcessId")
	procIsProcessInJob           = modKernel32.NewProc("IsProcessInJob")
	procExtractIconExW           = windows.NewLazySystemDLL("shell32.dll").NewProc("ExtractIconExW")
)

func refreshVerificationWindowIcons(ctx context.Context, job windows.Handle) {
	if job == 0 {
		return
	}
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for range 30 {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if jobActiveProcesses(job) == 0 {
				return
			}
			applyAuralisWindowIcons(job)
		}
	}
}

func auralisWindowIcons() (windows.Handle, windows.Handle) {
	verifyIconOnce.Do(func() {
		verifyIconProc = windows.NewCallback(enumAuralisVerifyWindow)
		exe, err := os.Executable()
		if err != nil {
			return
		}
		exePtr, err := windows.UTF16PtrFromString(exe)
		if err != nil {
			return
		}
		_, _, _ = procExtractIconExW.Call(
			uintptr(unsafe.Pointer(exePtr)),
			0,
			uintptr(unsafe.Pointer(&verifyIconBig)),
			uintptr(unsafe.Pointer(&verifyIconSmall)),
			1,
		)
	})
	return verifyIconBig, verifyIconSmall
}

func applyAuralisWindowIcons(job windows.Handle) {
	if job == 0 || verifyIconProc == 0 && !iconsReady() {
		return
	}
	big, small := auralisWindowIcons()
	verifyIconMu.Lock()
	verifyIconJob = job
	verifyIconBig = big
	verifyIconSmall = small
	verifyIconMu.Unlock()
	_ = windows.EnumWindows(verifyIconProc, nil)
	verifyIconMu.Lock()
	verifyIconJob = 0
	verifyIconMu.Unlock()
}

func iconsReady() bool {
	auralisWindowIcons()
	return verifyIconProc != 0
}

func enumAuralisVerifyWindow(hwnd uintptr, _ uintptr) uintptr {
	verifyIconMu.Lock()
	job := verifyIconJob
	big := verifyIconBig
	small := verifyIconSmall
	verifyIconMu.Unlock()
	if job == 0 || !isVerificationEdgeAppWindow(hwnd) {
		return 1
	}
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	if visible == 0 {
		return 1
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return 1
	}
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 1
	}
	var inJob int32
	r, _, _ := procIsProcessInJob.Call(uintptr(proc), uintptr(job), uintptr(unsafe.Pointer(&inJob)))
	windows.CloseHandle(proc)
	if r == 0 || inJob == 0 {
		return 1
	}
	if big != 0 {
		_, _, _ = procPostMessageW.Call(hwnd, wmSetIcon, 1, uintptr(big))
	}
	if small != 0 {
		_, _, _ = procPostMessageW.Call(hwnd, wmSetIcon, 0, uintptr(small))
	}
	return 1
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
