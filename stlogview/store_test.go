package stlogview

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

// TestOpenWindow 打开文件后按像素取窗口：行数、行高、base、几何都要自洽。
func TestOpenWindow(t *testing.T) {
	s := NewStore()
	s.Open(writeTestFile(t, defaultFrames()))
	waitReady(t, s)

	cfg := s.Configure(120, 42, 45, "hex", 0, false)
	if cfg.RowCount != len(defaultFrames()) {
		t.Fatalf("行数 %d", cfg.RowCount)
	}
	if cfg.PerLine != hexBytesPerLine(120, false) {
		t.Fatalf("perLine %d", cfg.PerLine)
	}

	w := s.Window(0, 200, 120)
	if w.Total != cfg.RowCount {
		t.Fatalf("Total %d", w.Total)
	}
	if len(w.Rows) == 0 {
		t.Fatal("窗口里没有行")
	}
	if w.ContentHeight != cfg.ContentH {
		t.Fatalf("contentHeight %v vs %v", w.ContentHeight, cfg.ContentH)
	}
	// 第一行必须从 y=0 开始，base 等于第一行的 y
	if w.Rows[0].Y != 0 || w.Base != 0 {
		t.Fatalf("首行 y=%v base=%v", w.Rows[0].Y, w.Base)
	}
	// 行高 = 时间戳行 + 若干数据行 + 间距；相邻行必须首尾相接
	for i, r := range w.Rows {
		if i > 0 {
			prev := w.Rows[i-1]
			if math.Abs(prev.Y+prev.H-r.Y) > 1e-9 {
				t.Fatalf("第 %d 行 y=%v，上一行底部 %v", r.Idx, r.Y, prev.Y+prev.H)
			}
		}
		wantH := float64(42+linesFor(uint32(len(framePayload(r.Idx))), cfg.PerLine)*45+rowGap2) / 2
		if math.Abs(r.H-wantH) > 1e-9 {
			t.Fatalf("第 %d 行高 %v，期望 %v", r.Idx, r.H, wantH)
		}
		if r.Hex == "" {
			t.Fatalf("第 %d 行没有 Hex 文本", r.Idx)
		}
	}
	if w.MaxOffset != math.Max(0, w.ContentHeight-200) {
		t.Fatalf("maxOffset %v", w.MaxOffset)
	}

	// 越界 contentY 要被夹到 maxOffset
	big := s.Window(1<<30, 200, 0)
	if big.ContentY != int(w.MaxOffset) {
		t.Fatalf("越界没夹住：%d vs %d", big.ContentY, int(w.MaxOffset))
	}
	if big.RenderTo != big.Total-1 {
		t.Fatalf("滚到底最后一行应是 %d，实际 %d", big.Total-1, big.RenderTo)
	}
}

// framePayload 按默认夹具取第 i 帧的 payload（只给行高核对用）。
func framePayload(i int) []byte { return defaultFrames()[i].payload }

// TestConfigureTextRebuild 切文本模式会重算宽度与行高，并且能按锚点给出新 y。
func TestConfigureTextRebuild(t *testing.T) {
	s := NewStore()
	s.Open(writeTestFile(t, defaultFrames()))
	waitReady(t, s)

	hexCfg := s.Configure(60, 42, 45, "hex", 0, false)
	textCfg := s.Configure(60, 42, 45, "text", 3, false)
	if textCfg.Mode != "text" {
		t.Fatalf("mode %q", textCfg.Mode)
	}
	if textCfg.ContentH <= 0 {
		t.Fatal("文本模式内容高度为 0")
	}
	// 同一个字符宽度下，Hex 一个字节要占 3 格、文本只占 1 格，
	// 所以这份 ASCII 夹具在文本模式里必须比 Hex 矮。
	if textCfg.ContentH >= hexCfg.ContentH {
		t.Fatalf("文本 %v 不比 Hex %v 矮", textCfg.ContentH, hexCfg.ContentH)
	}
	w := s.Window(0, 200, 0)
	anchor := w.Rows[0]
	for _, r := range w.Rows {
		if r.Idx == 3 {
			anchor = r
		}
	}
	if math.Abs(anchor.Y-textCfg.AnchorY) > 1e-9 {
		t.Fatalf("锚点 y=%v，窗口里第 3 行 y=%v", textCfg.AnchorY, anchor.Y)
	}
	// 文本模式下拿到的是分段（segments），不是 Hex 串
	if len(anchor.Segs) == 0 {
		t.Fatal("文本模式没有 segs")
	}
	if len(anchor.Cells) == 0 {
		t.Fatal("没有逻辑行格子数")
	}
}

// TestSetOptPrefix 0x 前缀会改变一行放几个字节，从而改变总高度。
func TestSetOptPrefix(t *testing.T) {
	s := NewStore()
	s.Open(writeTestFile(t, defaultFrames()))
	waitReady(t, s)

	s.Configure(120, 42, 45, "hex", 0, false)
	before := s.Configure(120, 42, 45, "hex", 0, false)
	s.SetOpt("prefix", true)
	after := s.Configure(120, 42, 45, "hex", 0, true)
	if after.PerLine >= before.PerLine {
		t.Fatalf("加 0x 前缀后 perLine 反而没变小：%d → %d", before.PerLine, after.PerLine)
	}
	if after.ContentH < before.ContentH {
		t.Fatalf("perLine 变小，内容高度应更大：%v → %v", before.ContentH, after.ContentH)
	}
}

// TestRuler 来源道：两种来源各占一段，Lead 指向该段里发得最多的那个。
func TestRulerOrigins(t *testing.T) {
	s := NewStore()
	s.Open(writeTestFile(t, defaultFrames()))
	waitReady(t, s)
	s.Configure(120, 42, 45, "hex", 0, false)

	r := s.Ruler(64)
	if r.Total != len(defaultFrames()) {
		t.Fatalf("Total %d", r.Total)
	}
	if len(r.Buckets) != 64 {
		t.Fatalf("桶数 %d", len(r.Buckets))
	}
	if len(r.Origins) != 2 || r.Origins[0] != "cli-aaaa1111" || r.Origins[1] != "gui-bbbb2222" {
		t.Fatalf("来源 = %v", r.Origins)
	}
	var anyLead, anyMask bool
	for _, b := range r.Buckets {
		if b.Lead != 0 {
			anyLead = true
		}
		if b.Orig&0b11 != 0 {
			anyMask = true
		}
	}
	if !anyLead || !anyMask {
		t.Fatalf("来源道没有数据：lead=%v mask=%v", anyLead, anyMask)
	}
	if !r.DiskOK {
		t.Fatal("正常文件应该是完整的")
	}
	if len(r.DiskLine) != 0 {
		t.Fatalf("正常文件不该有不完整标记：%v", r.DiskLine)
	}
}

// TestClose 关掉文档后状态归零，窗口与滚动条都空。
func TestClose(t *testing.T) {
	s := NewStore()
	s.Open(writeTestFile(t, defaultFrames()))
	waitReady(t, s)
	s.Close()
	if st := s.Status(); st.Phase != "idle" || st.Frames != 0 {
		t.Fatalf("Close 之后 %+v", st)
	}
	if w := s.Window(0, 200, 0); len(w.Rows) != 0 || w.Total != 0 {
		t.Fatalf("Close 之后还有行: %+v", w)
	}
	if r := s.Ruler(64); len(r.Buckets) != 0 {
		t.Fatalf("Close 之后还有桶: %d", len(r.Buckets))
	}
}

// TestOpenMissing 打不开的文件要进 error 态，而不是停在 scan。
func TestOpenMissing(t *testing.T) {
	s := NewStore()
	s.Open(filepath.Join(t.TempDir(), "nope.stlog"))
	deadline := time.Now().Add(10 * time.Second)
	for s.Status().Phase != "error" {
		if time.Now().After(deadline) {
			t.Fatalf("没有进 error 态: %+v", s.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.Status().Err == "" {
		t.Fatal("error 态没有错误信息")
	}
}
