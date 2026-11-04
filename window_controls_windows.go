//go:build windows

package main

import "syscall"

var procShowWindow = syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow")

// SW_MINIMIZE
const swMinimise = 6

// minimiseNative 直接对原生窗口发 SW_MINIMIZE。
//
// 为什么不走 Wails 的 WebviewWindow.Minimise：那个在无边框窗口上实测不生效
// （它内部也是 ShowWindow(SW_MINIMIZE)，但经过 InvokeSync 投递之后没有落地，
// 和 Maximise 同一个毛病 —— 那次至少还把 WS_MAXIMIZE 状态位改上了，窗口尺寸
// 却一动不动）。直接对 HWND 发就没这个问题。
func minimiseNative(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	r, _, _ := procShowWindow.Call(hwnd, uintptr(swMinimise))
	return r != 0
}
