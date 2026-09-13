package main

import (
	"encoding/json"
	"testing"

	"github.com/nienieai/serial-debugger/client"
)

// CLI 的探测输出必须带上 busy —— 文档里承诺了它，并且建议用 `grep busy` 自检。
// 曾经守护进程返回了 busy，而 CLI 序列化时把它丢在门外，导致文档写的自检永远匹配不到
// （外部测试报告 0.7.5.2 轮 §三 点名）。这条测试就是把字段集合锁住。
func TestProbeOutcomeJSONIncludesBusy(t *testing.T) {
	raw, err := json.Marshal(probeOutcomeJSON(&client.ProbeOutcome{
		Skipped: []map[string]any{{"port": "COM9", "reason": "已有另一次设备探测正在进行，本次未执行"}},
		Busy:    true,
	}))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	for _, k := range []string{"results", "skipped", "attempts", "elapsedMs", "budgetExhausted", "busy"} {
		if _, ok := m[k]; !ok {
			t.Errorf("CLI 探测输出缺少字段 %q（文档承诺了它）", k)
		}
	}
	if b, _ := m["busy"].(bool); !b {
		t.Errorf("busy 应为 true，实际 %v", m["busy"])
	}
}

// 非忙时也必须显式给 busy:false（而不是省略），否则 grep busy 依然匹配不到。
func TestProbeOutcomeJSONBusyAlwaysPresent(t *testing.T) {
	m := probeOutcomeJSON(&client.ProbeOutcome{})
	v, ok := m["busy"]
	if !ok {
		t.Fatal("busy 字段必须始终存在")
	}
	if v != false {
		t.Errorf("busy 应为 false，实际 %v", v)
	}
}
