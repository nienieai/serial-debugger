package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 设置文件必须落在 <exe>/settings/gui.ini —— 目录拆分是这个功能的全部意义，
// 路径拼错会让 GUI 又写回 exe 同级的旧位置。
func TestSettingsPathUsesSettingsDir(t *testing.T) {
	p, err := settingsPath()
	if err != nil {
		t.Fatalf("settingsPath 报错: %v", err)
	}
	if got := filepath.Base(p); got != "gui.ini" {
		t.Fatalf("文件名=%q, want gui.ini", got)
	}
	if got := filepath.Base(filepath.Dir(p)); got != "settings" {
		t.Fatalf("父目录=%q, want settings", got)
	}

	dir, err := settingsDir()
	if err != nil {
		t.Fatalf("settingsDir 报错: %v", err)
	}
	if filepath.Dir(p) != dir {
		t.Fatalf("settingsPath 与 settingsDir 不一致: %q vs %q", filepath.Dir(p), dir)
	}
}

// INI 往返：写出去的内容必须能原样读回（AppSettings 字段多，靠反射，
// 最容易在新增字段时漏掉 ini tag）。
func TestSettingsINIRoundTrip(t *testing.T) {
	cfg := defaultSettings()
	cfg.DisplayMode = "hex"
	cfg.SendRatio = 0.42
	cfg.HexPrefix = false
	cfg.DisplayFontSize = 18
	cfg.Language = "en"
	cfg.Serial.Baud = 9600
	cfg.Serial.Parity = "even"
	cfg.AutoSend.IntervalMs = 250
	cfg.Append.Suffix = "\\r\\n"

	f, err := os.CreateTemp(t.TempDir(), "gui-*.ini")
	if err != nil {
		t.Fatalf("建临时文件失败: %v", err)
	}
	defer f.Close()
	if err := marshalINI(f, cfg); err != nil {
		t.Fatalf("marshalINI 失败: %v", err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("读回临时文件失败: %v", err)
	}

	got := defaultSettings()
	if err := unmarshalINI(data, got); err != nil {
		t.Fatalf("unmarshalINI 失败: %v", err)
	}
	if *got != *cfg {
		t.Fatalf("往返后不一致:\n got=%+v\nwant=%+v", *got, *cfg)
	}
}

// SaveAppSettings 必须在 settings/ 目录还不存在时就把它建出来，并且写进去的
// 内容能被 LoadAppSettings 原样读回——这是「拆分目录后 GUI 仍能保存」的实证。
func TestSaveAppSettingsCreatesDirAndReloads(t *testing.T) {
	p, err := settingsPath()
	if err != nil {
		t.Fatalf("settingsPath 报错: %v", err)
	}
	// 互不影响：先清掉本次测试二进制目录下可能残留的上一轮文件。
	_ = os.Remove(p)
	_ = os.Remove(filepath.Dir(p))
	t.Cleanup(func() { _ = os.Remove(p) })

	app := &App{}
	cfg := defaultSettings()
	cfg.Theme = "dark"
	cfg.Language = "en"
	if err := app.SaveAppSettings(cfg); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("保存后文件不存在: %v", err)
	}

	got := app.LoadAppSettings()
	if got.Theme != "dark" || got.Language != "en" {
		t.Fatalf("读回的值不对: theme=%q language=%q", got.Theme, got.Language)
	}
}
