package stlogview

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// index 是整份 .stlog 的只读索引。
//
// 为什么不用 sessionfile.ReadFile：它把所有帧的 payload 都物化成 []Frame，
// 50 MB 的文件 ≈ 120 万帧，光切片头就要 60 MB，还要按帧各分配一次。这里改成
// 一边扫页一边往 arena 里追加，帧只留定长字段：
//
//	ts / aoff / ln / dir / org  ——  每条 8+4+4+1+1 = 18 B
//	segIdx / seg                ——  折行用的格子数（见 width.go）
//
// 帧在文件里是流式的、没有独立偏移可查，所以 arena 是唯一的原始字节来源；
// 索引建完就能把文件句柄关掉 —— 之后只读 arena。
type index struct {
	path     string
	size     int64
	settings *sessionfile.Settings

	pages     int
	records   int
	stoppedAt int64 // 第一个不可用页的偏移（0 = 没有）
	stopErr   error
	recovered []int64
	truncated bool // 末帧没闭合
	broken    bool // 续片链在页边界被切断
	noHead    bool // 首页不是从帧首开始，第一帧缺头

	// 帧表
	//
	// 帧表是「物理数组 + 逻辑起点」：head 之前的那些帧已经被裁掉，但仍然留在
	// 数组里没搬走 —— 裁头只把 head 往前挪一帧，是 O(1) 的。逻辑第 i 帧在
	// 物理下标 head+i 上。head 攒大了由 compactLocked 一次性搬回去（摊销 O(1)）。
	//
	// 为什么要这样：实时文档按守护进程的环形缓冲语义裁头（超一帧丢一帧），
	// 每帧都触发一次；要是每次都把 5 MB 的 arena 和整张前缀和重算一遍，
	// 高吞吐时这一项就能吃掉一成 CPU。
	n    int
	head int
	ts   []int64
	aoff []int32
	ln   []uint32
	dir  []uint8
	org  []uint8

	orgNames []string // org 字段是下标，0 = 无来源

	// 折行：第 i 帧的逻辑行格子数 = seg[segIdx[i]:segIdx[i+1]]（segIdx 有 n+1 项）
	//
	// 格子数依赖显示样式（控制字符、转义字节、Hex 分隔符都会改宽度），所以带一个
	// segKey：Configure 传入的样式与它不同就整表重算（见 Store.ensureSegLocked）。
	segIdx []int32
	seg    []uint32
	segKey Style

	arena []byte

	// 统计：文件浮窗要显示时间范围与双向字节数（转发录制 A→B / B→A）。
	firstTs, lastTs  int64
	rxBytes, txBytes int64
	p1Bytes, p2Bytes int64
}

// scanner 是扫描期的可变状态（不放进 index，免得索引里躺着一堆临时字段）。
type scanner struct {
	ix     *index
	cells  []uint32
	open   bool
	openAt int32
	openTs int64
	openD  uint8
	openO  uint8
}

func scanStlog(path string, progress func(frames int, done int64)) (*index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < sessionfile.HeaderSize {
		return nil, fmt.Errorf("文件只有 %d B，不够一个 .stlog 文件头", st.Size())
	}

	ix := &index{
		path:  path,
		size:  st.Size(),
		arena: make([]byte, 0, st.Size()),
	}
	if err := readSettingsInto(f, ix); err != nil {
		return nil, err
	}

	sc := &scanner{ix: ix}
	buf := make([]byte, sessionfile.PageSize)
	off := int64(sessionfile.RecordsOffset)
	prevOpen := false
	pageNo := 0

	for off < st.Size() {
		torn := off+sessionfile.PageSize > st.Size()
		var p *sessionfile.Page

		if torn {
			// 尾页不完整：只有影子页里还留着一份完整拷贝才救得回来。
			rp, ok := recoverAt(f, off)
			if !ok {
				ix.stoppedAt = off
				ix.stopErr = sessionfile.ErrPartialTail
				break
			}
			p = rp
			ix.recovered = append(ix.recovered, off)
		} else {
			if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
				return nil, err
			}
			pp, derr := sessionfile.DecodePage(buf, uint64(off))
			if derr != nil {
				rp, ok := recoverAt(f, off)
				if !ok {
					ix.stoppedAt = off
					ix.stopErr = derr
					break
				}
				pp = rp
				ix.recovered = append(ix.recovered, off)
			}
			p = pp
		}

		if pageNo == 0 && p.Header.FirstRecOff != sessionfile.RecordsStart {
			ix.noHead = true
		}
		ix.pages++
		ix.records += len(p.Records)

		// 页头说「本页从帧首开始」，上一页却留着未完帧 —— 中间丢了一页，
		// 必须切断，否则会把两个不相干的帧粘一起（规范 §2.9）。
		if p.Header.FirstRecOff == sessionfile.RecordsStart && prevOpen && len(p.Records) > 0 {
			ix.broken = true
			sc.close()
		}

		sc.addRecords(p.Records)

		if n := len(p.Records); n > 0 {
			prevOpen = p.Records[n-1].Dir&sessionfile.DirMore != 0
		}
		if torn {
			break
		}
		off += sessionfile.PageSize
		pageNo++
		if pageNo%512 == 0 && progress != nil {
			progress(ix.n, off)
		}
	}

	if sc.open {
		ix.truncated = true
		sc.close()
	}
	if len(ix.segIdx) < ix.n+1 {
		ix.segIdx = append(ix.segIdx, int32(len(ix.seg)))
	}
	ix.segKey = ix.scanStyle()
	if progress != nil {
		progress(ix.n, st.Size())
	}
	return ix, nil
}

// scanStyle 是扫描时预填格子用的样式：只按文件头的编码，其余取主工程默认。
// Configure 传来不同的样式时由 Store.ensureSegLocked 重算。
func (ix *index) scanStyle() Style { return DefaultStyle(ix.encoding()) }

// addRecords 把一页里的记录接进当前帧。
func (sc *scanner) addRecords(recs []sessionfile.Record) {
	ix := sc.ix
	for i := range recs {
		r := &recs[i]
		if !sc.open {
			sc.open = true
			sc.openAt = int32(len(ix.arena))
			sc.openTs = int64(r.Ts)
			sc.openD = sessionfile.DirBase(r.Dir)
			sc.openO = ix.originID(r)
		}
		ix.arena = append(ix.arena, r.Payload...)
		if r.Dir&sessionfile.DirMore == 0 {
			sc.close()
		}
	}
}

// close 结束当前帧（续片链已断、或正常收尾）。
func (sc *scanner) close() {
	if !sc.open {
		return
	}
	ix := sc.ix
	sc.open = false

	payload := ix.arena[sc.openAt:]
	l := len(payload)
	ix.ts = append(ix.ts, sc.openTs)
	ix.aoff = append(ix.aoff, sc.openAt)
	ix.ln = append(ix.ln, uint32(l))
	ix.dir = append(ix.dir, sc.openD)
	ix.org = append(ix.org, sc.openO)
	ix.segIdx = append(ix.segIdx, int32(len(ix.seg)))
	sc.cells = cellsOf(payload, ix.scanStyle(), sc.cells[:0])
	ix.seg = append(ix.seg, sc.cells...)
	if ix.n == 0 || int64(sc.openTs) < ix.firstTs {
		ix.firstTs = int64(sc.openTs)
	}
	if int64(sc.openTs) > ix.lastTs {
		ix.lastTs = int64(sc.openTs)
	}
	ix.n++
	switch sc.openD {
	case sessionfile.DirRx:
		ix.rxBytes += int64(l)
	case sessionfile.DirTx:
		ix.txBytes += int64(l)
	case sessionfile.DirP1:
		ix.p1Bytes += int64(l)
	case sessionfile.DirP2:
		ix.p2Bytes += int64(l)
	}
}

// originID 把来源串映射成小整数；整份文件里出现过的串存进 orgNames。
func (ix *index) originID(r *sessionfile.Record) uint8 {
	if r.Origin != sessionfile.OriginInline || r.OriginStr == "" {
		return 0
	}
	for i, s := range ix.orgNames {
		if s == r.OriginStr {
			return uint8(i + 1)
		}
	}
	if len(ix.orgNames) >= 7 {
		return 0
	}
	ix.orgNames = append(ix.orgNames, r.OriginStr)
	return uint8(len(ix.orgNames))
}

// encoding 是格子计算的编码档位：0=utf-8 1=ascii 2=gb2312。
func (ix *index) encoding() uint8 {
	if ix.settings == nil {
		return 0
	}
	switch ix.settings.Display["encoding"] {
	case "ascii":
		return 1
	case "gb2312", "gbk":
		return 2
	}
	return 0
}

// readSettingsInto 只读 16 KiB 头里的设置槽（不扫记录）。
func readSettingsInto(f *os.File, ix *index) error {
	var a, b *sessionfile.Slot
	for i, off := range []int64{sessionfile.SlotAOffset, sessionfile.SlotBOffset} {
		buf := make([]byte, sessionfile.SlotSize)
		if _, err := f.ReadAt(buf, off); err != nil {
			continue
		}
		s, err := sessionfile.DecodeSlot(buf)
		if err != nil {
			continue
		}
		if i == 0 {
			a = s
		} else {
			b = s
		}
	}
	chosen := sessionfile.ChooseSlot(a, b)
	if chosen == nil || len(chosen.Payload) == 0 {
		return fmt.Errorf("文件头里没有可用的设置槽，不像是 .stlog 会话文件")
	}
	if chosen.Payload[0] == '{' { // 老文件（SlotPayloadVer <= 2）的 payload 是 JSON
		var set sessionfile.Settings
		if err := json.Unmarshal(chosen.Payload, &set); err != nil {
			return nil
		}
		ix.settings = &set
		return nil
	}
	set, err := sessionfile.DecodeSettings(chosen.Payload)
	if err != nil {
		return nil // 设置解不出来不影响读帧，按默认显示
	}
	ix.settings = &set
	return nil
}

// recoverAt 用影子页救一页：只有影子自己的 pageOffset 与期望一致才算数。
func recoverAt(f *os.File, off int64) (*sessionfile.Page, bool) {
	var best *sessionfile.Page
	buf := make([]byte, sessionfile.PageSize)
	for _, so := range []int64{sessionfile.ShadowAOffset, sessionfile.ShadowBOffset} {
		if _, err := f.ReadAt(buf, so); err != nil {
			continue
		}
		p, err := sessionfile.DecodePage(buf, uint64(off))
		if err != nil {
			continue
		}
		if best == nil || p.Header.CommittedLen > best.Header.CommittedLen {
			best = p
		}
	}
	return best, best != nil
}
