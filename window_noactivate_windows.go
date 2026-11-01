//go:build windows

package main

// 拖动 tear-off 时用的「不激活窗口」开关。
//
// 背景：标签拖出去后如果新窗口抢到焦点，源窗口的 pointer capture 会被系统结束，
// pointermove 立刻断供 —— 窗口只来得及跟随一次就停住（实测就是这样）。
// v3 没有 ShowInactive / no-activate 选项，所以直接改窗口的扩展样式：
// WS_EX_NOACTIVATE 让窗口可以显示、但不激活、不抢焦点。

import (
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	modUser32NoActivate   = syscall.NewLazyDLL("user32.dll")
	procGetWindowLongPtrW = modUser32NoActivate.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = modUser32NoActivate.NewProc("SetWindowLongPtrW")
	procGetCursorPos      = modUser32NoActivate.NewProc("GetCursorPos")
	procGetAsyncKeyState  = modUser32NoActivate.NewProc("GetAsyncKeyState")
)

type point struct{ X, Y int32 }

// cursorPos 返回光标屏幕坐标（物理像素）。
func cursorPos() (int, int) {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return int(p.X), int(p.Y)
}

// leftButtonDown 判断鼠标左键当前是否按下（VK_LBUTTON 最高位）。
func leftButtonDown() bool {
	const vkLeftButton = 0x01
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vkLeftButton))
	return r&0x8000 != 0
}

// 用变量而不是常量：GWL_EXSTYLE 是 -20，常量转换到 uint32 会溢出。
var (
	gwlExStyle     = int32(-20)
	wsExNoActivate = uint32(0x08000000)
)

// setNoActivate 设置/清除 WS_EX_NOACTIVATE。窗口不存在时静默返回。
func setNoActivate(w *application.WebviewWindow, on bool) {
	if w == nil {
		return
	}
	hwnd := uintptr(w.NativeWindow())
	if hwnd == 0 {
		return
	}
	idx := uintptr(uint32(gwlExStyle))
	ex, _, _ := procGetWindowLongPtrW.Call(hwnd, idx)
	if on {
		ex |= uintptr(wsExNoActivate)
	} else {
		ex &^= uintptr(wsExNoActivate)
	}
	procSetWindowLongPtrW.Call(hwnd, idx, ex)
}
