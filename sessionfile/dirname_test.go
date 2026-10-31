package sessionfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 名字 → 目录名的收敛规则。非法字符是删掉而不是替换：a/b 与 a_b 替换后会撞在一起。
func TestSanitizeDirName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"现场调试", "现场调试"},
		{"现场 调试", "现场 调试"},
		{"  取数口  ", "取数口"},
		{`a/b\c:d*e?f"g<h>i|j`, "abcdefghij"},
		{"a\tb", "ab"},   // 控制字符删掉
		{"a\x7fb", "ab"}, // DEL 也是控制字符
		{"...", ""},      // 全是点 → 没法当目录名
		{"   ", ""},
		{"///", ""},
		{"con", "con_"},         // Windows 保留设备名
		{"CON.txt", "CON.txt_"}, // 带扩展名同样算
		{"COM1", "COM1_"},
		{"LPT9", "LPT9_"},
		{"COM0", "COM0"},       // 只有 1-9 是保留的
		{"COM10", "COM10"},     //
		{"console", "console"}, // 前缀像但不是
	}
	for _, c := range cases {
		if got := SanitizeDirName(c.in); got != c.want {
			t.Errorf("SanitizeDirName(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// 超长按 rune 截断，不是按字节
	long := strings.Repeat("长", 70)
	got := SanitizeDirName(long)
	if n := len([]rune(got)); n != MaxDirNameRunes {
		t.Fatalf("超长名字截断到 %d 个 rune，期望 %d", n, MaxDirNameRunes)
	}
}

// 清空名字要能回到原始目录名：从 created + sessionId 重算，不依赖记着旧值。
func TestSessionDirNameFor(t *testing.T) {
	tm := time.Date(2026, 10, 31, 16, 51, 22, 0, time.Local)
	created := tm.Format(time.RFC3339)
	got := SessionDirNameFor(created, "c773613d4a1f00000000000000000000")
	want := "20261031_165122_c773613d"
	if got != want {
		t.Fatalf("SessionDirNameFor = %q, want %q", got, want)
	}
	// created 解析不了时也不能 panic（退回当前时间）
	if s := SessionDirNameFor("坏时间", "abc"); s == "" {
		t.Fatal("坏 created 也应给出一个目录名")
	}
}

// 重名依次加 (2)、(3)…，绝不返回一个已存在的路径。
func TestUniqueDirName(t *testing.T) {
	parent := t.TempDir()
	first := UniqueDirName(parent, "现场调试")
	if filepath.Base(first) != "现场调试" {
		t.Fatalf("首个名字 = %q", filepath.Base(first))
	}
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	second := UniqueDirName(parent, "现场调试")
	if filepath.Base(second) != "现场调试 (2)" {
		t.Fatalf("第二个名字 = %q，期望 现场调试 (2)", filepath.Base(second))
	}
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	third := UniqueDirName(parent, "现场调试")
	if filepath.Base(third) != "现场调试 (3)" {
		t.Fatalf("第三个名字 = %q，期望 现场调试 (3)", filepath.Base(third))
	}
}

// 录制中改名：关句柄 → 改名 → 重新打开续写，前后数据一条不少。
func TestSessionRenameDir(t *testing.T) {
	parent := t.TempDir()
	s, err := CreateSessionIn(parent, Settings{Label: "x"})
	if err != nil {
		t.Fatalf("建会话失败: %v", err)
	}
	oldDir := s.Dir()

	if err := s.Append(1000, DirRx, OriginNone, "", []byte("aa")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(1001, DirTx, OriginNone, "", []byte("bb")); err != nil {
		t.Fatal(err)
	}

	newDir := filepath.Join(parent, "现场调试")
	if err := s.RenameDir(newDir); err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	if s.Dir() != newDir {
		t.Fatalf("s.Dir() = %q, want %q", s.Dir(), newDir)
	}

	// 改名后必须能接着写：句柄得已经挂到新路径上
	if err := s.Append(1002, DirRx, OriginNone, "", []byte("cc")); err != nil {
		t.Fatalf("改名后追加失败: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("旧目录应该已经不在: %v", err)
	}

	info, err := ReadSession(newDir)
	if err != nil {
		t.Fatalf("读回新目录失败: %v", err)
	}
	if len(info.Frames) != 3 {
		t.Fatalf("帧数 = %d, want 3", len(info.Frames))
	}
	if got := string(info.Frames[2].Payload); got != "cc" {
		t.Fatalf("第 3 帧 = %q, want cc", got)
	}
	if info.StopErr != nil {
		t.Fatalf("结构不该有问题: %v", info.StopErr)
	}
}

// 目标目录已存在时拒绝改名，并且会话必须还能继续写（不能把写者弄丢）。
func TestSessionRenameDirRejectsExisting(t *testing.T) {
	parent := t.TempDir()
	s, err := CreateSessionIn(parent, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	occupied := filepath.Join(parent, "占用")
	if err := os.MkdirAll(occupied, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameDir(occupied); err == nil {
		t.Fatal("目标目录已存在时应当报错")
	}
	if err := s.Append(1000, DirRx, OriginNone, "", []byte("x")); err != nil {
		t.Fatalf("改名失败后会话应仍可写: %v", err)
	}
	// 改到同一个路径是空操作
	if err := s.RenameDir(s.Dir()); err != nil {
		t.Fatalf("改到同一路径不该报错: %v", err)
	}
}
