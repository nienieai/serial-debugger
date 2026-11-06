//go:build !windows

package main

import "os/exec"

func openDevToolsWindow() {}

func openFolder(dir string) error { return nil }

// openURL 用桌面默认浏览器打开网址（Linux 走 xdg-open）。
func openURL(url string) error { return exec.Command("xdg-open", url).Start() }
