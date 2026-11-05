package stlogview

import (
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nienieai/serial-debugger/decode"
	"github.com/nienieai/serial-debugger/sessionfile"
)

// 这个包是「虚窗 + 泳道滚动条」的文档侧：把 .stlog 扫成索引，按像素窗口吐行，
// 再给出滚动条三泳道要的桶。
//
// 与主工程前端的分工：
//   - 行高（第 N 条在第几像素）由这里算，前端只做 translate3d，不量 DOM；
//   - 划选的格 → 字节换算也在这里（copy.go），前端只报「落在第几行第几格」；
//   - 滚动条按帧号均分成桶，前端只画矩形，不逐帧遍历。
//
// 为什么必须自己扫：sessionfile.ReadFile 会把每一帧的 payload 都物化成切片，
// 50 MB 的文件 ≈ 120 万帧，光切片头就 60 MB。这里改成 arena + 定长字段
// （见 index.go）。

const (
	minChars = 16
	maxChars = 400
	minLine  = 12
	maxLine  = 48

	// 行高一律按「半像素」存：.data-line 是 15px × 1.5 = 22.5px，取整会让
	// 十几万行累出几百像素的偏差。前端量出来 ×2 上报，这里全程整数。
	rowGap2 = 4 // .data-row 的 margin-bottom: 2px
)

// Store 是暴露给前端的那一个服务（stlogview.Store.<Method>）。
//
// 一个 Store 对应一个文档（一个标签页）。文件是只读的：Open 之后后台扫描，
// 扫完 phase 变 ready；实时会话的文档在阶段 2 接进来（同一个窗口/桶/复制模型）。
type Store struct {
	mu sync.RWMutex

	path string
	ix   *index

	phase    string // idle | scan | ready | error
	scanErr  string
	scanMs   int64
	scanDone int64

	// 布局
	viewportH    int
	mode         string
	dirMode      [5]string // 按方向覆盖 mode（索引 = sessionfile 方向号）；空串 = 沿用 mode
	charsPerLine int
	perLine      int
	tsLineH2     int
	dataLineH2   int
	showYear     bool
	style        Style
	prefix       []int64 // 半像素
	totalH2      int64

	// 滚动条的桶（按帧号均分）
	ruler       []Bucket
	rulerNB     int
	rulerOrigNB int
	rulerN      int
	stopFlag    chan struct{}

	// 落盘状态（第三条泳道）。落盘是**会话级**开关：开着的时候新进来的帧才写进
	// 会话文件，中途来回关就会在同一条泳道里出现「有的落、有的没落」。
	// 只记切换点（逻辑帧号 + 当时的状态）就够，不用给每帧存一位：
	//   diskInit = 第一个切换点之前的状态（静态文件全为 true，实时文档默认 false）
	//   diskSegs = 之后每次切换的位置，按帧号升序
	diskInit bool
	diskSegs []diskSeg

	// 宿主注入的两个钩子：一个写剪贴板，一个取原生窗口句柄（切标题栏明暗）。
	// 测试里都留 nil，包本身不依赖 Wails。
	clip func(string)
	hwnd func() uintptr

	// 实时文档（见 live.go）：帧一帧一帧进来，超上限从头部裁。
	live     bool
	absBase  int   // 已经被裁掉的帧数（绝对帧号 = absBase + 下标）
	maxBytes int64 // arena 上限
	// trimmedH2 是从头部裁掉的内容总高度（半像素）。前端靠它把视口锚在用户
	// 正在看的那几帧上：裁掉多少像素，offset 就减多少，画面才不会往下滑。
	trimmedH2 int64
}

// NewStore 造一个空文档。布局默认按 15px 字体、120 格宽、Hex 模式。
func NewStore() *Store {
	return &Store{
		phase:        "idle",
		mode:         "hex",
		charsPerLine: 120,
		tsLineH2:     42, // 15px × 1.4 = 21px
		dataLineH2:   45, // 15px × 1.5 = 22.5px
		showYear:     true,
		style:        DefaultStyle(0),
	}
}

// SetClipboard 注入「把文本写进系统剪贴板」的实现（主工程是 app.Clipboard）。
func (s *Store) SetClipboard(fn func(string)) {
	s.mu.Lock()
	s.clip = fn
	s.mu.Unlock()
}

// SetWindowHandle 注入「当前窗口的原生句柄」，切标题栏明暗用。
func (s *Store) SetWindowHandle(fn func() uintptr) {
	s.mu.Lock()
	s.hwnd = fn
	s.mu.Unlock()
}

// MetaResult 是打开后一次性的元信息。
type MetaResult struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Pages   int    `json:"pages"`
	Records int    `json:"records"`
	Frames  int    `json:"frames"`
	RxBytes int64  `json:"rxBytes"`
	TxBytes int64  `json:"txBytes"`
	Phase   string `json:"phase"`
	ScanMs  int64  `json:"scanMs"`

	StoppedAt int64    `json:"stoppedAt"`
	StopErr   string   `json:"stopErr"`
	Truncated bool     `json:"truncated"`
	Broken    bool     `json:"broken"`
	NoHead    bool     `json:"noHead"`
	Recovered []int64  `json:"recovered"`
	Origins   []string `json:"origins"`

	Port    string `json:"port"`
	PortB   string `json:"portB"`
	Baud    int    `json:"baud"`
	Created string `json:"created"`
	UUID    string `json:"uuid"`
	Label   string `json:"label"`
	Enc     string `json:"enc"`

	// Mode 是录制模式（single / forward）；View 是当前布局（hex / text）。
	// 两个「模式」是两件事，别混。
	Mode         string `json:"mode"`
	FirstTs      int64  `json:"firstTs"`
	LastTs       int64  `json:"lastTs"`
	P1Bytes      int64  `json:"p1Bytes"`
	P2Bytes      int64  `json:"p2Bytes"`
	View         string `json:"view"`
	CharsPerLine int    `json:"charsPerLine"`
	PerLine      int    `json:"perLine"`
	TsLineH2     int    `json:"tsLineH2"`
	DataLineH2   int    `json:"dataLineH2"`
	HexPrefix    bool   `json:"hexPrefix"`
	ShowYear     bool   `json:"showYear"`
}

// StatusResult 是扫描进度（前端每 150 ms 轮一次）。
type StatusResult struct {
	Phase     string `json:"phase"`
	Frames    int    `json:"frames"`
	Done      int64  `json:"done"`
	Size      int64  `json:"size"`
	ScanMs    int64  `json:"scanMs"`
	Err       string `json:"err"`
	StopErr   string `json:"stopErr"`
	StoppedAt int64  `json:"stoppedAt"`
}

// ConfigureResult 是改宽度/行高/模式之后的新几何。
type ConfigureResult struct {
	Mode         string  `json:"mode"`
	CharsPerLine int     `json:"charsPerLine"`
	PerLine      int     `json:"perLine"`
	TsLineH2     int     `json:"tsLineH2"`
	DataLineH2   int     `json:"dataLineH2"`
	HexPrefix    bool    `json:"hexPrefix"`
	ContentH     float64 `json:"contentHeight"`
	MaxOffset    float64 `json:"maxOffset"`
	AnchorY      float64 `json:"anchorY"`
	RowCount     int     `json:"rowCount"`
}

// Row 是窗口里的一行（一帧）。Hex 与 Segs 只给可见窗口，不在索引里常驻。
type Row struct {
	Idx    int              `json:"idx"`
	Y      float64          `json:"y"`
	H      float64          `json:"h"`
	Ts     int64            `json:"ts"`
	Dir    string           `json:"dir"`
	Origin string           `json:"origin"`
	Size   int              `json:"size"`
	Hex    string           `json:"hex,omitempty"`
	Segs   []decode.Segment `json:"segs,omitempty"`
	Cells  []uint32         `json:"cells,omitempty"` // 每个逻辑行占几格（自绘选区要按行算宽度）
}

type WindowResult struct {
	Rows          []Row   `json:"rows"`
	Base          float64 `json:"base"`
	ContentHeight float64 `json:"contentHeight"`
	MaxOffset     float64 `json:"maxOffset"`
	VisibleFrom   int     `json:"visibleFrom"`
	VisibleTo     int     `json:"visibleTo"`
	RenderFrom    int     `json:"renderFrom"`
	RenderTo      int     `json:"renderTo"`
	Total         int     `json:"total"`
	ContentY      int     `json:"contentY"`
	ViewportH     int     `json:"viewportH"`
	OverscanPx    int     `json:"overscanPx"`
	FetchUs       int     `json:"fetchUs"`
	Mode          string  `json:"mode"`
	PerLine       int     `json:"perLine"`
	HexPrefix     bool    `json:"hexPrefix"`
}

// Bucket 是滚动条上的一格，纵向按帧号均分。
//
// 只有「来源」这一道是真实数据（.stlog 里 tx 记录带 origin）；「落盘」道是
// 文件本身的完整性（缺头/断链/末帧未闭合），见 Ruler 的 DiskLine。
// 「读取」道（MCP / 非交互 CLI 读到哪）在格式里还没有这一维，等工程文件补上
// 读取记录之后再加，见 TODO.md 的「会话『读取』记录」。
type Bucket struct {
	Orig uint8 `json:"orig"` // bit i = 该段里出现过第 i 个来源（1 起，见 Origins）
	Lead uint8 `json:"lead"` // 这一段里发得最多的那个来源（0 = 全是 rx，没人发）
	// Disk 是这一段的落盘情况：bit0 = 有已落盘的帧，bit1 = 有没落盘的帧。
	// 前端据此上色：1 绿、2 透明、3 黄（一段里两种都有）。
	Disk uint8 `json:"disk"`
}

// diskSeg 是落盘状态的一次切换：从逻辑帧号 At 起，状态变成 On。
type diskSeg struct {
	At int  `json:"at"`
	On bool `json:"on"`
}

type RulerResult struct {
	Buckets  []Bucket  `json:"buckets"`
	Origins  []string  `json:"origins"`
	Total    int       `json:"total"`
	DiskLine []float64 `json:"diskLine"` // 数据不完整的帧，位置 0..1，画成一根线
	DiskOK   bool      `json:"diskOk"`
}

// Open 打开一个 .stlog：立即返回，扫描在后台 goroutine 里跑。
//
// 打开新文件会先掐掉上一个文件的扫描（同一个 Store 只承载一个文档）。
func (s *Store) Open(path string) StatusResult {
	s.mu.Lock()
	if s.stopFlag != nil {
		close(s.stopFlag)
		s.stopFlag = nil
	}
	s.path = path
	s.live = false
	s.absBase = 0
	s.trimmedH2 = 0
	s.ix = nil
	s.phase = "scan"
	s.scanErr = ""
	s.scanMs = 0
	s.scanDone = 0
	s.prefix = nil
	s.totalH2 = 0
	s.ruler = nil
	s.rulerUpToResetLocked()
	// 静态文件：里面的帧全都在磁盘上（不完整的那几帧由 DiskLine 单独标）。
	s.diskInit = true
	s.diskSegs = nil
	stop := make(chan struct{})
	s.stopFlag = stop
	s.mu.Unlock()

	go func() {
		t0 := time.Now()
		ix, err := scanStlog(path, func(frames int, done int64) {
			s.mu.Lock()
			s.scanDone = done
			s.mu.Unlock()
		})
		if err != nil {
			s.mu.Lock()
			s.phase = "error"
			s.scanErr = err.Error()
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		select {
		case <-stop: // 期间又 Open 了别的文件：这份索引丢掉
			s.mu.Unlock()
			return
		default:
		}
		s.ix = ix
		// 默认显示样式取文件头里的编码；其余取主工程默认值（前端 Configure 会覆盖）。
		s.style = DefaultStyle(ix.encoding())
		s.scanMs = time.Since(t0).Milliseconds()
		s.scanDone = ix.size
		s.phase = "ready"
		s.buildLayoutLocked()
		s.ruler = nil
		s.rulerUpToResetLocked()
		s.mu.Unlock()
	}()
	return s.Status()
}

// rulerUpToResetLocked 让下次 Ruler 全量重算桶（换了文件/帧数变了）。
func (s *Store) rulerUpToResetLocked() {
	s.rulerNB = 0
	s.rulerOrigNB = 0
	s.rulerN = 0
}

// Close 停掉后台扫描并丢掉索引。
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopFlag != nil {
		close(s.stopFlag)
		s.stopFlag = nil
	}
	s.path = ""
	s.ix = nil
	s.phase = "idle"
	s.prefix = nil
	s.totalH2 = 0
	s.trimmedH2 = 0
	s.ruler = nil
	s.rulerUpToResetLocked()
}

func (s *Store) Status() StatusResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := StatusResult{
		Phase:  s.phase,
		Done:   s.scanDone,
		ScanMs: s.scanMs,
		Err:    s.scanErr,
	}
	if s.ix != nil {
		out.Frames = s.absBase + s.ix.n
		out.Size = s.ix.size
		out.StoppedAt = s.ix.stoppedAt
		if s.ix.stopErr != nil {
			out.StopErr = s.ix.stopErr.Error()
		}
	}
	return out
}

func (s *Store) Meta() MetaResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := MetaResult{
		Path:         s.path,
		Phase:        s.phase,
		ScanMs:       s.scanMs,
		View:         s.mode,
		CharsPerLine: s.charsPerLine,
		PerLine:      s.perLine,
		TsLineH2:     s.tsLineH2,
		DataLineH2:   s.dataLineH2,
		HexPrefix:    s.style.HexPrefix,
		ShowYear:     s.showYear,
	}
	if i := strings.LastIndexAny(s.path, `\/`); i >= 0 {
		out.Name = s.path[i+1:]
	} else {
		out.Name = s.path
	}
	ix := s.ix
	if ix == nil {
		return out
	}
	out.Size = ix.size
	out.Pages = ix.pages
	out.Records = ix.records
	out.Frames = s.absBase + ix.n
	out.RxBytes = ix.rxBytes
	out.TxBytes = ix.txBytes
	out.StoppedAt = ix.stoppedAt
	out.Truncated = ix.truncated
	out.Broken = ix.broken
	out.NoHead = ix.noHead
	out.Recovered = ix.recovered
	out.Origins = ix.orgNames
	out.Enc = "utf-8"
	out.FirstTs = ix.firstTs
	out.LastTs = ix.lastTs
	out.P1Bytes = ix.p1Bytes
	out.P2Bytes = ix.p2Bytes
	out.Mode = "single"
	if ix.stopErr != nil {
		out.StopErr = ix.stopErr.Error()
	}
	if st := ix.settings; st != nil {
		out.Port = st.Port
		out.PortB = st.PortB
		out.Baud = st.Baud
		out.Created = st.Created
		out.UUID = st.UUID
		out.Label = st.Label
		if st.Mode == sessionfile.ModeForward {
			out.Mode = sessionfile.ModeForward
		}
		if e := st.Display["encoding"]; e != "" {
			out.Enc = e
		}
	}
	return out
}

// Configure 重算布局：改了宽度、行高、模式或显示样式（控制字符 / 转义字节 /
// Hex 分隔与前缀），都要重来一遍格子缓存与前缀和。
//
// dirModes 是按方向的模式覆盖（下标 = sessionfile 方向号，空串表示沿用 mode）：
// 转发页的 P1 / P2 各有独立的「字 / H」，一个全局 mode 表达不了。
func (s *Store) Configure(charsPerLine, tsLineH2, dataLineH2 int, mode string, dirModes []string, anchorIdx int, style Style) ConfigureResult {
	if mode != "hex" {
		mode = "text"
	}
	charsPerLine = clampInt(charsPerLine, minChars, maxChars)
	tsLineH2 = clampInt(tsLineH2, minLine*2, maxLine*2)
	dataLineH2 = clampInt(dataLineH2, minLine*2, maxLine*2)
	var dm [5]string
	for i := 0; i < len(dm) && i < len(dirModes); i++ {
		// 只认这两个值：空串 / 不认识的写法一律当「没覆盖」，回落到 mode。
		if v := dirModes[i]; v == "hex" || v == "text" {
			dm[i] = v
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	changed := mode != s.mode || dm != s.dirMode || charsPerLine != s.charsPerLine ||
		tsLineH2 != s.tsLineH2 || dataLineH2 != s.dataLineH2 ||
		style != s.style || s.prefix == nil
	s.mode, s.dirMode, s.charsPerLine, s.tsLineH2, s.dataLineH2, s.style = mode, dm, charsPerLine, tsLineH2, dataLineH2, style
	if changed {
		s.buildLayoutLocked()
	}
	out := ConfigureResult{
		Mode:         s.mode,
		CharsPerLine: s.charsPerLine,
		PerLine:      s.perLine,
		TsLineH2:     s.tsLineH2,
		DataLineH2:   s.dataLineH2,
		HexPrefix:    s.style.HexPrefix,
		ContentH:     float64(s.totalH2) / 2,
		MaxOffset:    s.maxOffsetLocked(),
	}
	if s.ix != nil {
		out.RowCount = s.absBase + s.ix.n
		if anchorIdx < 0 {
			anchorIdx = 0
		}
		if anchorIdx >= s.ix.n {
			anchorIdx = s.ix.n - 1
		}
		// anchorIdx 是逻辑下标；前缀和按物理下标存，要加上 head。
		if p := s.ix.head + anchorIdx; anchorIdx >= 0 && p+1 < len(s.prefix) {
			out.AnchorY = float64(s.prefix[p]-s.trimmedH2) / 2
		}
	}
	return out
}

// SetOpt 切年份这类只影响显示的开关（会改宽度的开关走 Configure 的 Style）。
func (s *Store) SetOpt(name string, on bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch name {
	case "year":
		s.showYear = on
	}
	return true
}

// SetTheme 把原生标题栏切成跟应用主题一致（前端按 T 或系统偏好变化时调用）。
// 只碰 DWM 属性，不动文档状态，所以不拿锁。
func (s *Store) SetTheme(dark bool) bool {
	if s.hwnd == nil {
		return false
	}
	if hwnd := s.hwnd(); hwnd != 0 {
		return setTitlebarDark(hwnd, dark)
	}
	return false
}

// modeOf 返回某个方向实际用的显示模式（hex / text）。
//
// 转发页的 P1 / P2 各有独立的「字 / H」开关（老渲染按 dir 取 p1DisplayMode
// 之类），所以模式不能只有一个全局值：dirMode 里没写的方向沿用 mode。
func (s *Store) modeOf(dir uint8) string {
	if d := sessionfile.DirBase(dir); d < 5 && s.dirMode[d] != "" {
		return s.dirMode[d]
	}
	return s.mode
}

// perLineFor 是某个方向一行放几个单位：文本=格，Hex=字节（尽量用满宽度）。
func (s *Store) perLineFor(dir uint8) int {
	if s.modeOf(dir) == "hex" {
		return hexBytesPerLine(s.charsPerLine, s.style)
	}
	return s.charsPerLine
}

// perLineLocked 是**默认方向**的每行单位数（滚动条桶、没有方向的地方用）。
func (s *Store) perLineLocked() int { return s.perLineFor(sessionfile.DirRx) }

// hexBytesPerLine 按满宽折行：再塞一个字节就会超，不留 8 字节对齐的余量
// （对齐会白扔最多 7 个字节 ≈ 155 px，宽窗口下很明显）。
//
// 一行的字符数 = bpl*per - 分隔符长度（最后一个字节后面没有分隔符）。
func hexBytesPerLine(chars int, st Style) int {
	per := st.hexPerByte()
	slen := st.hexSepLen()
	bpl := (chars + slen) / per
	if bpl < 1 {
		bpl = 1
	}
	return bpl
}

func linesFor(units uint32, perLine int) int {
	if perLine < 1 {
		perLine = 80
	}
	if units == 0 {
		return 1
	}
	return int((units + uint32(perLine) - 1) / uint32(perLine))
}

// linesOfLocked 是一帧占几行：文本模式按逻辑行分别折行后相加。
// i 是物理下标（逻辑帧号 = absBase + i - head）。
func (s *Store) linesOfLocked(i int) int {
	ix := s.ix
	// 系统消息在前端固定画成一行（logview.js 的 buildRow 把时间、标签、正文并排放，
	// 且 lineCount 返回 1）。这里也按 1 行算，否则长消息会多留几行空白，滚动手感也跟着飘。
	if ix.dir[i] == sessionfile.DirSystem {
		return 1
	}
	per := s.perLineFor(ix.dir[i])
	if s.modeOf(ix.dir[i]) == "hex" {
		return linesFor(ix.ln[i], per)
	}
	lo := ix.segIdx[i]
	hi := ix.segIdx[i+1]
	n := 0
	for _, c := range ix.seg[lo:hi] {
		n += linesFor(c, per)
	}
	return n
}

// ensureSegLocked 保证格子缓存与当前样式一致。样式变了就整表重算：
// 格子数只依赖字节流与样式，重扫一遍 arena 比重建索引便宜得多。
//
// 重扫只覆盖逻辑帧，所以先把逻辑起点搬回物理 0。
func (s *Store) ensureSegLocked() {
	ix := s.ix
	if ix == nil || ix.n == 0 || ix.segKey == s.style {
		return
	}
	s.compactLocked()
	st := s.style
	buf := ix.seg[:0]
	segIdx := ix.segIdx[:0]
	for i := 0; i < ix.n; i++ {
		p := ix.arena[ix.aoff[i] : ix.aoff[i]+int32(ix.ln[i])]
		segIdx = append(segIdx, int32(len(buf)))
		buf = cellsOf(p, st, buf)
	}
	segIdx = append(segIdx, int32(len(buf)))
	ix.seg = buf
	ix.segIdx = segIdx
	// arena/aoff 也被 compact 搬过了，aoff 已经相对新起点
	ix.segKey = st
}

// buildLayoutLocked 重算行高前缀和。改宽度走的就是这里：只有除法加法，
// 不重新 decode、不建字符串。
func (s *Store) buildLayoutLocked() {
	s.ensureSegLocked()
	s.compactLocked()
	ix := s.ix
	if ix == nil || ix.n == 0 {
		// 一项也要留，而且第一项就是 trimmedH2：前缀和是绝对高度，
		// 全裁光之后再追加的帧要从「已经裁掉那段」的顶上接着算。
		s.prefix = []int64{s.trimmedH2}
		s.totalH2 = 0
		s.perLine = s.perLineLocked()
		if ix != nil {
			ix.head = 0
		}
		return
	}
	s.perLine = s.perLineLocked()
	n := ix.n
	if cap(s.prefix) < n+1 {
		s.prefix = make([]int64, n+1)
	}
	buf := s.prefix[:n+1]
	// 前缀和是绝对高度：第一项是 trimmedH2（已经裁掉的那一段），
	// 逻辑高度 = 末项 - 首项。
	y := s.trimmedH2
	buf[0] = y
	for i := 0; i < n; i++ {
		lines := s.linesOfLocked(i)
		y += int64(s.tsLineH2 + lines*s.dataLineH2 + rowGap2)
		buf[i+1] = y
	}
	s.prefix = buf
	s.totalH2 = y - s.trimmedH2
}

func (s *Store) maxOffsetLocked() float64 {
	m := float64(s.totalH2)/2 - float64(s.viewportH)
	if m < 0 {
		return 0
	}
	return m
}

// Window 按像素取窗口。contentY / viewportH / overscan 都是像素（整数），
// 内部一律换算成半像素再查前缀和。
func (s *Store) Window(contentY, viewportH, overscanPx int) WindowResult {
	t0 := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewportH = viewportH

	out := WindowResult{
		Mode:          s.mode,
		PerLine:       s.perLine,
		HexPrefix:     s.style.HexPrefix,
		ContentHeight: float64(s.totalH2) / 2,
		ViewportH:     viewportH,
		OverscanPx:    overscanPx,
		MaxOffset:     s.maxOffsetLocked(),
	}
	ix := s.ix
	if ix == nil || ix.n == 0 {
		out.Rows = []Row{}
		return out
	}
	if overscanPx < 0 {
		overscanPx = 0
	}
	if contentY < 0 {
		contentY = 0
	}
	if maxPx := int(out.MaxOffset); contentY > maxPx {
		contentY = maxPx
	}
	out.ContentY = contentY
	out.Total = s.absBase + ix.n

	cy2 := int64(contentY)*2 + s.trimmedH2
	vh2 := int64(viewportH) * 2
	ov2 := int64(overscanPx) * 2

	// 前缀和按物理下标存、是绝对高度；二分范围限定在逻辑帧 [head, head+n] 里。
	lo, hi := ix.head, ix.head+ix.n
	from := floorIndex(s.prefix, lo, hi, cy2-ov2)
	to := lowerBound(s.prefix, lo, hi, cy2+vh2+ov2)
	out.RenderFrom, out.RenderTo = s.absBase+from-ix.head, s.absBase+to-ix.head-1
	out.Base = float64(s.prefix[from]-s.trimmedH2) / 2

	// 可见带（不带 overscan）单独算一次给信息块用。
	out.VisibleFrom = s.absBase + lowerBound(s.prefix, lo, hi, cy2) - ix.head
	out.VisibleTo = s.absBase + lowerBound(s.prefix, lo, hi, cy2+vh2) - ix.head - 1

	rows := make([]Row, 0, to-from)
	for i := from; i < to; i++ {
		rows = append(rows, s.makeRowLocked(i))
	}
	out.Rows = rows
	out.FetchUs = int(time.Since(t0) / time.Microsecond)
	return out
}

// floorIndex 在 prefix[lo:hi] 里找最后一个 prefix[i] <= y 的 i（y 落在行中间时
// 取那一行本身）。prefix 是按物理下标存的绝对高度，lo/hi 由调用方夹在逻辑帧范围里。
//
// 窗口起点必须用它：用上界（lowerBound）会把 offset 所在的那一行整行跳过，
// Base 落到下一行，整层被往下推，顶部空出最多一整行高（帧很高时就是一整屏）。
func floorIndex(prefix []int64, lo, hi int, y int64) int {
	if lo >= hi {
		return lo
	}
	l, r := lo, hi-1
	for l < r {
		mid := (l + r + 1) / 2
		if prefix[mid] <= y {
			l = mid
		} else {
			r = mid - 1
		}
	}
	return l
}

// lowerBound 在 prefix[lo:hi] 里找第一个 prefix[i] >= y 的 i（prefix 单调不减）。
func lowerBound(prefix []int64, lo, hi int, y int64) int {
	l, r := lo, hi
	for l < r {
		mid := (l + r) / 2
		if prefix[mid] < y {
			l = mid + 1
		} else {
			r = mid
		}
	}
	return l
}

// makeRowLocked 造一行。i 是**物理下标**（逻辑帧号 = absBase + i - head）。
func (s *Store) makeRowLocked(i int) Row {
	ix := s.ix
	p := ix.arena[ix.aoff[i] : ix.aoff[i]+int32(ix.ln[i])]
	r := Row{
		Idx:  s.absBase + i - ix.head,
		Y:    float64(s.prefix[i]-s.trimmedH2) / 2,
		H:    float64(s.prefix[i+1]-s.prefix[i]) / 2,
		Ts:   ix.ts[i],
		Dir:  dirName(ix.dir[i]),
		Size: len(p),
	}
	if o := ix.org[i]; o > 0 && int(o) <= len(ix.orgNames) {
		r.Origin = ix.orgNames[o-1]
	}
	r.Cells = append(r.Cells, ix.seg[ix.segIdx[i]:ix.segIdx[i+1]]...)
	// 系统消息在界面上固定按文本渲染，Hex 串只是给前端解码回文本用的：
	// 不能带 0x 前缀 —— 前缀会被解码器当成两个十六进制字符，显示成一串点。
	//
	// 文本模式下也得填：前端的 system 行只认 r.Hex（见 logview.js 的 buildRow），
	// 之前只在 hex 模式下填，切到「字」之后系统消息就整行空白（只剩时间戳和字节数）。
	isSys := ix.dir[i] == sessionfile.DirSystem
	isHex := s.modeOf(ix.dir[i]) == "hex"
	if isHex || isSys {
		st := s.style
		if isSys {
			st.HexPrefix = false
		}
		r.Hex = formatHexLines(p, s.perLineFor(ix.dir[i]), st)
	}
	if !isHex {
		r.Segs = decode.Decode(hex.EncodeToString(p), encName(s.style.Enc))
	}
	return r
}

func encName(e uint8) string {
	switch e {
	case 1:
		return "ascii"
	case 2:
		return "gb2312"
	}
	return "utf-8"
}

// dirName 与前端 history.js 的 stlogDirName 同一套口径（会话页的 CSS 类按它取色）。
func dirName(d uint8) string {
	switch d {
	case sessionfile.DirRx:
		return "rx"
	case sessionfile.DirTx:
		return "tx"
	case sessionfile.DirP1:
		return "1"
	case sessionfile.DirP2:
		return "2"
	}
	return "system"
}

// formatHexLines 把一段字节排成 bpl 字节一行的十六进制，行间用 \n 分隔。
// 与主工程的 formatHexString 同一套规则（大小写 / 分隔符 / 可选 0x 前缀），
// 区别只在先把行折好 —— 主工程是 break-all 乱折，这里按整字节折。
func formatHexLines(p []byte, bpl int, st Style) string {
	if len(p) == 0 {
		return ""
	}
	if bpl < 1 {
		bpl = 1
	}
	const up = "0123456789ABCDEF"
	const low = "0123456789abcdef"
	digits := up
	if !st.HexUpper {
		digits = low
	}
	var sep string
	switch st.HexSep {
	case 1:
		sep = ","
	case 2:
		sep = ""
	default:
		sep = " "
	}
	per := st.hexPerByte()
	var sb strings.Builder
	sb.Grow(len(p) * (per + 1))
	for i := 0; i < len(p); i += bpl {
		if i > 0 {
			sb.WriteByte('\n')
		}
		end := i + bpl
		if end > len(p) {
			end = len(p)
		}
		for j := i; j < end; j++ {
			if j > i {
				sb.WriteString(sep)
			}
			if st.HexPrefix {
				sb.WriteString("0x")
			}
			sb.WriteByte(digits[p[j]>>4])
			sb.WriteByte(digits[p[j]&0x0F])
		}
	}
	return sb.String()
}

// Ruler 给滚动条两道：来源道按桶统计 origin，落盘道报文件完整性。
func (s *Store) Ruler(nb int) RulerResult {
	nb = clampInt(nb, 32, 4000)
	s.mu.Lock()
	defer s.mu.Unlock()
	ix := s.ix
	out := RulerResult{}
	if ix == nil || ix.n == 0 {
		out.Buckets = []Bucket{}
		return out
	}
	out.Origins = ix.orgNames
	if s.rulerNB != nb || len(s.ruler) != nb || s.rulerN != ix.n {
		s.ruler = make([]Bucket, nb)
		s.rulerNB = nb
		s.rulerOrigNB = 0
	}
	out.Total = ix.n

	// 来源道与校验无关：帧头一落盘就能算，不用等任何扫描。
	// 只在桶数或帧数变化时重算一次。
	if s.rulerOrigNB != nb {
		for b := 0; b < nb; b++ {
			lo, hi := b*ix.n/nb, (b+1)*ix.n/nb
			var orgN [8]int
			var mask uint8
			var disk uint8
			for i := lo; i < hi; i++ {
				if o := ix.org[ix.head+i]; o > 0 {
					orgN[o]++
					mask |= 1 << (o - 1)
				}
				// 落盘状态按逻辑帧号查切换点：这一段里两种都有就两个位都置上。
				if s.diskAtLocked(s.absBase + i) {
					disk |= 1
				} else {
					disk |= 2
				}
			}
			lead := 0
			for o := 1; o < len(orgN); o++ {
				if orgN[o] > orgN[lead] {
					lead = o
				}
			}
			s.ruler[b].Orig = mask
			s.ruler[b].Lead = uint8(lead)
			s.ruler[b].Disk = disk
		}
		s.rulerOrigNB = nb
		s.rulerN = ix.n
	}
	out.Buckets = s.ruler

	// 落盘道：数据不全的帧（缺头 / 续片链断 / 末帧未闭合）画成一根线。
	if ix.noHead && ix.n > 0 {
		out.DiskLine = append(out.DiskLine, 0)
	}
	if ix.broken {
		out.DiskLine = append(out.DiskLine, 0.5)
	}
	if ix.truncated && ix.n > 0 {
		out.DiskLine = append(out.DiskLine, float64(ix.n-1)/float64(ix.n))
	}
	out.DiskOK = ix.stoppedAt == 0
	return out
}

// FrameDebug 返回某一帧的原始字节与 Go 侧算出来的格子/行数，最长 96 个字符。
// 只给「行高对账」定位问题用：一眼看出是模型算错了，还是前端量错了。
func (s *Store) FrameDebug(idx int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ix := s.ix
	i := idx - s.absBase
	if ix == nil || i < 0 || i >= ix.n {
		return ""
	}
	i += ix.head // 逻辑下标 → 物理下标
	p := ix.arena[ix.aoff[i] : ix.aoff[i]+int32(ix.ln[i])]
	segs := ix.seg[ix.segIdx[i]:ix.segIdx[i+1]]
	fresh := cellsOf(p, s.style, nil)
	out := formatHexLines(p, len(p), s.style)
	if len(out) > 64 {
		out = out[:64] + "…"
	}
	return fmt.Sprintf("存%v 现%v len=%d enc=%d · %s", segs, fresh, len(p), s.style.Enc, out)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
