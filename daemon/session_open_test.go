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

	// List 里必须带 sessionPath：前端靠它区分「文件会话」和普通串口会话，
	// 否则会给这个进程再建一个多余标签页（GUI 侧 syncDaemonSessions 直接跳过）。
	var listed bool
	for _, e := range pm.List() {
		if e["processId"] != proc.id {
			continue
		}
		listed = true
		if got, _ := e["sessionPath"].(string); got != sess {
			t.Fatalf("List 的 sessionPath = %q，期望 %q", got, sess)
		}
	}
	if !listed {
		t.Fatal("List 里找不到刚建的文件会话进程")
	}
}

// 帧格式与流控要跟着工程回来：按 19200·7E2 + RTS/CTS 录的工程，推回守护进程
// 后进程的配置必须是同一份，否则用户点「打开串口」按的是 8N1，一开就是乱码。
func TestOpenSessionRestoresSerialParams(t *testing.T) {
	root := t.TempDir()
	sess := filepath.Join(root, "p7e2")
	s, err := sessionfile.CreateSession(sess, sessionfile.Settings{
		Port: "COM4", Baud: 19200,
		DataBits: 7, StopBits: "2", Parity: "even", FlowControl: "rts_cts",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	pm := NewProcessManager()
	pm.counter.Store(1500)
	t.Cleanup(pm.DestroyAll)

	proc, _, err := pm.OpenSession(sess)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	want := SerialConfig{Port: "COM4", Baud: 19200, DataBits: 7, StopBits: "2", Parity: "even", FlowControl: "rts_cts"}
	if proc.config != want {
		t.Fatalf("config = %+v，期望 %+v", proc.config, want)
	}
	// List 也要带上：GUI 的工具栏与悬浮窗都从这里取值
	var entry map[string]any
	for _, e := range pm.List() {
		if e["processId"] == proc.id {
			entry = e
		}
	}
	if entry == nil {
		t.Fatal("List 里找不到刚打开的工程")
	}
	if entry["flowControl"] != "rts_cts" || entry["dataBits"] != 7 || entry["parity"] != "even" {
		t.Fatalf("List 参数不全: %v %v %v", entry["flowControl"], entry["dataBits"], entry["parity"])
	}
}

// 用户改过名的工程（labelCustom）打开时用改过的名字，不是目录名。
// 目录名会跟着改名走，所以这两种情况下界面上看到的是同一串 —— 守的是
// 「改名之后目录没跟着动（比如被占用改不了）」时名字不至于丢。
func TestOpenSessionUsesCustomLabel(t *testing.T) {
	root := t.TempDir()
	sess := filepath.Join(root, "20261106_101010_abcd1234")
	s, err := sessionfile.CreateSession(sess, sessionfile.Settings{Port: "COM4", Baud: 9600})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	doc, err := sessionfile.LoadSettingsDoc(sess)
	if err != nil {
		t.Fatal(err)
	}
	doc.Label, doc.LabelCustom = "现场调试", true
	if err := sessionfile.SaveSettingsDoc(sess, doc); err != nil {
		t.Fatal(err)
	}

	pm := NewProcessManager()
	pm.counter.Store(1600)
	t.Cleanup(pm.DestroyAll)

	proc, _, err := pm.OpenSession(sess)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if got := proc.Name(); got != "现场调试" {
		t.Fatalf("名字 = %q，期望 settings.json 里的现场调试", got)
	}
	if !proc.HasCustomName() {
		t.Fatal("改过的名字应当报告为自定义名")
	}
}

// 没记端口的工程：建成空闲进程，端口留空让用户自己选。
//
// 不拿目录名冒充串口名 —— 会话页显示「单端口空闲」比显示一个假端口诚实，
// 而且点「打开串口」时不会拿目录名去开设备。
func TestOpenSessionWithoutPort(t *testing.T) {
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
	if proc.config.Port != "" {
		t.Fatalf("端口 = %q，期望留空（工程里没记端口）", proc.config.Port)
	}
	if proc.status != "idle" {
		t.Fatalf("status = %q，期望 idle", proc.status)
	}
}

// 单个 .stlog 是包文件、不是工程：守护进程必须拒绝，而且不能留下半个进程。
//
// 前端的 _stlogOpen 已经不会把它推过来（单文件一律本地读），这里守的是别的
// 客户端（CLI / MCP）绕过去的情况。
func TestOpenSessionRejectsSingleFile(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg-0001.stlog")
	w, err := sessionfile.CreateWriter(pkg, sessionfile.Settings{Port: "COM7"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1000, sessionfile.DirRx, sessionfile.OriginNone, "", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	pm := NewProcessManager()
	t.Cleanup(pm.DestroyAll)

	proc, n, err := pm.OpenSession(pkg)
	if err == nil {
		t.Fatalf("单文件应当被拒绝，却建出了进程（%d 条）", n)
	}
	if proc != nil {
		t.Fatal("拒绝时不该返回进程")
	}
	if got := len(pm.List()); got != 0 {
		t.Fatalf("拒绝后进程表应当为空，实际 %d 个", got)
	}
}

// 空目录 / 普通目录也不是工程：同样拒绝，理由要能说清缺什么。
func TestOpenSessionRejectsPlainDir(t *testing.T) {
	pm := NewProcessManager()
	t.Cleanup(pm.DestroyAll)

	dir := t.TempDir()
	if _, _, err := pm.OpenSession(dir); err == nil {
		t.Fatal("普通目录应当被拒绝")
	}
	if got := len(pm.List()); got != 0 {
		t.Fatalf("拒绝后进程表应当为空，实际 %d 个", got)
	}
}
