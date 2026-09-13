package main

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

// BenchmarkHistoryRead 量化「一次历史读取要付多少」——这是 0.7.5.5 分页改动存在的理由。
//
// 背景：整环 5MB ≈ 19 万条，`session.history` 一次把整环返回给界面，这条路径上的
// 开销是「环里逐包复制 + 逐条建 HistoryEntry + JSON 序列化 + 过桥 + 前端逐条 decode」。
// 界面一次只看得见一屏，分页只取其中最后一页。
//
// 跑法：go test ./daemon/ -bench BenchmarkHistoryRead -benchmem -run XXX
func BenchmarkHistoryRead(b *testing.B) {
	setHistoryEnabled(false)
	b.Cleanup(func() { setHistoryEnabled(true) })

	pm := NewProcessManager()
	b.Cleanup(pm.DestroyAll)

	proc, err := pm.Create("single", "", SerialConfig{}, nil, false)
	if err != nil || proc == nil {
		b.Fatalf("建进程失败: %v", err)
	}
	_ = pm.ClearHistory(proc.id)

	// 8 字节数据帧 → 每条 11+16 = 27 字节，5MB 环约 19 万条
	const payload = "A1B2C3D4E5F60718"
	const frames = 200000
	for i := 0; i < frames; i++ {
		// 每 50 帧共用一毫秒，制造真实的时间戳撞车
		proc.recordHistoryAt(1700000000000+int64(i/50), payload, "rx")
	}

	packets := proc.historyRing.Snapshot()
	b.Logf("环里 %d 条 8 字节数据帧，环容量 %d 字节", len(packets), proc.historyRing.Capacity())

	// 游标取环正中间那一条的时间戳 —— 往回翻时最坏的一步（要走完游标之前的所有包）
	midTs := int64(0)
	if len(packets) > 0 {
		midTs = int64(binary.LittleEndian.Uint64(packets[len(packets)/2][0:8]))
	}

	report := func(name string, res map[string]any) {
		entries, _ := res["history"].([]HistoryEntry)
		raw, _ := json.Marshal(res)
		b.Logf("%s: %d 条 → JSON %.2f MB", name, len(entries), float64(len(raw))/(1024*1024))
	}

	b.Run("整环不限量", func(b *testing.B) {
		b.ReportAllocs()
		var res map[string]any
		for i := 0; i < b.N; i++ {
			res = pm.GetHistory(proc.id)
		}
		report("整环", res)
	})

	b.Run("分页10000条", func(b *testing.B) {
		b.ReportAllocs()
		var res map[string]any
		for i := 0; i < b.N; i++ {
			res = pm.GetHistoryPage(proc.id, 10000, 0, 0)
		}
		report("一页 10000", res)
	})

	b.Run("分页5000条", func(b *testing.B) {
		b.ReportAllocs()
		var res map[string]any
		for i := 0; i < b.N; i++ {
			res = pm.GetHistoryPage(proc.id, 5000, 0, 0)
		}
		report("一页 5000", res)
	})

	b.Run("游标在环中部", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = pm.GetHistoryPage(proc.id, 5000, midTs, 1)
		}
	})
}
