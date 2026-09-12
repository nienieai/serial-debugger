package client

import (
	"bufio"
	"strings"
	"testing"
	"time"

	"github.com/nienieai/serial-debugger/pipe"
	"github.com/nienieai/serial-debugger/protocol"
)

// fakeDaemonThatRejectsRegister 在守护进程端点上演一个「收到注册就报错退出」
// 的假守护进程：不回连客户端的 resp/sub 管道，而是把失败原因写在注册管道上
// ——这正是真实守护进程 handleRegister 回连失败时的行为
// （daemon/ipc.go 的三处 WriteMessage 错误分支）。
//
// 返回一个停止函数。若端点无法监听（例如真实守护进程正在运行）则返回 nil，
// 调用方应跳过测试而不是给出误导性的失败。
func fakeDaemonThatRejectsRegister(t *testing.T, reason string) func() {
	t.Helper()

	ln, err := pipe.Listen(pipe.Addr)
	if err != nil {
		// 机器上已经有真守护进程在跑，占用同一个管道名。
		return nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// 读掉 register 请求，然后只回错误——绝不回连客户端管道。
		reader := bufio.NewReader(conn)
		if _, err := protocol.ReadMessage(reader); err != nil {
			return
		}
		_ = protocol.WriteMessage(conn, protocol.Response{ID: 0, Error: reason})

		// 给客户端足够时间读到这条消息，再断开。
		time.Sleep(300 * time.Millisecond)
	}()

	return func() {
		ln.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

// TestHandshakeSurfacesDaemonReason 确认守护进程回连失败时，客户端报出的是
// 守护进程给出的**真实原因**，而不是一句没有信息量的超时。
//
// 为什么重要（TODO #29）：客户端此前在等 respCh 的 5 秒里从不读 daemonConn，
// 守护进程辛苦写下的「连接客户端 resp 管道失败」（daemon/ipc.go:483/491）
// 落在一条没人读的管道上被丢弃。用户报的「先开守护进程再开 GUI 有时连不上」
// 因此完全无法定位。这个测试通过**耗时**来区分两条路径：
//
//	修复后：毫秒级返回，且错误信息含守护进程给的原文
//	修复前：一直干等到 handshakeTimeout，错误信息里没有原文
func TestHandshakeSurfacesDaemonReason(t *testing.T) {
	const reason = "cannot connect resp pipe: fake-reason-marker"

	stop := fakeDaemonThatRejectsRegister(t, reason)
	if stop == nil {
		t.Skip("守护进程端点已被占用（可能有真实守护进程在运行），跳过")
	}
	defer stop()

	// 超时设得比较宽松：Windows 命名管道的连接/读取在负载下可能花费数百毫秒，
	// 卡得太紧会变成 flaky。这里要区分的是「读到原因」与「一直干等到超时」，
	// 两者差一个数量级，2 秒足够大也足够小。
	old := handshakeTimeout
	handshakeTimeout = 2 * time.Second
	defer func() { handshakeTimeout = old }()

	start := time.Now()
	_, err := NewDaemonClient("test")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("假守护进程拒绝注册，客户端不应成功")
	}
	t.Logf("客户端总耗时 %.3fms; 错误: %v", float64(elapsed.Microseconds())/1000.0, err)
	if !strings.Contains(err.Error(), reason) {
		t.Fatalf("错误信息里应包含守护进程给出的原因 %q，实际为: %v", reason, err)
	}
	// 必须是快速失败路径，而不是干等到超时。留足余量以容纳管道抖动。
	if elapsed > handshakeTimeout/2 {
		t.Fatalf("应当快速失败（读到 daemonConn 上的错误），实际耗时 %v（超时 %v）", elapsed, handshakeTimeout)
	}
}

// TestHandshakeReasonDoesNotClaimRejection 确认「注册确认没到达」时不会
// 断言成「守护进程拒绝了注册」。
//
// 为什么重要：实测反例——守护进程日志显示它**已经**走完注册（有「已注册」一行，
// 而那一行位于两条回连管道都成功之后），却仍可能没能把确认送到客户端。
// 此时说「守护进程拒绝了注册请求」是事实错误，会把排查方向完全带偏——这正是
// 外部测试报告里点名的那句话。
func TestHandshakeReasonDoesNotClaimRejection(t *testing.T) {
	// 假守护进程收到 register 后**直接关闭**连接，不写任何响应。
	// 真实场景对应「守护进程已注册但确认未送达」。
	ln, err := pipe.Listen(pipe.Addr)
	if err != nil {
		t.Skip("守护进程端点已被占用（可能有真实守护进程在运行），跳过")
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = protocol.ReadMessage(bufio.NewReader(conn)) // 读掉 register
		// 不回任何东西，直接关——模拟确认未送达
	}()

	old := handshakeTimeout
	handshakeTimeout = 2 * time.Second
	defer func() { handshakeTimeout = old }()

	_, err = NewDaemonClient("test")
	if err == nil {
		t.Fatal("守护进程未回送确认，客户端不应成功")
	}
	msg := err.Error()
	if strings.Contains(msg, "拒绝") {
		t.Fatalf("不应断言「拒绝」——守护进程可能已接受注册，实际: %s", msg)
	}
	<-done
}
