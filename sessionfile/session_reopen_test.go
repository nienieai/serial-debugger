package sessionfile

import (
	"os"
	"path/filepath"
	"testing"
)

// 已存在的工程被重新打开续写：封过的包不碰，另开一包接着写，数据一条不丢。
func TestOpenSessionDirContinues(t *testing.T) {
	parent := t.TempDir()
	s, err := CreateSessionIn(parent, Settings{Label: "x", Port: "COM9", Baud: 115200})
	if err != nil {
		t.Fatalf("建会话失败: %v", err)
	}
	dir := s.Dir()
	if err := s.Append(1000, DirRx, OriginNone, "", []byte("aa")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	// 关闭时是封印的，所以重新打开应该另开一个包而不是往旧包里塞
	s2, err := OpenSessionDir(dir)
	if err != nil {
		t.Fatalf("OpenSessionDir: %v", err)
	}
	if s2.PkgSeq() != 2 {
		t.Fatalf("续写的包序号 = %d, want 2", s2.PkgSeq())
	}
	if err := s2.Append(2000, DirTx, OriginNone, "", []byte("bb")); err != nil {
		t.Fatalf("续写失败: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("关闭续写失败: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, PkgFileName(2))); err != nil {
		t.Fatalf("第二个包不存在: %v", err)
	}

	info, err := ReadSession(dir)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if len(info.Frames) != 2 {
		t.Fatalf("帧数 = %d, want 2", len(info.Frames))
	}
	if got := string(info.Frames[0].Payload); got != "aa" {
		t.Fatalf("第 1 帧 = %q", got)
	}
	if got := string(info.Frames[1].Payload); got != "bb" {
		t.Fatalf("第 2 帧 = %q", got)
	}
	// 打包时登记的两个包都在 manifest 里
	if len(info.Packages) != 2 {
		t.Fatalf("包数 = %d, want 2", len(info.Packages))
	}
}

// save 字段：进 settings.json 且能读回（推送后据此决定要不要接着录）。
func TestSettingsDocSaveFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := NewSettingsDoc(Settings{Label: "x"})
	doc.Save = true
	if err := SaveSettingsDoc(dir, doc); err != nil {
		t.Fatal(err)
	}
	back, err := LoadSettingsDoc(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Save {
		t.Fatal("save 标记没读回来")
	}
}
