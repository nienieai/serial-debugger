package main

import (
	"strings"
	"testing"
	"time"
)

// newAutoSendTestProcess 造一个带发送环的空闲进程，用于 autosend 测试。
//
// 不落盘历史：historyDir() 会指向测试二进制所在目录，开着会往那里堆 *.log。
func newAutoSendTestProcess(t *testing.T) *Process {
	t.Helper()
	setHistoryEnabled(false)
	t.Cleanup(func() { setHistoryEnabled(true) })

	pm := NewProcessManager()
	t.Cleanup(pm.DestroyAll)

	proc, err := pm.Create("single", "", SerialConfig{}, nil, false)
	if err != nil {
		t.Fatalf("创建进程失败: %v", err)
	}
	t.Cleanup(func() { _ = pm.Destroy(proc.id) })
	return proc
}

// writeTestEntries 往发送环里装载条目（与 MultistrWriteEntries 同路径）。
func writeTestEntries(t *testing.T, proc *Process, entries []MultistrEntry) {
	t.Helper()
	proc.sendRingMu.Lock()
	err := WriteAllEntries(proc.sendRing, entries)
	proc.sendRingMu.Unlock()
	if err != nil {
		t.Fatalf("装载条目失败: %v", err)
	}
}

// TestAutoSendOnIdleProcessDoesNotPanic 确认空闲进程（未连接串口）上跑自动发送
// 不会打崩守护进程。
//
// 为什么重要：writePortSilent 此前直接解引用 p.port，而空闲进程的 port 是 nil
// 接口。生产路径上 AutoSendStart 有 status=="connected" 守卫，但**自动发送跑
// 到一半进程被断开**时循环仍会 tick，那次解引用就是一次 nil panic——而 dispatch
// 路径没有 recover、守护进程又是机器级单例，一崩即所有会话与串口一起断。
func TestAutoSendOnIdleProcessDoesNotPanic(t *testing.T) {
	proc := newAutoSendTestProcess(t)
	writeTestEntries(t, proc, []MultistrEntry{{Content: "ONE", Enabled: true}})

	if err := proc.startAutoSend(10, "queue", true); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	t.Cleanup(proc.stopAutoSend)

	// 让它至少 tick 几轮；有 guard 时应记为发送错误，无 guard 时进程直接 panic。
	time.Sleep(120 * time.Millisecond)

	st := proc.getAutoSendStatus()
	if st.SendCount != 0 {
		t.Errorf("没有串口时不应计入成功发送，实际 sendCount=%d", st.SendCount)
	}
	if st.ErrorCount == 0 {
		t.Errorf("没有串口时每次尝试都应计入发送错误，实际 errorCount=0")
	}
}

// TestAutoSendQueueRejectsEmptyQueue 确认 queue 模式在空队列时明确报错。
//
// 为什么重要：此前返回 success 然后空转——一个字节不发、零告警。外部测试报告
// 观察到的「autosend 不发送数据」里就混着这一种：使用者看到 success、看到
// enabled:true，却什么都发不出去，与「发送路径坏掉」无法区分。
func TestAutoSendQueueRejectsEmptyQueue(t *testing.T) {
	proc := newAutoSendTestProcess(t)

	err := proc.startAutoSend(200, "queue", true)
	if err == nil {
		t.Fatal("空队列时 queue 模式应报错，而不是静默成功")
	}
	if !strings.Contains(err.Error(), "队列为空") {
		t.Fatalf("错误信息应说明队列为空，实际: %v", err)
	}
	// 失败后不应留在「已启用」状态，否则后续调用会被 already running 挡住。
	if proc.getAutoSendStatus().Enabled {
		t.Fatal("启动失败后不应仍处于启用状态")
	}
}

// TestAutoSendQueueWithEntriesStarts 确认有条目时 queue 模式能正常启动。
func TestAutoSendQueueWithEntriesStarts(t *testing.T) {
	proc := newAutoSendTestProcess(t)
	writeTestEntries(t, proc, []MultistrEntry{{Content: "ONE", Enabled: true}})

	if err := proc.startAutoSend(200, "queue", true); err != nil {
		t.Fatalf("有条目时应能启动: %v", err)
	}
	t.Cleanup(proc.stopAutoSend)
}

// TestAutoSendStartResetsCounters 确认 start 会清零计数。
//
// 为什么重要：此前 start 不复位，sendCount/errorCount 跨轮累加，于是
// 「这一轮到底发了多少」根本读不出来——实测上一轮 queue 留下的 sendCount=26
// 会原样出现在下一轮 single 的 status 里，看起来像「刚启动就已经发了 26 次」。
func TestAutoSendStartResetsCounters(t *testing.T) {
	proc := newAutoSendTestProcess(t)

	// 伪造上一轮的残留计数
	proc.autoSendSendCount.Store(26)
	proc.autoSendErrorCount.Store(7)
	proc.autoSendMu.Lock()
	proc.autoSendLastSend = time.Now()
	proc.autoSendMu.Unlock()

	if err := proc.startAutoSend(200, "single", false); err != nil {
		t.Fatalf("single 模式应能启动: %v", err)
	}
	t.Cleanup(proc.stopAutoSend)

	st := proc.getAutoSendStatus()
	if st.SendCount != 0 {
		t.Errorf("启动后 sendCount 应归零，实际 %d", st.SendCount)
	}
	if st.ErrorCount != 0 {
		t.Errorf("启动后 errorCount 应归零，实际 %d", st.ErrorCount)
	}
	if st.LastSend != "" {
		t.Errorf("启动后 lastSend 应清空，实际 %q", st.LastSend)
	}
}

// TestAutoSendIntervalIsHonored 确认 intervalMs 真的被记录并生效。
//
// 为什么重要：外部测试报告称「queue 模式下 intervalMs 不生效，恒定约 1 条/秒」
// 并列为 P0。实测证伪（200ms → 块间隔中位数 0.201s），这里从调度侧再钉一道：
// 参数进了 startAutoSend 就必须能在 status 里读回来。
func TestAutoSendIntervalIsHonored(t *testing.T) {
	proc := newAutoSendTestProcess(t)
	writeTestEntries(t, proc, []MultistrEntry{{Content: "ONE", Enabled: true}})

	if err := proc.startAutoSend(1234, "queue", false); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	t.Cleanup(proc.stopAutoSend)

	if got := proc.getAutoSendStatus().IntervalMs; got != 1234 {
		t.Fatalf("intervalMs 应为 1234，实际 %d（参数被丢弃）", got)
	}

	// 运行中改间隔也应生效
	if err := proc.setAutoSendInterval(500); err != nil {
		t.Fatalf("修改间隔失败: %v", err)
	}
	if got := proc.getAutoSendStatus().IntervalMs; got != 500 {
		t.Fatalf("改后 intervalMs 应为 500，实际 %d", got)
	}
}
