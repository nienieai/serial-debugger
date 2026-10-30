package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// 拖放预检：工程目录、包文件、v1.4 单文件都能认；其余给原因。
func TestProbeDropPath(t *testing.T) {
	root := t.TempDir()

	// 1) 空目录：认不出来
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := probeDropPath(empty); got.Kind != "unknown" || got.Err == "" {
		t.Fatalf("空目录 = %+v，期望 unknown 且带原因", got)
	}

	// 2) v2 会话目录
	sess := filepath.Join(root, "20261030_104512_ab12cd34")
	s, err := sessionfile.CreateSession(sess, sessionfile.Settings{Label: "COM4", Port: "COM4"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(1, sessionfile.DirRx, sessionfile.OriginNone, "", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := probeDropPath(sess); got.Kind != "session" || got.Err != "" {
		t.Fatalf("会话目录 = %+v，期望 session", got)
	}

	// 3) 目录里的包文件：按 v1.4 单文件认
	pkg := filepath.Join(sess, sessionfile.PkgFileName(1))
	if got := probeDropPath(pkg); got.Kind != "file" || got.Err != "" {
		t.Fatalf("包文件 = %+v，期望 file", got)
	}

	// 4) 独立的 v1.4 单文件
	single := filepath.Join(root, "old.stlog")
	w, err := sessionfile.CreateWriter(single, sessionfile.Settings{Label: "old"})
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Append(1, sessionfile.DirTx, sessionfile.OriginNone, "", []byte("x"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := probeDropPath(single); got.Kind != "file" {
		t.Fatalf("v1.4 单文件 = %+v，期望 file", got)
	}

	// 5) 非 .stlog 文件
	txt := filepath.Join(root, "note.txt")
	if err := os.WriteFile(txt, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := probeDropPath(txt); got.Kind != "unknown" || got.Err == "" {
		t.Fatalf("txt = %+v，期望 unknown", got)
	}

	// 6) 扩展名对但内容不对
	fake := filepath.Join(root, "fake.stlog")
	if err := os.WriteFile(fake, make([]byte, 40000), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := probeDropPath(fake); got.Kind != "unknown" || got.Err == "" {
		t.Fatalf("假 stlog = %+v，期望 unknown", got)
	}

	// 7) 太小的 .stlog
	tiny := filepath.Join(root, "tiny.stlog")
	if err := os.WriteFile(tiny, []byte("SL"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := probeDropPath(tiny); got.Kind != "unknown" || got.Err == "" {
		t.Fatalf("过小的 stlog = %+v，期望 unknown", got)
	}

	// 8) 不存在的路径
	if got := probeDropPath(filepath.Join(root, "nope.stlog")); got.Kind != "unknown" {
		t.Fatalf("不存在 = %+v，期望 unknown", got)
	}
}

// 批量接口按输入顺序逐条返回。
func TestProbeDroppedPathsOrder(t *testing.T) {
	root := t.TempDir()
	sess := filepath.Join(root, "s")
	s, err := sessionfile.CreateSession(sess, sessionfile.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	a := &App{}
	got := a.ProbeDroppedPaths([]string{sess, filepath.Join(root, "missing")})
	if len(got) != 2 {
		t.Fatalf("返回 %d 条，期望 2", len(got))
	}
	if got[0].Kind != "session" || got[1].Kind != "unknown" {
		t.Fatalf("结果 = %+v", got)
	}
}
