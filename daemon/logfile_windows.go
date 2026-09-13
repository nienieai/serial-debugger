//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// openLogFilePlatform 以「允许他人读写 + 追加写」的方式打开日志文件。
//
// 不能直接用 os.OpenFile：它在 Windows 上以独占共享模式打开
// （dwShareMode = 0），守护进程一旦在运行，任何人都读不到日志文件——
// File.ReadAllBytes、编辑器、`serial-cli logs` 全部报「正被另一进程使用」。
// 而「守护进程在跑的时候去看它的日志」正是这个功能唯一的用途。
//
// FILE_SHARE_DELETE 一并给出，这样轮转时的 rename 也不会被句柄挡住。
//
// dwDesiredAccess 用 FILE_APPEND_DATA 而不是 GENERIC_WRITE，对应 POSIX 的
// O_APPEND：CreateFile 的 OPEN_ALWAYS 会**停在偏移 0**（它不隐含追加语义），
// 第二个守护进程实例写那句「已在运行中」时就会覆盖掉日志开头——实测文件里
// 留下一截被截断的旧行。FILE_APPEND_DATA 让每次写都原子地落到文件末尾。
func openLogFilePlatform(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(
		p,
		windows.FILE_APPEND_DATA,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

// rotateLogIfNeeded 在文件超过 limit 时把它改名为 path+".1"。
//
// 失败不致命：日志文件被别处按住（编辑器/杀软）时继续追加即可，
// 不值得为此拒绝启动。
func rotateLogIfNeeded(path string, limit int64) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= limit {
		return
	}
	_ = os.Rename(path, path+".1")
}
