package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenLogFilePlatformAllowsConcurrentRead 确认日志文件在守护进程持有时
// 仍可被其它进程读取。
//
// 为什么重要（TODO #30）：os.OpenFile 在 Windows 上以独占共享模式打开，
// 守护进程一旦运行，日志就对所有人不可读——`serial-cli logs`、编辑器、
// Get-Content 全部报「正被另一进程使用」。而「守护进程在跑的时候去看它的
// 日志」正是日志功能唯一的用途。此测试同时是 Windows 修复的守卫：
// 换回 os.OpenFile 会立刻失败。
func TestOpenLogFilePlatformAllowsConcurrentRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")

	w, err := openLogFilePlatform(path)
	if err != nil {
		t.Fatalf("openLogFilePlatform: %v", err)
	}
	defer w.Close()

	if _, err := w.WriteString("hello log\n"); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 模拟另一个进程（如 serial-cli logs / 外部编辑器）读取。
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("写入方仍持有文件时读取失败（共享模式没生效）: %v", err)
	}
	if string(got) != "hello log\n" {
		t.Fatalf("读到的内容不对: %q", string(got))
	}
}

// TestOpenLogFilePlatformAppends 确认打开已有日志是**追加**而不是从头覆盖。
//
// 为什么重要：Windows 的 CreateFile(OPEN_ALWAYS) 会把文件指针放在偏移 0，
// 它不像 POSIX 的 O_APPEND 那样隐含追加语义。第二实例写「已在运行中」
// 时会覆盖掉日志开头，留下一截被截断的旧行（实测过）。必须用
// FILE_APPEND_DATA 才能拿到 O_APPEND 的行为。
func TestOpenLogFilePlatformAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")

	if err := os.WriteFile(path, []byte("first instance\n"), 0644); err != nil {
		t.Fatal(err)
	}

	w, err := openLogFilePlatform(path)
	if err != nil {
		t.Fatalf("openLogFilePlatform: %v", err)
	}
	if _, err := w.WriteString("second instance\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.Close()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "first instance\nsecond instance\n"
	if string(got) != want {
		t.Fatalf("追加写结果不对\n got: %q\nwant: %q", string(got), want)
	}
}

// TestRotateLogIfNeeded 确认超过阈值时轮转、未超过时不动。
func TestRotateLogIfNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")

	if err := os.WriteFile(path, []byte("small"), 0644); err != nil {
		t.Fatal(err)
	}
	rotateLogIfNeeded(path, 1024)
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Fatal("未超过阈值不应轮转")
	}

	if err := os.WriteFile(path, make([]byte, 2048), 0644); err != nil {
		t.Fatal(err)
	}
	rotateLogIfNeeded(path, 1024)
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("超过阈值应轮转为 .1: %v", err)
	}
}
