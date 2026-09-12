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

// disableQuickEdit 关闭控制台的「快速编辑模式」。
//
// 这是必须的。开启快速编辑时，只要用户点击一下控制台窗口，控制台就进入选择
// 状态，此时**任何进程往该控制台写输出都会阻塞**。而守护进程的日志是同步的
// fmt.Printf，日志一被卡住，正在处理连接的 handleRegister 就停在中途，
// 客户端只能等到 5 秒超时——表现为「守护进程明明在运行，GUI / CLI 却连不上」，
// 且守护进程控制台标题会带上「选择」前缀。
//
// 注意：要让 ENABLE_QUICK_EDIT_MODE 的修改生效，必须同时设置
// ENABLE_EXTENDED_FLAGS，否则该位会被忽略。
//
// 返回 true 表示确实改动过（调用方据此决定要不要记一条日志）。
func disableQuickEdit() bool {
	h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return false
	}
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		// 没有控制台——守护进程被客户端以 --silent 静默拉起时就是这种情况。
		return false
	}
	if mode&windows.ENABLE_QUICK_EDIT_MODE == 0 {
		return false // 本来就是关的
	}
	newMode := (mode &^ windows.ENABLE_QUICK_EDIT_MODE) | windows.ENABLE_EXTENDED_FLAGS
	if err := windows.SetConsoleMode(h, newMode); err != nil {
		return false
	}
	return true
}
