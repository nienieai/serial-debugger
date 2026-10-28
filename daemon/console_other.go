//go:build !windows

package main

import "os"

// setConsoleOutputCP 在类 Unix 上是空操作：终端本身即为 UTF-8。
func setConsoleOutputCP() {}

// stdoutIsTTY 判断标准输出是不是终端（管道/重定向时为 false，日志不带转义）。
func stdoutIsTTY() bool {
	st, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// enableVirtualTerminal 在类 Unix 上无需额外开启，终端天然支持 ANSI。
func enableVirtualTerminal() bool { return true }

// disableQuickEdit 在类 Unix 上是空操作：终端没有 Windows 控制台的选择模式，
// 也就不存在「点击终端导致进程写输出阻塞」的问题。
func disableQuickEdit() bool { return false }
