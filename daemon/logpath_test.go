package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// daemon.info 上报的日志路径必须就是它真正写的那一份：GUI 的「查看守护进程日志」
// 靠它找文件，猜错就只能显示「日志文件还没生成」。
func TestLogFilePathPlainMatchesLogFilePath(t *testing.T) {
	plain := logFilePathPlain()
	if plain == "" {
		t.Fatal("logFilePathPlain 返回空路径")
	}
	if !filepath.IsAbs(plain) {
		t.Fatalf("上报路径不是绝对路径: %q", plain)
	}
	if !strings.HasSuffix(plain, filepath.Join(logDirName, logFileName)) {
		t.Fatalf("上报路径 = %q，期望以 %q 结尾", plain, filepath.Join(logDirName, logFileName))
	}

	// 和「真正写入用的那份」逐字一致：两边各推一遍 exe 目录，但推导必须同源
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("取程序路径失败: %v", err)
	}
	want := filepath.Join(filepath.Dir(exe), logDirName, logFileName)
	if plain != want {
		t.Fatalf("上报路径 = %q，期望 %q", plain, want)
	}
}

// logFilePath 负责建目录、返回同一个路径；上报用的 logFilePathPlain 不碰磁盘。
func TestLogFilePathCreatesDirAndStaysInSync(t *testing.T) {
	plain := logFilePathPlain()
	dir := filepath.Dir(plain)
	// 测试进程的 exe 目录里通常没有 logs/；有也不能删（可能是真守护进程的），
	// 所以这条只断言两者指向同一个目录、且 logFilePath 不报错。
	got, err := logFilePath()
	if err != nil {
		t.Fatalf("logFilePath 失败: %v", err)
	}
	if got != plain {
		t.Fatalf("logFilePath = %q，logFilePathPlain = %q，两者应一致", got, plain)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("logFilePath 之后目录应存在: %v", err)
	}
}
