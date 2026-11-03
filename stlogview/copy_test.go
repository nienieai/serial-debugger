package stlogview

import (
	"strings"
	"testing"
	"time"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// buildIndexForTest 手搓一个只有几帧的索引，用来测格→字节和三种格式。
// 走的是和生产同一套 cellsOf，所以 seg 与真实扫描出来的形状一致。
func buildIndexForTest(payloads [][]byte, dirs []uint8) *index {
	ix := &index{}
	var arena []byte
	for i, p := range payloads {
		ix.ts = append(ix.ts, 1700000000000+int64(i*1000))
		ix.aoff = append(ix.aoff, int32(len(arena)))
		ix.ln = append(ix.ln, uint32(len(p)))
		d := sessionfile.DirRx
		if i < len(dirs) {
			d = dirs[i]
		}
		ix.dir = append(ix.dir, d)
		ix.org = append(ix.org, 0)
		ix.segIdx = append(ix.segIdx, int32(len(ix.seg)))
		ix.seg = append(ix.seg, cellsOf(p, DefaultStyle(0), nil)...)
		arena = append(arena, p...)
	}
	ix.segIdx = append(ix.segIdx, int32(len(ix.seg)))
	ix.n = len(payloads)
	ix.arena = arena
	return ix
}

func newTestStore(payloads [][]byte, mode string, perLine int) *Store {
	s := NewStore()
	s.ix = buildIndexForTest(payloads, nil)
	s.mode = mode
	s.charsPerLine = perLine
	s.buildLayoutLocked()
	return s
}

// 格 → 字节：文本、Hex 两套都要对得上。
func TestPosToByte(t *testing.T) {
	// "ABCDEFGHIJ\r\nKL"：segs = [12, 2]（10 + CR + LF 算在第一个逻辑行里）
	s := newTestStore([][]byte{[]byte("ABCDEFGHIJ\r\nKL")}, "text", 10)
	cases := []struct{ vline, cell, want int }{
		{0, 0, 0},
		{0, 5, 5},
		{1, 0, 10}, // 第一个折行的开头 = CR
		{1, 1, 10}, // ←↵ 是一对，落在中间就左贴到 CR
		{1, 2, 12}, // 行尾 = KL 的开头
		{2, 0, 12}, // 第二个逻辑行
		{2, 1, 13},
		{9, 0, 14}, // 越界 → 帧尾
	}
	for _, c := range cases {
		if got := s.posToByteLocked(0, c.vline, c.cell); got != c.want {
			t.Errorf("文本 vline=%d cell=%d → %d，期望 %d", c.vline, c.cell, got, c.want)
		}
	}

	// Hex：charsPerLine=11 → 一行 4 字节（11 格够放 "AA BB CC DD" 的 11 个字），
	// 第 2 行第 0 格 = 第 5 个字节
	h := newTestStore([][]byte{{1, 2, 3, 4, 5, 6}}, "hex", 11)
	if h.perLine != 4 {
		t.Fatalf("Hex perLine = %d，期望 4", h.perLine)
	}
	if got := h.posToByteLocked(0, 1, 0); got != 4 {
		t.Errorf("Hex vline=1 → %d，期望 4", got)
	}
	if got := h.posToByteLocked(0, 0, 6); got != 2 { // 第 6 格 = 第 3 个 16 进制字符 = 第 2 个字节
		t.Errorf("Hex cell=6 → %d，期望 2", got)
	}
	if got := h.posToByteLocked(0, 9, 9); got != 6 {
		t.Errorf("Hex 越界 → %d，期望 6", got)
	}
}

// 三种格式对同一段字节的输出。
func TestFormatBytes(t *testing.T) {
	p := []byte{'A', 0x00, ' ', 0xFF, '\r', '\n', 'B'}
	cases := []struct{ format, want string }{
		{copyHex, "41 00 20 FF 0D 0A 42"},
		{"hex0x", "0x41 0x00 0x20 0xFF 0x0D 0x0A 0x42"},
		{copyEscaped, "A/00 /FF\r\nB"},
		// 原始模式跳过 NUL：进了剪贴板会被按 C 字符串截断，静默丢后半截
		{copyRaw, "A ÿ\r\nB"},
	}
	for _, c := range cases {
		prefix := false
		f := c.format
		if f == "hex0x" {
			f, prefix = copyHex, true
		}
		if got := formatBytes(p, DefaultStyle(0), f, prefix); got != c.want {
			t.Errorf("%s → %q，期望 %q", c.format, got, c.want)
		}
	}
}

// 整段复制：帧数、字节数、每帧的前缀与换行。
func TestCopySelection(t *testing.T) {
	s := newTestStore([][]byte{
		[]byte("hello"),
		[]byte("world!"),
		[]byte("tail"),
	}, "text", 40)

	// 剪贴板走注入的钩子：这里接一个记录器，验证「写完真的调了」。
	var copied string
	s.SetClipboard(func(text string) { copied = text })

	r := s.CopySelection(CopyReq{
		FromIdx: 0, FromVline: 0, FromCell: 0,
		ToIdx: 2, ToVline: 1e9, ToCell: 1e9,
		Format: copyEscaped,
	})
	if r.Err != "" {
		t.Fatal(r.Err)
	}
	if r.Frames != 3 || r.Bytes != 5+6+4 {
		t.Fatalf("帧 %d / 字节 %d", r.Frames, r.Bytes)
	}
	if r.Text != "hello\nworld!\ntail\n" {
		t.Fatalf("文本 = %q", r.Text)
	}
	if copied != r.Text {
		t.Fatalf("剪贴板收到 %q，期望 %q", copied, r.Text)
	}

	// 带前缀：时间戳 + 方向 + 长度
	r2 := s.CopySelection(CopyReq{
		FromIdx: 1, FromVline: 0, FromCell: 0,
		ToIdx: 1, ToVline: 1e9, ToCell: 1e9,
		Format: copyHex, WithTS: true, WithDir: true, WithLen: true,
	})
	wantTS := time.UnixMilli(1700000001000).Format("2006-01-02 15:04:05.000")
	if !strings.HasPrefix(r2.Text, "["+wantTS+"] Rx ⇃ ~6B 77 6F 72 6C 64 21") {
		t.Fatalf("带前缀的 Hex = %q", r2.Text)
	}

	// 反过来选（从右下往左上拖）也要得到同样的区间
	r3 := s.CopySelection(CopyReq{
		FromIdx: 1, FromVline: 0, FromCell: 6,
		ToIdx: 1, ToVline: 0, ToCell: 0,
		Format: copyEscaped,
	})
	if r3.Text != "world!\n" {
		t.Fatalf("反选 = %q", r3.Text)
	}

	// 半帧：只选前两个字节
	r4 := s.CopySelection(CopyReq{
		FromIdx: 0, FromVline: 0, FromCell: 0,
		ToIdx: 0, ToVline: 0, ToCell: 2,
		Format: copyEscaped,
	})
	if r4.Text != "he\n" || r4.Bytes != 2 {
		t.Fatalf("半帧 = %q（%d B）", r4.Text, r4.Bytes)
	}

	// 没有文档时给错误，不写剪贴板
	empty := NewStore()
	if r := empty.CopySelection(CopyReq{}); r.Err == "" {
		t.Fatal("空文档应该返回错误")
	}
}

// 跨逻辑行的选区：换行要跟着带走，不能丢。
func TestCopyAcrossLines(t *testing.T) {
	s := newTestStore([][]byte{[]byte("abc\ndef")}, "text", 80)
	r := s.CopySelection(CopyReq{
		FromIdx: 0, FromVline: 0, FromCell: 0,
		ToIdx: 0, ToVline: 1, ToCell: 3,
		Format: copyEscaped,
	})
	if r.Text != "abc\ndef\n" {
		t.Fatalf("跨行 = %q", r.Text)
	}
}
