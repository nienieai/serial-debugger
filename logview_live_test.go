package main

import (
	"testing"

	"github.com/nienieai/serial-debugger/sessionfile"
	"github.com/nienieai/serial-debugger/stlogview"
)

// TestLiveFrameFrom 钉住「哪些帧算日志、参数怎么取」：Stream 上混着对话框、
// 拖标签之类的控制帧，只有四个数据方向与 system 能进文档。
func TestLiveFrameFrom(t *testing.T) {
	cases := []struct {
		name string
		in   any
		ok   bool
		dir  uint8
	}{
		{"rx", map[string]any{"hex": "4142", "tsMs": int64(1000)}, true, sessionfile.DirRx},
		{"tx", map[string]any{"hex": "4142", "tsMs": int64(1000), "origin": "gui-1"}, true, sessionfile.DirTx},
		{"1", map[string]any{"hex": "4142"}, true, sessionfile.DirP1},
		{"2", map[string]any{"hex": "4142"}, true, sessionfile.DirP2},
		{"system", map[string]any{"hex": "737973"}, true, sessionfile.DirSystem},
		{"dialog-result", map[string]any{"hex": "4142"}, false, 0},
		{"tab-drop-begin", map[string]any{"hex": "4142"}, false, 0},
		{"rx", map[string]any{"hex": ""}, false, 0},
		{"rx", "不是 map", false, 0},
	}
	for _, c := range cases {
		f, ok := liveFrameFrom(c.name, c.in)
		if ok != c.ok {
			t.Fatalf("%s → ok=%v，期望 %v", c.name, ok, c.ok)
		}
		if ok && f.Dir != c.dir {
			t.Fatalf("%s → dir=%d，期望 %d", c.name, f.Dir, c.dir)
		}
	}
	if f, _ := liveFrameFrom("tx", map[string]any{"hex": "41", "tsMs": int64(77), "origin": "gui-9"}); f.Ts != 77 || f.Origin != "gui-9" {
		t.Fatalf("ts/origin 没取对：%+v", f)
	}
}

// TestLogViewLive 实时文档经由服务壳：同一会话复用文档、数据帧能追加、
// 窗口/清空/关闭都通。
func TestLogViewLive(t *testing.T) {
	lv := NewApp().logview
	doc, err := lv.OpenLive("sid-1")
	if err != nil || doc.ID == "" {
		t.Fatalf("OpenLive: %+v %v", doc, err)
	}
	// 重复开同一会话：复用同一个文档（文档里已经有历史，不能重建）
	again, err := lv.OpenLive("sid-1")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != doc.ID {
		t.Fatalf("重复开应该复用：%q vs %q", again.ID, doc.ID)
	}

	lv.appendSession("sid-1", stlogview.FrameIn{Ts: 1000, Dir: sessionfile.DirRx, Hex: "4142"})
	lv.appendSession("sid-1", stlogview.FrameIn{Ts: 1010, Dir: sessionfile.DirTx, Hex: "4344", Origin: "gui-1"})
	info, err := lv.LiveInfo(doc.ID)
	if err != nil || info.Total != 2 {
		t.Fatalf("LiveInfo: %+v %v", info, err)
	}

	cfg, err := lv.Configure(doc.ID, 120, 42, 45, "hex", 0, stlogview.DefaultStyle(0))
	if err != nil || cfg.RowCount != 2 {
		t.Fatalf("Configure: %+v %v", cfg, err)
	}
	w, err := lv.Window(doc.ID, 0, 400, 0)
	if err != nil || len(w.Rows) != 2 || w.Total != 2 {
		t.Fatalf("Window: %d 行 / %d / %v", len(w.Rows), w.Total, err)
	}
	if w.Rows[0].Dir != "rx" || w.Rows[1].Dir != "tx" || w.Rows[1].Origin != "gui-1" {
		t.Fatalf("行方向/来源：%+v", w.Rows)
	}

	// 往回补历史
	older := []stlogview.FrameIn{{Ts: 900, Dir: sessionfile.DirRx, Hex: "4546"}}
	if _, err := lv.PrependFrames(doc.ID, older); err != nil {
		t.Fatal(err)
	}
	if info, _ := lv.LiveInfo(doc.ID); info.Total != 3 {
		t.Fatalf("补历史之后 %+v", info)
	}

	// 清空
	if _, err := lv.ResetLive(doc.ID); err != nil {
		t.Fatal(err)
	}
	if info, _ := lv.LiveInfo(doc.ID); info.Total != 0 {
		t.Fatalf("清空之后 %+v", info)
	}

	// 关掉之后：再喂帧不会 panic，重新开会拿新文档
	if err := lv.Close(doc.ID); err != nil {
		t.Fatal(err)
	}
	lv.appendSession("sid-1", stlogview.FrameIn{Ts: 1, Dir: sessionfile.DirRx, Hex: "41"})
	fresh, err := lv.OpenLive("sid-1")
	if err != nil || fresh.ID == doc.ID {
		t.Fatalf("关掉之后应该开新文档：%+v %v", fresh, err)
	}
	if info, _ := lv.LiveInfo(fresh.ID); info.Total != 0 {
		t.Fatalf("新文档不该有帧：%+v", info)
	}
}
