package stlogview

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

// stlogFiles 把「打开目标」变成按顺序要扫的文件列表。
//
// 单个 .stlog 就是它自己；工程目录则是 manifest 登记的包（manifest 缺失时按
// 文件名里的序号认领，见 sessionfile.PackageRefs）。多个包按序号接成一条时间线
// —— 帧绝不跨包，拼接是无损的。
func stlogFiles(path string) ([]string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return []string{path}, nil
	}
	refs := sessionfile.PackageRefs(path)
	if len(refs) == 0 {
		return nil, fmt.Errorf("这是一个会话目录，但里面没有 pkg-*.stlog")
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, filepath.Join(path, r.File))
	}
	return out, nil
}

func scanStlog(path string, progress func(frames int, done int64)) (*index, error) {
	files, err := stlogFiles(path)
	if err != nil {
		return nil, err
	}
	// 先把所有包的大小加起来：进度按「整条会话」走，不是每个包各走一遍。
	total := int64(0)
	sizes := make([]int64, len(files))
	for i, one := range files {
		fi, err := os.Stat(one)
		if err != nil {
			return nil, err
		}
		sizes[i] = fi.Size()
		total += fi.Size()
	}

	ix := &index{path: path, arena: make([]byte, 0, total)}
	sc := &scanner{ix: ix}
	done := int64(0)
	for i, one := range files {
		stop, err := scanStlogFile(ix, sc, one, i == 0, done, progress)
		if err != nil {
			return nil, err
		}
		done += sizes[i]
		if stop {
			break
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
		progress(ix.n, total)
	}
	return ix, nil
}

// scanStlogFile 扫一个包，帧继续追加进同一个 index。
//
// first 时才读设置槽（各包槽里是同一份快照，以第一个为准）。
// stop=true 表示这个包在中途断了：后面的包排在它之后，硬接上去时间线会跳，
// 所以整条会话就停在这里（上面会显示 StopErr）。
func scanStlogFile(ix *index, sc *scanner, path string, first bool,
	done int64, progress func(frames int, done int64)) (bool, error) {

	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	if st.Size() < sessionfile.HeaderSize {
		return false, fmt.Errorf("%s：文件只有 %d B，不够一个 .stlog 文件头", filepath.Base(path), st.Size())
	}
	ix.size += st.Size()
	if first {
		if err := readSettingsInto(f, ix); err != nil {
			return false, err
		}
	} else if sc.open {
		// 跨包的未完帧不该存在（规范：帧绝不跨包）。真遇到就按「末帧未结束」切断，
		// 否则两个包的数据会被粘成一帧。
		ix.truncated = true
		sc.close()
	}

	buf := make([]byte, sessionfile.PageSize)
	off := int64(sessionfile.RecordsOffset)
	prevOpen := false
	pageNo := 0

	stop := false
	for off < st.Size() {
		torn := off+sessionfile.PageSize > st.Size()
		var p *sessionfile.Page

		if torn {
			// 尾页不完整：只有影子页里还留着一份完整拷贝才救得回来。
			rp, ok := recoverAt(f, off)
			if !ok {
				ix.stoppedAt = off
				ix.stopErr = sessionfile.ErrPartialTail
				stop = true
				break
			}
			p = rp
			ix.recovered = append(ix.recovered, off)
		} else {
			if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
				return false, err
			}
			pp, derr := sessionfile.DecodePage(buf, uint64(off))
			if derr != nil {
				rp, ok := recoverAt(f, off)
				if !ok {
					ix.stoppedAt = off
					ix.stopErr = derr
					stop = true
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
			// 末页不完整：可能是这个包被硬断过（崩溃 / 拔盘）。后面的包排在它
			// 之后，接上去时间线会跳，所以整条会话停在这里。
			stop = true
			break
		}
		off += sessionfile.PageSize
		pageNo++
		if pageNo%512 == 0 && progress != nil {
			progress(ix.n, done+off)
		}
	}
	return stop, nil
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
