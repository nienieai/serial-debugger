//go:build windows

package main

import "golang.org/x/sys/windows"

// setConsoleOutputCP 把控制台输出代码页切到 UTF-8。
//
// 只影响 Windows：默认代码页（简体中文下为 936）会把守护进程日志里的中文和
// 符号输出成乱码。类 Unix 终端本身就是 UTF-8，无需处理。
func setConsoleOutputCP() {
	windows.SetConsoleOutputCP(65001)
}
