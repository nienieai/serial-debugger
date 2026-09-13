package main

import "testing"

// MCP 边界的 delay 归一化：省略与显式 0 必须分开。
//
// 外部测试报告 0.7.5.5 轮 §八：CLI/MCP 都把 delay 当 int 直传，JSON 的零值语义
// 让「没写」和「写了 0」都变成 0，守护进程再 `delay < 1 → 1000` 兜底，于是
// 显式写 0 等于等 1 秒。
func TestNormalizeEntryDelays(t *testing.T) {
	entries := []map[string]any{
		{"content": "A"},                          // 省略 → 1000
		{"content": "B", "delay": nil},            // null → 1000
		{"content": "C", "delay": float64(0)},     // 显式 0 → 0
		{"content": "D", "delay": float64(5)},     // 原样
		{"content": "E", "delay": float64(70000)}, // 夹到 60000
	}
	if err := normalizeEntryDelays(entries); err != nil {
		t.Fatalf("归一化失败: %v", err)
	}
	want := []int{1000, 1000, 0, 5, 60000}
	for i, w := range want {
		got := entries[i]["delay"]
		n, ok := got.(int)
		if !ok {
			t.Fatalf("第 %d 条 delay 类型不对: %T", i+1, got)
		}
		if n != w {
			t.Errorf("第 %d 条 delay 应当是 %d，实际 %d", i+1, w, n)
		}
	}
}

func TestNormalizeEntryDelaysRejectsBadValues(t *testing.T) {
	cases := []struct {
		name  string
		entry map[string]any
	}{
		{"负数", map[string]any{"content": "A", "delay": float64(-1)}},
		{"非数字", map[string]any{"content": "A", "delay": "abc"}},
		{"小数", map[string]any{"content": "A", "delay": 1.5}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := normalizeEntryDelays([]map[string]any{c.entry}); err == nil {
				t.Fatalf("%s 应当报错", c.name)
			}
		})
	}
}
