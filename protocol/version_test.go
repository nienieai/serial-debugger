package protocol

import "testing"

// TestProtocolVersionWellFormed 保证常量本身是可解析的 MAJOR.MINOR.PATCH：
// 兼容判定只看 MAJOR.MINOR，多余或缺失的段会让判定悄悄失效。
func TestProtocolVersionWellFormed(t *testing.T) {
	if _, ok := protoMajorMinor(ProtocolVersion); !ok {
		t.Fatalf("ProtocolVersion=%q 不是 MAJOR.MINOR[.PATCH] 形式", ProtocolVersion)
	}
	if got, _ := protoMajorMinor("1"); got != "" {
		t.Fatalf("单段版本号应当解析失败，得到 %q", got)
	}
	if _, ok := protoMajorMinor("0.2.0"); !ok {
		t.Fatal("0.2.0 应当解析成功")
	}
}

// TestCompatible 钉住「只看 MAJOR.MINOR」这条约定：
// 打补丁不该把所有人的守护进程踢下线，换 MINOR 才拒绝。
func TestCompatible(t *testing.T) {
	cases := []struct {
		name   string
		theirs string
		want   bool
	}{
		{"同一个号", ProtocolVersion, true},
		{"补丁号不同（同 MINOR）", "0.2.9", true},
		{"老补丁号（同 MINOR）", "0.2.0", true},
		{"MINOR 不同", "0.3.0", false},
		{"MAJOR 不同", "1.2.0", false},
		{"旧版整数协议号", "1", false},
		{"空串", "", false},
		{"乱写", "v0.2", false},
	}
	for _, c := range cases {
		got := DaemonInfo{ProtocolVersion: c.theirs}.Compatible()
		if got != c.want {
			t.Errorf("%s：对端 %q Compatible()=%v，期望 %v", c.name, c.theirs, got, c.want)
		}
	}
}

// TestDecodeDaemonInfoStringProtocol 确认新报文（字符串协议号）能解出来；
// 旧守护进程回整数时应当解码失败而不是当成兼容 —— 这正是「明确拒绝」的落点。
func TestDecodeDaemonInfoStringProtocol(t *testing.T) {
	info, err := DecodeDaemonInfo(map[string]any{
		"version":         "0.8.4",
		"protocolVersion": "0.2.0",
		"pid":             float64(4242),
		"startTime":       "2026-10-31T00:00:00+08:00",
		"uptimeSec":       float64(90),
	})
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if info.ProtocolVersion != "0.2.0" || !info.Compatible() {
		t.Fatalf("解出的协议号不对: %+v", info)
	}
	if info.PID != 4242 {
		t.Fatalf("PID = %d，期望 4242", info.PID)
	}

	if _, err := DecodeDaemonInfo(map[string]any{"protocolVersion": float64(1)}); err == nil {
		t.Fatal("整数协议号应当解码失败（旧守护进程必须被明确拒绝）")
	}
}
