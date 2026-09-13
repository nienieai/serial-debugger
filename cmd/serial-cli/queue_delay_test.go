package main

import "testing"

// 省略 delay 与显式写 0 必须是两件事：前者落回文档承诺的默认 1000 ms，
// 后者是「不额外延时，立即发下一条」。
//
// 外部测试报告 0.7.5.5 轮 §八：`delay` 显式写 0 时 20 条耗时 19.2 s（≈961 ms/条），
// 与 delay=1000 几乎一致 —— 因为 JSON 零值语义把「显式 0」与「省略」混成了一件
// 事，再被 `delay < 1 → 1000` 落回默认。要连发只能写 1，与直觉相反。
func TestQueueEntryOmittedVersusExplicitZero(t *testing.T) {
	got, err := parseQueueEntries([]byte(`[
		{"content":"A"},
		{"content":"B","delay":0},
		{"content":"C","delay":1},
		{"content":"D","delay":70000}
	]`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := []int{1000, 0, 1, 60000}
	for i, w := range want {
		if got[i].Delay == nil {
			t.Fatalf("第 %d 条 delay 未被解析成确定值", i+1)
		}
		if *got[i].Delay != w {
			t.Errorf("第 %d 条 delay 应当是 %d，实际 %d", i+1, w, *got[i].Delay)
		}
	}
}

// 负数 delay 是写错了，必须报错而不是静默按默认值走
func TestQueueEntryRejectsNegativeDelay(t *testing.T) {
	_, err := parseQueueEntries([]byte(`[{"content":"A","delay":-1}]`))
	if err == nil {
		t.Fatal("负 delay 应当报错")
	}
}
