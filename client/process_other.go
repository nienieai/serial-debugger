//go:build !windows

package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func hideWindow(cmd *exec.Cmd) {}

// daemonExeName 返回守护进程可执行文件名。Unix 上没有扩展名，
// 拼成 serial-daemon.exe 会让 findDaemonExe 永远找不到。
func daemonExeName() string {
	return "serial-daemon"
}

// IsDaemonProcessRunning 判断守护进程是否在运行。
//
// 与 Windows 的 tasklist 版本一样，这只是**启发式**判断：只能说明「系统里
// 有个叫 serial-daemon 的进程」，不代表 IPC 可用（拨号才是权威判据）。
//
// 但它不能恒返回 false：waitDaemonExit 靠它等旧进程让出 flock 单例锁
// （见 pipe.acquireLock），恒 false 会让「重启守护进程」在旧进程还没退出时
// 就去拉起新实例，新实例抢不到锁直接以「已在运行」退出——静默失败。
//
// 只有 Linux 有 /proc；其它非 Windows 平台（darwin 等）保持 false。
func IsDaemonProcessRunning() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// /proc 下混着 self、sys、net 这类非 pid 条目，只认纯数字目录。
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil {
			continue // 进程正好退出
		}
		// comm 被内核截断到 15 字符；serial-daemon 是 13 字符，能完整匹配。
		if strings.TrimSpace(string(comm)) == "serial-daemon" {
			return true
		}
	}
	return false
}
