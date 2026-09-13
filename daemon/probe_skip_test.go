package main

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// testProbeConfig 一条规则一个波特率，便于构造确定的用例。
func testProbeConfig() *ProbeConfig {
	return &ProbeConfig{
		TimeoutMs: 200,
		BaudRates: []int{9600},
		Rules: []ProbeRule{{
			Name: "t", PortPattern: ".*", ProbeHex: "01030008000105C8",
			MatchType: "substring", MatchValue: "ZZZZ", MinResponseLen: 5,
		}},
	}
}

// 「端口已被会话占用」必须出现在 Skipped 里。
// 原实现是裸 continue：返回空 results，与「探测过但没有设备」完全同形，
// 于是设备在线也会被报成「未检测到已知设备」。
func TestProbeOccupiedPortIsReportedNotSilentlyDropped(t *testing.T) {
	out := ProbePorts(
		[]string{"COM_TEST_OCCUPIED"},
		map[string]bool{"COM_TEST_OCCUPIED": true},
		testProbeConfig(), nil, nil, 30*time.Second)

	if len(out.Results) != 0 {
		t.Fatalf("被占用的端口不应产生结果，实际 %d 条", len(out.Results))
	}
	if len(out.Skipped) != 1 {
		t.Fatalf("被占用的端口必须记进 Skipped，实际 %d 条", len(out.Skipped))
	}
	if !strings.Contains(out.Skipped[0].Reason, "占用") {
		t.Errorf("跳过原因应说明是被占用，实际: %q", out.Skipped[0].Reason)
	}
	if out.Attempts != 0 {
		t.Errorf("被占用的端口不应产生探测尝试，实际 %d 次", out.Attempts)
	}
}

// 预算用尽时必须显式说明「还有端口没试」，而不是返回一个空结果了事。
// 真实时钟在纳秒粒度下无法稳定构造「刚好超预算」的时刻，这里替换时钟。
func TestProbeBudgetExhaustedIsReported(t *testing.T) {
	base := time.Now()
	old := probeNow
	calls := 0
	probeNow = func() time.Time {
		calls++
		if calls == 1 { // 函数入口取 start
			return base
		}
		return base.Add(time.Hour) // 之后一律视为已超预算
	}
	defer func() { probeNow = old }()

	out := ProbePorts([]string{"COM_A", "COM_B"}, nil, testProbeConfig(), nil, nil, 5*time.Second)

	if !out.BudgetExhausted {
		t.Error("预算用尽时 BudgetExhausted 应为 true")
	}
	if len(out.Skipped) != 2 {
		t.Fatalf("两个端口都应记进 Skipped，实际 %d 条: %+v", len(out.Skipped), out.Skipped)
	}
	for _, s := range out.Skipped {
		if !strings.Contains(s.Reason, "预算") {
			t.Errorf("跳过原因应提到总预算，实际: %q", s.Reason)
		}
	}
	if out.Attempts != 0 {
		t.Errorf("预算已用尽就不该再尝试，实际 %d 次", out.Attempts)
	}
}

// 同一时刻只允许一次探测；并发的第二个必须说明「本次未执行」，
// 而不是因端口被占返回空结果（那会被读成「没有设备」）。
func TestProbeConcurrentCallReportsBusy(t *testing.T) {
	probeMu.Lock()
	defer probeMu.Unlock()

	out := ProbePorts([]string{"COM_X"}, nil, testProbeConfig(), nil, nil, time.Second)

	if !out.Busy {
		t.Error("并发探测应置 Busy=true")
	}
	if len(out.Results) != 0 {
		t.Errorf("并发探测不应产生结果，实际 %d 条", len(out.Results))
	}
	if len(out.Skipped) != 1 || !strings.Contains(out.Skipped[0].Reason, "正在进行") {
		t.Errorf("应说明已有另一次探测在进行，实际: %+v", out.Skipped)
	}
}

// 串口打不开原先也是裸 continue，连日志都没有。现在必须给出原因。
func TestProbeOpenFailureIsReported(t *testing.T) {
	cfg := testProbeConfig()
	cfg.BaudRates = []int{9600, 19200}

	out := ProbePorts([]string{"COM_DOES_NOT_EXIST_ZZZ"}, nil, cfg, nil, nil, 30*time.Second)

	if len(out.Results) != 0 {
		t.Fatalf("不存在的端口不应产生结果，实际 %d 条", len(out.Results))
	}
	if len(out.Skipped) != 1 {
		t.Fatalf("打不开的端口必须记进 Skipped，实际 %d 条: %+v", len(out.Skipped), out.Skipped)
	}
	if !strings.Contains(out.Skipped[0].Reason, "打不开") {
		t.Errorf("跳过原因应说明打不开，实际: %q", out.Skipped[0].Reason)
	}
	if out.BudgetExhausted {
		t.Error("这只是打不开，不应被算成预算用尽")
	}
}

// 规则名过滤后一条都不剩时，也要说明，而不是返回空结果。
func TestProbeNoMatchingRuleIsReported(t *testing.T) {
	out := ProbePorts([]string{"COM_Y"}, nil, testProbeConfig(), nil, []string{"不存在的规则名"}, 30*time.Second)

	if len(out.Results) != 0 || len(out.Skipped) != 1 {
		t.Fatalf("应记为 1 条跳过，实际 results=%d skipped=%d", len(out.Results), len(out.Skipped))
	}
	if !strings.Contains(out.Skipped[0].Reason, "不存在的规则名") {
		t.Errorf("跳过原因应点名缺失的规则，实际: %q", out.Skipped[0].Reason)
	}
}

// errReader 立刻返回错误，用来验证 probeRead 不再把读取错误丢掉。
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// elapsedMs 必须真的落到返回值上。原先用 defer 改局部变量的字段，
// 而 return 在 defer 之前就完成了拷贝，于是它恒为 0（实测如此）。
// 用可控时钟构造确定的耗时，避免「快调用恰好 0ms」的假通过。
func TestProbeElapsedMsLandsOnReturnValue(t *testing.T) {
	base := time.Now()
	old := probeNow
	calls := 0
	probeNow = func() time.Time {
		calls++
		if calls == 1 {
			return base
		}
		return base.Add(1500 * time.Millisecond)
	}
	defer func() { probeNow = old }()

	out := ProbePorts([]string{"COM_OCC"}, map[string]bool{"COM_OCC": true},
		testProbeConfig(), nil, nil, time.Second)

	if out.ElapsedMs != 1500 {
		t.Fatalf("elapsedMs 应为 1500，实际 %d（defer 改的是命名返回值吗？）", out.ElapsedMs)
	}
}

// 读取错误必须被带出来：此前 probeRead 直接 break 且不返回错误，
// 「串口打开成功但根本读不了」于是也表现为「未检测到已知设备」。
func TestProbeReadSurfacesReadError(t *testing.T) {
	want := errors.New("boom")
	resp, err := probeRead(errReader{err: want}, 10*time.Millisecond)

	if len(resp) != 0 {
		t.Errorf("没有数据时不应有响应，实际 %q", resp)
	}
	if !errors.Is(err, want) {
		t.Fatalf("应返回读取错误，实际: %v", err)
	}
}

// quietReader 永远返回 (0, nil)：这就是静默串口的样子（库的读超时到点）。
type quietReader struct{}

func (quietReader) Read([]byte) (int, error) { return 0, nil }

var _ io.Reader = quietReader{}

// 静默端口不应被当成错误，且必须在预算内返回（不能挂住）。
func TestProbeReadQuietPortIsNotAnError(t *testing.T) {
	start := time.Now()
	resp, err := probeRead(quietReader{}, 20*time.Millisecond)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("读空不是错误，实际: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("静默端口不应有响应，实际 %q", resp)
	}
	// 预算取 max(3×timeout, 1s)
	if elapsed > 3*time.Second {
		t.Errorf("静默端口应在预算内返回，实际耗时 %v", elapsed)
	}
}
