package stlogview

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// newLiveDoc 造一个已经配好布局的实时文档。
func newLiveDoc(t *testing.T, maxBytes int64) *Store {
	t.Helper()
	s := NewStore()
	s.MarkLive(maxBytes)
	s.Configure(120, 42, 45, "hex", 0, DefaultStyle(0))
	return s
}

// liveFrame 造一帧：payload 是字节，hex 由它转。
func liveFrame(ts int64, dir uint8, payload []byte) FrameIn {
	return FrameIn{Ts: ts, Dir: dir, Hex: hex.EncodeToString(payload), Origin: ""}
}

// TestLiveAppend 追加之后窗口/总帧数/行高都要跟着涨，帧号从 0 开始。
func TestLiveAppend(t *testing.T) {
	s := newLiveDoc(t, 1<<20)
	for i := 0; i < 10; i++ {
		s.AppendFrame(liveFrame(int64(1000+i*10), 1, []byte(fmt.Sprintf("frame-%02d", i))))
	}
	info := s.LiveInfo()
	if info.Total != 10 || info.Frames != 10 || info.Evicted != 0 {
		t.Fatalf("概况 %+v", info)
	}
	w := s.Window(0, 2000, 0)
	if w.Total != 10 || len(w.Rows) != 10 {
		t.Fatalf("窗口 total=%d rows=%d", w.Total, len(w.Rows))
	}
	if w.Rows[0].Idx != 0 || w.Rows[9].Idx != 9 {
		t.Fatalf("帧号 %d..%d", w.Rows[0].Idx, w.Rows[9].Idx)
	}
	if w.ContentHeight <= 0 || w.MaxOffset < 0 {
		t.Fatalf("高度 %v / maxOffset %v", w.ContentHeight, w.MaxOffset)
	}
	if w.Rows[0].Dir != "rx" {
		t.Fatalf("方向 %q", w.Rows[0].Dir)
	}
	if !strings.HasPrefix(w.Rows[0].Hex, "66 72") { // "fr"
		t.Fatalf("Hex = %q", w.Rows[0].Hex)
	}
}

// TestLiveEvict 超过字节上限就从最旧的一端裁，帧号继续往前涨。
func TestLiveEvict(t *testing.T) {
	s := newLiveDoc(t, 4096)
	payload := make([]byte, 100)
	for i := range payload {
		payload[i] = byte(i)
	}
	for i := 0; i < 200; i++ {
		s.AppendFrame(liveFrame(int64(1000+i), 1, payload))
	}
	info := s.LiveInfo()
	if info.Evicted <= 0 {
		t.Fatalf("没有裁剪：%+v", info)
	}
	if info.Bytes > 4096 {
		t.Fatalf("arena 还在超限：%d", info.Bytes)
	}
	if info.Total != 200 {
		t.Fatalf("总帧数应该还是 200：%d", info.Total)
	}
	w := s.Window(0, 200, 0)
	if len(w.Rows) == 0 {
		t.Fatal("裁完没有行")
	}
	if w.Rows[0].Idx != info.Evicted {
		t.Fatalf("首行帧号 %d，期望 %d", w.Rows[0].Idx, info.Evicted)
	}
	if w.Total != 200 {
		t.Fatalf("窗口 total %d", w.Total)
	}
	// 帧号连续、没有空洞
	for i := 1; i < len(w.Rows); i++ {
		if w.Rows[i].Idx != w.Rows[i-1].Idx+1 {
			t.Fatalf("帧号跳了：%d -> %d", w.Rows[i-1].Idx, w.Rows[i].Idx)
		}
	}
	// 滚到底应当看到最后一帧
	last := s.Window(int(w.MaxOffset), 200, 0)
	if len(last.Rows) == 0 || last.Rows[len(last.Rows)-1].Idx != 199 {
		t.Fatalf("滚到底最后一帧 = %+v", last.Rows[len(last.Rows)-1].Idx)
	}
}

// TestLiveTrimmedPx 裁头时要把「被裁掉多少像素」报给前端：用户顶着旧数据看时，
// 前端按这个增量把 offset 减下去，画面才不会往下滑。
func TestLiveTrimmedPx(t *testing.T) {
	s := newLiveDoc(t, 4096)
	payload := make([]byte, 100)
	for i := range payload {
		payload[i] = byte(i)
	}
	for i := 0; i < 300; i++ {
		s.AppendFrame(liveFrame(int64(1000+i), 1, payload))
	}
	info := s.LiveInfo()
	if info.Evicted <= 0 {
		t.Fatalf("没有触发裁剪：%+v", info)
	}
	if info.TrimmedPx <= 0 {
		t.Fatalf("TrimmedPx 没算：%+v", info)
	}
	w := s.Window(0, 100, 0)
	if len(w.Rows) == 0 {
		t.Fatal("没有行")
	}
	// 每帧 payload 相同 → 行高相同，被裁掉的高度应正好是 evicted × 行高。
	if got, want := info.TrimmedPx, float64(info.Evicted)*w.Rows[0].H; math.Abs(got-want) > 0.6 {
		t.Fatalf("TrimmedPx = %v，期望 %v（%d 帧 × %v）", got, want, info.Evicted, w.Rows[0].H)
	}
	// 清空之后必须归零，否则新会话一打开就顶着「历史被覆盖」。
	if r := s.ResetLive(); r.TrimmedPx != 0 || r.Evicted != 0 || r.Total != 0 {
		t.Fatalf("清空后 %+v", r)
	}
}

// TestLiveEvictCompacts 逐帧裁头攒够一批会把物理数组搬回去（compact）。
// 搬之前和搬之后，帧号、行高、复制出来的内容都必须一样。
func TestLiveEvictCompacts(t *testing.T) {
	s := newLiveDoc(t, 4096) // 每帧 4 字节，能留 1024 帧
	const total = 6000
	for i := 0; i < total; i++ {
		s.AppendFrame(liveFrame(int64(1000+i), 1, []byte(fmt.Sprintf("F%03d", i%1000))))
		if i%997 == 0 {
			if info := s.LiveInfo(); info.Bytes > 4096 {
				t.Fatalf("第 %d 帧时 arena 超限：%d", i, info.Bytes)
			}
		}
	}
	info := s.LiveInfo()
	if info.Total != total {
		t.Fatalf("总帧数 %d，期望 %d", info.Total, total)
	}
	if info.Evicted < 1024 {
		t.Fatalf("被裁帧数 %d，没触发 compact", info.Evicted)
	}
	if s.ix.head >= 1024 {
		t.Fatalf("head=%d，物理数组没搬回去", s.ix.head)
	}
	// 物理 arena 不能无限涨：逻辑占用 4 KB，物理留到 ~2 倍就该搬了。
	if n := len(s.ix.arena); n > 4096*3 {
		t.Fatalf("物理 arena 涨到 %d", n)
	}
	w := s.Window(0, 200, 0)
	if len(w.Rows) == 0 {
		t.Fatal("没有行")
	}
	if w.Rows[0].Idx != info.Evicted {
		t.Fatalf("首行帧号 %d，期望 %d", w.Rows[0].Idx, info.Evicted)
	}
	for i := 1; i < len(w.Rows); i++ {
		if w.Rows[i].Idx != w.Rows[i-1].Idx+1 {
			t.Fatalf("帧号跳了：%d -> %d", w.Rows[i-1].Idx, w.Rows[i].Idx)
		}
	}
	// 行高一致（payload 等长），被裁掉的高度 = 帧数 × 行高
	if got, want := info.TrimmedPx, float64(info.Evicted)*w.Rows[0].H; math.Abs(got-want) > 0.6 {
		t.Fatalf("TrimmedPx = %v，期望 %v", got, want)
	}
	// 复制首行：内容要正好是被裁到的那一帧
	var copied string
	s.SetClipboard(func(text string) { copied = text })
	r := s.CopySelection(CopyReq{
		FromIdx: info.Evicted, FromVline: 0, FromCell: 0,
		ToIdx: info.Evicted, ToVline: 1 << 30, ToCell: 1 << 30,
		Format: "escaped",
	})
	if r.Err != "" {
		t.Fatal(r.Err)
	}
	want := fmt.Sprintf("F%03d\n", info.Evicted%1000)
	if r.Text != want || copied != want {
		t.Fatalf("复制 %q / 剪贴板 %q，期望 %q", r.Text, copied, want)
	}
	// 滚到底看最后一帧
	last := s.Window(int(s.Window(0, 200, 0).MaxOffset), 200, 0)
	if n := len(last.Rows); n == 0 || last.Rows[n-1].Idx != total-1 {
		t.Fatalf("滚到底最后一帧 = %+v", last.Rows)
	}
}

// TestLiveEvictAll 单帧就超过上限时会被整帧丢掉（文档暂时是空的），
// 之后再来一帧能留住的，帧号和高度都要从正确的位置接着算。
func TestLiveEvictAll(t *testing.T) {
	s := newLiveDoc(t, 4)
	s.AppendFrame(liveFrame(1000, 1, []byte("abcde"))) // 5 B > 4 B，留不住
	info := s.LiveInfo()
	if info.Frames != 0 || info.Total != 1 || info.Evicted != 1 {
		t.Fatalf("全裁之后 %+v", info)
	}
	if info.ContentH != 0 || info.TrimmedPx <= 0 {
		t.Fatalf("高度 %v / 裁掉 %v", info.ContentH, info.TrimmedPx)
	}
	s.AppendFrame(liveFrame(1001, 1, []byte("fghij")))
	if info := s.LiveInfo(); info.Frames != 0 || info.Total != 2 {
		t.Fatalf("第二帧之后 %+v", info)
	}
	// 这一帧只有 2 B，留得住
	s.AppendFrame(liveFrame(1002, 1, []byte("xy")))
	info = s.LiveInfo()
	if info.Frames != 1 || info.Total != 3 || info.Evicted != 2 {
		t.Fatalf("小帧之后 %+v", info)
	}
	w := s.Window(0, 100, 0)
	if len(w.Rows) != 1 || w.Rows[0].Idx != 2 {
		t.Fatalf("窗口 %+v", w.Rows)
	}
	if w.ContentHeight <= 0 || w.Rows[0].Y != 0 {
		t.Fatalf("高度 %v / 首行 Y %v", w.ContentHeight, w.Rows[0].Y)
	}
}

// TestSystemRowHexNoPrefix 系统消息行在前端固定按文本渲染，Hex 串不能带 0x
// 前缀 —— 解码器会把前缀当两个十六进制字符，中文会显示成一串点。
func TestSystemRowHexNoPrefix(t *testing.T) {
	s := NewStore()
	s.MarkLive(0)
	st := DefaultStyle(0)
	st.HexPrefix = true
	s.Configure(120, 42, 45, "hex", 0, st)
	s.AppendFrame(FrameIn{Ts: 1000, Dir: sessionfile.DirSystem, Hex: hex.EncodeToString([]byte("串口已关闭: COM4"))})
	s.AppendFrame(FrameIn{Ts: 1001, Dir: sessionfile.DirRx, Hex: "41"})
	w := s.Window(0, 400, 0)
	if len(w.Rows) != 2 {
		t.Fatalf("行数 %d", len(w.Rows))
	}
	if strings.Contains(w.Rows[0].Hex, "0x") {
		t.Fatalf("系统行带了 0x 前缀：%q", w.Rows[0].Hex)
	}
	if !strings.Contains(w.Rows[1].Hex, "0x41") {
		t.Fatalf("数据行丢了 0x 前缀：%q", w.Rows[1].Hex)
	}
}

// TestSystemRowHexInTextMode 文本模式下系统消息也要带 Hex 串。
//
// 前端 system 行只认 r.Hex（解码回文本），之前只在 hex 模式填，
// 切到「字」之后系统消息整行空白 —— 只剩时间戳和字节数。
func TestSystemRowHexInTextMode(t *testing.T) {
	s := NewStore()
	s.MarkLive(0)
	s.Configure(120, 42, 45, "text", 0, DefaultStyle(0))
	want := "串口已关闭: COM4"
	s.AppendFrame(FrameIn{Ts: 1000, Dir: sessionfile.DirSystem, Hex: hex.EncodeToString([]byte(want))})
	s.AppendFrame(FrameIn{Ts: 1001, Dir: sessionfile.DirRx, Hex: "41"})
	w := s.Window(0, 400, 0)
	if len(w.Rows) != 2 {
		t.Fatalf("行数 %d", len(w.Rows))
	}
	sys := w.Rows[0]
	if sys.Hex == "" {
		t.Fatal("文本模式下系统行没带 Hex，前端会渲染成空白行")
	}
	if strings.Contains(sys.Hex, "0x") {
		t.Fatalf("系统行带了 0x 前缀：%q", sys.Hex)
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(sys.Hex, " ", ""))
	if err != nil || string(raw) != want {
		t.Fatalf("系统行解不回原文：%q -> %q %v", sys.Hex, raw, err)
	}
	if w.Rows[1].Hex != "" {
		t.Fatalf("文本模式下数据行不该带 Hex：%q", w.Rows[1].Hex)
	}
}

// TestLivePrepend 往回补历史：插到最前面、重叠的部分丢掉。
func TestLivePrepend(t *testing.T) {
	s := newLiveDoc(t, 1<<20)
	for i := 0; i < 20; i++ {
		s.AppendFrame(liveFrame(int64(1000+i), 1, []byte("new")))
	}
	var older []FrameIn
	for i := 0; i < 100; i++ {
		older = append(older, liveFrame(int64(900+i), 2, []byte("old")))
	}
	s.PrependFrames(older)
	info := s.LiveInfo()
	if info.Total != 120 {
		t.Fatalf("补完总帧数 %d", info.Total)
	}
	w := s.Window(0, 200, 0)
	if w.Rows[0].Ts != 900 || w.Rows[0].Dir != "tx" {
		t.Fatalf("首帧 ts=%d dir=%s", w.Rows[0].Ts, w.Rows[0].Dir)
	}
	if w.Rows[0].Idx != 0 {
		t.Fatalf("首帧帧号 %d", w.Rows[0].Idx)
	}

	// 再补一批与现有内容重叠的：只保留比 900 更早的
	overlap := []FrameIn{
		liveFrame(890, 1, []byte("x")),
		liveFrame(895, 1, []byte("y")),
		liveFrame(900, 1, []byte("dup")),
		liveFrame(950, 1, []byte("dup")),
	}
	s.PrependFrames(overlap)
	if got := s.LiveInfo().Total; got != 122 {
		t.Fatalf("重叠去重后总帧数 %d，期望 122", got)
	}
	w = s.Window(0, 200, 0)
	if w.Rows[0].Ts != 890 {
		t.Fatalf("最早一帧 ts=%d", w.Rows[0].Ts)
	}
}

// TestLiveCopyAfterEvict 裁剪之后复制仍按绝对帧号取到正确的字节。
func TestLiveCopyAfterEvict(t *testing.T) {
	s := newLiveDoc(t, 200)
	var copied string
	s.SetClipboard(func(text string) { copied = text })
	for i := 0; i < 100; i++ {
		s.AppendFrame(liveFrame(int64(1000+i), 1, []byte(fmt.Sprintf("F%02d", i))))
	}
	info := s.LiveInfo()
	if info.Evicted == 0 {
		t.Fatal("没触发裁剪")
	}
	from := info.Evicted
	r := s.CopySelection(CopyReq{
		FromIdx: from, FromVline: 0, FromCell: 0,
		ToIdx: from + 1, ToVline: 1 << 30, ToCell: 1 << 30,
		Format: "escaped",
	})
	if r.Err != "" {
		t.Fatal(r.Err)
	}
	want := fmt.Sprintf("F%02d\nF%02d\n", from, from+1)
	if r.Text != want {
		t.Fatalf("复制 %q，期望 %q", r.Text, want)
	}
	if copied != want {
		t.Fatalf("剪贴板 %q", copied)
	}
}

// TestLiveReset 清空：帧号从头开始，高度归零。
func TestLiveReset(t *testing.T) {
	s := newLiveDoc(t, 1<<20)
	for i := 0; i < 5; i++ {
		s.AppendFrame(liveFrame(int64(1000+i), 1, []byte("x")))
	}
	s.ResetLive()
	info := s.LiveInfo()
	if info.Total != 0 || info.Frames != 0 || info.Evicted != 0 {
		t.Fatalf("清空之后 %+v", info)
	}
	if w := s.Window(0, 200, 0); len(w.Rows) != 0 || w.ContentHeight != 0 {
		t.Fatalf("清空之后还有行：%+v", w)
	}
	s.AppendFrame(liveFrame(2000, 1, []byte("y")))
	if got := s.LiveInfo().Total; got != 1 {
		t.Fatalf("清空后第一帧的总数 %d", got)
	}
}

// TestLiveTextStyleRecompute 实时文档切文本模式：格子缓存整表重算，行高跟着变。
func TestLiveTextStyleRecompute(t *testing.T) {
	s := newLiveDoc(t, 1<<20)
	// 一帧 200 个可见字符：文本模式 2 行（每行 120 格），Hex 模式一行 28 字节 → 8 行
	payload := make([]byte, 200)
	for i := range payload {
		payload[i] = 'a'
	}
	s.AppendFrame(liveFrame(1000, 1, payload))
	hexH := s.Window(0, 200, 0).ContentHeight
	s.Configure(120, 42, 45, "text", 0, DefaultStyle(0))
	textH := s.Window(0, 200, 0).ContentHeight
	if textH >= hexH {
		t.Fatalf("文本模式 %v 不比 Hex %v 矮", textH, hexH)
	}
	// 再切回来，行高要恢复
	s.Configure(120, 42, 45, "hex", 0, DefaultStyle(0))
	if got := s.Window(0, 200, 0).ContentHeight; got != hexH {
		t.Fatalf("切回 Hex 高度 %v，期望 %v", got, hexH)
	}
}
