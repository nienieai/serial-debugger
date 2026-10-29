package main

import (
	"fmt"
	"os"
	"time"

	"github.com/nienieai/serial-debugger/config"
	"github.com/nienieai/serial-debugger/pipe"
)

var daemonLang = "zh"

func dt(key string, args ...any) string {
	return config.T(daemonLang, key, args...)
}

func main() {
	setConsoleOutputCP()
	// 必须在任何日志输出之前关掉：控制台一旦被点击进入选择模式，
	// 之后所有往控制台写日志的操作都会阻塞，进而卡住连接处理。
	quickEditDisabled := disableQuickEdit()
	// 设置来自 <exe>/settings/daemon.ini（语言 + 着色），只由守护进程读。
	ds := processDaemonSettings()
	daemonLang = ds.LogLanguage
	initColor(os.Args[1:], ds.LogColor)

	// 日志文件要尽早打开：由客户端拉起的守护进程没有控制台（Stdout/Stderr
	// 被 os/exec 接到空设备），不开文件就等于没有任何日志（TODO #30）。
	logErr := initLogFile()
	defer closeLogFile()

	silent := false
	for _, a := range os.Args[1:] {
		if a == "--silent" {
			silent = true
			break
		}
	}

	if !pipe.AcquireLock() {
		// 「已在运行中」是双击场景下唯一能看到的线索，但进程一退出控制台就关，
		// 所以必须同时落盘（此前只写 stderr，控制台关闭即永久丢失）。
		line := logToFile("启动", "%s", dt("daemon.already_running"))
		fmt.Fprint(os.Stderr, line)
		os.Exit(1)
	}
	defer pipe.ReleaseLock()

	if logErr != nil {
		logOp("警告", "日志文件不可用，本次仅输出到控制台: %v", logErr)
	} else if p, err := logFilePath(); err == nil {
		logOp("启动", "日志文件: %s", p)
	}

	if !silent {
		fmt.Println(dt("daemon.starting"))
		fmt.Println(dt("daemon.ctrl_c"))
	}

	logOp("启动", "守护进程已启动")
	if quickEditDisabled {
		logOp("启动", "已关闭控制台快速编辑模式（否则点击控制台会使日志写入阻塞、卡住连接）")
	}

	pm := NewProcessManager()
	srv := NewIpcServer(pm)

	ln, err := pipe.Listen(pipe.Addr)
	if err != nil {
		logOp("错误", "创建命名管道失败: %v", err)
		os.Exit(1)
	}
	defer ln.Close()

	logOp("启动", "命名管道已创建: %s", pipe.Addr)
	logOp("启动", "等待客户端连接...")

	go func() {
		<-srv.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-srv.Done():
				logOp("关闭", "守护进程已停止")
				return
			default:
			}
			logOp("错误", "接受连接失败: %v", err)
			continue
		}
		go srv.Handle(conn)
	}
}

func logOp(category string, format string, args ...any) {
	ts := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	// 控制台可彩色，文件永远纯文本（否则 `serial-cli logs` 读到的是转义串）
	if colorEnabled {
		fmt.Print(formatLogLineColor(ts, category, msg))
	} else {
		fmt.Print(formatLogLine(ts, category, msg))
	}
	writeLogLine(formatLogLine(ts, category, msg))
}
