//go:build !windows

package main

// minimiseNative 只在 Windows 上有原生实现；别的平台返回 false，调用方
// 会退回 Wails 的 Minimise。
func minimiseNative(hwnd uintptr) bool { return false }
