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
//   - 有上限：arena 超过 maxBytes 就从最旧的一段裁掉（默认 8 MB，与守护进程的
//     5 MB 环同量级）。裁掉的帧号不回收 —— 对外用「绝对帧号」= absBase + 下标，
//     界面上的行号一直往前涨，不会因为裁剪而跳位。
//   - 可以往前补：PrependFrames 把更早的历史（GetSessionHistory 回来的那批）
//     插到最前面，帧号相应变小。

// DefaultLiveBytes 是实时文档 arena 的默认上限：超过就从最旧的一段裁掉。
const DefaultLiveBytes = 8 << 20

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
	Total   int    `json:"total"`   // 绝对帧数（含已裁掉的）
	Frames  int    `json:"frames"`  // 现在保留着几帧
	Evicted int    `json:"evicted"` // 已经从头部裁掉几帧
	Bytes   int64  `json:"bytes"`   // arena 占了多少字节
	Phase   string `json:"phase"`
}

// MarkLive 把文档切成实时模式：清空索引，之后只能 Append / Prepend。
//
// maxBytes <= 0 取 DefaultLiveBytes。
func (s *Store) MarkLive(maxBytes int64) {
	if maxBytes <= 0 {
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
		out.Bytes = int64(len(s.ix.arena))
	}
	out.Evicted = s.absBase
	out.Total = s.absBase + out.Frames
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
		s.prefix = []int64{0}
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

	// 前缀和只追加一行：高度 = 时间戳行 + 若干数据行 + 间距
	lines := s.linesOfLocked(ix.n - 1)
	h2 := int64(s.tsLineH2 + lines*s.dataLineH2 + rowGap2)
	last := s.prefix[len(s.prefix)-1]
	s.prefix = append(s.prefix, last+h2)
	s.totalH2 = last + h2
}

// evictLocked 在 arena 超上限时从最旧的一端裁到 75%，并把前缀和重建一次。
func (s *Store) evictLocked() {
	ix := s.ix
	if ix == nil || s.maxBytes <= 0 || int64(len(ix.arena)) <= s.maxBytes {
		return
	}
	target := s.maxBytes * 3 / 4
	cut, kept := 0, int64(len(ix.arena))
	for cut < ix.n && kept > target {
		kept -= int64(ix.ln[cut])
		cut++
	}
	if cut <= 0 || cut >= ix.n {
		return
	}
	s.dropFrontLocked(cut)
}

// dropFrontLocked 丢掉最前面的 cut 帧（裁旧用）。
func (s *Store) dropFrontLocked(cut int) {
	ix := s.ix
	if cut <= 0 || cut > ix.n {
		return
	}
	n := ix.n - cut
	base := ix.aoff[cut]
	segBase := ix.segIdx[cut]

	// 数组左移（dst 在 src 之前，copy 允许重叠）
	copy(ix.ts, ix.ts[cut:])
	ix.ts = ix.ts[:n]
	copy(ix.aoff, ix.aoff[cut:])
	ix.aoff = ix.aoff[:n]
	copy(ix.ln, ix.ln[cut:])
	ix.ln = ix.ln[:n]
	copy(ix.dir, ix.dir[cut:])
	ix.dir = ix.dir[:n]
	copy(ix.org, ix.org[cut:])
	ix.org = ix.org[:n]
	// segIdx 有 n+1 项：移位后整体减去被切掉的那段长度
	copy(ix.segIdx, ix.segIdx[cut:])
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
	ix.n = n
	s.absBase += cut
	if n > 0 {
		ix.firstTs = ix.ts[0]
	} else {
		ix.firstTs, ix.lastTs = 0, 0
	}
	s.rebuildLayoutLocked()
}

// prependLocked 把一批更早的帧插到最前面（整表重建一次，很少发生）。
func (s *Store) prependLocked(fs []FrameIn) {
	ix := s.ix
	add := len(fs)
	if add == 0 {
		return
	}
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
	s.rebuildLayoutLocked()
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

// rebuildLayoutLocked 从头重算前缀和（裁剪 / 前插 / 改样式之后）。
func (s *Store) rebuildLayoutLocked() {
	ix := s.ix
	if ix == nil || ix.n == 0 {
		s.prefix = []int64{0}
		s.totalH2 = 0
		s.perLine = s.perLineLocked()
		return
	}
	s.perLine = s.perLineLocked()
	if cap(s.prefix) < ix.n+1 {
		s.prefix = make([]int64, ix.n+1)
	}
	buf := s.prefix[:ix.n+1]
	var y int64
	buf[0] = 0
	for i := 0; i < ix.n; i++ {
		y += int64(s.tsLineH2 + s.linesOfLocked(i)*s.dataLineH2 + rowGap2)
		buf[i+1] = y
	}
	s.prefix = buf
	s.totalH2 = y
}
