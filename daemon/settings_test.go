package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDaemonINI(t *testing.T, exeDir, body string) {
	t.Helper()
	dir := filepath.Join(exeDir, "settings")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("建设置目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.ini"), []byte(body), 0644); err != nil {
		t.Fatalf("写 daemon.ini 失败: %v", err)
	}
}

func TestLoadDaemonSettings(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantLang     string
		wantColor    string
		wantAutoSave bool
	}{
		{"正常", "[log]\nlanguage = en\ncolor = never\n", "en", "never", false},
		{"缺字段回落", "[log]\nlanguage = ja\n", "ja", colorModeAuto, false},
		{"非法值回落", "[log]\nlanguage = klingon\ncolor = rainbow\n[history]\nauto_save = 乱写\n", "zh", colorModeAuto, false},
		{"大小写与引号", "[Log]\nLanguage = \"ko\"\nColor = always\n", "ko", "always", false},
		{"历史落盘", "[history]\nauto_save = true\n", "zh", colorModeAuto, true},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeDaemonINI(t, dir, c.body)
		got := loadDaemonSettings(dir)
		if got.LogLanguage != c.wantLang || got.LogColor != c.wantColor || got.HistoryAutoSave != c.wantAutoSave {
			t.Fatalf("%s: got %+v, want language=%q color=%q auto_save=%v", c.name, got, c.wantLang, c.wantColor, c.wantAutoSave)
		}
	}
}

// 文件不存在时必须回落默认值，而不是报错或留空——守护进程启动早期没有设置
// 目录是常态（旧安装、首次运行）。历史落盘默认必须是「不保存」。
func TestLoadDaemonSettingsMissingFile(t *testing.T) {
	got := loadDaemonSettings(t.TempDir())
	if got.LogLanguage != "zh" || got.LogColor != colorModeAuto || got.HistoryAutoSave {
		t.Fatalf("缺失文件时 got %+v, want zh/auto/auto_save=false", got)
	}
}
