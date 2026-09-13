package main

import (
	"encoding/binary"
	"testing"
)

// 队列条目的 delay 字段有三个地方各自判断，必须互相一致：
//   EncodeEntry            写进发送环时怎么规格化
//   DecodeEntryContentOnly trigger 路径剥头时怎么校验
//   queueSendLoop          实际等待时怎么取
// 一旦不一致就会出现「写 0 等于 1 秒」「delay=1~4 的条目被当成人肉数据发出去」这类
// 现象（外部测试报告 0.7.5.5 轮 §八 报的是前者）。

// trigger 路径（send.trigger，raw=false）靠 DecodeEntryContentOnly 剥掉条目头。
// 它的校验范围必须覆盖编码器允许写出的全部取值，否则那些条目会被当成「不是条目」，
// 整段二进制（版本+标志+delay+note_len）原样发给串口。
func TestDecodeEntryContentOnlyStripsHeaderForEveryEncodableDelay(t *testing.T) {
	const content = "HELLO"
	for _, delay := range []int{0, 1, 2, 4, 5, 10, 999, 1000, 60000} {
		pkt := EncodeEntry(MultistrEntry{Enabled: true, Content: content, Delay: delay})
		got := string(DecodeEntryContentOnly(pkt))
		if got != content {
			t.Errorf("delay=%d：剥头后应当是 %q，实际 %q（%d 字节，头没被剥掉）",
				delay, content, got, len(got))
		}
	}
}

// 非条目数据必须原样返回（这是这个函数存在的意义：trigger 也用于直接发送）
func TestDecodeEntryContentOnlyPassesThroughRawData(t *testing.T) {
	raw := []byte("PING\r\n")
	if got := DecodeEntryContentOnly(raw); string(got) != string(raw) {
		t.Errorf("普通数据不该被改动: %q → %q", raw, got)
	}
}

// 编码器写出来的 delay 必须落在文档承诺的 0–60000 内，且原样可读回
func TestEncodeEntryDelayNormalization(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, 0},         // 0 = 不额外延时（显式写 0 与省略由 API 边界区分）
		{1, 1},         // 1 ms 合法：GUI 的输入下限就是 1
		{1000, 1000},   //
		{60000, 60000}, //
		{70000, 60000}, // 超过上限夹住
		{-5, 0},        // 负数按 0 处理（不额外延时）
	}
	for _, c := range cases {
		pkt := EncodeEntry(MultistrEntry{Enabled: true, Content: "X", Delay: c.in})
		got := int(binary.LittleEndian.Uint16(pkt[2:4]))
		if got != c.want {
			t.Errorf("delay %d 应当写成 %d，实际 %d", c.in, c.want, got)
		}
		e, err := DecodeEntry(pkt)
		if err != nil {
			t.Fatalf("delay %d：解码失败 %v", c.in, err)
		}
		if e.Delay != c.want {
			t.Errorf("delay %d：读回应当是 %d，实际 %d", c.in, c.want, e.Delay)
		}
		if e.Content != "X" {
			t.Errorf("delay %d：内容错位 %q", c.in, e.Content)
		}
	}
}
