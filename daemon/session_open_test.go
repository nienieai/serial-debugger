package main

import (
	"path/filepath"
	"testing"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// 把 .stlog 推给守护进程：应当建出一个进程，模式取自文件，数据灌进它的环。
func TestOpenSessionAsProcess(t *testing.T) {
	root := t.TempDir()
	sess := filepath.Join(root, "20261030_104512_ab12cd34")
	s, err := sessionfile.CreateSession(sess, sessionfile.Settings{
		Label: "A↔B", Mode: sessionfile.ModeForward,
		Port: "COM4", PortB: "COM14", Baud: 115200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(1000, sessionfile.DirP1, sessionfile.OriginNone, "", []byte("A->B")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(1010, sessionfile.DirP2, sessionfile.OriginNone, "", []byte("B->A!")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	pm := NewProcessManager()
	t.Cleanup(pm.DestroyAll)

	proc, n, err := pm.OpenSession(sess)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if proc == nil {
		t.Fatal("没建出进程")
	}
	if proc.mode != "forward" {
		t.Fatalf("mode = %q，期望 forward（取自文件）", proc.mode)
	}
	if proc.config.Port != "COM4" || proc.forwardConfigB.Port != "COM14" {
		t.Fatalf("端口 = %q/%q，期望 COM4/COM14", proc.config.Port, proc.forwardConfigB.Port)
	}
	if proc.sessionPath != sess {
		t.Fatalf("sessionPath = %q，期望 %q", proc.sessionPath, sess)
	}
	if n != 2 {
		t.Fatalf("灌入 %d 条，期望 2", n)
	}

	proc.ringBufMu.Lock()
	pkts := proc.historyRing.Snapshot()
	proc.ringBufMu.Unlock()
	if len(pkts) != 2 {
		t.Fatalf("环里 %d 条，期望 2（Create 的「端口已声明」应被清掉）", len(pkts))
	}
	e0, ok := decodeHistoryPacket(pkts[0])
	if !ok || e0.Direction != "1" {
		t.Fatalf("第一条方向 = %q，期望 \"1\"（A→B）", e0.Direction)
	}
	e1, ok := decodeHistoryPacket(pkts[1])
	if !ok || e1.Direction != "2" {
		t.Fatalf("第二条方向 = %q，期望 \"2\"（B→A）", e1.Direction)
	}

	// 通过正常会话接口能读到
	res := pm.GetHistoryPage(proc.id, 10, 0, 0)
	if res == nil {
		t.Fatal("GetHistoryPage 返回 nil")
	}
}

// 单口文件：mode 保持 single，没记端口时用文件名兜底（只影响显示）。
func TestOpenSessionSingleWithoutPort(t *testing.T) {
	root := t.TempDir()
	sess := filepath.Join(root, "s1")
	s, err := sessionfile.CreateSession(sess, sessionfile.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Append(1, sessionfile.DirRx, sessionfile.OriginNone, "", []byte("x"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	pm := NewProcessManager()
	t.Cleanup(pm.DestroyAll)
	proc, n, err := pm.OpenSession(sess)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if proc.mode != "single" || n != 1 {
		t.Fatalf("mode=%q n=%d，期望 single/1", proc.mode, n)
	}
	if proc.config.Port == "" {
		t.Fatal("端口兜底名不该为空")
	}
}
