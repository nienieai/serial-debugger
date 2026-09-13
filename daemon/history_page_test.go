package main

import (
	"fmt"
	"strings"
	"testing"
)

// newHistoryProc 建一个不接串口的单端口进程，只为拿它的历史环。
func newHistoryProc(t *testing.T) (*ProcessManager, *Process) {
	t.Helper()
	// 不落盘：historyDir() 会指向测试二进制所在目录。
	setHistoryEnabled(false)
	t.Cleanup(func() { setHistoryEnabled(true) })

	pm := NewProcessManager()
	t.Cleanup(pm.DestroyAll)

	proc, err := pm.Create("single", "", SerialConfig{}, nil, false)
	if err != nil || proc == nil {
		t.Fatalf("建进程失败: %v", err)
	}
	// 建进程本身会记一条 system 事件进环，先清干净再灌测试数据。
	if err := pm.ClearHistory(proc.id); err != nil {
		t.Fatalf("清空历史失败: %v", err)
	}
	return pm, proc
}

func historyOf(t *testing.T, res map[string]any) []HistoryEntry {
	t.Helper()
	if res == nil {
		t.Fatal("历史读取返回 nil")
	}
	entries, ok := res["history"].([]HistoryEntry)
	if !ok {
		t.Fatalf("history 字段类型不对: %T", res["history"])
	}
	return entries
}

// cursorOf 是**前端 _cursorFrom 的同一个规则**：拿缓存里最旧那条的毫秒时间戳，
// 以及缓存里一共有几条属于这一毫秒。注意必须作用在「整个缓存」上而不是单页上：
// 翻第二页时手上已经有第一页那一毫秒的若干条了，只数当页会把它们重复取回来。
func cursorOf(cache []HistoryEntry) (beforeMs int64, skip int) {
	beforeMs = cache[0].TsMs
	for _, e := range cache {
		if e.TsMs != beforeMs {
			break
		}
		skip++
	}
	return beforeMs, skip
}

// 分页存在的意义：一页页往上翻必须能**无缝**重建整环 —— 不重不漏。
// 这是 daemon + ringbuf 这条链路上唯一真正要保证的性质。
func TestHistoryPagingReconstructsFullRing(t *testing.T) {
	pm, proc := newHistoryProc(t)

	const total = 500
	// 每 7 条同一毫秒，故意让时间戳撞车；7 与页大小 33 互质，
	// 于是每翻几页就会正好从某一毫秒组中间切开。
	for i := 0; i < total; i++ {
		proc.recordHistoryAt(1000+int64(i/7), fmt.Sprintf("%04X", i), "rx")
	}

	full := historyOf(t, pm.GetHistory(proc.id))
	if len(full) != total {
		t.Fatalf("整环应当有 %d 条, got %d", total, len(full))
	}

	var got []HistoryEntry
	var before int64
	var skip, pages int
	for {
		pages++
		if pages > 200 {
			t.Fatal("翻页没有收敛")
		}
		res := pm.GetHistoryPage(proc.id, 33, before, skip)
		page := historyOf(t, res)
		if len(page) == 0 {
			break
		}
		got = append(append([]HistoryEntry{}, page...), got...)
		if !res["hasMore"].(bool) {
			break
		}
		// 游标按「整份缓存」算，与前端一致
		before, skip = cursorOf(got)
	}

	if len(got) != len(full) {
		t.Fatalf("翻页重建出 %d 条，整环是 %d 条（漏了或多给了）", len(got), len(full))
	}
	for i := range got {
		if got[i].Hex != full[i].Hex || got[i].TsMs != full[i].TsMs || got[i].Direction != full[i].Direction {
			t.Fatalf("第 %d 条对不上: 翻页 %+v vs 整环 %+v", i, got[i], full[i])
		}
	}
	if pages < 3 {
		t.Fatalf("500 条按 33 条一页至少要翻 15 页, got %d", pages)
	}
}

// 全部落在同一毫秒：只用 `ts <` 做游标会把这一组剩下的 67 条永久丢掉，
// 这是游标设计里最容易写错的一处。
func TestHistoryPagingSingleMsGroup(t *testing.T) {
	pm, proc := newHistoryProc(t)

	const total = 100
	for i := 0; i < total; i++ {
		proc.recordHistoryAt(5000, fmt.Sprintf("%04X", i), "rx")
	}

	var got []HistoryEntry
	var before int64
	var skip int
	for i := 0; i < 20; i++ {
		res := pm.GetHistoryPage(proc.id, 33, before, skip)
		page := historyOf(t, res)
		if len(page) == 0 {
			break
		}
		got = append(append([]HistoryEntry{}, page...), got...)
		if !res["hasMore"].(bool) {
			break
		}
		before, skip = cursorOf(got)
		if before != 5000 {
			t.Fatalf("同一毫秒组的游标应当始终是 5000, got %d", before)
		}
	}
	if len(got) != total {
		t.Fatalf("同一毫秒组翻页重建出 %d 条, 期望 %d", len(got), total)
	}
	for i := range got {
		if want := fmt.Sprintf("%04X", i); got[i].Hex != want {
			t.Fatalf("第 %d 条应当是 %s, got %s", i, want, got[i].Hex)
		}
	}
}

// limit<=0 是 CLI/MCP 的历史行为：不限量，一次给全。
func TestHistoryPageUnlimitedKeepsOldBehaviour(t *testing.T) {
	pm, proc := newHistoryProc(t)
	for i := 0; i < 20; i++ {
		proc.recordHistoryAt(int64(1000+i), "AABB", "rx")
	}

	res := pm.GetHistory(proc.id)
	if n := len(historyOf(t, res)); n != 20 {
		t.Fatalf("不限量应当回全部 20 条, got %d", n)
	}
	if res["hasMore"].(bool) {
		t.Fatal("不限量时 hasMore 应为 false")
	}
	if res["oldestTsMs"].(int64) != 1000 {
		t.Fatalf("oldestTsMs 应当是 1000, got %v", res["oldestTsMs"])
	}

	// 空环：不能 panic，也不能伪造 hasMore
	empty, _ := pm.Create("single", "", SerialConfig{}, nil, false)
	if err := pm.ClearHistory(empty.id); err != nil {
		t.Fatalf("清空历史失败: %v", err)
	}
	res2 := pm.GetHistory(empty.id)
	if n := len(historyOf(t, res2)); n != 0 {
		t.Fatalf("空环应当回 0 条, got %d", n)
	}
	if _, ok := res2["oldestTsMs"]; ok {
		t.Fatal("空环不该有 oldestTsMs")
	}
}

// 环写满后历史必须继续前进。修复前 recordHistory 忽略 Write 的返回值，
// 5MB 一写满历史就**永久停住**，「往回翻」翻到的永远是同一段。
func TestHistoryRingDoesNotFreezeWhenFull(t *testing.T) {
	pm, proc := newHistoryProc(t)

	// 每包 4KB 数据 → 5MB 的环装得下约 1280 条，这里写 2000 条逼出挤出路径。
	const total = 2000
	hexData := strings.Repeat("AB", 4096)
	for i := 0; i < total; i++ {
		proc.recordHistoryAt(1000+int64(i), hexData, "rx")
	}
	if d := proc.ringDrops.Load(); d != 0 {
		t.Fatalf("这些包都装得下，不该有丢弃: %d", d)
	}

	res := pm.GetHistoryPage(proc.id, 3, 0, 0)
	page := historyOf(t, res)
	if len(page) != 3 {
		t.Fatalf("应当回 3 条, got %d", len(page))
	}
	if got := page[2].TsMs; got != 1000+total-1 {
		t.Fatalf("环里最新一条应当 ts=%d，说明写满后停住了, got %d", 1000+total-1, got)
	}
	if !res["hasMore"].(bool) {
		t.Fatal("环里还有更早的, hasMore 应为 true")
	}
	// 最旧的那条必须是「最新的 5MB」，而不是「最早的 5MB」
	all := historyOf(t, pm.GetHistory(proc.id))
	if got := all[0].TsMs; got < 1000+total-1500 {
		t.Fatalf("环里留下的应当是最新一段, 最旧一条 ts=%d 太旧", got)
	}
}
