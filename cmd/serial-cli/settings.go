package main

import (
	"os"
	"path/filepath"

	"github.com/nienieai/serial-debugger/config"
	"github.com/nienieai/serial-debugger/ini"
)

// cliSettings 是 CLI 自己的设置（<exe>/settings/cli.ini）。
//
// 第一版只读：由用户手改文件，改完下次执行生效。优先级固定为
// 命令行 > 环境变量 > 本文件 > 默认值（见 main.go 的 detectLang 与
// color.go 的 stripColorFlag）。
type cliSettings struct {
	Language string
	Color    string
}

func defaultCLISettings() cliSettings {
	return cliSettings{}
}

// loadCLISettings 读取 exe 同级的 settings/cli.ini。
// 文件缺失或字段非法时返回零值，调用方按「未配置」处理。
func loadCLISettings(exeDir string) cliSettings {
	s := defaultCLISettings()
	f, err := ini.Load(ini.Path(exeDir, "cli.ini"))
	if err != nil {
		return s
	}
	if v := f.Get("general", "language", ""); config.HasLang(v) {
		s.Language = v
	}
	switch v := f.Get("general", "color", ""); v {
	case colorModeAuto, colorModeAlways, colorModeNever:
		s.Color = v
	}
	return s
}

// processCLISettings 按当前进程的 exe 目录读设置。
func processCLISettings() cliSettings {
	exe, err := os.Executable()
	if err != nil {
		return defaultCLISettings()
	}
	return loadCLISettings(filepath.Dir(exe))
}
