package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCLIINI(t *testing.T, exeDir, body string) {
	t.Helper()
	dir := filepath.Join(exeDir, "settings")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("建设置目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cli.ini"), []byte(body), 0644); err != nil {
		t.Fatalf("写 cli.ini 失败: %v", err)
	}
}

func TestLoadCLISettings(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantLang  string
		wantColor string
	}{
		{"正常", "[general]\nlanguage = en\ncolor = always\n", "en", "always"},
		{"部分字段", "[general]\nlanguage = ja\n", "ja", ""},
		{"非法值回落", "[general]\nlanguage = klingon\ncolor = rainbow\n", "", ""},
		{"大小写不敏感", "[General]\nLanguage = ko\nColor = never\n", "ko", "never"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeCLIINI(t, dir, c.body)
		got := loadCLISettings(dir)
		if got.Language != c.wantLang || got.Color != c.wantColor {
			t.Fatalf("%s: got %+v, want language=%q color=%q", c.name, got, c.wantLang, c.wantColor)
		}
	}
}

func TestLoadCLISettingsMissingFile(t *testing.T) {
	got := loadCLISettings(t.TempDir())
	if got.Language != "" || got.Color != "" {
		t.Fatalf("缺失文件时应返回零值, got %+v", got)
	}
}

// 语言优先级：--lang > LANG > settings/cli.ini > 默认 zh。
func TestDetectLangPriority(t *testing.T) {
	oldArgs, oldCfg := os.Args, cliCfg
	t.Cleanup(func() { os.Args = oldArgs; cliCfg = oldCfg })

	t.Setenv("LANG", "")
	cliCfg = cliSettings{Language: "ja"}
	os.Args = []string{"serial-cli", "status"}
	if got := detectLang(); got != "ja" {
		t.Fatalf("文件语言未生效: got %q", got)
	}

	t.Setenv("LANG", "ko")
	if got := detectLang(); got != "ko" {
		t.Fatalf("环境变量应压过设置文件: got %q", got)
	}

	os.Args = []string{"serial-cli", "--lang", "en", "status"}
	if got := detectLang(); got != "en" {
		t.Fatalf("命令行应压过环境变量: got %q", got)
	}

	cliCfg = defaultCLISettings()
	t.Setenv("LANG", "")
	os.Args = []string{"serial-cli", "status"}
	if got := detectLang(); got != "zh" {
		t.Fatalf("都没配时应回默认 zh: got %q", got)
	}
}

// 颜色默认值来自 cli.ini，但命令行可以覆盖。
func TestStripColorFlagDefaultFromSettings(t *testing.T) {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		t.Skip("当前环境设置了 NO_COLOR，颜色断言不适用")
	}
	oldMode := colorMode
	t.Cleanup(func() { colorMode = oldMode })

	args := stripColorFlag([]string{"status"}, colorModeNever)
	if colorMode != colorModeNever {
		t.Fatalf("设置里的 color=never 未生效: %q", colorMode)
	}
	if len(args) != 1 || args[0] != "status" {
		t.Fatalf("参数表应保持 status, got %v", args)
	}

	args = stripColorFlag([]string{"--color=always", "status"}, colorModeNever)
	if colorMode != colorModeAlways {
		t.Fatalf("命令行应覆盖设置: %q", colorMode)
	}
	if len(args) != 1 || args[0] != "status" {
		t.Fatalf("--color 应从参数表里摘掉, got %v", args)
	}
}
