//go:build !windows

package main

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}

func forceKillDaemon() {}

func openDevToolsWindow() {}

func openFolder(dir string) error { return nil }
