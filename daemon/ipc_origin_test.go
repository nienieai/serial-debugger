package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/nienieai/serial-debugger/contract"
	"github.com/nienieai/serial-debugger/protocol"
	"github.com/nienieai/serial-debugger/ringbuf"
)

// newOriginTestProc 造一个「已连接」的假进程（只用到 sendCh/sendRing），
// 让 dispatch 层的来源组装能被单独验证，不必接串口。
func newOriginTestProc(t *testing.T) (*IpcServer, *Process) {
	t.Helper()
	pm := NewProcessManager()
	proc := &Process{
		id:     "p1",
		status: "connected",
		mode:   "single",
		sendCh: make(chan sendJob, 4),
	}
	pm.mu.Lock()
	pm.processes[proc.id] = proc
	pm.mu.Unlock()
	return NewIpcServer(pm), proc
}

func newTestSession(source, clientId string) *clientSession {
	return &clientSession{clientId: clientId, source: source, subs: map[string]bool{}}
}

// session.send：数据在请求里，来源直接取会话身份。
func TestDispatchSessionSendCarriesOrigin(t *testing.T) {
	srv, proc := newOriginTestProc(t)
	sess := newTestSession("cli", "cli-deadbeef")

	resp := srv.dispatchForSession(sess, &protocol.Request{
		ID:     1,
		Method: string(contract.SessionSend),
		Params: map[string]any{"processId": proc.id, "data": "AA", "format": "hex"},
	})
	if resp.Error != "" {
		t.Fatalf("session.send 失败: %s", resp.Error)
	}
	select {
	case job := <-proc.sendCh:
		if job.origin != "cli-deadbeef" {
			t.Fatalf("origin=%q, want cli-deadbeef", job.origin)
		}
	case <-time.After(time.Second):
		t.Fatal("没有收到 sendJob")
	}
}

// send.trigger：数据在共享环里，但触发者是这个会话，来源记它。
func TestDispatchSendTriggerCarriesOrigin(t *testing.T) {
	srv, proc := newOriginTestProc(t)

	name := fmt.Sprintf("st-test-trigger-%d", time.Now().UnixNano())
	ring, err := ringbuf.CreateSharedRing(name, 1<<20)
	if err != nil {
		t.Fatalf("建共享环失败: %v", err)
	}
	defer ring.Close()
	if !ring.Write([]byte{0xAA, 0xBB}) {
		t.Fatal("写发送环失败")
	}
	proc.sendRing = ring
	proc.sendRingName = name

	sess := newTestSession("mcp", "mcp-00112233")
	resp := srv.dispatchForSession(sess, &protocol.Request{
		ID:     2,
		Method: string(contract.SendTrigger),
		Params: map[string]any{"processId": proc.id, "raw": true},
	})
	if resp.Error != "" {
		t.Fatalf("send.trigger 失败: %s", resp.Error)
	}
	select {
	case job := <-proc.sendCh:
		if job.origin != "mcp-00112233" {
			t.Fatalf("origin=%q, want mcp-00112233", job.origin)
		}
		if len(job.raw) != 2 || job.format != "trigger" {
			t.Fatalf("job=%+v", job)
		}
	case <-time.After(time.Second):
		t.Fatal("没有收到 sendJob")
	}
}

// 不登记会话的一次性调用（CallOnce）：来源退化成 "<source>-once"，
// 至少能看出是哪个入口发的，而不是被当成引擎来源。
func TestOriginOfOneShot(t *testing.T) {
	if got := originOf(nil, &protocol.Request{Source: "cli"}); got != "cli-once" {
		t.Fatalf("originOf(nil, cli)=%q, want cli-once", got)
	}
	if got := originOf(nil, nil); got != "" {
		t.Fatalf("originOf(nil, nil)=%q, want 空", got)
	}
	if got := originOf(newTestSession("gui", "gui-ab12"), nil); got != "gui-ab12" {
		t.Fatalf("originOf(sess)=%q, want gui-ab12", got)
	}
}
