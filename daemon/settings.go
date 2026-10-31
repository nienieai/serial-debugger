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
	// Display 是 [display] 段：新会话的显示设置默认值（编码 / Hex / 控制字符 /
	// 转义字节）。这些设置本身按标签页走，这里只提供新建会话时的初始值。
	//
	// 键用 JSON 风格（hexCase 而不是 hex_case）：这份 map 直接发给 GUI，
	// ini 键名只在 loadDaemonSettings 里用一次。
	Display map[string]string
}

// displayKeys 是 [display] 段认识的键与内置默认值。
//
// 只影响「怎么看」，不影响收发的字节，所以放在守护进程这边当默认值；
// 单条会话的实际值由 GUI 按标签页保存，工程会话再写进工程目录的 settings.json。
var displayKeys = []struct{ ini, json, def string }{
	{"encoding", "encoding", "utf-8"},
	{"hex_case", "hexCase", "upper"},
	{"hex_prefix", "hexPrefix", "true"},
	{"hex_sep", "hexSep", "space"},
	{"cr_visible", "crVisible", "true"},
	{"hex_escape_mode", "hexEscapeMode", "show"},
	{"hex_escape_format", "hexEscapeFormat", "slash"},
	{"copy_hex_escapes", "copyHexEscapes", "true"},
	// 制表符长度与行尾序列也是「怎么看 / 怎么发」的一部分，同样按标签页走
	{"tab_size", "tabSize", "4"},
	{"eol_sequence", "eolSequence", "lf"},
}

func defaultDaemonSettings() daemonSettings {
	s := daemonSettings{LogLanguage: "zh", LogColor: colorModeAuto, HistoryAutoSave: false}
	s.Display = defaultDisplaySettings()
	return s
}

func defaultDisplaySettings() map[string]string {
	m := make(map[string]string, len(displayKeys))
	for _, d := range displayKeys {
		m[d.json] = d.def
	}
	return m
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
	// [display] 段：缺键保留内置默认值，非法值也当缺键（宁可回默认，别让
	// 一个拼错的 encoding 把界面搞成一片乱码）。
	for _, d := range displayKeys {
		if v := f.Get("display", d.ini, ""); v != "" {
			s.Display[d.json] = v
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
