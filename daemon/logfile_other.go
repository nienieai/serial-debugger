//go:build !windows

package main

import "os"

// openLogFilePlatform 在类 Unix 上直接追加打开。
//
// 这里不需要 Windows 那套共享模式：POSIX 的 open(2) 默认允许多方同时打开，
// 正在写入的文件照样可以被 tail / 编辑器读取。
func openLogFilePlatform(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
}

// rotateLogIfNeeded 在文件超过 limit 时把它改名为 path+".1"。
func rotateLogIfNeeded(path string, limit int64) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= limit {
		return
	}
	_ = os.Rename(path, path+".1")
}
