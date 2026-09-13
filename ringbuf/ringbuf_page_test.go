package ringbuf

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

// newTestRing 构造一个内存环（不走共享内存）。
func newTestRing(bufSize uint32) *RingBuffer {
	data := make([]byte, headerTotal+int(bufSize))
	copy(data, newHeader(bufSize))
	return wrapMemory(data, false, nil, 0)
}

// mkPkt 造一个历史包：| ts(8B) | dir(1B) | hexLen(2B) | hex |
func mkPkt(ts int64, dir byte, hexText string) []byte {
	pkt := make([]byte, 11+len(hexText))
	binary.LittleEndian.PutUint64(pkt[0:8], uint64(ts))
	pkt[8] = dir
	binary.LittleEndian.PutUint16(pkt[9:11], uint16(len(hexText)))
	copy(pkt[11:], hexText)
	return pkt
}

func pktTs(pkt []byte) int64   { return int64(binary.LittleEndian.Uint64(pkt[0:8])) }
func pktHex(pkt []byte) string { return string(pkt[11:]) }

func tsList(pkts [][]byte) []int64 {
	out := make([]int64, 0, len(pkts))
	for _, p := range pkts {
		out = append(out, pktTs(p))
	}
	return out
}

func hexList(pkts [][]byte) []string {
	out := make([]string, 0, len(pkts))
	for _, p := range pkts {
		out = append(out, pktHex(p))
	}
	return out
}

func eqInt64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func eqStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSnapshotPageEmpty(t *testing.T) {
	rb := newTestRing(4096)
	res := rb.SnapshotPage(10, 0, 0)
	if len(res.Packets) != 0 || res.HasMore || res.Older != 0 || res.OldestMs != 0 {
		t.Fatalf("空环应当返回空结果, got %d 包 hasMore=%v older=%d oldest=%d",
			len(res.Packets), res.HasMore, res.Older, res.OldestMs)
	}
}

// 首次加载：无游标、带 limit，应当只回最新的 limit 个，并报出还有更早的。
func TestSnapshotPageNewestOnly(t *testing.T) {
	rb := newTestRing(4096)
	for i := 0; i < 10; i++ {
		if !rb.Write(mkPkt(int64(1000+i), 1, fmt.Sprintf("AA%02X", i))) {
			t.Fatalf("写入第 %d 个包失败", i)
		}
	}

	res := rb.SnapshotPage(3, 0, 0)
	if got, want := tsList(res.Packets), []int64{1007, 1008, 1009}; !eqInt64(got, want) {
		t.Fatalf("应当只回最新 3 个, got %v want %v", got, want)
	}
	if !res.HasMore || res.Older != 7 {
		t.Fatalf("应当报出还有 7 个更早的, got hasMore=%v older=%d", res.HasMore, res.Older)
	}
	if res.OldestMs != 1000 {
		t.Fatalf("OldestMs 应当是环里最旧包的 ts=1000, got %d", res.OldestMs)
	}

	// limit<=0 = 不限量（CLI/MCP 的历史行为）
	all := rb.SnapshotPage(0, 0, 0)
	if len(all.Packets) != 10 || all.HasMore {
		t.Fatalf("不限量应当回全部 10 个且 hasMore=false, got %d hasMore=%v",
			len(all.Packets), all.HasMore)
	}
}

// 游标：只回严格早于游标毫秒组的包；游标那一毫秒本身按 sameTsSkip 切。
func TestSnapshotPageCursorStrict(t *testing.T) {
	rb := newTestRing(4096)
	for i := 0; i < 10; i++ {
		rb.Write(mkPkt(int64(1000+i), 1, "AA"))
	}

	// 调用方手上有 1 条 ts=1005（就是它的最早一条），所以要 1000..1004
	res := rb.SnapshotPage(0, 1005, 1)
	if got, want := tsList(res.Packets), []int64{1000, 1001, 1002, 1003, 1004}; !eqInt64(got, want) {
		t.Fatalf("游标 (1005,skip=1) 应当回 1000..1004, got %v", got)
	}
	if res.HasMore {
		t.Fatal("1000..1004 就是环里最旧的一段, hasMore 不应为 true")
	}

	// skip=0 表示「这一毫秒我一条都没有」→ 整组都算缺失，一并回给调用方
	zero := rb.SnapshotPage(0, 1005, 0)
	if got, want := tsList(zero.Packets), []int64{1000, 1001, 1002, 1003, 1004, 1005}; !eqInt64(got, want) {
		t.Fatalf("游标 (1005,skip=0) 应当回 1000..1005, got %v", got)
	}

	// 带 limit：从候选里取最新的 2 个，并如实报出还剩多少
	lim := rb.SnapshotPage(2, 1005, 1)
	if got, want := tsList(lim.Packets), []int64{1003, 1004}; !eqInt64(got, want) {
		t.Fatalf("limit=2 应当回 1003,1004, got %v", got)
	}
	if !lim.HasMore || lim.Older != 3 {
		t.Fatalf("应当还有 3 个更早的, got hasMore=%v older=%d", lim.HasMore, lim.Older)
	}

	// 游标比环里所有包都旧 → 没有更早的了
	none := rb.SnapshotPage(10, 999, 0)
	if len(none.Packets) != 0 || none.HasMore {
		t.Fatalf("游标 999 之前没有数据, got %d hasMore=%v", len(none.Packets), none.HasMore)
	}
}

// 同一毫秒组：只用 ts< 做游标会丢掉边界那一毫秒剩下的包，sameTsSkip 就是
// 用来把「调用方已经持有的那几条」摘出去的。这是本设计里最容易写错的一处。
func TestSnapshotPageSameMsGroup(t *testing.T) {
	rb := newTestRing(4096)
	for i := 0; i < 4; i++ {
		rb.Write(mkPkt(1000, 1, fmt.Sprintf("A%d", i)))
	}
	for i := 0; i < 6; i++ {
		rb.Write(mkPkt(2000, 1, fmt.Sprintf("B%d", i)))
	}

	// 调用方手上已有 ts=2000 里最新的 3 条（B3 B4 B5），要更早的
	res := rb.SnapshotPage(0, 2000, 3)
	if got, want := hexList(res.Packets), []string{"A0", "A1", "A2", "A3", "B0", "B1", "B2"}; !eqStr(got, want) {
		t.Fatalf("同一毫秒组切分错误, got %v want %v", got, want)
	}
	if res.HasMore {
		t.Fatal("这里应当正好取完，hasMore 不应为 true")
	}

	// 带 limit 时截断的必须是更早的那一端
	lim := rb.SnapshotPage(2, 2000, 3)
	if got, want := hexList(lim.Packets), []string{"B1", "B2"}; !eqStr(got, want) {
		t.Fatalf("limit=2 应当回 B1,B2, got %v", got)
	}
	if !lim.HasMore || lim.Older != 5 {
		t.Fatalf("应当还有 5 个更早的, got hasMore=%v older=%d", lim.HasMore, lim.Older)
	}
}

// 调用方持有的比环里留着的还多（旧的那部分已被挤掉）：这一毫秒组一条都不该回，
// 更不能把 sameTsSkip 减成负数、顺着往下多吃掉更早的包。
func TestSnapshotPageSkipBeyondRing(t *testing.T) {
	rb := newTestRing(4096)
	for i := 0; i < 4; i++ {
		rb.Write(mkPkt(1000, 1, fmt.Sprintf("A%d", i)))
	}
	for i := 0; i < 6; i++ {
		rb.Write(mkPkt(2000, 1, fmt.Sprintf("B%d", i)))
	}

	res := rb.SnapshotPage(0, 2000, 10)
	if got, want := hexList(res.Packets), []string{"A0", "A1", "A2", "A3"}; !eqStr(got, want) {
		t.Fatalf("skip 超出环内该组条数时应当只回更早的包, got %v", got)
	}
	if res.HasMore {
		t.Fatal("A0..A3 已是最旧, hasMore 不应为 true")
	}
}

// 环写满后必须挤掉最旧的、保住最新的 —— 这是 WriteEvict 存在的理由。
func TestWriteEvictKeepsNewest(t *testing.T) {
	rb := newTestRing(256)
	const required = 11 + 4 + lenPrefixSize // 包体 15 字节 + 2 字节长度前缀
	const capacity = 256 / required         // 17 字节一条 → 15 条

	total := capacity + 30
	for i := 0; i < total; i++ {
		if !rb.WriteEvict(mkPkt(int64(i), 1, fmt.Sprintf("%04d", i))) {
			t.Fatalf("第 %d 个包写入失败", i)
		}
	}

	res := rb.SnapshotPage(0, 0, 0)
	if len(res.Packets) != capacity {
		t.Fatalf("环里应当恰好保留 %d 个包, got %d", capacity, len(res.Packets))
	}
	// 保留的必须是最后 capacity 个，且顺序不乱
	for i, p := range res.Packets {
		wantIdx := total - capacity + i
		if got := pktHex(p); got != fmt.Sprintf("%04d", wantIdx) {
			t.Fatalf("第 %d 个保留包是 %s, 期望 %04d（应当只挤掉最旧的）", i, got, wantIdx)
		}
	}
	if res.HasMore {
		t.Fatal("环里已无更早的包, hasMore 不应为 true")
	}

	// 旧行为对照：Write 满时直接失败，环就永久停在那一刻
	rb2 := newTestRing(256)
	for i := 0; i < total; i++ {
		rb2.Write(mkPkt(int64(i), 1, fmt.Sprintf("%04d", i)))
	}
	old := rb2.SnapshotPage(0, 0, 0)
	if len(old.Packets) != capacity {
		t.Fatalf("对照环应当也是 %d 个包, got %d", capacity, len(old.Packets))
	}
	if got := pktHex(old.Packets[len(old.Packets)-1]); got != fmt.Sprintf("%04d", capacity-1) {
		t.Fatalf("Write 满即停：最新一条应当停在 %04d, got %s", capacity-1, got)
	}
}

// 绕圈写：包长不整齐，读写指针会跨过缓冲区末端，位置记录不能算错。
func TestWriteEvictWrapAroundIntegrity(t *testing.T) {
	rb := newTestRing(512)
	const total = 400
	// 包体长度 2..34 字节（恒为偶数），内容里编上序号，便于回读核对
	payload := func(i int) string {
		return strings.Repeat(fmt.Sprintf("%02X", i%256), 1+i%17)
	}
	for i := 0; i < total; i++ {
		if !rb.WriteEvict(mkPkt(int64(1000+i), 1, payload(i))) {
			t.Fatalf("第 %d 个包写入失败", i)
		}
	}

	res := rb.SnapshotPage(0, 0, 0)
	if len(res.Packets) == 0 {
		t.Fatal("绕圈后环不应为空")
	}
	// 必须是一段连续的后缀：ts 逐 1 递增，包体完好且与序号对得上
	for i, p := range res.Packets {
		ts := pktTs(p)
		if i > 0 && ts != pktTs(res.Packets[i-1])+1 {
			t.Fatalf("第 %d 个包的 ts=%d 与上一个不连续，绕圈后顺序被破坏", i, ts)
		}
		idx := int(ts - 1000)
		if got, want := pktHex(p), payload(idx); got != want {
			t.Fatalf("序号 %d 的包体被破坏: got %q want %q", idx, got, want)
		}
	}
	// 最后一条必须是最新写入的那条
	if ts := pktTs(res.Packets[len(res.Packets)-1]); ts != int64(1000+total-1) {
		t.Fatalf("最新一条应当 ts=%d, got %d", 1000+total-1, ts)
	}

	// 分页在绕圈后同样成立：拿已知的一段做游标
	midIdx := len(res.Packets) / 2
	mid := res.Packets[midIdx]
	page := rb.SnapshotPage(4, pktTs(mid), 1)
	if len(page.Packets) != 4 {
		t.Fatalf("绕圈后 limit=4 应当回 4 条, got %d", len(page.Packets))
	}
	if !page.HasMore || page.Older != midIdx-4 {
		t.Fatalf("绕圈后应当报出还有 %d 条更早的, got hasMore=%v older=%d",
			midIdx-4, page.HasMore, page.Older)
	}
	for i, p := range page.Packets {
		if pktTs(p) >= pktTs(mid) {
			t.Fatalf("第 %d 条 ts=%d 不小于游标 %d", i, pktTs(p), pktTs(mid))
		}
	}
	// 不限量时应当正好回 mid 之前的全部
	allOlder := rb.SnapshotPage(0, pktTs(mid), 1)
	if len(allOlder.Packets) != midIdx {
		t.Fatalf("游标之前应当有 %d 条, got %d", midIdx, len(allOlder.Packets))
	}
	if allOlder.HasMore {
		t.Fatal("取到环里最旧一条后 hasMore 不应为 true")
	}
	if page.OldestMs != pktTs(res.Packets[0]) {
		t.Fatalf("OldestMs 应当是 %d, got %d", pktTs(res.Packets[0]), page.OldestMs)
	}
}
