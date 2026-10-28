//go:build !windows

package main

// enableVirtualTerminal 在类 Unix 上无需额外开启，终端天然支持 ANSI。
func enableVirtualTerminal() bool { return true }
