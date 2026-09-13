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

// 未知的长选项必须报错，不能被当成端口名。
//
// 此前 `probe --json` 会把 `--json` 当成端口，结果里多出一条
// 「端口 --json 打不开」的 skipped，读者会以为真有个端口有问题
// （外部测试报告 0.7.5.3 轮 §4.2）。
func TestParseProbeArgsRejectsUnknownFlags(t *testing.T) {
	for _, bad := range [][]string{{"--json"}, {"-j"}, {"COM3", "--json"}, {"--nope=1"}} {
		ports, _, _, err := parseProbeArgs(bad)
		if err == nil {
			t.Errorf("%v 应报错，实际把 %v 当成了端口", bad, ports)
		}
	}
}

// 缺值的 --config / --budget 要报清楚，而不是退化成端口名。
func TestParseProbeArgsRejectsMissingValues(t *testing.T) {
	if _, _, _, err := parseProbeArgs([]string{"--config"}); err == nil {
		t.Error("--config 缺值应报错")
	}
	if _, _, _, err := parseProbeArgs([]string{"--budget"}); err == nil {
		t.Error("--budget 缺值应报错")
	}
	if _, _, _, err := parseProbeArgs([]string{"--budget", "abc"}); err == nil {
		t.Error("--budget 非数字应报错")
	}
	if _, _, _, err := parseProbeArgs([]string{"--budget", "0"}); err == nil {
		t.Error("--budget 0 应报错")
	}
}

// 正常用法必须照旧工作（端口可以有多个，选项可以混在中间）。
func TestParseProbeArgsAcceptsValidForms(t *testing.T) {
	ports, cfg, budget, err := parseProbeArgs([]string{"COM3", "COM4", "--config", "x.toml", "--budget", "3000"})
	if err != nil {
		t.Fatalf("正常参数不应报错: %v", err)
	}
	if len(ports) != 2 || ports[0] != "COM3" || ports[1] != "COM4" {
		t.Errorf("端口解析错误: %v", ports)
	}
	if cfg != "x.toml" {
		t.Errorf("configPath 解析错误: %q", cfg)
	}
	if budget != 3000 {
		t.Errorf("budgetMs 解析错误: %d", budget)
	}

	ports2, cfg2, budget2, err2 := parseProbeArgs(nil)
	if err2 != nil || len(ports2) != 0 || cfg2 != "" || budget2 != 0 {
		t.Errorf("无参数应得到空结果: ports=%v cfg=%q budget=%d err=%v", ports2, cfg2, budget2, err2)
	}
}
