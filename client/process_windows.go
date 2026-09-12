//go:build windows

package client

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// IsDaemonProcessRunning 检查进程列表里是否有 serial-daemon.exe。
//
// 注意这只是一个**启发式**判断：tasklist 被安全软件拦截、系统繁忙导致
// CreateProcess 失败、或输出格式变化时都会返回 false，而此时守护进程可能
// 正常运行。调用方不应把它当作「能否连接」的判据（IPC 拨号才是权威的），
// 只适合用于展示或快速预筛。
//
// 带 3 秒超时：本函数曾被当作硬闸门使用，一旦 tasklist 卡住会永久阻塞调用方。
func IsDaemonProcessRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tasklist", "/FI", "IMAGENAME eq serial-daemon.exe", "/NH")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "serial-daemon.exe")
}
