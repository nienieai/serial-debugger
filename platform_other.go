//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

func hideWindow(cmd *exec.Cmd) {}

// forceKillDaemon 强杀守护进程，与 Windows 的 taskkill /F /IM 对称。
//
// 只在 Linux 上生效：pkill -x 按进程名精确匹配（内核把 comm 截断到 15 字符，
// serial-daemon 只有 13 字符，匹配得到）。其它非 Windows 平台保持 no-op，
// 免得用不可靠的进程名误杀。
func forceKillDaemon() {
	if runtime.GOOS != "linux" {
		return
	}
	_ = exec.Command("pkill", "-9", "-x", "serial-daemon").Run()
}

func openDevToolsWindow() {}

func openFolder(dir string) error { return nil }
