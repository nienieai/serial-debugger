package main

import (
	"fmt"
	"testing"
	"time"

	"go.bug.st/serial"

	"github.com/nienieai/serial-debugger/protocol"
	"github.com/nienieai/serial-debugger/ringbuf"
)

// stubPort 只实现写循环需要的那几个方法，让 Tx 来源能端到端跑通
// （sendJob → writeLoop → 广播事件 + 历史环），不必接真实串口。
type stubPort struct {
	written [][]byte
}

func (s *stubPort) SetMode(*serial.Mode) error { return nil }
func (s *stubPort) Read(p []byte) (int, error) { return 0, fmt.Errorf("stub: no data") }
func (s *stubPort) Drain() error               { return nil }
func (s *stubPort) ResetInputBuffer() error    { return nil }
func (s *stubPort) ResetOutputBuffer() error   { return nil }
func (s *stubPort) SetDTR(bool) error          { return nil }
func (s *stubPort) SetRTS(bool) error          { return nil }
func (s *stubPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (s *stubPort) SetReadTimeout(time.Duration) error { return nil }
func (s *stubPort) Break(time.Duration) error          { return nil }
func (s *stubPort) Close() error                       { return nil }

func (s *stubPort) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	s.written = append(s.written, cp)
	return len(p), nil
}

// 一次发送要同时体现在两个地方：广播的 Tx 事件（实时，所有订阅客户端）
// 与历史环（GUI 回补 / CLI / MCP 读历史）。两者的来源必须一致。
func TestWriteLoopTagsTxOrigin(t *testing.T) {
	setDefaultSaveHistory(false)
	t.Cleanup(func() { setDefaultSaveHistory(false) })

	name := fmt.Sprintf("st-test-txorigin-%d", time.Now().UnixNano())
	ring, err := ringbuf.CreateSharedRing(name, 1<<20)
	if err != nil {
		t.Fatalf("建共享环失败: %v", err)
	}
	defer ring.Close()

	proc := &Process{
		id:          "p1",
		status:      "connected",
		mode:        "single",
		config:      SerialConfig{Port: "COM_STUB"},
		port:        &stubPort{},
		sendCh:      make(chan sendJob, 4),
		historyRing: ring,
		historyName: name,
	}
	proc.startTime = time.Now()

	var events []RxTxMessage
	proc.sendCh <- sendJob{raw: []byte{0xAA, 0xBB}, format: "hex", origin: "cli-deadbeef"}
	proc.sendCh <- sendJob{raw: []byte{0xCC}, format: "text", origin: ""}
	close(proc.sendCh)

	proc.writeLoop(func(m RxTxMessage) { events = append(events, m) }, func(protocol.Event) {})

	if len(events) != 2 {
		t.Fatalf("广播了 %d 条, want 2", len(events))
	}
	if events[0].Direction != "tx" || events[0].Origin != "cli-deadbeef" {
		t.Fatalf("第 1 条事件: dir=%q origin=%q", events[0].Direction, events[0].Origin)
	}
	if events[1].Origin != "" {
		t.Fatalf("无来源的发送不应带 origin, got %q", events[1].Origin)
	}

	entries := parseHistoryPackets(ring.Snapshot())
	if len(entries) != 2 {
		t.Fatalf("历史 %d 条, want 2", len(entries))
	}
	if entries[0].Hex != "AABB" || entries[0].Origin != "cli-deadbeef" {
		t.Fatalf("第 1 条历史: %+v", entries[0])
	}
	if entries[1].Origin != "" {
		t.Fatalf("第 2 条历史不应带来源: %+v", entries[1])
	}
}
