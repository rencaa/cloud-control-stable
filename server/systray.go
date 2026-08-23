//go:build windows
// +build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	createWindowEx   = user32.NewProc("CreateWindowExW")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	getMessage       = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	postQuitMessage  = user32.NewProc("PostQuitMessage")
	registerClassEx  = user32.NewProc("RegisterClassExW")
	loadCursor       = user32.NewProc("LoadCursorW")
	loadIcon         = user32.NewProc("LoadIconW")
	getModuleHandle  = kernel32.NewProc("GetModuleHandleW")

	shellNotifyIcon     = shell32.NewProc("Shell_NotifyIconW")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
	getSystemMetrics    = user32.NewProc("GetSystemMetrics")

	// 托盘消息
	WM_USER        = uint32(0x0400)
	WM_TRAYICON    = WM_USER + 1
	WM_COMMAND     = uint32(0x0111)
	WM_DESTROY     = uint32(0x0002)
	WM_LBUTTONDOWN = uint32(0x0201)
	WM_RBUTTONUP   = uint32(0x0205)

	NIM_ADD     = uint32(0x00000000)
	NIM_DELETE  = uint32(0x00000002)
	NIM_MODIFY  = uint32(0x00000001)
	NIF_MESSAGE = uint32(0x00000001)
	NIF_ICON    = uint32(0x00000002)
	NIF_TIP     = uint32(0x00000004)

	TPM_RIGHTBUTTON = uint32(0x0002)
	TPM_BOTTOMALIGN = uint32(0x0004)
)

type NOTIFYICONDATA struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Timeout         uint32
	BalloonTitle    [64]uint16
	BalloonInfo     [256]uint16
	BalloonFlags    uint32
}

type WNDCLASSEX struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type POINT struct {
	X int32
	Y int32
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

var (
	windowClassRegistered bool
	trayWnd               uintptr
	onOpen                func()
	onExit                func()
	openURL               string
)

func createTrayIcon(wnd uintptr, tip string) {
	var iconData NOTIFYICONDATA
	iconData.Size = uint32(unsafe.Sizeof(iconData))
	iconData.Wnd = wnd
	iconData.ID = 1
	iconData.Flags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	iconData.CallbackMessage = WM_TRAYICON
	// IDI_APPLICATION = MAKEINTRESOURCE(32512)
	iconData.Icon, _, _ = loadIcon.Call(0, uintptr(32512))

	tipUTF16, _ := syscall.UTF16FromString(tip)
	copy(iconData.Tip[:], tipUTF16)

	shellNotifyIcon.Call(uintptr(NIM_ADD), uintptr(unsafe.Pointer(&iconData)))
}

func removeTrayIcon(wnd uintptr) {
	var iconData NOTIFYICONDATA
	iconData.Size = uint32(unsafe.Sizeof(iconData))
	iconData.Wnd = wnd
	iconData.ID = 1
	shellNotifyIcon.Call(uintptr(NIM_DELETE), uintptr(unsafe.Pointer(&iconData)))
}

func showPopupMenu(wnd uintptr) {
	menu := createPopupMenu()
	appendMenu(menu, 1, "打开主页")
	appendMenu(menu, 2, "-")
	appendMenu(menu, 3, "退出")

	setForegroundWindow.Call(wnd)

	var pt POINT
	getCursorPos(&pt)

	trackPopupMenu(menu, TPM_RIGHTBUTTON|TPM_BOTTOMALIGN, pt.X, pt.Y, wnd)
}

func windowProc(hwnd uintptr, msg uint32, wparam uintptr, lparam uintptr) uintptr {
	switch msg {
	case WM_TRAYICON:
		if lparam == uintptr(WM_LBUTTONDOWN) {
			// 单击托盘图标 → 打开主页
			if onOpen != nil {
				onOpen()
			}
		} else if lparam == uintptr(WM_RBUTTONUP) {
			// 右键托盘图标 → 弹出菜单
			showPopupMenu(hwnd)
		}
	case WM_COMMAND:
		switch wparam {
		case 1: // 打开主页
			if onOpen != nil {
				onOpen()
			}
		case 3: // 退出
			if onExit != nil {
				onExit()
			}
		}
	case WM_DESTROY:
		postQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := defWindowProc.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

func registerWindowClass() {
	if windowClassRegistered {
		return
	}
	className, _ := syscall.UTF16PtrFromString("CloudControlTray")
	var wc WNDCLASSEX
	wc.Size = uint32(unsafe.Sizeof(wc))
	wc.WndProc = syscall.NewCallback(windowProc)
	wc.Instance, _, _ = getModuleHandle.Call(0)
	wc.Cursor, _, _ = loadCursor.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("IDC_ARROW"))))
	wc.Icon, _, _ = loadIcon.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("IDI_APPLICATION"))))
	wc.ClassName = className
	registerClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	windowClassRegistered = true
}

func createTrayWindow() uintptr {
	className, _ := syscall.UTF16PtrFromString("CloudControlTray")
	windowName, _ := syscall.UTF16PtrFromString("")
	hwnd, _, _ := createWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0, 0, 0,
	)
	return hwnd
}

// ========== Windows Popup Menu API ==========

var popupMenuProcs = struct {
	createPopupMenu *syscall.LazyProc
	appendMenu      *syscall.LazyProc
	trackPopupMenu  *syscall.LazyProc
	destroyMenu     *syscall.LazyProc
	getCursorPos    *syscall.LazyProc
}{
	createPopupMenu: user32.NewProc("CreatePopupMenu"),
	appendMenu:      user32.NewProc("AppendMenuW"),
	trackPopupMenu:  user32.NewProc("TrackPopupMenu"),
	destroyMenu:     user32.NewProc("DestroyMenu"),
	getCursorPos:    user32.NewProc("GetCursorPos"),
}

func createPopupMenu() uintptr {
	menu, _, _ := popupMenuProcs.createPopupMenu.Call()
	return menu
}

func appendMenu(menu uintptr, id uintptr, text string) {
	if text == "-" {
		popupMenuProcs.appendMenu.Call(menu, 0x0800, id, 0) // MF_SEPARATOR
		return
	}
	textUTF16, _ := syscall.UTF16PtrFromString(text)
	popupMenuProcs.appendMenu.Call(menu, 0x0000, id, uintptr(unsafe.Pointer(textUTF16))) // MF_STRING
}

func trackPopupMenu(menu uintptr, flags uint32, x, y int32, wnd uintptr) {
	popupMenuProcs.trackPopupMenu.Call(menu, uintptr(flags), uintptr(x), uintptr(y), 0, wnd, 0)
}

func getCursorPos(pt *POINT) {
	popupMenuProcs.getCursorPos.Call(uintptr(unsafe.Pointer(pt)))
}

// runTray 运行系统托盘（阻塞，需要在goroutine中调用）
func runTray(tip string, openFn func(), exitFn func()) {
	onOpen = openFn
	onExit = exitFn

	registerWindowClass()
	trayWnd = createTrayWindow()
	createTrayIcon(trayWnd, tip)

	// 消息循环
	var msg MSG
	for {
		ret, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 || ret == ^uintptr(0) {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}

	removeTrayIcon(trayWnd)
}
