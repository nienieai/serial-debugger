package stlogview

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// tf 是测试里的一帧。
type tf struct {
	ts        uint64
	dir       uint8
	origin    uint8
	originStr string
	payload   []byte
}

// writeTestFile 用主工程自己的 Writer 造一个 .stlog，避免仓库里带样本文件。
//
// 长 payload（> 一页能放下的字节数）会在这里被 Writer 拆成续片记录并封页，
// 正好覆盖 scanner 的续片拼接路径。
func writeTestFile(t *testing.T, frames []tf) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "unit.stlog")
	w, err := sessionfile.CreateWriter(p, sessionfile.Settings{
		Label: "unit", Port: "COM4", Baud: 115200,
		Display: map[string]string{"encoding": "utf-8"},
	})
	if err != nil {
		t.Fatalf("CreateWriter: %v", err)
	}
	for _, f := range frames {
		if err := w.Append(f.ts, f.dir, f.origin, f.originStr, f.payload); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return p
}

// defaultFrames 是一份覆盖各方向、各长度（含跨页续片）的夹具。
func defaultFrames() []tf {
	long := make([]byte, 9000)
	for i := range long {
		long[i] = byte('a' + i%26)
	}
	return []tf{
		{ts: 1000, dir: sessionfile.DirRx, payload: []byte("hello")},
		{ts: 1010, dir: sessionfile.DirTx, origin: sessionfile.OriginInline, originStr: "cli-aaaa1111", payload: []byte("world")},
		{ts: 1020, dir: sessionfile.DirRx, payload: []byte("line1\r\nline2\ttab")},
		{ts: 1030, dir: sessionfile.DirSystem, payload: []byte("sys.port_connected|COM4|115200")},
		{ts: 1040, dir: sessionfile.DirP1, payload: []byte("A->B")},
		{ts: 1050, dir: sessionfile.DirP2, payload: []byte("B->A!")},
		{ts: 1060, dir: sessionfile.DirTx, origin: sessionfile.OriginInline, originStr: "gui-bbbb2222", payload: long},
		{ts: 1070, dir: sessionfile.DirRx, payload: []byte{0x00, 0xFF, 0x7F, 'x'}},
	}
}

// waitReady 等后台扫描结束（Open 是异步的）。
func waitReady(t *testing.T, s *Store) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st := s.Status()
		switch st.Phase {
		case "ready":
			return
		case "error":
			t.Fatalf("扫描失败: %s", st.Err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("扫描超时")
}

// TestScanMatchesReader 拿主工程的 sessionfile.ReadFile 当唯一标准答案：
// 帧数、时间戳、方向、payload 逐条对齐。这条一旦红，说明自己的续片拼接与
// reader.go 的 BuildFrames 想的不一样 —— 那才是真正危险的分歧。
func TestScanMatchesReader(t *testing.T) {
	frames := defaultFrames()
	p := writeTestFile(t, frames)

	info, err := sessionfile.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	ix, err := scanStlog(p, nil)
	if err != nil {
		t.Fatalf("scanStlog: %v", err)
	}
	if ix.n != len(info.Frames) {
		t.Fatalf("帧数 %d，标准答案 %d", ix.n, len(info.Frames))
	}
	if ix.records != info.Records {
		t.Fatalf("记录数 %d，标准答案 %d", ix.records, info.Records)
	}
	if ix.pages != len(info.Pages) {
		t.Fatalf("页数 %d，标准答案 %d", ix.pages, len(info.Pages))
	}
	for i := range info.Frames {
		f := &info.Frames[i]
		if ix.ts[i] != int64(f.Ts) {
			t.Fatalf("第 %d 帧 ts=%d，标准答案 %d", i, ix.ts[i], f.Ts)
		}
		if ix.dir[i] != f.Dir {
			t.Fatalf("第 %d 帧 dir=%d，标准答案 %d", i, ix.dir[i], f.Dir)
		}
		got := ix.arena[ix.aoff[i] : ix.aoff[i]+int32(ix.ln[i])]
		if string(got) != string(f.Payload) {
			t.Fatalf("第 %d 帧 payload 不一致：%d B vs %d B", i, len(got), len(f.Payload))
		}
	}
	if len(ix.orgNames) != 2 || ix.orgNames[0] != "cli-aaaa1111" || ix.orgNames[1] != "gui-bbbb2222" {
		t.Fatalf("来源字典 = %v", ix.orgNames)
	}
	if ix.rxBytes != int64(info.RxTotal) || ix.txBytes != int64(info.TxTotal) {
		t.Fatalf("rx/tx %d/%d，标准答案 %d/%d", ix.rxBytes, ix.txBytes, info.RxTotal, info.TxTotal)
	}
	t.Logf("帧 %d · 记录 %d · 页 %d · rx/tx %d/%d", ix.n, ix.records, ix.pages, ix.rxBytes, ix.txBytes)
}

// TestScanRejectsNonStlog 不是 .stlog 的文件要报错，而不是扫出 0 帧。
func TestScanRejectsNonStlog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tiny.stlog")
	if err := writeBytes(p, []byte("not a session file")); err != nil {
		t.Fatal(err)
	}
	if _, err := scanStlog(p, nil); err == nil {
		t.Fatal("应该报错")
	}
}

// TestHexBytesPerLine 钉住十六进制折行：一行不得超过给定字符数，而且要尽量用满 ——
// 再加一个字节就会超（以前按 8 对齐，宽窗口下白扔一百多像素）。
func TestHexBytesPerLine(t *testing.T) {
	for _, chars := range []int{16, 20, 39, 40, 60, 80, 100, 120, 200} {
		for _, prefix := range []bool{false, true} {
			bpl := hexBytesPerLine(chars, prefix)
			per := 3
			if prefix {
				per = 5
			}
			if line := bpl*per - 1; line > chars {
				t.Fatalf("chars=%d prefix=%v → bpl=%d 一行 %d 字符，超了", chars, prefix, bpl, line)
			}
			if line := (bpl+1)*per - 1; line <= chars {
				t.Fatalf("chars=%d prefix=%v → bpl=%d 还能再塞一个字节（一行只有 %d 字符）", chars, prefix, bpl, bpl*per-1)
			}
		}
	}
}
