package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 守护进程文件日志。
//
// 背景（TODO #30）：由客户端拉起的守护进程是以 `serial-daemon.exe --silent`
// 启动且 Stdout/Stderr 未重定向（os/exec 约定接到空设备），而 logOp 此前只做
// fmt.Printf。结果是回连失败、串口异常这类守护进程侧的错误**没有任何地方能
// 看到**，直接导致「先开守护进程再开 GUI 有时连不上」这类问题无从定位。
//
// 因此日志改为双路输出：
//   - 有控制台时照旧打印，保持手工运行时的可观察性；
//   - 始终追加到 <exe>/logs/daemon.log，让无人值守启动也留得下现场。
//
// 写入失败不得影响主流程：出错就把文件路关掉、退回纯控制台输出。
const (
	logDirName  = "logs"
	logFileName = "daemon.log"
	// 单文件上限，超过则在启动时轮转为 daemon.log.1。
	maxLogBytes = 2 << 20
)

var (
	logMu   sync.Mutex
	logFile *os.File
)

// logFilePath 返回（并创建）日志文件路径。目录创建失败时返回错误。
func logFilePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(exe), logDirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, logFileName), nil
}

// initLogFile 打开日志文件。必须在任何 logOp 之前调用。
//
// 返回错误而不是 panic：日志不可用是可接受的降级，不能因此让守护进程起不来。
func initLogFile() error {
	path, err := logFilePath()
	if err != nil {
		return err
	}
	// 启动时轮转，避免长期运行把单文件撑大。失败不阻塞启动。
	rotateLogIfNeeded(path, maxLogBytes)
	f, err := openLogFilePlatform(path)
	if err != nil {
		return err
	}
	logMu.Lock()
	logFile = f
	logMu.Unlock()
	return nil
}

func closeLogFile() {
	logMu.Lock()
	f := logFile
	logFile = nil
	logMu.Unlock()
	if f != nil {
		f.Close()
	}
}

// writeLogLine 把一行日志追加到文件。文件不可用时静默忽略。
func writeLogLine(line string) {
	logMu.Lock()
	f := logFile
	if f != nil {
		if _, err := f.WriteString(line); err != nil {
			// 盘满或句柄失效：关掉文件，后续只走控制台，避免每次日志都失败。
			f.Close()
			logFile = nil
		}
	}
	logMu.Unlock()
}

// formatLogLine 统一日志行格式（控制台与文件共用，保证两处可对照）。
func formatLogLine(ts, category, msg string) string {
	return fmt.Sprintf("%s [%-6s] %s\n", ts, category, msg)
}

// logToFile 供 logOp 以及启动早期的告警使用。
func logToFile(category, format string, args ...any) string {
	line := formatLogLine(time.Now().Format("15:04:05.000"), category, fmt.Sprintf(format, args...))
	writeLogLine(line)
	return line
}
