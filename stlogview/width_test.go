package stlogview

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/nienieai/serial-debugger/decode"
)

// writeBytes 是个小工具：测试里造一个非 .stlog 文件。
func writeBytes(path string, b []byte) error { return os.WriteFile(path, b, 0o644) }

// 这两条是实测出来的、反直觉的规则（见 width.go 的注释）：
//   - 裸 CR 不换行，只占 `←` 那一格；
//   - 末尾的 LF 不多出一个空行。
//
// 钉住它们，免得以后「看着像 bug」又被改回去。
func TestLineBreakRules(t *testing.T) {
	cases := []struct {
		in   string
		want []uint32
		note string
	}{
		{"4141" + "0d" + "4242", []uint32{5}, "AA\\rBB 一行（← 1 格）"},
		{"4141" + "0a" + "4242", []uint32{3, 2}, "AA\\nBB 两行"},
		{"4141" + "0d0a" + "4242", []uint32{4, 2}, "AA\\r\\nBB 两行（←↵ 2 格）"},
		{"4141" + "0a", []uint32{3}, "末尾 LF 不多一行"},
		{"4141" + "0a0a", []uint32{3, 1}, "两个 LF 才是两行"},
		{"0d", []uint32{1}, "只有一个 CR：一行一格"},
	}
	for _, c := range cases {
		p, err := hex.DecodeString(c.in)
		if err != nil {
			t.Fatal(err)
		}
		got := cellsOf(p, DefaultStyle(0), nil)
		ok := len(got) == len(c.want)
		if ok {
			for i := range got {
				if got[i] != c.want[i] {
					ok = false
				}
			}
		}
		if !ok {
			t.Fatalf("%s（%s）：得到 %v，期望 %v", c.note, c.in, got, c.want)
		}
	}
}

// TestCellsMatchDecode 是格子模型的独立校验：用主工程真正的 decode.Decode 算出
// 每段文本，再按 renderSegments 的规则累加宽度，与 cellsOf 的结果比。
// 两边任何一处对「空格 / 控制字节 / Tab / 非法字节」的看法不同，这里就会红。
func TestCellsMatchDecode(t *testing.T) {
	cases := [][]byte{
		[]byte("hello world"),
		[]byte("a b  c"),
		[]byte("line1\r\nline2"),
		[]byte("a\rb\nc"),
		[]byte("tab\there\tand\tthere"),
		{0x00, 0x01, 0x7F, 0xFF},
		[]byte("中文混排 abc"),
		{0xE4, 0xB8, 0xAD, 0x41},      // 中A
		{0xE4, 0xB8},                  // 半个汉字 → 两个 /XX
		{0xF0, 0x9F, 0x98, 0x80, 'x'}, // 😀x
		{},
		[]byte("\r\n"),
		[]byte("trailing\r\n"),
	}
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 300; i++ {
		n := rng.Intn(40)
		b := make([]byte, n)
		rng.Read(b)
		// 掺一些控制字节与换行，别让随机数据全靠 /XX
		for j := range b {
			if b[j] < 0x80 && b[j] >= 0x20 {
				if rng.Intn(4) == 0 {
					b[j] = byte(rng.Intn(0x20))
				}
			}
		}
		cases = append(cases, b)
	}
	for _, enc := range []uint8{0, 1} {
		for _, c := range cases {
			got := cellsOf(c, DefaultStyle(enc), nil)
			want := oracleCells(c, enc)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("enc=%d payload=%x\n  计算 %v\n  标准 %v", enc, c, got, want)
			}
		}
	}
}

// oracleCells 用 decode.Decode + renderSegments 的规则重算一遍。
func oracleCells(p []byte, enc uint8) []uint32 {
	segs := decode.Decode(hex.EncodeToString(p), encName(enc))
	var out []uint32
	var cells uint32
	for _, s := range segs {
		switch s.Type {
		case decode.SegText:
			for _, r := range s.Value {
				cells += uint32(runeCells(r))
			}
		case decode.SegHex:
			cells += 3 // /XX
		case decode.SegCR:
			cells++ // ← 只是标记，不换行（见 width.go 里的实测说明）
		case decode.SegLF:
			cells++
			out = append(out, cells)
			cells = 0
		case decode.SegTab:
			cells++ // → 箭头
			if r := cells % tabSize; r != 0 {
				cells += tabSize - r
			} else {
				cells += tabSize
			}
		case decode.SegCRLF:
			cells += 2 // ←↵
			out = append(out, cells)
			cells = 0
		}
	}
	if cells > 0 || len(out) == 0 {
		out = append(out, cells)
	}
	return out
}

// TestLinesMatchCells 索引里存的格子数必须与按 payload 现算的一致，
// 行高也必须等于公式。这条一红，说明 scanStlog 的续片拼接和 segIdx 错位了。
func TestLinesMatchCells(t *testing.T) {
	p := writeTestFile(t, defaultFrames())
	ix, err := scanStlog(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	s.ix = ix
	s.mode = "text"
	s.charsPerLine = 160
	s.buildLayoutLocked()
	bad := 0
	for i := 0; i < ix.n; i++ {
		pay := ix.arena[ix.aoff[i] : ix.aoff[i]+int32(ix.ln[i])]
		want := 0
		for _, c := range cellsOf(pay, ix.scanStyle(), nil) {
			want += linesFor(c, s.perLine)
		}
		got := s.linesOfLocked(i)
		gotH := int(s.prefix[i+1] - s.prefix[i])
		wantH := s.tsLineH2 + want*s.dataLineH2 + rowGap2
		if got != want || gotH != wantH {
			bad++
			if bad <= 5 {
				t.Errorf("第 %d 帧：索引 %d 行 / 现算 %d 行，高 %d / %d，payload=%x",
					i, got, want, gotH, wantH, pay)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d 帧的行数对不上", bad)
	}
	t.Logf("%d 帧全部一致", ix.n)
}
