package main

import (
	"path/filepath"
	"testing"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// TestOpenStlogFile 钉住拖入 .stlog 时 Go 侧返回的结构。
//
// 前端 app.js 的拖放回调直接消费这些字段名（frames / preview / recovered …），
// 改名字会让对话框静默显示 undefined，所以这里把契约固定下来。
func TestOpenStlogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.stlog")
	w, err := sessionfile.CreateWriter(path, sessionfile.Settings{Label: "unit"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1000, sessionfile.DirRx, 0, "", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1001, sessionfile.DirTx, 0, "", []byte("world")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	a := &App{}
	m, err := a.OpenStlogFile(path)
	if err != nil {
		t.Fatalf("OpenStlogFile: %v", err)
	}

	if got := m["frames"]; got != 2 {
		t.Fatalf("frames = %v, want 2", got)
	}
	if got := m["records"]; got != 2 {
		t.Fatalf("records = %v, want 2", got)
	}
	if got := m["pages"]; got != 1 {
		t.Fatalf("pages = %v, want 1", got)
	}

	preview, ok := m["preview"].([]map[string]any)
	if !ok {
		t.Fatalf("preview has type %T", m["preview"])
	}
	if len(preview) != 2 {
		t.Fatalf("preview len = %d, want 2", len(preview))
	}
	if head, _ := preview[0]["head"].(string); head != "68656C6C6F" {
		t.Fatalf("preview[0].head = %q, want 68656C6C6F", head)
	}
	if dir, _ := preview[1]["dir"].(uint8); dir != sessionfile.DirTx {
		t.Fatalf("preview[1].dir = %v, want %v", preview[1]["dir"], sessionfile.DirTx)
	}

	settings, ok := m["settings"].(*sessionfile.Settings)
	if !ok || settings.Label != "unit" {
		t.Fatalf("settings = %#v", m["settings"])
	}
}

// TestOpenStlogFileMissing 确认坏路径返回 error 而不是 panic——
// 拖动一个被删掉/改名过的文件是很容易发生的。
func TestOpenStlogFileMissing(t *testing.T) {
	a := &App{}
	if _, err := a.OpenStlogFile(filepath.Join(t.TempDir(), "nope.stlog")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
