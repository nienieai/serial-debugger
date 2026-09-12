//go:build !windows

package main

// setConsoleOutputCP 在类 Unix 上是空操作：终端本身即为 UTF-8。
func setConsoleOutputCP() {}

// disableQuickEdit 在类 Unix 上是空操作：终端没有 Windows 控制台的选择模式，
// 也就不存在「点击终端导致进程写输出阻塞」的问题。
func disableQuickEdit() bool { return false }
