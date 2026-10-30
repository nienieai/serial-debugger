package sessionfile

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func newFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.stlog")
}

func TestRoundTrip(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{Label: "rt"})
	if err != nil {
		t.Fatal(err)
	}
	type sent struct {
		ts      uint64
		dir     uint8
		payload []byte
	}
	var want []sent
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 300; i++ {
		p := make([]byte, 40+rng.Intn(300))
		rng.Read(p)
		ts := uint64(1000 + i*10)
		dir := uint8(1 + i%2)
		want = append(want, sent{ts, dir, p})
		if err := w.Append(ts, dir, 0, "", p); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Frames) != len(want) {
		t.Fatalf("frames: got %d want %d", len(info.Frames), len(want))
	}
	for i := range want {
		f := info.Frames[i]
		if f.Ts != want[i].ts {
			t.Fatalf("frame %d ts: got %d want %d", i, f.Ts, want[i].ts)
		}
		if f.Dir != want[i].dir {
			t.Fatalf("frame %d dir: got %d want %d", i, f.Dir, want[i].dir)
		}
		if !bytes.Equal(f.Payload, want[i].payload) {
			t.Fatalf("frame %d payload mismatch (%d vs %d bytes)", i, len(f.Payload), len(want[i].payload))
		}
	}
	if info.Truncated {
		t.Fatal("file must not report truncation")
	}
}

func TestSplitFrameAcrossPages(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	big := make([]byte, 10000)
	rand.New(rand.NewSource(3)).Read(big)
	if err := w.Append(42, DirRx, 0, "", big); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Frames) != 1 {
		t.Fatalf("a split frame must still read back as one frame, got %d", len(info.Frames))
	}
	if !bytes.Equal(info.Frames[0].Payload, big) {
		t.Fatalf("reassembled payload differs (%d vs %d)", len(info.Frames[0].Payload), len(big))
	}
	if info.Frames[0].Partial {
		t.Fatal("frame must not be partial")
	}
	if len(info.Pages) < 2 {
		t.Fatalf("expected the frame to span pages, got %d page(s)", len(info.Pages))
	}
	// The second page holds only the tail segment: no frame starts there.
	if info.Pages[1].FirstRecOff != NoFrameStart {
		t.Fatalf("continuation-only page must have firstRecOff=0xFFFF, got %#x", info.Pages[1].FirstRecOff)
	}
	if info.Pages[0].FirstRecOff != RecordsStart {
		t.Fatalf("page 0 should start with the frame, got firstRecOff=%d", info.Pages[0].FirstRecOff)
	}
}

func TestFirstWriteDoesNotWriteShadow(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1, DirRx, 0, "", make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if w.ShadowWrites != 0 {
		t.Fatalf("fresh page must not write a shadow, got %d shadow writes", w.ShadowWrites)
	}
	if w.WriteCalls != 1 {
		t.Fatalf("expected exactly 1 write, got %d", w.WriteCalls)
	}

	// Rewriting the same page must produce a shadow.
	if err := w.Append(2, DirRx, 0, "", make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if w.ShadowWrites != 1 {
		t.Fatalf("rewrite must write one shadow, got %d", w.ShadowWrites)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestShadowRotation(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	// First write: no shadow. Then three rewrites -> A, B, A.
	for i := 0; i < 4; i++ {
		if err := w.Append(uint64(i+1), DirRx, 0, "", make([]byte, 50)); err != nil {
			t.Fatal(err)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	if w.ShadowWrites != 3 {
		t.Fatalf("expected 3 shadow writes, got %d", w.ShadowWrites)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Verify the alternation physically: last two shadows must differ because
	// they were written at different offsets.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	a := make([]byte, PageSize)
	b := make([]byte, PageSize)
	if _, err := f.ReadAt(a, ShadowAOffset); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadAt(b, ShadowBOffset); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("shadow A and B should hold different revisions")
	}
}

// TestCorruptPageRecoveredFromShadow covers a page whose bytes are all present
// but whose CRC no longer matches (bit rot, partial overwrite). This is *not* a
// torn write; see TestTornTailRecoveredFromShadow for that.
func TestCorruptPageRecoveredFromShadow(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1, DirRx, 0, "", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil { // fresh page, no shadow
		t.Fatal(err)
	}
	if err := w.Append(2, DirTx, 0, "", []byte("world")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil { // rewrite, shadow written
		t.Fatal(err)
	}
	tail := w.TailOffset()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Tear the home page's CRC.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, PageSize)
	if _, err := f.ReadAt(buf, tail); err != nil {
		t.Fatal(err)
	}
	buf[CRCOffset] ^= 0xFF
	if _, err := f.WriteAt(buf, tail); err != nil {
		t.Fatal(err)
	}
	f.Close()

	info, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Recovered) != 1 {
		t.Fatalf("expected 1 restored page, got %v", info.Recovered)
	}
	if len(info.Frames) != 2 {
		t.Fatalf("both frames must survive, got %d", len(info.Frames))
	}
	if string(info.Frames[0].Payload) != "hello" || string(info.Frames[1].Payload) != "world" {
		t.Fatalf("unexpected payloads: %q %q", info.Frames[0].Payload, info.Frames[1].Payload)
	}
}

func TestTornTailRecoveredFromShadow(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1, DirRx, 0, "", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil { // fresh page: no shadow yet
		t.Fatal(err)
	}
	if err := w.Append(2, DirTx, 0, "", []byte("second")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil { // rewrite: shadow now holds a full copy
		t.Fatal(err)
	}
	tail := w.TailOffset()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// A real torn write: the file ends in the middle of the tail page. The
	// shadows sit before the records area and are therefore still intact.
	if err := os.Truncate(path, tail+2048); err != nil {
		t.Fatal(err)
	}

	info, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Recovered) != 1 || info.Recovered[0] != tail {
		t.Fatalf("expected page %d to be restored from a shadow, got %v", tail, info.Recovered)
	}
	if len(info.Frames) != 2 {
		t.Fatalf("torn tail must not lose committed frames, got %d of 2", len(info.Frames))
	}
	if string(info.Frames[0].Payload) != "first" || string(info.Frames[1].Payload) != "second" {
		t.Fatalf("unexpected payloads: %q %q", info.Frames[0].Payload, info.Frames[1].Payload)
	}
	if info.Truncated {
		t.Fatal("restored page should end on a frame boundary")
	}
}

// TestOpenWriterResumesOnTornPage makes sure a writer that reopens a file whose
// tail page was torn resumes on that page (after restoring it from a shadow)
// instead of skipping past it and burying the surviving copy.
func TestOpenWriterResumesOnTornPage(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("x"), 100)
	for i := 0; i < 40; i++ {
		if err := w.Append(uint64(i+1), DirRx, 0, "", payload); err != nil {
			t.Fatal(err)
		}
		if err := w.Flush(); err != nil { // every frame flushed: tail has a shadow
			t.Fatal(err)
		}
	}
	tail := w.TailOffset()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.Truncate(path, tail+2048); err != nil {
		t.Fatal(err)
	}

	w2, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := w2.TailOffset(); got != tail {
		t.Fatalf("writer must resume on the torn page %d, resumed on %d instead", tail, got)
	}
	if err := w2.Append(9999, DirTx, 0, "", []byte("appended")); err != nil {
		t.Fatal(err)
	}
	if err := w2.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Frames) != 41 {
		t.Fatalf("expected the 40 recovered frames plus 1 appended, got %d", len(info.Frames))
	}
	if string(info.Frames[40].Payload) != "appended" {
		t.Fatalf("last frame = %q", info.Frames[40].Payload)
	}
	// The rewritten page also heals the file: it must be page-aligned again.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() < tail+PageSize {
		t.Fatalf("home page should have been rewritten to full length, file is %d B", st.Size())
	}
}

// TestOpenWriterReportsSkippedTornTail pins the "do not lose data silently"
// rule: when the tail page is unrecoverable, the writer must report which page
// it abandoned so the caller can warn, instead of quietly overwriting it.
func TestOpenWriterReportsSkippedTornTail(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("y"), 100)
	for i := 0; i < 40; i++ {
		if err := w.Append(uint64(i+1), DirRx, 0, "", payload); err != nil {
			t.Fatal(err)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	tail := w.TailOffset()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Tear the tail page and destroy both shadows: nothing can be recovered.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	zero := make([]byte, PageSize)
	for _, off := range []int64{ShadowAOffset, ShadowBOffset} {
		if _, err := f.WriteAt(zero, off); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	if err := os.Truncate(path, tail+2048); err != nil {
		t.Fatal(err)
	}

	w2, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := w2.SkippedTail(); got != tail {
		t.Fatalf("writer must report the abandoned page %d, got %d", tail, got)
	}
	if got := w2.TailOffset(); got == tail {
		t.Fatal("writer must fall back to an earlier page")
	}
	if err := w2.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestTornTailThenAppendDoesNotMergeFrames covers the nastiest consequence of a
// lost page: page P ends with "frame continues", while the replacement page
// starts on a frame boundary. Gluing them together would fuse two unrelated
// frames into one and report nothing. The reader must cut the chain instead.
func TestTornTailThenAppendDoesNotMergeFrames(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("z"), 100)
	for i := 0; i < 40; i++ {
		if err := w.Append(uint64(i+1), DirRx, 0, "", payload); err != nil {
			t.Fatal(err)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	tail := w.TailOffset()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	zero := make([]byte, PageSize)
	for _, off := range []int64{ShadowAOffset, ShadowBOffset} {
		if _, err := f.WriteAt(zero, off); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	if err := os.Truncate(path, tail+2048); err != nil {
		t.Fatal(err)
	}

	w2, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := w2.Append(uint64(9000+i), DirTx, 0, "", []byte("new-frame")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w2.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.BrokenChain {
		t.Fatal("reader must detect the cut continuation chain")
	}
	// Page 0 holds 35 whole frames plus the head of frame 36; the new page holds
	// 3 frames. 35 + 1 partial + 3 = 39, and the partial one must stay separate.
	if len(info.Frames) != 39 {
		t.Fatalf("expected 39 frames, got %d", len(info.Frames))
	}
	if !info.Frames[35].Partial {
		t.Fatal("frame 35 must be the orphaned head of the lost frame")
	}
	if string(info.Frames[38].Payload) != "new-frame" {
		t.Fatalf("last frame = %q", info.Frames[38].Payload)
	}
}

func TestNoShadowMeansLossIsBounded(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1, DirRx, 0, "", []byte("kept")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(2, DirRx, 0, "", []byte("lost")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil { // rewrite: shadow written
		t.Fatal(err)
	}
	tail := w.TailOffset()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Destroy both the home page and both shadows: nothing can be recovered.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	bad := make([]byte, PageSize)
	for _, off := range []int64{tail, ShadowAOffset, ShadowBOffset} {
		if _, err := f.WriteAt(bad, off); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	info, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Frames) != 0 {
		t.Fatalf("expected no recoverable frames, got %d", len(info.Frames))
	}
	if info.StoppedAt == 0 {
		t.Fatal("scan must report where it stopped")
	}
}

func TestCRCRejectsCorruption(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1, DirRx, 0, "", make([]byte, 500)); err != nil {
		t.Fatal(err)
	}
	tail := w.TailOffset()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, PageSize)
	if _, err := f.ReadAt(buf, tail); err != nil {
		t.Fatal(err)
	}
	buf[RecordsStart+20] ^= 0x01 // flip a payload bit
	if _, err := f.WriteAt(buf, tail); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if _, err := DecodePage(mustRead(t, path, tail), uint64(tail)); err != ErrBadCRC {
		t.Fatalf("expected ErrBadCRC, got %v", err)
	}
}

func mustRead(t *testing.T, path string, off int64) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, PageSize)
	if _, err := f.ReadAt(buf, off); err != nil {
		t.Fatal(err)
	}
	return buf
}

func TestPageWasteBound(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(11))
	for i := 0; i < 500; i++ {
		p := make([]byte, 30+rng.Intn(200))
		rng.Read(p)
		if err := w.Append(uint64(i+1), uint8(1+i%2), 0, "", p); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Pages) < 2 {
		t.Fatal("need several pages")
	}
	// Every sealed page must leave at most 27 bytes unused.
	for i := 0; i < len(info.Pages)-1; i++ {
		waste := CRCOffset - info.Pages[i].CommittedLen
		if waste > 27 {
			t.Fatalf("page %d wastes %d bytes (limit 27)", i, waste)
		}
	}
}

func TestSlotsRejectCorruption(t *testing.T) {
	s := &Slot{Seq: 3, Ts: 100, Payload: []byte(`{"v":2}`)}
	buf, err := EncodeSlot(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSlot(buf); err != nil {
		t.Fatalf("fresh slot must decode: %v", err)
	}
	buf[40] ^= 0xFF
	if _, err := DecodeSlot(buf); err == nil {
		t.Fatal("corrupted slot must be rejected")
	}
}

func TestChooseSlotRule(t *testing.T) {
	a := &Slot{Seq: 5, Ts: 100}
	b := &Slot{Seq: 6, Ts: 90}
	if ChooseSlot(a, b) != b || ChooseSlot(b, a) != b {
		t.Fatal("higher seq must win regardless of order")
	}
	c := &Slot{Seq: 5, Ts: 200}
	if ChooseSlot(a, c) != c {
		t.Fatal("equal seq: higher ts wins")
	}
	d := &Slot{Seq: 5, Ts: 100}
	if ChooseSlot(a, d) != a {
		t.Fatal("full tie: A wins")
	}
	if ChooseSlot(nil, a) != a || ChooseSlot(a, nil) != a {
		t.Fatal("valid slot must be used when the other is invalid")
	}
	if ChooseSlot(nil, nil) != nil {
		t.Fatal("no valid slot")
	}
}

func TestContinuationChainIsSingle(t *testing.T) {
	// Two frames in a row must not be merged: the first terminates.
	recs := []Record{
		{Ts: 1, Dir: DirRx | DirMore, Payload: []byte("a")},
		{Ts: 2, Dir: DirRx, Payload: []byte("b")},
		{Ts: 3, Dir: DirTx, Payload: []byte("c")},
	}
	frames, truncated := BuildFrames(recs, nil)
	if truncated {
		t.Fatal("must not be truncated")
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if string(frames[0].Payload) != "ab" {
		t.Fatalf("frame 0 payload = %q", frames[0].Payload)
	}
	if string(frames[1].Payload) != "c" {
		t.Fatalf("frame 1 payload = %q", frames[1].Payload)
	}
}

func TestOrphanContinuationMarkedPartial(t *testing.T) {
	// BuildFrames trusts its input to start on a frame boundary; a first page
	// whose firstRecOff is not RecordsStart is what flags a missing head.
	recs := []Record{
		{Ts: 1, Dir: DirRx | DirMore, Payload: []byte("tail")},
		{Ts: 2, Dir: DirRx, Payload: []byte("end")},
	}
	frames, _ := BuildFrames(recs, nil)
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	if frames[0].Partial {
		t.Fatal("BuildFrames must not guess from the first record alone")
	}
}

// 转发录制：文件头要记 mode 与 B 口，A→B / B→A 的字节数只能扫记录得到
// （统计块只有 rx/tx 两栏，这里也顺带锁住"统计块不累计 p1/p2"这件事）。
func TestForwardModeSettingsAndDirectionTotals(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{
		V: SlotPayloadVer, Port: "COM4", Baud: 115200,
		Mode: ModeForward, PortB: "COM14",
	})
	if err != nil {
		t.Fatal(err)
	}
	writes := []struct {
		dir  uint8
		data string
	}{
		{DirP1, "A1A2A3"},    // A→B 6 B（Append 收的是原始字节，不是 hex）
		{DirP2, "B1B2"},      // B→A 4 B
		{DirP1, "A4"},        // A→B 2 B
		{DirSystem, "sys.x"}, // 系统帧不计入任何方向
	}
	for i, s := range writes {
		if err := w.Append(uint64(1000+i), s.dir, OriginNone, "", []byte(s.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Settings == nil {
		t.Fatal("settings 必须读得回来")
	}
	if !info.Settings.IsForward() {
		t.Fatalf("IsForward = false, Mode = %q", info.Settings.Mode)
	}
	if info.Settings.Port != "COM4" || info.Settings.PortB != "COM14" {
		t.Fatalf("端口快照: A=%q B=%q", info.Settings.Port, info.Settings.PortB)
	}
	if info.P1Total != 8 || info.P2Total != 4 {
		t.Fatalf("方向字节: A→B=%d(期望 8) B→A=%d(期望 4)", info.P1Total, info.P2Total)
	}
	if info.RxTotal != 0 || info.TxTotal != 0 {
		t.Fatalf("统计块不该累计 p1/p2: rx=%d tx=%d", info.RxTotal, info.TxTotal)
	}
	if len(info.Frames) != 4 {
		t.Fatalf("frames = %d, 期望 4", len(info.Frames))
	}
}

// 老文件没有 mode 字段：读回来是空串，必须按单端口处理（不能当成转发）。
func TestMissingModeReadsAsSingle(t *testing.T) {
	path := newFile(t)
	w, err := CreateWriter(path, Settings{V: SlotPayloadVer, Port: "COM3", Baud: 9600})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1000, DirRx, OriginNone, "", []byte("AA")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Settings == nil {
		t.Fatal("settings 读不到")
	}
	if info.Settings.Mode != "" {
		t.Fatalf("Mode 应为空串, got %q", info.Settings.Mode)
	}
	if info.Settings.IsForward() {
		t.Fatal("空 Mode 必须按单端口处理")
	}
	if info.P1Total != 0 || info.P2Total != 0 {
		t.Fatal("单口文件不该有 p1/p2 累计")
	}
}
