package sessionfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 会话的基本链路：建目录 → 写帧 → 关闭 → 按目录读回。
func TestSessionRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s1")
	s, err := CreateSession(dir, Settings{Label: "COM4", Port: "COM4", Baud: 115200})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := s.Append(uint64(1000+i), DirRx, OriginNone, "", []byte("hello")); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for _, n := range []string{ManifestName, SettingsName, IndexName, PkgFileName(1)} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("%s 不存在: %v", n, err)
		}
	}

	info, err := ReadSession(dir)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if !info.IsSession {
		t.Fatal("IsSession = false，目录应被识别为会话")
	}
	if len(info.Frames) != 5 {
		t.Fatalf("帧数 = %d, want 5", len(info.Frames))
	}
	if info.Records != 5 {
		t.Fatalf("记录数 = %d, want 5", info.Records)
	}
	if info.RxTotal != 25 {
		t.Fatalf("RxTotal = %d, want 25", info.RxTotal)
	}
	if info.Settings == nil || info.Settings.Port != "COM4" || info.Settings.Baud != 115200 {
		t.Fatalf("Settings 没从 settings.json 读出来: %+v", info.Settings)
	}
	if info.SessionID == "" {
		t.Fatal("SessionID 为空")
	}
	if len(info.Packages) != 1 || info.Packages[0].Seq != 1 {
		t.Fatalf("Packages = %+v, want 1 个 seq=1", info.Packages)
	}
	if len(info.Pages) == 0 || info.Pages[0].Package != 1 {
		t.Fatalf("Pages[0].Package = %v, want 1", info.Pages)
	}
}

// 关闭后再写应报错，重复 Close 应无害。
func TestSessionCloseIsFinal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s1")
	s, err := CreateSession(dir, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Append(1, DirRx, OriginNone, "", []byte("a"))
	if err := s.Close(); err != nil {
		t.Fatalf("第一次 Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("重复 Close 应无害: %v", err)
	}
	if err := s.Append(2, DirRx, OriginNone, "", []byte("b")); err == nil {
		t.Fatal("Close 之后 Append 应该失败")
	}
}

// 切包：包上限只够一页时，每写一帧就换个包，且帧不跨包。
func TestSessionRotatesPackages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s1")
	s, err := CreateSession(dir, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	s.pkgMax = HeaderSize + PageSize // 16384 + 4096

	payload := make([]byte, 2000)
	for i := 0; i < 3; i++ {
		if err := s.Append(uint64(100*i+1), DirRx, OriginNone, "", payload); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}
	if s.PkgSeq() != 3 {
		t.Fatalf("包序号 = %d, want 3", s.PkgSeq())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 3; i++ {
		if _, err := os.Stat(filepath.Join(dir, PkgFileName(i))); err != nil {
			t.Fatalf("包 %d 不存在: %v", i, err)
		}
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files.Packages) != 3 {
		t.Fatalf("manifest 登记了 %d 个包, want 3", len(m.Files.Packages))
	}
	for i, ref := range m.Files.Packages {
		if ref.Seq != i+1 {
			t.Fatalf("manifest[%d].Seq = %d, want %d", i, ref.Seq, i+1)
		}
		if !ref.Sealed {
			t.Fatalf("包 %d 应已封", ref.Seq)
		}
	}

	info, err := ReadSession(dir)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if len(info.Frames) != 3 {
		t.Fatalf("合并后帧数 = %d, want 3", len(info.Frames))
	}
	if len(info.Packages) != 3 {
		t.Fatalf("Packages = %d, want 3", len(info.Packages))
	}
	// 页级 Package 标记：每个包各有一页
	seen := map[int]bool{}
	for _, p := range info.Pages {
		seen[p.Package] = true
	}
	if len(seen) != 3 {
		t.Fatalf("页分布在 %d 个包里, want 3", len(seen))
	}
}

// 软上限：单帧比包还大时独占一个包、包临时超限，数据一条不丢。
func TestSessionSoftLimitSingleFrame(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s1")
	s, err := CreateSession(dir, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	s.pkgMax = HeaderSize + PageSize

	big := make([]byte, 30000) // >> pkgMax，会跨多页
	for i := range big {
		big[i] = byte('A' + i%26)
	}
	if err := s.Append(1, DirRx, OriginNone, "", big); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(filepath.Join(dir, PkgFileName(1)))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() <= s.pkgMax {
		t.Fatalf("包大小 %d 应该超过上限 %d（软上限）", fi.Size(), s.pkgMax)
	}

	info, err := ReadSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Frames) != 1 {
		t.Fatalf("帧数 = %d, want 1", len(info.Frames))
	}
	if got := len(info.Frames[0].Payload); got != len(big) {
		t.Fatalf("帧负载 = %d B, want %d", got, len(big))
	}
	if info.P1Total != 0 || info.RxTotal != uint64(len(big)) {
		t.Fatalf("RxTotal = %d, want %d", info.RxTotal, len(big))
	}
}

// 索引：写完能被读回，且内容与 manifest 对得上。
func TestSessionIndex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s1")
	s, err := CreateSession(dir, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	s.pkgMax = HeaderSize + PageSize
	for i := 0; i < 3; i++ {
		if err := s.Append(uint64(100*i+1), DirRx, OriginNone, "", make([]byte, 2000)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	idx, ok := LoadIndex(dir, m.Files.Packages)
	if !ok {
		t.Fatal("索引应该可用")
	}
	if len(idx.Entries) != 3 {
		t.Fatalf("索引条目 = %d, want 3", len(idx.Entries))
	}
	if e := idx.Entries[0]; e.Frames != 1 || e.Seq != 1 || !e.Sealed {
		t.Fatalf("索引条目内容不对: %+v", e)
	}
}

// 索引与磁盘不符时必须整份丢弃，而不是返回半旧的数据。
func TestSessionIndexRejectsStale(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s1")
	s, err := CreateSession(dir, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Append(1, DirRx, OriginNone, "", []byte("abc"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadIndex(dir, m.Files.Packages); !ok {
		t.Fatal("刚写完的索引应可用")
	}
	// 让包文件变长：mtime/大小都对不上
	f, err := os.OpenFile(filepath.Join(dir, PkgFileName(1)), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("x"))
	f.Close()
	if _, ok := LoadIndex(dir, m.Files.Packages); ok {
		t.Fatal("包被改过之后索引必须失效")
	}
}

// manifest 被删掉时按磁盘包认领，数据仍可读，只丢 manifest 里的身份。
func TestReadSessionRebuildsWithoutManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s1")
	s, err := CreateSession(dir, Settings{Label: "L"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := s.Append(uint64(10+i), DirTx, OriginNone, "", []byte("xy")); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, ManifestName)); err != nil {
		t.Fatal(err)
	}

	info, err := ReadSession(dir)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if len(info.Frames) != 4 {
		t.Fatalf("帧数 = %d, want 4", len(info.Frames))
	}
	if info.TxTotal != 8 {
		t.Fatalf("TxTotal = %d, want 8", info.TxTotal)
	}
	if info.SessionID != "" {
		t.Fatalf("没有 manifest 时不应有 sessionId，得到 %q", info.SessionID)
	}
}

// v1.4 单文件仍然是合法输入。
func TestReadSessionAcceptsV14File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.stlog")
	w, err := CreateWriter(path, Settings{Label: "old", Port: "COM9"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1, DirTx, OriginNone, "", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := ReadSession(path)
	if err != nil {
		t.Fatalf("ReadSession(%s): %v", path, err)
	}
	if info.IsSession {
		t.Fatal("单文件不应被标记成会话")
	}
	if len(info.Frames) != 1 {
		t.Fatalf("帧数 = %d, want 1", len(info.Frames))
	}
	if info.Settings == nil || info.Settings.Port != "COM9" {
		t.Fatalf("单文件的设置应来自包头槽: %+v", info.Settings)
	}
	if info.Packages != nil {
		t.Fatal("单文件不应有 Packages")
	}
}

// 转发模式：mode 走 settings.json，读回来仍然是 forward。
func TestSessionForwardMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s1")
	s, err := CreateSession(dir, Settings{Label: "A↔B", Mode: ModeForward, Port: "COM4", PortB: "COM14"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Append(1, DirP1, OriginNone, "", []byte("abcd"))
	_ = s.Append(2, DirP2, OriginNone, "", []byte("xy"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := ReadSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Settings.IsForward() {
		t.Fatalf("mode = %q, want forward", info.Settings.Mode)
	}
	if info.Settings.Port != "COM4" || info.Settings.PortB != "COM14" {
		t.Fatalf("端口 = %q/%q, want COM4/COM14", info.Settings.Port, info.Settings.PortB)
	}
	if info.P1Total != 4 || info.P2Total != 2 {
		t.Fatalf("A→B/B→A = %d/%d, want 4/2", info.P1Total, info.P2Total)
	}
}

// 目录名的格式与撞号后缀。
func TestSessionDirNameFormat(t *testing.T) {
	ts := time.Date(2026, 10, 30, 10, 45, 12, 0, time.Local)
	name := SessionDirName(ts, "ab12cd34e5f60718293a4b5c6d7e8f90")
	if want := "20261030_104512_ab12cd34"; name != want {
		t.Fatalf("目录名 = %q, want %q", name, want)
	}
}

// CreateSessionIn 会自动建出规范命名的子目录，撞名时加后缀。
func TestCreateSessionIn(t *testing.T) {
	parent := t.TempDir()
	s, err := CreateSessionIn(parent, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dir := s.Dir()
	if filepath.Dir(dir) != parent {
		t.Fatalf("目录不在 parent 下: %s", dir)
	}
	base := filepath.Base(dir)
	if len(base) != 24 { // yyyyMMdd_HHmmss(15) + '_' + id8(8)
		t.Fatalf("目录名长度 = %d (%q), want 24", len(base), base)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
