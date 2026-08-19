//go:build !windows

package client

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}

// IsDaemonProcessRunning is a no-op on non-Windows platforms.
func IsDaemonProcessRunning() bool {
	return false
}
