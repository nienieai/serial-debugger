package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nienieai/serial-debugger/sessionfile"
	"github.com/nienieai/serial-debugger/stlogview"
)

// makeTestStlog 造一个几帧的 .stlog 给服务壳用。
func makeTestStlog(t *testing.T, frames int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc.stlog")
	w, err := sessionfile.CreateWriter(p, sessionfile.Settings{Label: "unit", Port: "COM4"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < frames; i++ {
		if err := w.Append(uint64(1000+i*10), sessionfile.DirRx, 0, "", []byte("frame")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestLogViewDocRegistry 钉住服务壳的分文档拆法：一个进程里多个文档各拿一个 id。
func TestLogViewDocRegistry(t *testing.T) {
	lv := NewApp().logview
	p := makeTestStlog(t, 5)

	first, err := lv.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" {
		t.Fatal("没有返回文档 id")
	}
	second, err := lv.Open(makeTestStlog(t, 9))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatalf("两份文档拿到了同一个 id：%q", first.ID)
	}
	if n := lv.Count(); n != 2 {
		t.Fatalf("文档数 %d", n)
	}

	// 两份文档互不影响：帧数各自对得上
	waitDocReady(t, lv, first.ID, 5)
	waitDocReady(t, lv, second.ID, 9)
	if m, _ := lv.Meta(first.ID); m.Frames != 5 {
		t.Fatalf("第一份帧数 %d", m.Frames)
	}
	if m, _ := lv.Meta(second.ID); m.Frames != 9 {
		t.Fatalf("第二份帧数 %d", m.Frames)
	}

	// 几何：Configure 之后能取到窗口
	cfg, err := lv.Configure(first.ID, 120, 42, 45, "hex", nil, 0, stlogview.DefaultStyle(0))
	if err != nil || cfg.RowCount != 5 {
		t.Fatalf("Configure: %+v %v", cfg, err)
	}
	w, err := lv.Window(first.ID, 0, 200, 100)
	if err != nil || len(w.Rows) == 0 || w.Total != 5 {
		t.Fatalf("Window: %d 行 / total %d / %v", len(w.Rows), w.Total, err)
	}
	if r, err := lv.Ruler(first.ID, 64); err != nil || r.Total != 5 {
		t.Fatalf("Ruler: %+v %v", r, err)
	}

	// 关掉一份，另一份还在；关掉的 id 再问就报错
	if err := lv.Close(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := lv.Window(first.ID, 0, 200, 0); err == nil {
		t.Fatal("关掉之后还能取窗口")
	}
	if _, err := lv.Status(second.ID); err != nil {
		t.Fatalf("关掉第一份影响了第二份：%v", err)
	}
	lv.CloseAll()
	if n := lv.Count(); n != 0 {
		t.Fatalf("CloseAll 之后还有 %d 份", n)
	}
}

// TestLogViewOpenEmptyPath 空路径要报错，而不是建一个空文档。
func TestLogViewOpenEmptyPath(t *testing.T) {
	lv := NewApp().logview
	if _, err := lv.Open(""); err == nil {
		t.Fatal("空路径应该报错")
	}
	if n := lv.Count(); n != 0 {
		t.Fatalf("空路径留下了 %d 份文档", n)
	}
}

func waitDocReady(t *testing.T, lv *LogView, id string, frames int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st, err := lv.Status(id)
		if err != nil {
			t.Fatal(err)
		}
		if st.Phase == "ready" {
			if st.Frames != frames {
				t.Fatalf("文档 %s 帧数 %d，期望 %d", id, st.Frames, frames)
			}
			return
		}
		if st.Phase == "error" {
			t.Fatalf("扫描失败：%s", st.Err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("文档 %s 扫描超时", id)
}
