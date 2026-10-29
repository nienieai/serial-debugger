package ini

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSectionsKeysAndComments(t *testing.T) {
	src := `
; 顶部注释
# 另一种注释
[General]
Language = zh
color=never

[connect]
process = 3 ; 这里的分号属于值，不该被当注释截掉

[log]
language = "en"
`
	f, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got := f.Get("general", "language", ""); got != "zh" {
		t.Fatalf("general.language=%q, want zh（section/key 应大小写不敏感）", got)
	}
	if got := f.Get("GENERAL", "COLOR", ""); got != "never" {
		t.Fatalf("general.color=%q, want never", got)
	}
	if got := f.Get("connect", "process", ""); got != "3 ; 这里的分号属于值，不该被当注释截掉" {
		t.Fatalf("值被截断: %q", got)
	}
	if got := f.Get("log", "language", ""); got != "en" {
		t.Fatalf("引号未被剥掉: %q", got)
	}
}

func TestParseIgnoresMalformedLines(t *testing.T) {
	f, err := Parse(strings.NewReader("[broken\nno-equals\n[]\n[a]\n=empty-key\n"))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got := f.Get("", "", ""); got != "" {
		t.Fatalf("空 section 不应有值, got %q", got)
	}
	if got := f.Get("a", "", ""); got != "empty-key" {
		t.Fatalf("空 key 的赋值应当保留, got %q", got)
	}
}

// Windows 记事本/PowerShell 写出的文件可能带 UTF-8 BOM，首行 section 不能被吃掉。
func TestParseStripsBOM(t *testing.T) {
	f, err := Parse(strings.NewReader("\ufeff[general]\ncolor = always\n"))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got := f.Get("general", "color", ""); got != "always" {
		t.Fatalf("BOM 未处理: general.color=%q", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.ini"))
	if !os.IsNotExist(err) {
		t.Fatalf("缺失文件应返回 IsNotExist 错误, got %v", err)
	}
}

func TestDirAndPath(t *testing.T) {
	exe := filepath.Join("C:", "apps", "serial")
	if got, want := Dir(exe), filepath.Join(exe, "settings"); got != want {
		t.Fatalf("Dir=%q, want %q", got, want)
	}
	if got, want := Path(exe, "gui.ini"), filepath.Join(exe, "settings", "gui.ini"); got != want {
		t.Fatalf("Path=%q, want %q", got, want)
	}
}
