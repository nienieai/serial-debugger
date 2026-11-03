package stlogview

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/nienieai/serial-debugger/decode"
)

// 复制。选区在模型里（帧号 + 折行后的第几行 + 行内第几格），这里把它换算成
// **字节区间**，再按三种格式之一输出。
//
// 为什么换算放在 Go：格→字节要认「宽字吃 2 格、制表符按 8 的倍数跳、非法字节显示
// 成 /XX 三格」，这套规则本来就只在 width.go 里有；换成前端的 DOM 去数，
// 稍微一改渲染就和复制对不上了。
//
// 为什么输出三种格式而不是跟显示走：显示是「看到的」，复制常常要「拿到的」——
// 把一段源码从 .stlog 里拷出来要的是干净的字符，把一帧报文拷给同事要的是 Hex。

const (
	copyEscaped = "escaped" // 看到的字符：非法字节 /XX、空格空格、换行真换行
	copyRaw     = "raw"     // 解出来的字符；非法字节按 Latin-1 兜底，一个字节一个码位
	copyHex     = "hex"     // AA BB CC
)

// CopyReq 是前端一次复制请求。对齐的是**折行后的视觉行**（vline），不是逻辑行 ——
// 前端只知道鼠标落在第几行，逻辑行要配合 perLine 才能算出来。
type CopyReq struct {
	FromIdx   int    `json:"fromIdx"`
	FromVline int    `json:"fromVline"`
	FromCell  int    `json:"fromCell"`
	ToIdx     int    `json:"toIdx"`
	ToVline   int    `json:"toVline"`
	ToCell    int    `json:"toCell"`
	Format    string `json:"format"`
	WithTS    bool   `json:"withTs"`
	TSFull    bool   `json:"tsFull"`
	WithDir   bool   `json:"withDir"`
	WithLen   bool   `json:"withLen"`
	HexPrefix bool   `json:"hexPrefix"`
}

type CopyResult struct {
	Text   string `json:"text"`
	Frames int    `json:"frames"`
	Bytes  int    `json:"bytes"`
	Chars  int    `json:"chars"`
	Nul    int    `json:"nul"`
	Ms     int64  `json:"ms"`
	Err    string `json:"err"`
}

// maxCopyFrames 是一次复制的帧数上限。剪贴板塞几百兆没人受得了，
// 而且拼字符串本身在主线程/Go 里也都要时间。
const maxCopyFrames = 50000

// posToByte 把（第几帧，第几个视觉行，行内第几格）换算成 payload 里的字节偏移。
// 落在半个字符/半个 /XX 里时向左贴齐 —— 选区不会把字节切开。
func (s *Store) posToByteLocked(idx, vline, cell int) int {
	ix := s.ix
	if ix == nil || idx < 0 || idx >= ix.n {
		return 0
	}
	p := ix.arena[ix.aoff[idx] : ix.aoff[idx]+int32(ix.ln[idx])]
	if cell < 0 {
		cell = 0
	}
	if s.mode == "hex" {
		per := s.style.hexPerByte() // "AA " 三格、带 0x 五格、无分隔两格……
		off := vline*s.perLine + cell/per
		if off > len(p) {
			off = len(p)
		}
		if off < 0 {
			off = 0
		}
		return off
	}
	// 文本：第几个视觉行 → 第几个逻辑行 + 行内格偏移
	segs := ix.seg[ix.segIdx[idx]:ix.segIdx[idx+1]]
	if vline < 0 {
		vline = 0
	}
	v := vline
	for L, c := range segs {
		wrap := linesFor(c, s.perLine)
		if v < wrap {
			want := v*s.perLine + cell
			if want > int(c) {
				want = int(c)
			}
			return byteAtPos(p, s.style, L, want)
		}
		v -= wrap
	}
	return len(p)
}

// byteAtPos 走到「第 wantLine 个逻辑行的第 wantCells 格」，返回那个字节的偏移。
// 逐单元走（宽字 2 格、Tab 跳到 8 的倍数、/XX 3 格），与 cellsOf 用的是同一个 unitAt。
func byteAtPos(p []byte, st Style, wantLine, wantCells int) int {
	line, col, i := 0, 0, 0
	for i < len(p) {
		if line > wantLine || (line == wantLine && col >= wantCells) {
			return i
		}
		c, size, brk := unitAt(p[i:], st, col)
		if line == wantLine && col+c > wantCells {
			return i // 落在单元中间 → 左贴，绝不把一半的 /XX 或 ←↵ 带出去
		}
		col += c
		i += size
		if brk != 0 {
			line++
			col = 0
		}
	}
	return len(p)
}

// copyRange 把请求里的两个端点排序、夹紧、换成字节区间。
func (s *Store) copyRangeLocked(req *CopyReq) (fromIdx, fromOff, toIdx, toOff int) {
	ix := s.ix
	if ix == nil || ix.n == 0 {
		return 0, 0, -1, 0
	}
	fromIdx = clampInt(req.FromIdx, 0, ix.n-1)
	toIdx = clampInt(req.ToIdx, 0, ix.n-1)
	if fromIdx > toIdx {
		fromIdx, toIdx = toIdx, fromIdx
		req.FromVline, req.ToVline = req.ToVline, req.FromVline
		req.FromCell, req.ToCell = req.ToCell, req.FromCell
	}
	fromOff = s.posToByteLocked(fromIdx, req.FromVline, req.FromCell)
	toOff = s.posToByteLocked(toIdx, req.ToVline, req.ToCell)
	if fromIdx == toIdx && toOff < fromOff {
		fromOff, toOff = toOff, fromOff
	}
	return
}

// CopySelection 生成复制的文本并写进剪贴板。
func (s *Store) CopySelection(req CopyReq) CopyResult {
	t0 := time.Now()
	format := req.Format
	switch format {
	case copyRaw, copyHex, copyEscaped:
	default:
		format = copyEscaped
	}

	s.mu.RLock()
	ix := s.ix
	if ix == nil || ix.n == 0 {
		s.mu.RUnlock()
		return CopyResult{Err: "没有打开文件"}
	}
	fromIdx, fromOff, toIdx, toOff := s.copyRangeLocked(&req)
	if toIdx < fromIdx {
		s.mu.RUnlock()
		return CopyResult{Err: "选区为空"}
	}
	if toIdx-fromIdx+1 > maxCopyFrames {
		s.mu.RUnlock()
		return CopyResult{Err: fmt.Sprintf("选了 %d 帧，超过一次 %d 帧的上限", toIdx-fromIdx+1, maxCopyFrames)}
	}
	showYear := s.showYear
	var b strings.Builder
	frames, nbytes, nuls := 0, 0, 0
	for i := fromIdx; i <= toIdx; i++ {
		p := ix.arena[ix.aoff[i] : ix.aoff[i]+int32(ix.ln[i])]
		lo, hi := 0, len(p)
		if i == fromIdx {
			lo = clampInt(fromOff, 0, len(p))
		}
		if i == toIdx {
			hi = clampInt(toOff, 0, len(p))
			if i == fromIdx && hi < lo {
				lo, hi = hi, lo
			}
		}
		part := p[lo:hi]
		if req.WithTS || req.WithDir || req.WithLen {
			b.WriteString(copyPrefix(ix.ts[i], dirName(ix.dir[i]), len(p), &req, showYear))
		}
		b.WriteString(formatBytes(part, s.style, format, req.HexPrefix))
		b.WriteByte('\n')
		frames++
		nbytes += len(part)
		if format == copyRaw {
			// NUL 进了剪贴板，很多程序按 C 字符串读到它就直接结束 —— 表现为
			// 「粘贴出来只有前半截」，而且是静默的。原始模式下跳过它，并报个数。
			nuls += bytes.Count(part, []byte{0})
		}
	}
	text := b.String()
	out := CopyResult{Text: text, Frames: frames, Bytes: nbytes, Chars: len(text), Nul: nuls}
	s.mu.RUnlock()

	out.Ms = time.Since(t0).Milliseconds()
	if s.clip != nil {
		// 走宿主给的剪贴板（主工程是 Go 侧的 app.Clipboard），
		// 不依赖 navigator.clipboard：WebView2 里那个还要安全上下文和用户手势。
		s.clip(text)
	}
	return out
}

// copyPrefix 是每帧行首那截：时间戳 / 方向 / 长度，各自可关。
func copyPrefix(ts int64, dir string, size int, req *CopyReq, showYear bool) string {
	var parts []string
	if req.WithTS {
		t := time.UnixMilli(ts)
		if req.TSFull || showYear {
			parts = append(parts, "["+t.Format("2006-01-02 15:04:05.000")+"]")
		} else {
			parts = append(parts, "["+t.Format("01-02 15:04:05.000")+"]")
		}
	}
	if req.WithDir {
		switch dir {
		case "rx":
			parts = append(parts, "Rx ⇃")
		case "tx":
			parts = append(parts, "Tx ↾")
		default:
			parts = append(parts, "Sys")
		}
	}
	if req.WithLen {
		parts = append(parts, fmt.Sprintf("~%dB", size))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

// formatBytes 按格式输出一段字节。三种口径都在这里，复制到哪儿都一样。
// hexPrefix 由请求决定（复制时可能和显示不一样），其余跟着文档的样式走。
func formatBytes(p []byte, st Style, format string, hexPrefix bool) string {
	if len(p) == 0 {
		return ""
	}
	switch format {
	case copyHex:
		st.HexPrefix = hexPrefix
		return formatHexLines(p, len(p), st)
	case copyRaw:
		return decodeForCopy(p, st.Enc, true)
	default:
		return decodeForCopy(p, st.Enc, false)
	}
}

// decodeForCopy 借主工程的 decode.Decode 摊成文本，保证字符与界面上看到的一致。
// raw=true 时非法字节按 Latin-1 兜底（一个字节一个码位，不丢字节）；
// raw=false 时按显示的样子输出 /XX。
func decodeForCopy(p []byte, enc uint8, raw bool) string {
	segs := decode.Decode(hex.EncodeToString(p), encName(enc))
	var b strings.Builder
	b.Grow(len(p) + 8)
	for _, s := range segs {
		switch s.Type {
		case decode.SegText:
			b.WriteString(s.Value)
		case decode.SegHex:
			if raw {
				if s.Value == "00" {
					continue // NUL 会把剪贴板里的字符串截断，见 CopySelection
				}
				var v byte
				fmt.Sscanf(s.Value, "%02X", &v)
				b.WriteRune(rune(v)) // Latin-1：0xFF → ÿ
			} else {
				b.WriteByte('/')
				b.WriteString(s.Value)
			}
		case decode.SegCR:
			b.WriteByte('\r')
		case decode.SegLF:
			b.WriteByte('\n')
		case decode.SegCRLF:
			b.WriteString("\r\n")
		case decode.SegTab:
			b.WriteByte('\t')
		}
	}
	return b.String()
}
