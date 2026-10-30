package main

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/nienieai/serial-debugger/config"
	"github.com/nienieai/serial-debugger/ini"
)

// daemonSettings 是守护进程自己的设置（<exe>/settings/daemon.ini）。
//
// 为什么由守护进程独占：日志语言与着色只影响它自己的控制台与日志文件；
// GUI 与 CLI 各写自己的文件（gui.ini / cli.ini），谁也不截断谁。
// MCP 不落盘，只用 LANG 环境变量。
type daemonSettings struct {
	LogLanguage string
	LogColor    string
	// HistoryAutoSave 是新进程默认是否把数据写进 history/*.log（默认关）。
	HistoryAutoSave bool
}

func defaultDaemonSettings() daemonSettings {
	return daemonSettings{LogLanguage: "zh", LogColor: colorModeAuto, HistoryAutoSave: false}
}

// loadDaemonSettings 读取 exe 同级的 settings/daemon.ini。
//
// 文件缺失、字段非法时一律回落默认值：日志语言必须是 config.HasLang 认可的
// 值（否则 dt() 会返回空串，日志里只剩时间戳），颜色只认 auto/always/never。
func loadDaemonSettings(exeDir string) daemonSettings {
	s := defaultDaemonSettings()
	f, err := ini.Load(ini.Path(exeDir, "daemon.ini"))
	if err != nil {
		return s
	}
	if v := f.Get("log", "language", ""); config.HasLang(v) {
		s.LogLanguage = v
	}
	switch v := f.Get("log", "color", ""); v {
	case colorModeAuto, colorModeAlways, colorModeNever:
		s.LogColor = v
	}
	if v := f.Get("history", "auto_save", ""); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			s.HistoryAutoSave = b
		}
	}
	return s
}

// processDaemonSettings 按当前进程的 exe 目录读设置。
func processDaemonSettings() daemonSettings {
	exe, err := os.Executable()
	if err != nil {
		return defaultDaemonSettings()
	}
	return loadDaemonSettings(filepath.Dir(exe))
}
