package client

import (
	"os/exec"
	"strings"
	"syscall"
)

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// IsDaemonProcessRunning checks whether serial-daemon.exe exists
// in the process list (fast tasklist check, ~100ms).
func IsDaemonProcessRunning() bool {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq serial-daemon.exe", "/NH")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "serial-daemon.exe")
}
