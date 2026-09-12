//go:build !windows

package main

// setConsoleOutputCP 在类 Unix 上是空操作：终端本身即为 UTF-8。
func setConsoleOutputCP() {}
