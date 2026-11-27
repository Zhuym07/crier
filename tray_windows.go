package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	trayID      = 1
	trayMessage = 0x8000 + 10
	wmClose     = 0x0010
	wmDestroy   = 0x0002
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205
	nimAdd      = 0
	nimDelete   = 2
	nifMessage  = 1
	nifIcon     = 2
	nifTip      = 4
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	registerClassEx       = user32.NewProc("RegisterClassExW")
	unregisterClass       = user32.NewProc("UnregisterClassW")
	createWindowEx        = user32.NewProc("CreateWindowExW")
	defWindowProc         = user32.NewProc("DefWindowProcW")
	destroyWindow         = user32.NewProc("DestroyWindow")
	loadImage             = user32.NewProc("LoadImageW")
	getSystemMetrics      = user32.NewProc("GetSystemMetrics")
	createPopupMenu       = user32.NewProc("CreatePopupMenu")
	appendMenu            = user32.NewProc("AppendMenuW")
	destroyMenu           = user32.NewProc("DestroyMenu")
	trackPopupMenuEx      = user32.NewProc("TrackPopupMenuEx")
	getCursorPos          = user32.NewProc("GetCursorPos")
	setForeground         = user32.NewProc("SetForegroundWindow")
	postMessage           = user32.NewProc("PostMessageW")
	postQuitMessage       = user32.NewProc("PostQuitMessage")
	getMessage            = user32.NewProc("GetMessageW")
	translateMessage      = user32.NewProc("TranslateMessage")
	dispatchMessage       = user32.NewProc("DispatchMessageW")
	registerWindowMessage = user32.NewProc("RegisterWindowMessageW")
	getModuleHandle       = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	shellNotifyIcon       = windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")
)

type wndClassEx struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}

type notifyIconData struct {
	Size                       uint32
	Wnd                        uintptr
	ID, Flags, CallbackMessage uint32
	Icon                       uintptr
	Tip                        [128]uint16
	State, StateMask           uint32
	Info                       [256]uint16
	Version                    uint32
	InfoTitle                  [64]uint16
	InfoFlags                  uint32
	GuidItem                   windows.GUID
	BalloonIcon                uintptr
}

type winMsg struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	PtX, PtY       int32
	Private        uint32
}

type tray struct {
	app  *App
	mu   sync.Mutex
	hwnd uintptr
	done chan struct{}
}

func newTray(app *App) *tray { return &tray{app: app} }

func (t *tray) start() error {
	type startup struct {
		hwnd uintptr
		err  error
	}
	result := make(chan startup, 1)
	done := make(chan struct{})
	go func() {
		// The window, its message pump, and its destruction must share an OS thread.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)

		instance, _, _ := getModuleHandle.Call(0)
		icon, err := loadTrayIcon(instance)
		if err != nil {
			result <- startup{err: err}
			return
		}
		className := windows.StringToUTF16Ptr("CrierTrayWindow")
		taskbarCreated, _, _ := registerWindowMessage.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))))
		data := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), ID: trayID, Flags: nifMessage | nifIcon | nifTip, CallbackMessage: trayMessage, Icon: icon}
		copy(data.Tip[:], windows.StringToUTF16("传令 Crier"))
		proc := syscall.NewCallback(func(hwnd, message, wParam, lParam uintptr) uintptr {
			switch message {
			case trayMessage:
				switch lParam {
				case wmLButtonUp:
					go t.app.showWindow()
				case wmRButtonUp:
					t.menu(hwnd)
				}
				return 0
			case wmDestroy:
				shellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
				postQuitMessage.Call(0)
				return 0
			default:
				if taskbarCreated != 0 && message == taskbarCreated && data.Wnd != 0 {
					shellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&data)))
					return 0
				}
				ret, _, _ := defWindowProc.Call(hwnd, message, wParam, lParam)
				return ret
			}
		})
		class := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: proc, Instance: instance, ClassName: className}
		if atom, _, err := registerClassEx.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
			result <- startup{err: fmt.Errorf("注册托盘窗口失败: %w", err)}
			return
		}
		defer unregisterClass.Call(uintptr(unsafe.Pointer(className)), instance)
		hwnd, _, err := createWindowEx.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, instance, 0)
		if hwnd == 0 {
			result <- startup{err: fmt.Errorf("创建托盘窗口失败: %w", err)}
			return
		}
		data.Wnd = hwnd
		if ok, _, err := shellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&data))); ok == 0 {
			destroyWindow.Call(hwnd)
			// Destroy posts WM_QUIT on this thread. Let this locked thread terminate.
			result <- startup{err: fmt.Errorf("注册托盘图标失败: %w", err)}
			var message winMsg
			getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			return
		}
		result <- startup{hwnd: hwnd}
		var message winMsg
		for {
			ret, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if int32(ret) <= 0 {
				break
			}
			translateMessage.Call(uintptr(unsafe.Pointer(&message)))
			dispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
		}
	}()
	state := <-result
	t.mu.Lock()
	t.hwnd, t.done = state.hwnd, done
	t.mu.Unlock()
	return state.err
}

func loadTrayIcon(instance uintptr) (uintptr, error) {
	width, _, _ := getSystemMetrics.Call(49)  // SM_CXSMICON
	height, _, _ := getSystemMetrics.Call(50) // SM_CYSMICON
	// Group resource 3 is the branded executable icon also used by Wails.
	icon, _, err := loadImage.Call(instance, 3, 1, width, height, 0x8000) // IMAGE_ICON | LR_SHARED
	if icon == 0 {
		return 0, fmt.Errorf("加载托盘图标失败: %w", err)
	}
	return icon, nil
}

func (t *tray) ready() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hwnd != 0
}

func (t *tray) remove() {
	t.mu.Lock()
	hwnd, done := t.hwnd, t.done
	t.hwnd, t.done = 0, nil
	t.mu.Unlock()
	if hwnd != 0 {
		if ok, _, _ := postMessage.Call(hwnd, wmClose, 0, 0); ok == 0 {
			return
		}
	}
	if done != nil {
		<-done
	}
}

func (t *tray) menu(hwnd uintptr) {
	menu, _, _ := createPopupMenu.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)
	for i, label := range []string{"打开传令 Crier", "设置", "退出"} {
		appendMenu.Call(menu, 0, uintptr(i+1), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(label))))
	}
	var point struct{ X, Y int32 }
	getCursorPos.Call(uintptr(unsafe.Pointer(&point)))
	setForeground.Call(hwnd)
	const tpmRightButtonAndReturn = 0x0002 | 0x0100
	choice, _, _ := trackPopupMenuEx.Call(menu, tpmRightButtonAndReturn, uintptr(point.X), uintptr(point.Y), hwnd, 0)
	postMessage.Call(hwnd, 0, 0, 0) // Allow the next notification area menu to dismiss normally.
	switch choice {
	case 1:
		go t.app.showWindow()
	case 2:
		go t.app.OpenSettings()
	case 3:
		go t.app.quit()
	}
}
