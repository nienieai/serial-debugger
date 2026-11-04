package stlogview

import (
	"encoding/hex"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// 实时文档：串口监视器 / 转发监视器用同一套虚窗模型，只是帧不是从磁盘扫出来的，
// 而是一帧一帧 Append 进来。
//
// 与静态文件的三点不同：
//   - 索引是增量的：每帧只追加 ts/aoff/ln/dir/org + 格子数，前缀和也只追加一行，
//     不重扫整体（高吞吐时这是硬要求）。
//   - 有上限：arena 超过 maxBytes 就从最旧的一端裁掉（默认 5 MB，与守护进程的
//     5 MB 环同量级）。裁头是 O(1) 的：只把逻辑起点往前挪，不搬 arena、也不重算
//     前缀和 —— 语义上就是守护进程那个环形缓冲，超一帧丢一帧。裁掉的帧号不回收
//     —— 对外用「绝对帧号」= absBase + 下标，界面上的行号一直往前涨，不会因为
//     裁剪而跳位。
//   - 可以往前补：PrependFrames 把更早的历史（GetSessionHistory 回来的那批）
//     插到最前面，帧号相应变小。

// DefaultLiveBytes 是实时文档 arena 的默认上限：超过就从最旧的一段裁掉。
//
// 和守护进程的 5 MB 环形缓冲取同一个数：界面上「缓冲到 5 MB 就开始丢最旧的」
// 才和用户看到的 Rx 字节数对得上（以前是 8 MB，5 MB 时什么都不发生）。
const DefaultLiveBytes = 5 << 20

// FrameIn 是一帧的输入（GUI 侧喂进来的形态）。
//
// Hex 用字符串而不是 []byte：GUI 收到的本来就是十六进制串，省一次 JSON 数组的
// 解析；Go 这边 hex.DecodeString 一次就够。
type FrameIn struct {
	Ts     int64  `json:"ts"`
	Dir    uint8  `json:"dir"`
	Origin string `json:"origin"`
	Hex    string `json:"hex"`
}

// LiveInfo 是实时文档的概况（前端定时轮一次，决定要不要重取窗口）。
type LiveInfo struct {
	Total   int   `json:"total"`   // 绝对帧数（含已裁掉的）
	Frames  int   `json:"frames"`  // 现在保留着几帧
	Evicted int   `json:"evicted"` // 已经从头部裁掉几帧
	Bytes   int64 `json:"bytes"`   // arena 占了多少字节
	// ContentH 是内容总高度（像素）：前端轮询时拿它判断「要不要重取窗口」，
	// 比每 120 ms 都取一次窗口便宜。
	ContentH float64 `json:"contentH"`
	// TrimmedPx 是从头部裁掉的内容总高度（像素）。用户顶着旧数据看的时候，
	// 前端按它的增量把 offset 往下减，视口就锁在原地、滚动条缓慢上推。
	TrimmedPx float64 `json:"trimmedPx"`
	Phase     string  `json:"phase"`
	// OldestTs / OldestSameMs 是文档里最旧那一帧的时间戳，以及它所在的这一
	// 毫秒在文档里有几帧（含它自己）。往回翻要靠这两个当游标向守护进程再要
	// 一页：毫秒会撞车，只用 ts 做游标会重复或漏帧（见 ringbuf.SnapshotPage）。
	OldestTs     int64 `json:"oldestTsMs"`
	OldestSameMs int   `json:"oldestSameMs"`
}

// MarkLive 把文档切成实时模式：清空索引，之后只能 Append / Prepend。
//
// maxBytes 是 arena 的字节上限：0 取 DefaultLiveBytes，负数表示不设上限
// （磁盘上有会话文件、窗口外的帧能按页取回来时才敢这么开；按需读盘还没接，
// 调用方目前一律传 0 或正数）。
func (s *Store) MarkLive(maxBytes int64) {
	if maxBytes == 0 {
		maxBytes = DefaultLiveBytes
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopFlag != nil {
		close(s.stopFlag)
		s.stopFlag = nil
	}
	s.live = true
	s.maxBytes = maxBytes
	s.path = ""
	s.ix = &index{segIdx: []int32{0}}
	s.absBase = 0
	s.trimmedH2 = 0
	s.perLine = s.perLineLocked()
	s.prefix = []int64{0}
	s.totalH2 = 0
	s.ruler = nil
	s.rulerUpToResetLocked()
	s.phase = "ready"
	s.scanErr = ""
}

// IsLive 说明这个文档是不是实时模式。
func (s *Store) IsLive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live
}

// AppendFrame 追加一帧（实时路径）。返回追加后的概况。
func (s *Store) AppendFrame(f FrameIn) LiveInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live {
		s.appendFrameLocked(f)
		s.evictLocked()
	}
	return s.liveInfoLocked()
}

// AppendFrames 批量追加（一次加锁，避免高吞吐时每帧抢锁）。
func (s *Store) AppendFrames(fs []FrameIn) LiveInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live {
		for _, f := range fs {
			s.appendFrameLocked(f)
		}
		s.evictLocked()
	}
	return s.liveInfoLocked()
}

// PrependFrames 把更早的历史插到最前面（往回翻加载更早的帧时用）。
//
// 只收比现有第一帧更早的帧：与流式收到的帧重叠的那部分直接丢掉，
// 否则同一帧会在文档里出现两次。
func (s *Store) PrependFrames(fs []FrameIn) LiveInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.live || len(fs) == 0 {
		return s.liveInfoLocked()
	}
	if s.ix.n > 0 {
		first := s.ix.ts[0]
		keep := 0
		for keep < len(fs) && fs[keep].Ts < first {
			keep++
		}
		fs = fs[:keep]
		if len(fs) == 0 {
			return s.liveInfoLocked()
		}
	}
	s.prependLocked(fs)
	return s.liveInfoLocked()
}

// ResetLive 清空实时文档（界面上的「清空」）：帧号从头开始。
func (s *Store) ResetLive() LiveInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live {
		s.ix = &index{segIdx: []int32{0}}
		s.absBase = 0
		s.trimmedH2 = 0
		s.prefix = []int64{0}
		s.totalH2 = 0
		s.ruler = nil
		s.rulerUpToResetLocked()
	}
	return s.liveInfoLocked()
}

// LiveInfo 取概况（前端每 50~120 ms 轮一次）。
func (s *Store) LiveInfo() LiveInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.liveInfoLocked()
}

func (s *Store) liveInfoLocked() LiveInfo {
	out := LiveInfo{Phase: s.phase}
	if s.ix != nil {
		out.Frames = s.ix.n
		// 只报逻辑占用：物理 arena 里还留着已经裁掉的帧（等 compact 才搬走），
		// 报物理长度会让「缓冲区 5 MB」对不上用户看到的数字。
		if s.ix.n > 0 {
			out.Bytes = int64(len(s.ix.arena)) - int64(s.ix.aoff[s.ix.head])
		}
	}
	out.ContentH = float64(s.totalH2) / 2
	out.TrimmedPx = float64(s.trimmedH2) / 2
	out.Evicted = s.absBase
	out.Total = s.absBase + out.Frames
	if s.ix != nil && s.ix.n > 0 {
		// 最旧帧取自逻辑起点（head）而不是物理 0：裁过头的那些已经不在文档里了。
		oldest := s.ix.ts[s.ix.head]
		n := 1
		for s.ix.head+n < len(s.ix.ts) && s.ix.ts[s.ix.head+n] == oldest {
			n++
		}
		out.OldestTs = oldest
		out.OldestSameMs = n
	}
	return out
}

// appendFrameLocked 追加一帧：只追加数组、只追加一行前缀和，不重扫。
func (s *Store) appendFrameLocked(f FrameIn) {
	ix := s.ix
	if ix == nil {
		ix = &index{segIdx: []int32{0}}
		s.ix = ix
	}
	if s.prefix == nil {
		s.prefix = []int64{s.trimmedH2}
		s.perLine = s.perLineLocked()
	}
	payload, err := hex.DecodeString(f.Hex)
	if err != nil {
		payload = nil
	}
	d := sessionfile.DirBase(f.Dir)
	if !sessionfile.DirValid(d) {
		d = sessionfile.DirRx
	}
	ts := f.Ts
	if ts <= 0 {
		ts = ix.lastTs + 1
	}
	if ts < ix.lastTs {
		ts = ix.lastTs
	}

	ix.ts = append(ix.ts, ts)
	ix.aoff = append(ix.aoff, int32(len(ix.arena)))
	ix.ln = append(ix.ln, uint32(len(payload)))
	ix.dir = append(ix.dir, d)
	ix.org = append(ix.org, ix.originIDStr(f.Origin))
	ix.arena = append(ix.arena, payload...)
	// segIdx 的约定是「n+1 项」：末项是总格数。新帧覆盖掉旧的末项当起点，
	// 算完格子再补一个新的末项。
	ix.segIdx[len(ix.segIdx)-1] = int32(len(ix.seg))
	ix.seg = cellsOf(payload, s.style, ix.seg)
	ix.segIdx = append(ix.segIdx, int32(len(ix.seg)))
	ix.addStats(d, len(payload), ts)
	ix.n++

	// 前缀和只追加一行：高度 = 时间戳行 + 若干数据行 + 间距。
	// 前缀和是按物理下标存的，绝对高度；逻辑第 i 帧的顶部 = prefix[head+i]。
	lines := s.linesOfLocked(ix.head + ix.n - 1)
	h2 := int64(s.tsLineH2 + lines*s.dataLineH2 + rowGap2)
	last := s.prefix[len(s.prefix)-1]
	s.prefix = append(s.prefix, last+h2)
	s.totalH2 = last + h2 - s.prefix[ix.head]
}

// evictLocked 按守护进程环形缓冲的语义裁头：超一帧丢一帧，丢到不超上限为止。
//
// 不攒「裁到 75%」那种批量：批量会让实际占用在 75%~100% 之间来回摆，用户看到的
// 是「缓冲区一会儿 3.7 MB、一会儿 5 MB」；逐帧丢才是环形缓冲的样子。
// 代价本来在 dropFrontLocked（以前要搬 5 MB arena + 重算整张前缀和），
// 改成挪逻辑起点之后这里只剩一个循环，摊销 O(1)。
func (s *Store) evictLocked() {
	ix := s.ix
	if ix == nil || ix.n == 0 || s.maxBytes < 0 {
		return
	}
	used := int64(len(ix.arena)) - int64(ix.aoff[ix.head])
	if used <= s.maxBytes {
		return
	}
	cut := 0
	for cut < ix.n && used > s.maxBytes {
		used -= int64(ix.ln[ix.head+cut])
		cut++
	}
	s.dropFrontLocked(cut)
}

// dropFrontLocked 丢掉最前面的 cut 帧（裁旧用）：只挪逻辑起点，不搬数据。
//
// 物理数组原样留着，head 指到新的第一帧；前缀和（绝对高度）也不用重算，
// 新起点的顶部高度正好就是「已经裁掉多少像素」。
func (s *Store) dropFrontLocked(cut int) {
	ix := s.ix
	if cut <= 0 || cut > ix.n {
		return
	}
	ix.head += cut
	ix.n -= cut
	s.absBase += cut
	// 记下这一刀裁掉多少高度（半像素），前端靠它把视口锚在原地。
	s.trimmedH2 = s.prefix[ix.head]
	if ix.n > 0 {
		ix.firstTs = ix.ts[ix.head]
	} else {
		ix.firstTs, ix.lastTs = 0, 0
	}
	s.totalH2 = s.prefix[ix.head+ix.n] - s.prefix[ix.head]
	s.maybeCompactLocked()
}

// maybeCompactLocked 在 head 攒得够多时把数组整体搬回去一次。
//
// 判据是「被裁的比留着的还多」：这样每帧平摊下来只被搬常数次，裁头仍然是
// 摊销 O(1)，而物理数组不会无限长下去。
func (s *Store) maybeCompactLocked() {
	ix := s.ix
	if ix == nil || ix.head == 0 || ix.head < 1024 || ix.head*2 < ix.n {
		return
	}
	s.compactLocked()
}

// compactLocked 把逻辑起点搬回物理 0：数组左移 head 帧，arena 与格子缓存跟着
// 前移，前缀和裁掉前 head 项（绝对值不变，所以第一项还是 trimmedH2）。
//
// 只由 maybeCompactLocked / 重建布局调用，不参与逐帧路径。
func (s *Store) compactLocked() {
	ix := s.ix
	if ix == nil {
		return
	}
	h := ix.head
	if h <= 0 {
		return
	}
	if ix.n == 0 {
		// 全裁光了：数组清空，起点归零（prefix 只留 trimmedH2 那一项）。
		ix.ts = ix.ts[:0]
		ix.aoff = ix.aoff[:0]
		ix.ln = ix.ln[:0]
		ix.dir = ix.dir[:0]
		ix.org = ix.org[:0]
		ix.seg = ix.seg[:0]
		ix.segIdx = ix.segIdx[:1]
		ix.segIdx[0] = 0
		ix.arena = ix.arena[:0]
		s.prefix = s.prefix[:1]
		ix.head = 0
		return
	}
	n := ix.n
	base := ix.aoff[h]
	segBase := ix.segIdx[h]
	copy(ix.ts, ix.ts[h:])
	ix.ts = ix.ts[:n]
	copy(ix.aoff, ix.aoff[h:])
	ix.aoff = ix.aoff[:n]
	copy(ix.ln, ix.ln[h:])
	ix.ln = ix.ln[:n]
	copy(ix.dir, ix.dir[h:])
	ix.dir = ix.dir[:n]
	copy(ix.org, ix.org[h:])
	ix.org = ix.org[:n]
	// segIdx 有 n+1 项：移位后整体减去被切掉的那段长度
	copy(ix.segIdx, ix.segIdx[h:])
	ix.segIdx = ix.segIdx[:n+1]
	for i := range ix.segIdx {
		ix.segIdx[i] -= segBase
	}
	copy(ix.seg, ix.seg[segBase:])
	ix.seg = ix.seg[:len(ix.seg)-int(segBase)]
	copy(ix.arena, ix.arena[base:])
	ix.arena = ix.arena[:len(ix.arena)-int(base)]
	for i := range ix.aoff {
		ix.aoff[i] -= base
	}
	// 前缀和是绝对高度：裁掉前面 h 项，第一项仍然等于 trimmedH2。
	copy(s.prefix, s.prefix[h:])
	s.prefix = s.prefix[:n+1]
	ix.head = 0
}

// prependLocked 把一批更早的帧插到最前面（整表重建一次，很少发生）。
func (s *Store) prependLocked(fs []FrameIn) {
	ix := s.ix
	add := len(fs)
	if add == 0 {
		return
	}
	// 前插要整表重建：先把逻辑起点搬回物理 0，这样老数组就是纯逻辑帧
	// （否则已经裁掉的头部会被一并复制进新表）。
	s.compactLocked()
	oldN := ix.n
	oldArena := ix.arena
	oldSeg := ix.seg
	oldSegIdx := ix.segIdx
	oldTs, oldAoff, oldLn, oldDir, oldOrg := ix.ts, ix.aoff, ix.ln, ix.dir, ix.org

	total := add + oldN
	ts := make([]int64, total)
	aoff := make([]int32, total)
	ln := make([]uint32, total)
	dir := make([]uint8, total)
	org := make([]uint8, total)
	seg := make([]uint32, 0, len(oldSeg)+add*8)
	segIdx := make([]int32, 0, total+1)
	arena := make([]byte, 0, len(oldArena)+add*32)

	for i, f := range fs {
		payload, err := hex.DecodeString(f.Hex)
		if err != nil {
			payload = nil
		}
		d := sessionfile.DirBase(f.Dir)
		if !sessionfile.DirValid(d) {
			d = sessionfile.DirRx
		}
		ts[i] = f.Ts
		aoff[i] = int32(len(arena))
		ln[i] = uint32(len(payload))
		dir[i] = d
		org[i] = ix.originIDStr(f.Origin)
		segIdx = append(segIdx, int32(len(seg)))
		seg = cellsOf(payload, s.style, seg)
		arena = append(arena, payload...)
	}
	baseA := int32(len(arena))
	baseS := int32(len(seg))
	for i := 0; i < oldN; i++ {
		ts[add+i] = oldTs[i]
		aoff[add+i] = oldAoff[i] + baseA
		ln[add+i] = oldLn[i]
		dir[add+i] = oldDir[i]
		org[add+i] = oldOrg[i]
	}
	for _, v := range oldSegIdx {
		segIdx = append(segIdx, v+baseS)
	}
	seg = append(seg, oldSeg...)
	arena = append(arena, oldArena...)

	ix.ts, ix.aoff, ix.ln, ix.dir, ix.org = ts, aoff, ln, dir, org
	ix.seg, ix.segIdx, ix.arena = seg, segIdx, arena
	ix.n = total
	for _, f := range fs {
		payload, _ := hex.DecodeString(f.Hex)
		d := sessionfile.DirBase(f.Dir)
		if !sessionfile.DirValid(d) {
			d = sessionfile.DirRx
		}
		ix.addStats(d, len(payload), f.Ts)
	}
	ix.firstTs = ix.ts[0]
	s.buildLayoutLocked()
}

// addStats 把一帧算进统计块（字节数 / 时间范围）。
func (ix *index) addStats(d uint8, n int, ts int64) {
	if ix.n == 0 && ix.firstTs == 0 || ts < ix.firstTs {
		ix.firstTs = ts
	}
	if ts > ix.lastTs {
		ix.lastTs = ts
	}
	switch d {
	case sessionfile.DirRx:
		ix.rxBytes += int64(n)
	case sessionfile.DirTx:
		ix.txBytes += int64(n)
	case sessionfile.DirP1:
		ix.p1Bytes += int64(n)
	case sessionfile.DirP2:
		ix.p2Bytes += int64(n)
	}
}

// originIDStr 把来源串映射成小整数；与扫描路径共用同一张字典。
func (ix *index) originIDStr(s string) uint8 {
	if s == "" {
		return 0
	}
	for i, v := range ix.orgNames {
		if v == s {
			return uint8(i + 1)
		}
	}
	if len(ix.orgNames) >= 7 {
		return 0
	}
	ix.orgNames = append(ix.orgNames, s)
	return uint8(len(ix.orgNames))
}
