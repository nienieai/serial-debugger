//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

var (
	moduser32   = syscall.NewLazyDLL("user32.dll")
	keybdEvent  = moduser32.NewProc("keybd_event")
)

const (
	vkCtrl          = 0x11
	vkShift         = 0x10
	vkF12           = 0x7B
	keyeventfKeyup  = 0x0002
)

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

func openFolder(dir string) error {
	return exec.Command("explorer", dir).Start()
}

func forceKillDaemon() {
	cmd := exec.Command("taskkill", "/F", "/IM", "serial-daemon.exe")
	hideWindow(cmd)
	cmd.Run()
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
