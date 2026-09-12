package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nienieai/serial-debugger/config"
	"github.com/nienieai/serial-debugger/pipe"
)

var daemonLang = "zh"

func loadDaemonLang() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	f, err := os.Open(filepath.Join(filepath.Dir(exe), "settings.ini"))
	if err != nil {
		return
	}
	defer f.Close()

	inDisplay := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			inDisplay = strings.EqualFold(line, "[display]")
			continue
		}
		if inDisplay {
			eq := strings.IndexByte(line, '=')
			if eq < 0 {
				continue
			}
			key := strings.TrimSpace(line[:eq])
			val := strings.TrimSpace(line[eq+1:])
			if key == "language" && config.HasLang(val) {
				daemonLang = val
				return
			}
		}
	}
}

func dt(key string, args ...any) string {
	return config.T(daemonLang, key, args...)
}

func main() {
	setConsoleOutputCP()
	// 必须在任何日志输出之前关掉：控制台一旦被点击进入选择模式，
	// 之后所有往控制台写日志的操作都会阻塞，进而卡住连接处理。
	quickEditDisabled := disableQuickEdit()
	loadDaemonLang()

	silent := false
	for _, a := range os.Args[1:] {
		if a == "--silent" {
			silent = true
			break
		}
	}

	if !pipe.AcquireLock() {
		fmt.Fprintln(os.Stderr, dt("daemon.already_running"))
		os.Exit(1)
	}
	defer pipe.ReleaseLock()

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
	fmt.Printf("%s [%-6s] %s\n", ts, category, msg)
}
