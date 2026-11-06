//go:build windows

package stlogview

import (
	"syscall"
	"unsafe"
)

// DwmSetWindowAttribute 是运行时改标题栏明暗的唯一入口：Wails v3 的
// WebviewWindow 没有 SetTheme，只有创建时给的 WindowsWindow.Theme。
var dwmSetWindowAttribute = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

// setTitlebarDark 把原生标题栏切成深色/浅色。
// 属性 20 是 Win10 20H1+ 的 DWMWA_USE_IMMERSIVE_DARK_MODE，更老的系统认 19；
// 两个都试一遍；都失败就返回 false —— 不改也不报错，画面里只是标题栏没跟上。
func setTitlebarDark(hwnd uintptr, dark bool) bool {
	var v int32
	if dark {
		v = 1
	}
	size := unsafe.Sizeof(v)
	for _, attr := range []uintptr{20, 19} {
		if ret, _, _ := dwmSetWindowAttribute.Call(hwnd, attr, uintptr(unsafe.Pointer(&v)), size); ret == 0 {
			return true
		}
	}
	return false
}
