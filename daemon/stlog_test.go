package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// findSessions 返回 dir 下所有会话目录（含 manifest.json 的子目录）。
// v2 起一次录制是一个文件夹，不再是单个 .stlog 文件。
func findSessions(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("读历史目录失败: %v", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(p, sessionfile.ManifestName)); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func mustReadStlog(t *testing.T, path string) *sessionfile.FileInfo {
	t.Helper()
	info, err := sessionfile.ReadSession(path)
	if err != nil {
		t.Fatalf("读会话失败 (%s): %v", path, err)
	}
	return info
}

// onlySession 断言目录下恰好一个会话，并返回它。
func onlySession(t *testing.T, dir string) string {
	t.Helper()
	dirs := findSessions(t, dir)
	if len(dirs) != 1 {
		t.Fatalf("会话数 = %d，期望 1 个: %v", len(dirs), dirs)
	}
	return dirs[0]
}

// 端到端：打开「保存历史」→ 写几条 rx/tx/system → 关闭 → 用格式包读回来，
// 逐项核对方向、payload、来源与统计。这条用例是 daemon 侧唯一的集成校验。
func TestStlogMirrorsHistory(t *testing.T) {
	_, proc, dir := newSaveTestProc(t)
	proc.config = SerialConfig{Port: "COM9", Baud: 9600}
	proc.SetSaveHistory(true)

	proc.recordHistoryAt(1000, "AABB", "rx", "")
	proc.recordHistoryAt(1001, "CCDDEE", "tx", "gui-a1b2c3d4")
	proc.recordHistoryAt(1002, "sys.port_opened|COM1|115200", "system", "")
	proc.recordHistoryAt(1003, "0102", "tx", "")
	proc.detachHistoryFile() // 封存：Close 内部 Flush + Sync

	files := findSessions(t, dir)
	if len(files) != 1 {
		t.Fatalf("会话数 = %d，期望 1 个: %v", len(files), files)
	}
	// 目录名按 v2 规则自动生成（时间戳 + id8 = 24 字符），不再和 .log 同名；
	// 与 .log 的配对关系记在 settings.json 的 log 字段里。
	if base := filepath.Base(files[0]); len(base) != 24 {
		t.Fatalf("目录名不像 v2 会话目录: %s", base)
	}
	info := mustReadStlog(t, files[0])
	if info.SettingsDoc == nil || !strings.HasSuffix(info.SettingsDoc.Log, "_"+proc.id+".log") {
		t.Fatalf("settings.json 的 log 字段对不上 .log: %+v", info.SettingsDoc)
	}
	if len(info.Pages) != 1 {
		t.Errorf("页数 = %d，期望 1", len(info.Pages))
	}
	if info.Records != 4 || len(info.Frames) != 4 {
		t.Fatalf("记录/帧 = %d/%d，期望 4/4", info.Records, len(info.Frames))
	}
	if info.Truncated || info.BrokenChain {
		t.Errorf("不该被标记为截断/剪链: truncated=%v brokenChain=%v", info.Truncated, info.BrokenChain)
	}

	wantDirs := []uint8{sessionfile.DirRx, sessionfile.DirTx, sessionfile.DirSystem, sessionfile.DirTx}
	for i, want := range wantDirs {
		if got := info.Frames[i].Dir; got != want {
			t.Errorf("帧 %d 方向 = %#x，期望 %#x", i, got, want)
		}
	}

	wantPayload := [][]byte{
		{0xAA, 0xBB},
		{0xCC, 0xDD, 0xEE},
		[]byte("sys.port_opened|COM1|115200"),
		{0x01, 0x02},
	}
	for i, want := range wantPayload {
		if got := info.Frames[i].Payload; !bytes.Equal(got, want) {
			t.Errorf("帧 %d payload = %X，期望 %X", i, got, want)
		}
	}

	// 来源：只有 tx 那条带 OriginInline，其余应为 OriginNone。
	if got := info.Frames[1].OriginStr; got != "gui-a1b2c3d4" {
		t.Errorf("帧 1 来源 = %q，期望 gui-a1b2c3d4", got)
	}
	if got := info.Frames[1].Origin; got != sessionfile.OriginInline {
		t.Errorf("帧 1 origin 字节 = %d，期望 OriginInline", got)
	}
	for _, i := range []int{0, 2, 3} {
		if got := info.Frames[i].Origin; got != sessionfile.OriginNone {
			t.Errorf("帧 %d origin 字节 = %d，期望 OriginNone", i, got)
		}
	}

	// 统计块：rx 计 2 B、tx 计 3+2 = 5 B。
	if info.RxTotal != 2 || info.TxTotal != 5 {
		t.Errorf("rx/tx 统计 = %d/%d，期望 2/5", info.RxTotal, info.TxTotal)
	}

	// 设置块取建文件那一刻的端口/波特率。
	if info.Settings == nil {
		t.Fatal("设置块为空")
	}
	if info.Settings.Port != "COM9" || info.Settings.Baud != 9600 {
		t.Errorf("设置块 port/baud = %q/%d，期望 COM9/9600", info.Settings.Port, info.Settings.Baud)
	}
	if info.Settings.Label != "COM9" {
		t.Errorf("设置块 label = %q，期望 COM9", info.Settings.Label)
	}
	if info.Settings.UUID == "" || info.Settings.Created == "" {
		t.Errorf("设置块缺 uuid/created: %+v", info.Settings)
	}
}

// 关闭「保存历史」之后不再追加，且不会另建一个会话目录。
func TestStlogStopsAfterDisable(t *testing.T) {
	_, proc, dir := newSaveTestProc(t)
	proc.SetSaveHistory(true)
	proc.recordHistoryAt(2000, "AABB", "rx", "")

	proc.SetSaveHistory(false)
	proc.recordHistoryAt(2001, "CCDD", "rx", "")
	proc.recordHistoryAt(2002, "EEFF", "tx", "")

	files := findSessions(t, dir)
	if len(files) != 1 {
		t.Fatalf("会话数 = %d，期望 1 个: %v", len(files), files)
	}
	info := mustReadStlog(t, files[0])
	if info.Records != 1 {
		t.Errorf("记录数 = %d，期望 1（关闭后不该再追加）", info.Records)
	}
	if got := info.Frames[0].Payload; !bytes.Equal(got, []byte{0xAA, 0xBB}) {
		t.Errorf("唯一一条记录 payload = %X，期望 AABB", got)
	}
}

// 没打开「保存历史」时，连一条数据都不产生 .stlog（与 .log 的默认行为一致）。
func TestStlogNotCreatedWhenSaveOff(t *testing.T) {
	_, proc, dir := newSaveTestProc(t)
	proc.recordHistoryAt(3000, "AABB", "rx", "")
	if files := findSessions(t, dir); len(files) != 0 {
		t.Fatalf("默认关时不该有会话，实际: %v", files)
	}
}

// 超过单记录上限的帧由格式层自动分片：多条记录、一个帧、多个页。
func TestStlogSplitsLargeFrame(t *testing.T) {
	_, proc, dir := newSaveTestProc(t)
	proc.SetSaveHistory(true)

	const n = 9000
	raw := make([]byte, n)
	for i := range raw {
		raw[i] = byte(i)
	}
	var sb strings.Builder
	for _, b := range raw {
		const hexdigits = "0123456789ABCDEF"
		sb.WriteByte(hexdigits[b>>4])
		sb.WriteByte(hexdigits[b&0x0F])
	}
	proc.recordHistoryAt(4000, sb.String(), "rx", "")
	proc.detachHistoryFile()

	info := mustReadStlog(t, onlySession(t, dir))
	if len(info.Frames) != 1 {
		t.Fatalf("帧数 = %d，期望 1（分片不该拆成多帧）", len(info.Frames))
	}
	if got := len(info.Frames[0].Payload); got != n {
		t.Errorf("帧长度 = %d，期望 %d", got, n)
	}
	if !bytes.Equal(info.Frames[0].Payload, raw) {
		t.Error("重组后的 payload 与原始字节不一致")
	}
	// 4016 B 一个记录：9000 = 4016 + 4016 + 968 → 3 条记录；每条独占一页。
	if info.Records != 3 {
		t.Errorf("记录数 = %d，期望 3", info.Records)
	}
	if len(info.Pages) != 3 {
		t.Errorf("页数 = %d，期望 3", len(info.Pages))
	}
	if info.Truncated || info.BrokenChain {
		t.Errorf("不该被标记为截断/剪链: truncated=%v brokenChain=%v", info.Truncated, info.BrokenChain)
	}
}

// 定时 flush：不关闭文件也能在磁盘上读到已提交的数据。
//
// 把间隔压到 50 ms 验证的是"到点会写"这一条；真正的写页动作仍由 Writer 完成。
func TestStlogFlushTimer(t *testing.T) {
	oldMS, oldTick := stlogFlushMS, stlogTickMS
	stlogFlushMS, stlogTickMS = 50, 10
	t.Cleanup(func() { stlogFlushMS, stlogTickMS = oldMS, oldTick })

	_, proc, dir := newSaveTestProc(t)
	proc.SetSaveHistory(true)
	proc.recordHistoryAt(5000, "AABBCC", "rx", "")

	path := onlySession(t, dir)
	// 定时 flush 之前磁盘上只有 16 KB 头，读不出记录；到点之后应当能读到。
	// 这期间 writer 还开着，读到的可能是"正在被整页写"的尾页，读失败就重试。
	deadline := time.Now().Add(2 * time.Second)
	var info *sessionfile.FileInfo
	for time.Now().Before(deadline) {
		got, err := sessionfile.ReadSession(path)
		if err == nil && got.Records == 1 {
			info = got
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if info == nil {
		t.Fatal("定时器到点后磁盘上仍读不到记录")
	}
	if got := info.Frames[0].Payload; !bytes.Equal(got, []byte{0xAA, 0xBB, 0xCC}) {
		t.Errorf("payload = %X，期望 AABBCC", got)
	}
}

// 转发录制：文件头记 mode=forward + B 口，记录方向走 p1/p2（3/4），
// 两个方向的字节数在读取侧按方向累加。
func TestStlogRecordsForwardMode(t *testing.T) {
	_, proc, dir := newSaveTestProc(t)
	proc.config = SerialConfig{Port: "COM4", Baud: 115200}
	proc.forwardConfigB = SerialConfig{Port: "COM14", Baud: 115200}
	proc.SetSaveHistory(true)

	proc.recordHistoryAt(1000, "AABB", "1", "")   // A→B 2 B
	proc.recordHistoryAt(1001, "CCDDEE", "2", "") // B→A 3 B
	proc.detachHistoryFile()

	info := mustReadStlog(t, onlySession(t, dir))
	if info.Settings == nil {
		t.Fatal("设置块为空")
	}
	if !info.Settings.IsForward() {
		t.Fatalf("Mode = %q，期望 forward", info.Settings.Mode)
	}
	if info.Settings.Port != "COM4" || info.Settings.PortB != "COM14" {
		t.Fatalf("端口快照 = A:%q B:%q，期望 COM4/COM14", info.Settings.Port, info.Settings.PortB)
	}
	if len(info.Frames) != 2 {
		t.Fatalf("帧数 = %d，期望 2", len(info.Frames))
	}
	if d := info.Frames[0].Dir; d != sessionfile.DirP1 {
		t.Errorf("帧 0 方向 = %d，期望 DirP1", d)
	}
	if d := info.Frames[1].Dir; d != sessionfile.DirP2 {
		t.Errorf("帧 1 方向 = %d，期望 DirP2", d)
	}
	if info.P1Total != 2 || info.P2Total != 3 {
		t.Errorf("方向字节 = A→B:%d B→A:%d，期望 2/3", info.P1Total, info.P2Total)
	}
	// 统计块只有 rx/tx 两栏，转发录制不该往里加数。
	if info.RxTotal != 0 || info.TxTotal != 0 {
		t.Errorf("统计块 rx/tx = %d/%d，期望 0/0", info.RxTotal, info.TxTotal)
	}
}
