package sessionfile

import (
	"fmt"
	"os"
	"time"
)

// Settings 是包头槽里那份自描述快照。
//
// 落盘编码见 payload.go（v3 起是 TLV 表）；这里的 json tag 只服务于
// settings.json 的映射与 Wails 的序列化，不再是槽里的字节格式。
type Settings struct {
	V       int    `json:"v"`
	UUID    string `json:"uuid"`
	Created string `json:"created"`
	Label   string `json:"label"`
	Port    string `json:"port,omitempty"`
	Baud    int    `json:"baud,omitempty"`
	// Mode 记的是「这次录制是单端口还是端口转发」。空串等价于 ModeSingle：
	// 早期文件没有这个字段，读回来是空串，不能因此把它们当成转发。
	Mode string `json:"mode,omitempty"`
	// PortB 只在转发录制里有值（A 口仍然放在 Port）。
	PortB string `json:"portB,omitempty"`
	// Display 是录制这一刻的「怎么看」：编码、Hex 大小写/前缀/分隔、控制字符、
	// 转义字节。键名与 settings.json 的 display 段一致，值统一是字符串。
	//
	// 放进槽里是为了让单个 pkg 拎出来也能按当初的样子显示；工程目录仍然以
	// settings.json 为准（它还会随用户改设置而变，槽里这份不会）。
	Display map[string]string `json:"display,omitempty"`
}

// Recording modes stored in Settings.Mode.
const (
	ModeSingle  = "single"
	ModeForward = "forward"
)

// IsForward reports whether the file was recorded in port-forwarding mode.
// A missing Mode (older files) reads as single-port.
func (s *Settings) IsForward() bool { return s != nil && s.Mode == ModeForward }

// Writer appends frames to a .stlog file using the v1.4 page model.
//
// The current tail page always lives in memory (pageBuf). Flushing rewrites the
// whole page; the shadow is only written when the page has been written before
// (see pageTouched).
type Writer struct {
	f    *os.File
	path string

	tailOff      int64  // offset of the current tail page
	pageBuf      []byte // full 4096-byte page buffer, always valid
	committedLen int    // [RecordsStart, CRCOffset]
	firstRecOff  uint16
	stats        Stats
	pageTouched  bool
	dirty        bool
	shadowNext   int64
	lastTs       uint64

	// instrumentation
	WriteCalls   int
	BytesWritten int64
	ShadowWrites int
	Seals        int

	// skippedTail is the offset of a torn/unreadable tail page that had to be
	// abandoned on open because no shadow could restore it (0 = nothing lost).
	skippedTail int64
}

// CreateWriter creates a new file (truncating any existing one).
func CreateWriter(path string, set Settings) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	if err := writeFreshHeader(f, set); err != nil {
		f.Close()
		return nil, err
	}
	w := &Writer{
		f:          f,
		path:       path,
		tailOff:    RecordsOffset,
		pageBuf:    make([]byte, PageSize),
		shadowNext: ShadowAOffset,
	}
	w.resetPage()
	return w, nil
}

// OpenWriter opens an existing file for appending.
func OpenWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	size, err := f.Seek(0, 2)
	if err != nil {
		f.Close()
		return nil, err
	}
	if size < HeaderSize {
		f.Close()
		return nil, fmt.Errorf("stlog: file too small (%d bytes)", size)
	}
	w := &Writer{f: f, path: path, pageBuf: make([]byte, PageSize)}

	tail, skipped, err := scanTail(f, size)
	if err != nil {
		f.Close()
		return nil, err
	}
	w.skippedTail = skipped
	if tail == nil {
		w.tailOff = RecordsOffset
		w.resetPage()
		return w, nil
	}
	w.tailOff = tail.offset
	copy(w.pageBuf, tail.raw)
	w.committedLen = int(tail.page.Header.CommittedLen)
	w.firstRecOff = tail.page.Header.FirstRecOff
	w.stats = tail.page.Stats
	w.lastTs = tail.page.Stats.LastTs
	w.pageTouched = w.committedLen > RecordsStart
	w.shadowNext = ShadowAOffset
	return w, nil
}

func (w *Writer) resetPage() {
	clear(w.pageBuf)
	w.committedLen = RecordsStart
	w.firstRecOff = NoFrameStart
	w.stats = Stats{Magic: StatsMagic, Ver: 1}
	w.dirty = false
}

// Append writes one logical frame. payload may exceed a page: it is split into
// as many records as needed, each marked with the continuation bit except the
// last one.
func (w *Writer) Append(ts uint64, dir uint8, origin uint8, originStr string, payload []byte) error {
	if !DirValid(dir) {
		return fmt.Errorf("stlog: invalid dir %#x", dir)
	}
	if ts <= w.lastTs {
		ts = w.lastTs + 1
	}
	w.lastTs = ts

	extra := 0
	if origin == OriginInline {
		extra = 1 + len(originStr)
	}

	// The first segment carries the frame start; later segments are
	// continuation-only and must not move firstRecOff.
	frameStart := true
	for {
		if len(payload) == 0 {
			return nil
		}
		remaining := CRCOffset - w.committedLen
		if remaining < MinPageRemain {
			if err := w.seal(); err != nil {
				return err
			}
			continue
		}
		canStore := remaining - RecordHeaderLen - extra
		if canStore < 1 {
			if err := w.seal(); err != nil {
				return err
			}
			continue
		}

		n := len(payload)
		more := false
		if n > canStore {
			n = canStore
			more = true
		}
		d := dir
		if more {
			d |= DirMore
		}
		if err := w.appendRecord(Record{
			Ts: ts, Dir: d, Origin: origin,
			Payload: payload[:n], OriginStr: originStr,
		}, frameStart); err != nil {
			return err
		}
		payload = payload[n:]
		if len(payload) > 0 {
			if err := w.seal(); err != nil {
				return err
			}
		}
		frameStart = false
	}
}

func (w *Writer) appendRecord(r Record, frameStart bool) error {
	if w.stats.Rec == 0 {
		w.stats.FirstTs = r.Ts
	}
	if frameStart && w.firstRecOff == NoFrameStart {
		w.firstRecOff = uint16(w.committedLen)
	}
	n := EncodeRecord(w.pageBuf[w.committedLen:], &r)
	w.committedLen += n
	w.stats.Rec++
	switch DirBase(r.Dir) {
	case DirRx:
		w.stats.RxTotal += uint64(len(r.Payload))
	case DirTx:
		w.stats.TxTotal += uint64(len(r.Payload))
	}
	w.stats.LastTs = r.Ts
	w.dirty = true
	return nil
}

// seal finishes the current page and moves to the next one. It performs no I/O
// when the page has already been written by a previous flush.
func (w *Writer) seal() error {
	if w.dirty {
		if err := w.writePage(); err != nil {
			return err
		}
	}
	w.Seals++
	w.tailOff += PageSize
	w.resetPage()
	w.pageTouched = false // fresh page: no shadow on its first write
	return nil
}

// Flush writes the current tail page if it has unsaved changes.
func (w *Writer) Flush() error {
	if !w.dirty {
		return nil
	}
	return w.writePage()
}

func (w *Writer) writePage() error {
	p := w.pageBuf
	h := PageHeader{
		Magic:        PageMagic,
		Ver:          PageVer,
		PageOffset:   uint64(w.tailOff),
		CommittedLen: uint16(w.committedLen),
		FirstRecOff:  w.firstRecOff,
	}
	h.Marshal(p[:PageHeaderLen])
	w.stats.Magic = StatsMagic
	w.stats.Ver = 1
	w.stats.Marshal(p[PageHeaderLen:RecordsStart])
	for i := w.committedLen; i < CRCOffset; i++ {
		p[i] = 0
	}
	SetPageCRC(p)

	// Shadow first: if we crash between the two writes the shadow already holds
	// the complete new version while the home page is still the old one.
	if w.pageTouched {
		if _, err := w.f.WriteAt(p, w.shadowNext); err != nil {
			return err
		}
		w.WriteCalls++
		w.ShadowWrites++
		w.BytesWritten += PageSize
		w.shadowNext = OtherShadow(w.shadowNext)
	}
	if _, err := w.f.WriteAt(p, w.tailOff); err != nil {
		return err
	}
	w.WriteCalls++
	w.BytesWritten += PageSize
	w.pageTouched = true
	w.dirty = false
	return nil
}

// Sync fsyncs the file.
func (w *Writer) Sync() error { return w.f.Sync() }

// Close flushes and closes the file.
func (w *Writer) Close() error {
	err := w.Flush()
	if serr := w.Sync(); err == nil {
		err = serr
	}
	if cerr := w.f.Close(); err == nil {
		err = cerr
	}
	return err
}

// TailOffset reports the offset of the current tail page.
func (w *Writer) TailOffset() int64 { return w.tailOff }

// AllocatedBytes 返回这个文件当前占用。尾页只要装过数据就算整页，
// 与切包时"按页向上取整"的口径一致。
func (w *Writer) AllocatedBytes() int64 {
	if w.committedLen > RecordsStart || w.pageTouched {
		return w.tailOff + PageSize
	}
	return w.tailOff
}

// CommittedLen reports how full the current page is.
func (w *Writer) CommittedLen() int { return w.committedLen }

// PageTouched reports whether the current page has been written before.
func (w *Writer) PageTouched() bool { return w.pageTouched }

// SkippedTail reports the offset of a tail page that could not be read or
// recovered when the file was opened, so the writer resumed on an earlier page.
// Zero means nothing was abandoned. When non-zero, the bytes of that page are
// lost: the caller should warn the user instead of silently overwriting them.
func (w *Writer) SkippedTail() int64 { return w.skippedTail }

func writeFreshHeader(f *os.File, set Settings) error {
	if set.V == 0 {
		set.V = SlotPayloadVer
	}
	if set.UUID == "" {
		set.UUID = fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	if set.Created == "" {
		set.Created = time.Now().Format(time.RFC3339)
	}
	payload := EncodeSettings(set)
	sa, err := EncodeSlot(&Slot{Seq: 1, Ts: uint64(time.Now().UnixMilli()), Payload: payload})
	if err != nil {
		return err
	}
	sb, err := EncodeSlot(&Slot{Seq: 0, Ts: 0, Payload: nil})
	if err != nil {
		return err
	}
	buf := make([]byte, HeaderSize)
	copy(buf[SlotAOffset:], sa)
	copy(buf[SlotBOffset:], sb)
	_, err = f.WriteAt(buf, 0)
	return err
}
