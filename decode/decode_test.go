package decode

import (
	"encoding/hex"
	"testing"
)

func h(s string) string { return s }

func TestDecode_Empty(t *testing.T) {
	segs := Decode("", "utf-8")
	if segs != nil {
		t.Fatalf("expected nil for empty hex, got %v", segs)
	}
}

func TestDecode_ASCII_PureText(t *testing.T) {
	segs := Decode(hex.EncodeToString([]byte("Hello")), "ascii")
	if len(segs) != 1 || segs[0].Type != SegText || segs[0].Value != "Hello" {
		t.Fatalf("expected [text:Hello], got %+v", segs)
	}
}

func TestDecode_ASCII_HighBytes(t *testing.T) {
	// 0x80, 0xFF — non-ASCII in ASCII mode → hex escapes
	raw := hex.EncodeToString([]byte{0x48, 0x80, 0x69, 0xFF})
	segs := Decode(raw, "ascii")
	// Expect: text("H"), hex("80"), text("i"), hex("FF")
	if len(segs) != 4 {
		t.Fatalf("expected 4 segments, got %d: %+v", len(segs), segs)
	}
	if segs[0].Type != SegText || segs[0].Value != "H" {
		t.Errorf("seg[0]: %+v", segs[0])
	}
	if segs[1].Type != SegHex || segs[1].Value != "80" {
		t.Errorf("seg[1]: %+v", segs[1])
	}
	if segs[2].Type != SegText || segs[2].Value != "i" {
		t.Errorf("seg[2]: %+v", segs[2])
	}
	if segs[3].Type != SegHex || segs[3].Value != "FF" {
		t.Errorf("seg[3]: %+v", segs[3])
	}
}

func TestDecode_ASCII_ControlChars(t *testing.T) {
	raw := hex.EncodeToString([]byte{0x0D, 0x0A, 0x09, 0x00, 0x7F})
	segs := Decode(raw, "ascii")
	if len(segs) != 5 {
		t.Fatalf("expected 5 segments, got %d", len(segs))
	}
	// Note: ASCII decoder does NOT merge CRLF — it produces CR + LF separately.
	if segs[0].Type != SegCR {
		t.Errorf("seg[0]: %+v", segs[0])
	}
	if segs[1].Type != SegLF {
		t.Errorf("seg[1]: %+v", segs[1])
	}
	if segs[2].Type != SegTab {
		t.Errorf("seg[2]: %+v", segs[2])
	}
	if segs[3].Type != SegHex || segs[3].Value != "00" {
		t.Errorf("seg[3]: %+v", segs[3])
	}
	if segs[4].Type != SegHex || segs[4].Value != "7F" {
		t.Errorf("seg[4]: %+v", segs[4])
	}
}

func TestDecode_UTF8_PureASCII(t *testing.T) {
	segs := Decode(hex.EncodeToString([]byte("Hello World")), "utf-8")
	if len(segs) != 1 || segs[0].Type != SegText || segs[0].Value != "Hello World" {
		t.Fatalf("expected [text:Hello World], got %+v", segs)
	}
}

func TestDecode_UTF8_ChineseChars(t *testing.T) {
	raw := hex.EncodeToString([]byte("你好世界"))
	segs := Decode(raw, "utf-8")
	if len(segs) != 1 || segs[0].Type != SegText || segs[0].Value != "你好世界" {
		t.Fatalf("expected [text:你好世界], got %+v", segs)
	}
}

func TestDecode_UTF8_CRLF(t *testing.T) {
	raw := hex.EncodeToString([]byte("AB\r\nCD"))
	segs := Decode(raw, "utf-8")
	// Expect: text("AB"), crlf, text("CD")
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d: %+v", len(segs), segs)
	}
	if segs[0].Type != SegText || segs[0].Value != "AB" {
		t.Errorf("seg[0]: %+v", segs[0])
	}
	if segs[1].Type != SegCRLF {
		t.Errorf("seg[1]: %+v", segs[1])
	}
	if segs[2].Type != SegText || segs[2].Value != "CD" {
		t.Errorf("seg[2]: %+v", segs[2])
	}
}

func TestDecode_UTF8_IsolatedCR(t *testing.T) {
	raw := hex.EncodeToString([]byte{0x41, 0x0D, 0x42})
	segs := Decode(raw, "utf-8")
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segs))
	}
	if segs[1].Type != SegCR {
		t.Errorf("seg[1]: %+v", segs[1])
	}
}

func TestDecode_UTF8_IsolatedLF(t *testing.T) {
	raw := hex.EncodeToString([]byte{0x41, 0x0A, 0x42})
	segs := Decode(raw, "utf-8")
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segs))
	}
	if segs[1].Type != SegLF {
		t.Errorf("seg[1]: %+v", segs[1])
	}
}

func TestDecode_UTF8_Tab(t *testing.T) {
	raw := hex.EncodeToString([]byte{0x41, 0x09, 0x42})
	segs := Decode(raw, "utf-8")
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segs))
	}
	if segs[1].Type != SegTab {
		t.Errorf("seg[1]: %+v", segs[1])
	}
}

func TestDecode_UTF8_ControlChars(t *testing.T) {
	raw := hex.EncodeToString([]byte{0x00, 0x01, 0x1F, 0x7F})
	segs := Decode(raw, "utf-8")
	if len(segs) != 4 {
		t.Fatalf("expected 4 segments, got %d", len(segs))
	}
	for i, s := range segs {
		if s.Type != SegHex {
			t.Errorf("seg[%d]: expected hex, got %s", i, s.Type)
		}
	}
}

func TestDecode_UTF8_Overlong(t *testing.T) {
	// 0xC0 0x80 is an overlong encoding of NUL — invalid
	raw := hex.EncodeToString([]byte{0x41, 0xC0, 0x80, 0x42})
	segs := Decode(raw, "utf-8")
	// Expect: text("A"), hex("C0"), hex("80"), text("B")
	// (0xC0 < 0xC2 so falls to default case, 0x80 < 0xC2 but is continuation byte with no lead → default)
	if len(segs) != 4 {
		t.Fatalf("expected 4 segments, got %d: %+v", len(segs), segs)
	}
	if segs[1].Type != SegHex || segs[1].Value != "C0" {
		t.Errorf("seg[1]: %+v", segs[1])
	}
	if segs[2].Type != SegHex || segs[2].Value != "80" {
		t.Errorf("seg[2]: %+v", segs[2])
	}
}

func TestDecode_UTF8_SurrogateHalf(t *testing.T) {
	// 0xED 0xA0 0x80 is a surrogate half — invalid in UTF-8
	raw := hex.EncodeToString([]byte{0x41, 0xED, 0xA0, 0x80, 0x42})
	segs := Decode(raw, "utf-8")
	// 0xED is in E0-EF range, but b2=0xA0 > 0x9F so the check fails
	// 0xED → hex, then 0xA0 → default (lone continuation → hex), 0x80 → default
	if segs[1].Type != SegHex || segs[1].Value != "ED" {
		t.Errorf("seg[1]: %+v", segs[1])
	}
}

func TestDecode_UTF8_Truncated(t *testing.T) {
	// 0xE4 0xBD is truncated 3-byte sequence
	raw := hex.EncodeToString([]byte{0x41, 0xE4, 0xBD})
	segs := Decode(raw, "utf-8")
	// text("A"), then 0xE4 in range E0-EF but not enough bytes → default → hex("E4"), then 0xBD → default → hex("BD")
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d: %+v", len(segs), segs)
	}
	if segs[1].Type != SegHex || segs[1].Value != "E4" {
		t.Errorf("seg[1]: %+v", segs[1])
	}
}

func TestDecode_UTF8_Mixed(t *testing.T) {
	// "A" + "你" + invalid 0xFF + "B"
	// Valid adjacent chars are batched: "A你" is one text segment.
	raw := hex.EncodeToString([]byte{0x41, 0xE4, 0xBD, 0xA0, 0xFF, 0x42})
	segs := Decode(raw, "utf-8")
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments (text A你 + hex FF + text B), got %d: %+v", len(segs), segs)
	}
	if segs[0].Type != SegText || segs[0].Value != "A你" {
		t.Errorf("seg[0]: %+v", segs[0])
	}
	if segs[1].Type != SegHex || segs[1].Value != "FF" {
		t.Errorf("seg[1]: %+v", segs[1])
	}
	if segs[2].Type != SegText || segs[2].Value != "B" {
		t.Errorf("seg[2]: %+v", segs[2])
	}
}

func TestDecode_UTF8_4ByteEmoji(t *testing.T) {
	// U+1F600 😀 → F0 9F 98 80
	raw := hex.EncodeToString([]byte{0xF0, 0x9F, 0x98, 0x80})
	segs := Decode(raw, "utf-8")
	if len(segs) != 1 || segs[0].Type != SegText {
		t.Fatalf("expected [text:😀], got %+v", segs)
	}
}

func TestDecode_UTF8_OutOfRange4Byte(t *testing.T) {
	// 0xF4 0x90 is beyond U+10FFFF
	raw := hex.EncodeToString([]byte{0x41, 0xF4, 0x90, 0x80, 0x80})
	segs := Decode(raw, "utf-8")
	// text("A"), 0xF4 check: b2=0x90 > 0x8F → invalid → hex("F4")
	if segs[1].Type != SegHex || segs[1].Value != "F4" {
		t.Errorf("seg[1]: %+v", segs[1])
	}
}

func TestDecode_GBK_ASCII(t *testing.T) {
	raw := hex.EncodeToString([]byte("Hello"))
	segs := Decode(raw, "gb2312")
	if len(segs) != 1 || segs[0].Type != SegText || segs[0].Value != "Hello" {
		t.Fatalf("expected [text:Hello], got %+v", segs)
	}
}

func TestDecode_GBK_Chinese(t *testing.T) {
	// "你好" in GBK: C4 E3 BA C3
	raw := hex.EncodeToString([]byte{0xC4, 0xE3, 0xBA, 0xC3})
	segs := Decode(raw, "gb2312")
	if len(segs) != 1 || segs[0].Type != SegText || segs[0].Value != "你好" {
		t.Fatalf("expected [text:你好], got %+v", segs)
	}
}

func TestDecode_GBK_InvalidFirstByte(t *testing.T) {
	// 0x80 is not a valid GBK lead byte (below 0x81) and not ASCII
	raw := hex.EncodeToString([]byte{0x41, 0x80, 0x42})
	segs := Decode(raw, "gb2312")
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d: %+v", len(segs), segs)
	}
	if segs[1].Type != SegHex || segs[1].Value != "80" {
		t.Errorf("seg[1]: %+v", segs[1])
	}
}

func TestDecode_GBK_InvalidTrailByte(t *testing.T) {
	// 0xC4 is valid lead, 0x00 is invalid trail (outside 0x40-0x7E, 0x80-0xFE)
	raw := hex.EncodeToString([]byte{0x41, 0xC4, 0x00, 0x42})
	segs := Decode(raw, "gb2312")
	if len(segs) < 3 {
		t.Fatalf("expected at least 3 segments, got %d: %+v", len(segs), segs)
	}
	// seg[1] should be hex("C4") since trail is invalid
	if segs[1].Type != SegHex || segs[1].Value != "C4" {
		t.Errorf("seg[1]: %+v", segs[1])
	}
}

func TestDecode_GBK_CRLF(t *testing.T) {
	raw := hex.EncodeToString([]byte("AB\r\nCD"))
	segs := Decode(raw, "gb2312")
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d: %+v", len(segs), segs)
	}
	if segs[1].Type != SegCRLF {
		t.Errorf("seg[1]: %+v", segs[1])
	}
}

func TestDecode_GBK_HighLeadWithValidTrail(t *testing.T) {
	// Valid GBK 2-byte: first byte 0x81-0xFE, second byte valid range
	// Test with 0xFE 0x40 (valid boundary)
	raw := hex.EncodeToString([]byte{0xFE, 0x40})
	segs := Decode(raw, "gb2312")
	t.Logf("segments: %+v", segs)
	// Should produce at least one segment
	if len(segs) == 0 {
		t.Fatal("expected non-empty segments")
	}
}

func TestBatchDecode(t *testing.T) {
	hexes := []string{
		hex.EncodeToString([]byte("Hello")),
		hex.EncodeToString([]byte("World")),
	}
	results := BatchDecode(hexes, "utf-8")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0][0].Value != "Hello" {
		t.Errorf("result[0]: %+v", results[0])
	}
	if results[1][0].Value != "World" {
		t.Errorf("result[1]: %+v", results[1])
	}
}

func TestDecode_GBK_InvalidPairTransform(t *testing.T) {
	// 0xFF is not a valid lead byte (> 0xFE), should be hex escape
	raw := hex.EncodeToString([]byte{0xFF})
	segs := Decode(raw, "gb2312")
	if len(segs) != 1 || segs[0].Type != SegHex || segs[0].Value != "FF" {
		t.Fatalf("expected [hex:FF], got %+v", segs)
	}
}

func TestDecode_TextBatching(t *testing.T) {
	// Adjacent ASCII chars should be batched into one text segment
	raw := hex.EncodeToString([]byte("ABCDEFGHIJ"))
	segs := Decode(raw, "utf-8")
	if len(segs) != 1 || segs[0].Type != SegText || segs[0].Value != "ABCDEFGHIJ" {
		t.Fatalf("expected single batched text, got %+v", segs)
	}
}
