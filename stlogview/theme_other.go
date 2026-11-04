//go:build !windows

package stlogview

// setTitlebarDark 在非 Windows 上是空实现：主工程的窗口本身就只有 Windows 这条
// 原生标题栏路径，留这个桩是为了 go vet ./... / go build 在别的平台上也能过。
func setTitlebarDark(hwnd uintptr, dark bool) bool { return false }
