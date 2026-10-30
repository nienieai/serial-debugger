//go:build windows

package main

import "golang.org/x/sys/windows"

// enableVirtualTerminal 打开控制台的 VT 处理，让 ANSI 颜色生效。
// 旧控制台默认不解析转义序列；开失败就降级无色。
func enableVirtualTerminal() bool {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return false
	}
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
