package decode

import (
	"encoding/hex"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Segment is one decoded chunk.
type Segment struct {
	Type  string `json:"t"` // text|hex|cr|lf|crlf|tab
	Value string `json:"v"` // text: content; hex: uppercase hex like "FF"; others: ""
}

// Segment types.
const (
	SegText = "text"
	SegHex  = "hex"
	SegCR   = "cr"
	SegLF   = "lf"
	SegCRLF = "crlf"
	SegTab  = "tab"
)

// Decode hex string (uppercase, no separator) into segments using encoding.
// encoding must be "utf-8", "gb2312", or "ascii".
func Decode(hexStr string, encoding string) []Segment {
	if hexStr == "" {
		return nil
	}
	raw, err := hex.DecodeString(hexStr)
	if err != nil || len(raw) == 0 {
		return nil
	}
	switch encoding {
	case "ascii":
		return decodeASCII(raw)
	case "gb2312":
		return decodeGBKTolerant(raw)
	default:
		return decodeUTF8Tolerant(raw)
	}
}

// BatchDecode decodes multiple hex strings.
func BatchDecode(hexStrings []string, encoding string) [][]Segment {
	result := make([][]Segment, len(hexStrings))
	for i, h := range hexStrings {
		result[i] = Decode(h, encoding)
	}
	return result
}

// ---- segmented builder ----

type segBuilder struct {
	segs []Segment
	buf  strings.Builder
}

func (b *segBuilder) pushText(s string) {
	b.buf.WriteString(s)
}

func (b *segBuilder) flushText() {
	if b.buf.Len() > 0 {
		b.segs = append(b.segs, Segment{Type: SegText, Value: b.buf.String()})
		b.buf.Reset()
	}
}

func (b *segBuilder) emit(t string, v string) {
	b.flushText()
	b.segs = append(b.segs, Segment{Type: t, Value: v})
}

func (b *segBuilder) result() []Segment {
	b.flushText()
	return b.segs
}

func hexByte(b byte) string {
	const u = "0123456789ABCDEF"
	return string([]byte{u[b>>4], u[b&0x0F]})
}

// ---- tolerant ASCII decoder ----

func decodeASCII(raw []byte) []Segment {
	var sb segBuilder
	for _, b := range raw {
		switch {
		case b == 0x0D:
			sb.emit(SegCR, "")
		case b == 0x0A:
			sb.emit(SegLF, "")
		case b == 0x09:
			sb.emit(SegTab, "")
		case b < 0x20 || b == 0x7F:
			sb.emit(SegHex, hexByte(b))
		case b < 0x80:
			sb.pushText(string(rune(b)))
		default:
			sb.emit(SegHex, hexByte(b))
		}
	}
	return sb.result()
}

// ---- tolerant UTF-8 decoder ----
// Mirrors JS _decodeUTF8Tolerant byte-for-byte.

func decodeUTF8Tolerant(raw []byte) []Segment {
	var sb segBuilder
	i := 0
	for i < len(raw) {
		b := raw[i]
		switch {
		case b == 0x0D:
			if i+1 < len(raw) && raw[i+1] == 0x0A {
				sb.emit(SegCRLF, "")
				i += 2
			} else {
				sb.emit(SegCR, "")
				i++
			}
		case b == 0x0A:
			sb.emit(SegLF, "")
			i++
		case b == 0x09:
			sb.emit(SegTab, "")
			i++
		case b < 0x20 || b == 0x7F:
			sb.emit(SegHex, hexByte(b))
			i++
		case b < 0x80:
			sb.pushText(string(rune(b)))
			i++
		case b >= 0xC2 && b <= 0xDF && i+1 < len(raw):
			b2 := raw[i+1]
			if (b2 & 0xC0) == 0x80 {
				sb.pushText(string(raw[i : i+2]))
				i += 2
			} else {
				sb.emit(SegHex, hexByte(b))
				i++
			}
		case b >= 0xE0 && b <= 0xEF && i+2 < len(raw):
			b2, b3 := raw[i+1], raw[i+2]
			if (b2&0xC0) == 0x80 && (b3&0xC0) == 0x80 &&
				!(b == 0xE0 && b2 < 0xA0) && !(b == 0xED && b2 > 0x9F) {
				sb.pushText(string(raw[i : i+3]))
				i += 3
			} else {
				sb.emit(SegHex, hexByte(b))
				i++
			}
		case b >= 0xF0 && b <= 0xF4 && i+3 < len(raw):
			b2, b3, b4 := raw[i+1], raw[i+2], raw[i+3]
			if (b2&0xC0) == 0x80 && (b3&0xC0) == 0x80 && (b4&0xC0) == 0x80 &&
				!(b == 0xF0 && b2 < 0x90) && !(b == 0xF4 && b2 > 0x8F) {
				sb.pushText(string(raw[i : i+4]))
				i += 4
			} else {
				sb.emit(SegHex, hexByte(b))
				i++
			}
		default:
			sb.emit(SegHex, hexByte(b))
			i++
		}
	}
	return sb.result()
}

// ---- tolerant GBK decoder ----
// Mirrors JS _decodeGBKTolerant.  Creates a fresh GBK decoder per call to
// avoid any internal state leakage and to be safe for concurrent use.

func decodeGBKTolerant(raw []byte) []Segment {
	var sb segBuilder
	dec := simplifiedchinese.GBK.NewDecoder()
	i := 0
	for i < len(raw) {
		b := raw[i]
		switch {
		case b == 0x0D:
			if i+1 < len(raw) && raw[i+1] == 0x0A {
				sb.emit(SegCRLF, "")
				i += 2
			} else {
				sb.emit(SegCR, "")
				i++
			}
		case b == 0x0A:
			sb.emit(SegLF, "")
			i++
		case b == 0x09:
			sb.emit(SegTab, "")
			i++
		case b < 0x20 || b == 0x7F:
			sb.emit(SegHex, hexByte(b))
			i++
		case b < 0x80:
			sb.pushText(string(rune(b)))
			i++
		case b >= 0x81 && b <= 0xFE && i+1 < len(raw):
			b2 := raw[i+1]
			if (b2 >= 0x40 && b2 <= 0x7E) || (b2 >= 0x80 && b2 <= 0xFE) {
				// Valid GBK lead+trail range.  Decode the pair.
				dst := make([]byte, 4)
				nd, _, err := dec.Transform(dst, raw[i:i+2], false)
				if err == nil && nd > 0 {
					sb.pushText(string(dst[:nd]))
				} else {
					// Transform failed — treat first byte as invalid.
					sb.emit(SegHex, hexByte(b))
				}
				i += 2
			} else {
				sb.emit(SegHex, hexByte(b))
				i++
			}
		default:
			sb.emit(SegHex, hexByte(b))
			i++
		}
	}
	return sb.result()
}
