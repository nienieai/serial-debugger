//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

var (
	moduser32  = syscall.NewLazyDLL("user32.dll")
	keybdEvent = moduser32.NewProc("keybd_event")
)

const (
	vkCtrl         = 0x11
	vkShift        = 0x10
	vkF12          = 0x7B
	keyeventfKeyup = 0x0002
)

func openFolder(dir string) error {
	return exec.Command("explorer", dir).Start()
}

// openURL 用系统默认浏览器打开一个网址（关于页的仓库 / 问题反馈）。
// 走 url.dll 而不是 cmd /c start：后者会把 URL 交给 cmd 重新解析，& 之类的
// 字符有被当成命令分隔符的风险。调用方传的永远是 Go 侧写死的常量。
func openURL(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// openDevToolsWindow simulates Ctrl+Shift+F12 to toggle DevTools in WebView2.
// Wails v2 uses Ctrl+Shift+F12 (not plain F12) as the DevTools accelerator.
// Requires the GUI to be built with `wails build -devtools`.
func openDevToolsWindow() {
	keybdEvent.Call(uintptr(vkCtrl), 0, 0, 0)
	keybdEvent.Call(uintptr(vkShift), 0, 0, 0)
	keybdEvent.Call(uintptr(vkF12), 0, 0, 0)
	keybdEvent.Call(uintptr(vkF12), 0, uintptr(keyeventfKeyup), 0)
	keybdEvent.Call(uintptr(vkShift), 0, uintptr(keyeventfKeyup), 0)
	keybdEvent.Call(uintptr(vkCtrl), 0, uintptr(keyeventfKeyup), 0)
}
