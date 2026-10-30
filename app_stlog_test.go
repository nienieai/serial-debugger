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

// TestReadStlogOrigins 顶栏「来源」汇总：按首次出现顺序去重；没有来源时是空切片
// （前端据此显示「无来源」，而不是留一片空白让人以为功能没做）。
func TestReadStlogOrigins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "origin.stlog")
	w, err := sessionfile.CreateWriter(path, sessionfile.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	appends := []struct {
		dir   uint8
		label string
	}{
		{sessionfile.DirRx, ""},
		{sessionfile.DirTx, "cli-aaaa1111"},
		{sessionfile.DirTx, "gui-bbbb2222"},
		{sessionfile.DirTx, "cli-aaaa1111"}, // 重复：只算一次
	}
	for i, a := range appends {
		origin := sessionfile.OriginNone
		if a.label != "" {
			origin = sessionfile.OriginInline
		}
		if err := w.Append(uint64(3000+i), a.dir, origin, a.label, []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	a := &App{}
	m, err := a.ReadStlogFrames(path, 0, 100)
	if err != nil {
		t.Fatalf("ReadStlogFrames: %v", err)
	}
	origins, ok := m["origins"].([]string)
	if !ok {
		t.Fatalf("origins 类型 = %T", m["origins"])
	}
	if len(origins) != 2 || origins[0] != "cli-aaaa1111" || origins[1] != "gui-bbbb2222" {
		t.Fatalf("origins = %v，期望 [cli-aaaa1111 gui-bbbb2222]", origins)
	}
}

// TestReadStlogFrames 钉住文件标签页用的分页接口：offset 由服务端回显，
// total 是总帧数，hex 大写。前端靠这三个字段算「加载更多」。
func TestReadStlogFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paging.stlog")
	w, err := sessionfile.CreateWriter(path, sessionfile.Settings{Label: "unit"})
	if err != nil {
		t.Fatal(err)
	}
	const n = 7
	for i := 0; i < n; i++ {
		payload := []byte{byte(i), 0xAB}
		if err := w.Append(uint64(2000+i), sessionfile.DirRx, 0, "", payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	a := &App{}

	// 第一页：offset=0, limit=3
	m, err := a.ReadStlogFrames(path, 0, 3)
	if err != nil {
		t.Fatalf("ReadStlogFrames: %v", err)
	}
	if got := m["total"]; got != n {
		t.Fatalf("total = %v, want %d", got, n)
	}
	if got := m["offset"]; got != 0 {
		t.Fatalf("offset = %v, want 0", got)
	}
	frames, ok := m["frames"].([]map[string]any)
	if !ok || len(frames) != 3 {
		t.Fatalf("frames = %#v (type %T)", m["frames"], m["frames"])
	}
	if hex, _ := frames[0]["hex"].(string); hex != "00AB" {
		t.Fatalf("frames[0].hex = %q, want 00AB", hex)
	}
	if idx, _ := frames[0]["index"].(int); idx != 0 {
		t.Fatalf("frames[0].index = %v, want 0", frames[0]["index"])
	}

	// 第二页：offset=3，取到 6
	m2, err := a.ReadStlogFrames(path, 3, 3)
	if err != nil {
		t.Fatalf("ReadStlogFrames(3,3): %v", err)
	}
	if got := m2["offset"]; got != 3 {
		t.Fatalf("offset = %v, want 3", got)
	}
	if frames, _ := m2["frames"].([]map[string]any); len(frames) != 3 {
		t.Fatalf("第二页帧数 = %d, want 3", len(frames))
	}

	// 末页：offset=6 只剩 1 帧，且 limit 被服务端夹到 <=1000
	m3, err := a.ReadStlogFrames(path, 6, 5000)
	if err != nil {
		t.Fatalf("ReadStlogFrames(6,5000): %v", err)
	}
	if frames, _ := m3["frames"].([]map[string]any); len(frames) != 1 {
		t.Fatalf("末页帧数 = %d, want 1", len(frames))
	}

	// 越界 offset：不报错，返回空切片
	m4, err := a.ReadStlogFrames(path, 99, 10)
	if err != nil {
		t.Fatalf("ReadStlogFrames(99,10): %v", err)
	}
	if got := m4["offset"]; got != n {
		t.Fatalf("越界 offset = %v, want %d", got, n)
	}
	if frames, _ := m4["frames"].([]map[string]any); len(frames) != 0 {
		t.Fatalf("越界帧数 = %d, want 0", len(frames))
	}
}
