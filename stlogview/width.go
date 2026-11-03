package stlogview

// 显示格子数：把一段字节换算成「主工程渲染出来会占几格」。
//
// 为什么要有这一层：窗口不滚动，所以「第 N 条在第几像素」必须自己算。文本模式下
// 一帧要折行，折几行取决于它占几格 —— 而它占几格又取决于主工程怎么渲染
// （空格画成 `·`、控制字节画成 `/XX`、CR/LF 真的换行）。所以这个文件是
// frontend/decoder.js 的 renderSegments 与 decode.Decode 的**度量版本**：
// 只数格子，不建字符串、不建 span。
//
// 三条硬对齐：
//   - 空格 → `·`                    1 格
//   - CR/LF/CRLF → `←` / `↵` / `←↵` 并换行
//   - 非法字节 → `/XX`              3 格
//   - 制表符 → `→` 加上真实的制表位推进（CSS tab-size 默认 8）
//
// 硬换行会把一帧切成若干「逻辑行」，每段单独折行 —— 这正是浏览器
// `white-space: pre-wrap` 干的事，所以高度能算准。
//
// Style 是「怎么看」里**会改变宽度**的那几项。宽度模型必须跟着它走，否则
// 折行数与实际渲染对不上、行高就飘：用户把控制字符关掉（CR 不占格）、把转义
// 字节设成隐藏或原始码位、把 Hex 改成逗号/无分隔/小写，都会改宽度。

const tabSize = 8

// Style 是宽度模型要用到的显示选项。默认值见 DefaultStyle。
type Style struct {
	Enc       uint8 `json:"enc"`       // 0=utf-8 1=ascii 2=gb2312（与 decode 三档一致）
	CRVisible bool  `json:"crVisible"` // 控制字符可视化：CR/CRLF 画成 ← / ←↵
	EscMode   uint8 `json:"escMode"`   // 非法字节：0=show(/XX) 1=hide 2=raw(一个码位)
	HexPrefix bool  `json:"hexPrefix"` // 0x 前缀
	HexSep    uint8 `json:"hexSep"`    // 0=空格 1=逗号 2=无分隔
	HexUpper  bool  `json:"hexUpper"`
}

// DefaultStyle 是主工程的默认显示：控制字符可见、转义字节按 /XX、Hex 大写空格分隔。
func DefaultStyle(enc uint8) Style {
	return Style{Enc: enc, CRVisible: true, EscMode: 0, HexPrefix: false, HexSep: 0, HexUpper: true}
}

// hexSepLen 是一个字节后面的分隔符占几格。
func (st Style) hexSepLen() int {
	if st.HexSep == 2 {
		return 0
	}
	return 1
}

// hexPerByte 是一个字节在 Hex 视图里占几格（"AA " = 3、逗号也是 3、无分隔 2、
// 带 0x 前缀各 +2）。
func (st Style) hexPerByte() int {
	per := 2 + st.hexSepLen()
	if st.HexPrefix {
		per += 2
	}
	return per
}

// cellsOf 把 payload 切成若干逻辑行的格子数，结果追加到 out 里返回。
func cellsOf(p []byte, st Style, out []uint32) []uint32 {
	start := len(out)
	var cells uint32
	var col int // 逻辑行内的当前列，制表位要用
	for i := 0; i < len(p); {
		c, size, brk := unitAt(p[i:], st, col)
		cells += uint32(c)
		col += c
		i += size
		if brk != 0 {
			out = append(out, cells)
			cells = 0
			col = 0
		}
	}
	if cells > 0 || len(out) == start {
		out = append(out, cells)
	}
	return out
}

// unitAt 是「一个显示单元」的唯一口径：占几格、吃几个字节、是不是这一行的结尾。
// 复制的格→字节换算（copy.go 的 byteAtPos）也走它，所以两边永远不会各算各的。
//
// col 是这个单元在**逻辑行内**的起始列，只有制表符用得上：制表位是绝对列，
// 不是「往后 8 格」。
func unitAt(p []byte, st Style, col int) (cells, size, brk int) {
	b := p[0]
	if b == 0x0D {
		// 单个 CR **不换行**。实测（headless Edge）：`AA<span>\r</span>BB` 是 1 行 22.5px，
		// 换成 `\n` 才是 45px。浏览器把裸 CR 当零宽字符吃掉了，只有 LF 会换行。
		// 关掉「可见控制字符」时连 `←` 都不画，CR 占 0 格。
		if !st.CRVisible {
			if len(p) >= 2 && p[1] == 0x0A {
				return 1, 2, 1 // 只剩 ↵
			}
			return 0, 1, 0
		}
		if len(p) >= 2 && p[1] == 0x0A {
			return 2, 2, 1 // ←↵
		}
		return 1, 1, 0 // ←
	}
	if b == 0x0A {
		return 1, 1, 1 // ↵
	}
	if b == 0x09 {
		if !st.CRVisible {
			// 不画 `→`，但制表位照样推进（浏览器里那个 \t 还在，只是不可见）
			n := col
			n += tabSize - n%tabSize
			return n - col, 1, 0
		}
		n := col + 1 // 先吃掉 `→` 那一格
		if n%tabSize == 0 {
			n += tabSize
		} else {
			n += tabSize - n%tabSize
		}
		return n - col, 1, 0
	}
	if b < 0x20 || b == 0x7F || (st.Enc == 1 && b >= 0x7F) {
		return escCells(st), 1, 0
	}
	if b < 0x80 {
		return 1, 1, 0
	}
	c, size := wideUnit(p, st.Enc)
	return int(c), size, 0
}

// escCells 是一个非法字节画出来占几格：/XX 三格、隐藏零格、原始码位一格。
func escCells(st Style) int {
	switch st.EscMode {
	case 1:
		return 0
	case 2:
		return 1
	}
	return 3
}

// wideUnit 处理 >=0x80 的字节：合法序列按字符宽度算，非法的只吃 1 字节、算 /XX。
func wideUnit(p []byte, enc uint8) (cells uint32, size int) {
	b := p[0]
	if enc == 2 { // GBK：首字节 0x81–0xFE + 合法尾字节 = 一个全角字符
		if b >= 0x81 && b <= 0xFE && len(p) >= 2 {
			b2 := p[1]
			if (b2 >= 0x40 && b2 <= 0x7E) || (b2 >= 0x80 && b2 <= 0xFE) {
				return 2, 2
			}
		}
		return 3, 1
	}
	// UTF-8 容错解码：区间判断与 decode.decodeUTF8Tolerant 逐条一致。
	switch {
	case b >= 0xC2 && b <= 0xDF && len(p) >= 2:
		if p[1]&0xC0 == 0x80 {
			return uint32(runeCells(rune(b&0x1F)<<6 | rune(p[1]&0x3F))), 2
		}
	case b >= 0xE0 && b <= 0xEF && len(p) >= 3:
		b2, b3 := p[1], p[2]
		if b2&0xC0 == 0x80 && b3&0xC0 == 0x80 &&
			!(b == 0xE0 && b2 < 0xA0) && !(b == 0xED && b2 > 0x9F) {
			r := rune(b&0x0F)<<12 | rune(b2&0x3F)<<6 | rune(b3&0x3F)
			return uint32(runeCells(r)), 3
		}
	case b >= 0xF0 && b <= 0xF4 && len(p) >= 4:
		b2, b3, b4 := p[1], p[2], p[3]
		if b2&0xC0 == 0x80 && b3&0xC0 == 0x80 && b4&0xC0 == 0x80 &&
			!(b == 0xF0 && b2 < 0x90) && !(b == 0xF4 && b2 > 0x8F) {
			r := rune(b&0x07)<<18 | rune(b2&0x3F)<<12 | rune(b3&0x3F)<<6 | rune(b4&0x3F)
			return uint32(runeCells(r)), 4
		}
	}
	return 3, 1 // 非法字节：/XX
}

// runeCells 是等宽字体下一个字符占几格：CJK / 全角 = 2，其余 = 1。
func runeCells(r rune) int {
	switch {
	case r < 0x1100:
		return 1
	case r <= 0x115F, // Hangul Jamo 初声
		r == 0x2329, r == 0x232A,
		r >= 0x2E80 && r <= 0x303E, // CJK 部首、标点
		r >= 0x3041 && r <= 0x33FF, // 假名、CJK 兼容
		r >= 0x3400 && r <= 0x4DBF, // CJK 扩展 A
		r >= 0x4E00 && r <= 0x9FFF, // CJK 基本区
		r >= 0xA000 && r <= 0xA4CF, // 彝文
		r >= 0xA960 && r <= 0xA97F,
		r >= 0xAC00 && r <= 0xD7A3, // 谚文音节
		r >= 0xF900 && r <= 0xFAFF, // CJK 兼容表意
		r >= 0xFE10 && r <= 0xFE19,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF00 && r <= 0xFF60, // 全角
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1F64F, // emoji
		r >= 0x1F900 && r <= 0x1F9FF,
		r >= 0x20000 && r <= 0x3FFFD: // CJK 扩展 B 及以上
		return 2
	}
	return 1
}
