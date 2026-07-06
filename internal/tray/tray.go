//go:build windows

// Package tray implements a Windows system-tray icon for sieve.
// It uses direct Win32 calls (Shell_NotifyIconW, CreateWindowExW, etc.)
// through golang.org/x/sys/windows — no external library needed.
//
//nolint:errcheck
package tray

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── Windows API ─────────────────────────────────────────────────────────────

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procGetConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")

	procLoadIconW           = user32.NewProc("LoadIconW")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procShowWindowProc      = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procUnregisterClassW    = user32.NewProc("UnregisterClassW")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
)

// ── Constants ────────────────────────────────────────────────────────────────

const (
	// Shell_NotifyIcon actions
	nimAdd    uint32 = 0
	nimDelete uint32 = 2

	// NIF flags
	nifMessage uint32 = 1
	nifIcon    uint32 = 2
	nifTip     uint32 = 4

	// wmTrayMsg is the custom callback message registered with Shell_NotifyIconW.
	// Windows sends it to our hidden window with the mouse event in lParam.
	wmTrayMsg uint32 = 0x8001 // WM_APP + 1
	// wmStop is posted to our window to terminate the message loop on exit.
	wmStop uint32 = 0x8002

	// Mouse events received via lParam of wmTrayMsg
	wmLButtonUp     uint32 = 0x0202
	wmRButtonUp     uint32 = 0x0205
	wmLButtonDblClk uint32 = 0x0203

	// ShowWindow commands
	swHide    = uintptr(0)
	swRestore = uintptr(9)

	// AppendMenuW flags
	mfString    uint32 = 0x0000
	mfSeparator uint32 = 0x0800

	// TrackPopupMenu flags
	tpmRightButton uint32 = 0x0002
	tpmRightAlign  uint32 = 0x0008
	tpmBottomAlign uint32 = 0x0020
	tpmReturnCmd   uint32 = 0x0100
	tpmNoNotify    uint32 = 0x0080

	// Context-menu item IDs
	menuRestore uintptr = 1
	menuQuit    uintptr = 2

	// IDI_APPLICATION (standard icon fallback)
	idiApplication = uintptr(32512)
)

// ── Win32 structs ────────────────────────────────────────────────────────────

// wndClassExW mirrors WNDCLASSEXW.
type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

// notifyIconData mirrors NOTIFYICONDATAW (full v3 layout for correct cbSize).
type notifyIconData struct {
	cbSize           uint32
	hWnd             windows.HWND
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

// winPoint mirrors POINT.
type winPoint struct{ x, y int32 }

// winMsg mirrors MSG.
// Field order and alignment match Win32 on amd64:
//
//	HWND(8) UINT(4) [pad4] WPARAM(8) LPARAM(8) DWORD(4) [pad4] POINT(8)
type winMsg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      winPoint
}

// ── Package-level wndProc callback ───────────────────────────────────────────

// activeMgr holds the singleton Manager so the static wndProc can reach it.
var activeMgr *Manager

// wndProcCB is the registered window-procedure callback; kept in a var so the
// GC never collects the underlying function pointer.
var wndProcCB = syscall.NewCallback(func(hwnd, message, wparam, lparam uintptr) uintptr {
	if m := activeMgr; m != nil {
		if ret, handled := m.handleMsg(windows.HWND(hwnd), uint32(message), wparam, lparam); handled {
			return ret
		}
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, message, wparam, lparam)
	return ret
})

// ── Manager ──────────────────────────────────────────────────────────────────

// Manager owns a hidden Win32 message window and a Shell_NotifyIcon entry.
// All exported methods are safe to call from any goroutine.
type Manager struct {
	mu        sync.Mutex
	hwnd      windows.HWND
	hIcon     uintptr
	iconAdded bool
	onRestore func()
	onQuit    func()
	readyC    chan struct{}
}

// IsAvailable reports whether the tray-minimise feature is safe to use.
// When sieve runs inside an existing console (PowerShell, cmd.exe …) hiding
// the console window would also hide the host shell, so the feature is
// disabled in that case.
func IsAvailable() bool {
	var pids [16]uint32
	n, _, _ := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&pids[0])),
		uintptr(len(pids)),
	)
	// n == 1 → only sieve itself is attached → we own this console.
	return n <= 1
}

// New creates a Manager, registers the Win32 message window, and returns once
// it is ready to receive messages. onRestore is called when the user wants to
// bring the window back; onQuit is called when the user chooses to exit.
func New(onRestore, onQuit func()) *Manager {
	m := &Manager{
		onRestore: onRestore,
		onQuit:    onQuit,
		readyC:    make(chan struct{}),
	}
	activeMgr = m
	go m.runLoop()
	<-m.readyC
	return m
}

// Show adds the tray icon and hides the console window.
func (m *Manager) Show() {
	m.mu.Lock()
	hwnd := m.hwnd
	hIcon := m.hIcon
	added := m.iconAdded
	m.mu.Unlock()

	if hwnd == 0 {
		return
	}
	if !added {
		m.addIcon(hwnd, hIcon)
		m.mu.Lock()
		m.iconAdded = true
		m.mu.Unlock()
	}
	hideConsole()
}

// Restore shows the console window and removes the tray icon.
func (m *Manager) Restore() {
	showConsole()

	m.mu.Lock()
	hwnd := m.hwnd
	added := m.iconAdded
	m.mu.Unlock()

	if added && hwnd != 0 {
		m.removeIcon(hwnd)
		m.mu.Lock()
		m.iconAdded = false
		m.mu.Unlock()
	}
}

// Stop removes the tray icon and terminates the message loop.
// Called on application exit.
func (m *Manager) Stop() {
	m.Restore()
	m.mu.Lock()
	hwnd := m.hwnd
	m.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(uintptr(hwnd), uintptr(wmStop), 0, 0)
	}
}

// ── Win32 message handling ───────────────────────────────────────────────────

func (m *Manager) handleMsg(hwnd windows.HWND, message uint32, _, lparam uintptr) (uintptr, bool) {
	switch message {
	case wmTrayMsg:
		switch uint32(lparam) {
		case wmLButtonUp, wmLButtonDblClk:
			go m.onRestore() // run off the message-loop thread
		case wmRButtonUp:
			m.showContextMenu(hwnd)
		}
		return 0, true
	case wmStop:
		procPostQuitMessage.Call(0)
		return 0, true
	}
	return 0, false
}

func (m *Manager) showContextMenu(hwnd windows.HWND) {
	// SetForegroundWindow is required so the menu dismisses when the user
	// clicks elsewhere (documented Win32 tray-menu requirement).
	procSetForegroundWindow.Call(uintptr(hwnd))

	var pt winPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	openPtr, _ := syscall.UTF16PtrFromString("Открыть")
	quitPtr, _ := syscall.UTF16PtrFromString("Выйти")

	procAppendMenuW.Call(hMenu, uintptr(mfString), menuRestore, uintptr(unsafe.Pointer(openPtr)))
	procAppendMenuW.Call(hMenu, uintptr(mfSeparator), 0, 0)
	procAppendMenuW.Call(hMenu, uintptr(mfString), menuQuit, uintptr(unsafe.Pointer(quitPtr)))

	// TPM_RETURNCMD makes TrackPopupMenu return the selected item ID directly.
	flags := uintptr(tpmRightButton | tpmRightAlign | tpmBottomAlign | tpmReturnCmd | tpmNoNotify)
	cmd, _, _ := procTrackPopupMenu.Call(
		hMenu, flags,
		uintptr(pt.x), uintptr(pt.y),
		0, uintptr(hwnd), 0,
	)
	// Flush the internal menu message so WM_NULL reaches the window.
	procPostMessageW.Call(uintptr(hwnd), 0 /*WM_NULL*/, 0, 0)

	switch cmd {
	case menuRestore:
		go m.onRestore()
	case menuQuit:
		go m.onQuit()
	}
}

// ── Tray icon lifecycle ──────────────────────────────────────────────────────

func (m *Manager) addIcon(hwnd windows.HWND, hIcon uintptr) {
	nid := &notifyIconData{
		hWnd:             hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTrayMsg,
		hIcon:            hIcon,
	}
	nid.cbSize = uint32(unsafe.Sizeof(*nid))
	tip, _ := windows.UTF16FromString("sieve · работает")
	copy(nid.szTip[:], tip)
	procShellNotifyIconW.Call(uintptr(nimAdd), uintptr(unsafe.Pointer(nid)))
}

func (m *Manager) removeIcon(hwnd windows.HWND) {
	nid := &notifyIconData{hWnd: hwnd, uID: 1}
	nid.cbSize = uint32(unsafe.Sizeof(*nid))
	procShellNotifyIconW.Call(uintptr(nimDelete), uintptr(unsafe.Pointer(nid)))
}

// ── Message loop ─────────────────────────────────────────────────────────────

const trayClassName = "SieveTrayWnd"

func (m *Manager) runLoop() {
	// The Win32 message loop must run on a dedicated OS thread.
	runtime.LockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	m.hIcon = loadAppIcon(hInst)

	classPtr, _ := syscall.UTF16PtrFromString(trayClassName)
	winPtr, _ := syscall.UTF16PtrFromString("SieveTray")

	wcx := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   wndProcCB,
		hInstance:     hInst,
		lpszClassName: classPtr,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx)))

	// HWND_MESSAGE (-3) creates a message-only window: invisible, no taskbar
	// button, receives window messages — perfect for a tray notification sink.
	const hwndMessage = ^uintptr(2) // (HWND)-3
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classPtr)),
		uintptr(unsafe.Pointer(winPtr)),
		0,
		0, 0, 0, 0,
		hwndMessage,
		0, hInst, 0,
	)

	m.mu.Lock()
	m.hwnd = windows.HWND(hwnd)
	m.mu.Unlock()
	close(m.readyC) // unblock New()

	if hwnd == 0 {
		return
	}

	var message winMsg
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&message)),
			0, 0, 0,
		)
		// GetMessage returns 0 on WM_QUIT, (BOOL)-1 on error.
		if ret == 0 || ^ret == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}

	procUnregisterClassW.Call(uintptr(unsafe.Pointer(classPtr)), hInst)
}

// ── Icon loading ─────────────────────────────────────────────────────────────

func loadAppIcon(hInst uintptr) uintptr {
	// The rsrc tool embeds the application icon with resource ID 1.
	// MAKEINTRESOURCE(1): an integer resource ID is passed as the pointer value.
	hIcon, _, _ := procLoadIconW.Call(hInst, uintptr(1))
	if hIcon != 0 {
		return hIcon
	}
	// Fall back to the generic Windows application icon.
	hIcon, _, _ = procLoadIconW.Call(0, idiApplication)
	return hIcon
}

// ── Console visibility ───────────────────────────────────────────────────────

func hideConsole() {
	if hwnd := consoleHWND(); hwnd != 0 {
		procShowWindowProc.Call(uintptr(hwnd), swHide)
	}
}

func showConsole() {
	if hwnd := consoleHWND(); hwnd != 0 {
		procShowWindowProc.Call(uintptr(hwnd), swRestore)
		procSetForegroundWindow.Call(uintptr(hwnd))
	}
}

func consoleHWND() windows.HWND {
	ret, _, _ := procGetConsoleWindow.Call()
	return windows.HWND(ret)
}
