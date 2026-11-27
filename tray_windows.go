package main

import (
	_ "embed"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:embed brand.ico
var trayIconICO []byte

const (
	trayID      = 1
	trayMessage = 0x8000 + 10
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205
	wmDestroy   = 0x0002
	mfString    = 0x0000
	mfSeparator = 0x0800
	tpmRight    = 0x0008
	tpmBottom   = 0x0020
	tpmNoNotify = 0x0080
	tpmReturn   = 0x0100
	niFIcon     = 0x01
	niFTip      = 0x04
	nimAdd      = 0x0000
	nimDelete   = 0x0002
	idOpen      = 1
	idSettings  = 2
	idQuit      = 3
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	registerClassEx  = user32.NewProc("RegisterClassExW")
	createWindowEx   = user32.NewProc("CreateWindowExW")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	destroyWindow    = user32.NewProc("DestroyWindow")
	loadCursor       = user32.NewProc("LoadCursorW")
	getModuleHandle  = kernel32.NewProc("GetModuleHandleW")
	createPopupMenu  = user32.NewProc("CreatePopupMenu")
	appendMenu       = user32.NewProc("AppendMenuW")
	destroyMenu      = user32.NewProc("DestroyMenu")
	trackPopupMenuEx = user32.NewProc("TrackPopupMenuEx")
	getCursorPos     = user32.NewProc("GetCursorPos")
	setForeground    = user32.NewProc("SetForegroundWindow")
	postMessage      = user32.NewProc("PostMessageW")
	getMessage       = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	destroyIcon      = user32.NewProc("DestroyIcon")

	createIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")

	shellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
)

type wndClassEx struct {
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

// notifyIconData 是 64 位下 NOTIFYICONDATAW 的结构。
type notifyIconData struct {
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
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        windows.GUID
	BalloonIcon     uintptr
}

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
	Private uint32
}

// tray 是常驻托盘图标。关闭窗口只隐藏界面，收信和打印继续在后台运行。
type tray struct {
	mu      sync.Mutex
	app     *App
	hwnd    uintptr
	icon    uintptr
	visible bool
}

func newTray(app *App) *tray { return &tray{app: app} }

// start 创建隐藏消息窗口并注册托盘图标。失败只影响托盘，不影响主界面。
func (t *tray) start() error {
	instance, _, _ := getModuleHandle.Call(0)
	className, err := syscall.UTF16PtrFromString("CrierTrayWindow")
	if err != nil {
		return err
	}
	arrow, _, _ := loadCursor.Call(0, 32512) // IDC_ARROW
	class := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(t.wndProc),
		Instance:  instance,
		ClassName: className,
		Cursor:    arrow,
	}
	if atom, _, callErr := registerClassEx.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		return fmt.Errorf("注册托盘窗口失败: %w", callErr)
	}
	window, _, callErr := createWindowEx.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if window == 0 {
		return fmt.Errorf("创建托盘窗口失败: %w", callErr)
	}
	icon := iconFromICO(trayIconICO, 16)
	t.mu.Lock()
	t.hwnd, t.icon = window, icon
	t.mu.Unlock()
	go t.loop()
	return t.send(nimAdd)
}

// send 提交一次 Shell_NotifyIconW。图标句柄为 0 时仍显示文字提示，
// 这样即使图标解析失败，托盘入口和右键菜单也能用。
func (t *tray) send(action uintptr) error {
	t.mu.Lock()
	hwnd, icon := t.hwnd, t.icon
	t.mu.Unlock()
	data := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Wnd: hwnd, ID: trayID, CallbackMessage: trayMessage, Icon: icon}
	data.Flags = niFTip
	if icon != 0 {
		data.Flags |= niFIcon
	}
	copy(data.Tip[:], windows.StringToUTF16("传令 Crier"))
	if result, _, callErr := shellNotifyIcon.Call(action, uintptr(unsafe.Pointer(&data))); result == 0 {
		return fmt.Errorf("托盘图标注册失败: %w", callErr)
	}
	t.mu.Lock()
	t.visible = true
	t.mu.Unlock()
	return nil
}

// iconFromICO 从嵌入的 ICO 里挑出最接近 size 的一张转成 HICON。
// ICO 目录项只记录宽度，DIB 图像（位图 + 掩码）的高度是宽度的两倍，
// PNG 图像的高度等于宽度；按 0x00030000 的 ICO 版本交给系统解析。
func iconFromICO(data []byte, size int) uintptr {
	if len(data) < 6 {
		return 0
	}
	count := int(le16(data[4:6]))
	bestOffset, bestLength, bestScore := -1, 0, 1<<30
	for i := 0; i < count; i++ {
		entry := 6 + i*16
		if entry+16 > len(data) {
			return 0
		}
		width := int(data[entry])
		if width == 0 {
			width = 256
		}
		length := int(le32(data[entry+8 : entry+12]))
		offset := int(le32(data[entry+12 : entry+16]))
		if length < 8 || offset < 0 || offset+length > len(data) {
			continue
		}
		score := abs(width-size) * 10
		if isPNG(data[offset : offset+length]) {
			score++
		}
		if score < bestScore {
			bestOffset, bestLength, bestScore = offset, length, score
		}
	}
	if bestOffset < 0 {
		return 0
	}
	icon, _, _ := createIconFromResourceEx.Call(uintptr(unsafe.Pointer(&data[bestOffset])), uintptr(bestLength), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	return icon
}

func isPNG(blob []byte) bool {
	return len(blob) >= 4 && blob[0] == 0x89 && blob[1] == 'P' && blob[2] == 'N' && blob[3] == 'G'
}

func le16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ready 表示托盘图标是否已经注册成功，用于判断关闭窗口时能否收进托盘。
func (t *tray) ready() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.visible
}

func (t *tray) remove() {
	t.mu.Lock()
	hwnd, icon, visible := t.hwnd, t.icon, t.visible
	t.hwnd, t.icon, t.visible = 0, 0, false
	t.mu.Unlock()
	if visible && hwnd != 0 {
		data := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Wnd: hwnd, ID: trayID}
		shellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	}
	if hwnd != 0 {
		destroyWindow.Call(hwnd)
	}
	if icon != 0 {
		destroyIcon.Call(icon)
	}
}

func (t *tray) loop() {
	var m winMsg
	for {
		result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(result) <= 0 {
			return
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (t *tray) wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch message {
	case trayMessage:
		switch lParam {
		case wmLButtonUp:
			t.app.showWindow()
		case wmRButtonUp:
			t.menu(hwnd)
		}
		return 0
	case wmDestroy:
		return 0
	}
	result, _, _ := defWindowProc.Call(hwnd, message, wParam, lParam)
	return result
}

func (t *tray) menu(hwnd uintptr) {
	m, _, _ := createPopupMenu.Call()
	if m == 0 {
		return
	}
	defer destroyMenu.Call(m)
	text := func(s string) uintptr {
		p, _ := syscall.UTF16PtrFromString(s)
		return uintptr(unsafe.Pointer(p))
	}
	appendMenu.Call(m, mfString, idOpen, text("打开传令 Crier"))
	appendMenu.Call(m, mfString, idSettings, text("设置"))
	appendMenu.Call(m, mfSeparator, 0, 0)
	appendMenu.Call(m, mfString, idQuit, text("退出"))

	var pt struct{ X, Y int32 }
	getCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	setForeground.Call(hwnd)
	choice, _, _ := trackPopupMenuEx.Call(m, tpmRight|tpmBottom|tpmNoNotify|tpmReturn, uintptr(pt.X), uintptr(pt.Y), hwnd, 0)
	switch choice {
	case idOpen:
		t.app.showWindow()
	case idSettings:
		t.app.OpenSettings()
	case idQuit:
		t.app.quit()
	}
}
