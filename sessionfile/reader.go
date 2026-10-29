package sessionfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// PageSummary is a compact view of one page for CLI output.
type PageSummary struct {
	Offset       int64
	CommittedLen int
	FirstRecOff  uint16
	Rec          uint32
	RxTotal      uint64
	TxTotal      uint64
	FirstTs      uint64
	LastTs       uint64
	Recovered    bool
}

// FileInfo is the full decoded view of a .stlog file.
type FileInfo struct {
	Path      string
	Size      int64
	Settings  *Settings
	Pages     []PageSummary
	Frames    []Frame
	Records   int
	Recovered []int64 // page offsets that had to be restored from a shadow
	StoppedAt int64   // offset of the first unusable page (0 = none)
	StopErr   error
	Truncated   bool // last frame never terminated
	BrokenChain bool // a continuation chain was cut at a page boundary
	RxTotal     uint64
	TxTotal     uint64
}

// pageAt holds a raw page plus its decoding.
type pageAt struct {
	offset int64
	raw    []byte
	page   *Page
}

// recoverAt tries both shadow pages for a copy of the page expected at off.
// A shadow only qualifies if its own pageOffset matches, so a stale shadow
// pointing at an older page is ignored automatically.
func recoverAt(f *os.File, off uint64) (*pageAt, bool) {
	var best *pageAt
	for _, so := range []int64{ShadowAOffset, ShadowBOffset} {
		buf := make([]byte, PageSize)
		if _, err := f.ReadAt(buf, so); err != nil {
			continue
		}
		p, err := DecodePage(buf, off)
		if err != nil {
			continue
		}
		if best == nil || p.Header.CommittedLen > best.page.Header.CommittedLen {
			best = &pageAt{offset: int64(off), raw: buf, page: p}
		}
	}
	return best, best != nil
}

// scanTail returns the last usable page of the file (nil if there is none) and,
// separately, the offset of a tail page that had to be abandoned because it was
// neither readable nor recoverable from a shadow. A torn tail page counts as
// usable when a shadow still holds a complete copy, otherwise a writer reopening
// the file would skip it and bury that copy.
func scanTail(f *os.File, size int64) (*pageAt, int64, error) {
	off := int64(RecordsOffset)
	var last *pageAt
	var skipped int64
	for off < size {
		if off+PageSize > size {
			if rp, ok := recoverAt(f, uint64(off)); ok {
				return rp, 0, nil
			}
			skipped = off
			break
		}
		buf := make([]byte, PageSize)
		n, err := f.ReadAt(buf, off)
		if err != nil && err != io.EOF {
			return nil, 0, err
		}
		if n < PageSize {
			skipped = off
			break
		}
		p, derr := DecodePage(buf, uint64(off))
		if derr != nil {
			if rp, ok := recoverAt(f, uint64(off)); ok {
				return rp, 0, nil
			}
			skipped = off
			break
		}
		last = &pageAt{offset: off, raw: buf, page: p}
		off += PageSize
	}
	return last, skipped, nil
}

// ReadFile decodes every page, restores damaged tail pages from the shadows,
// and reassembles frames.
func ReadFile(path string) (*FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	info := &FileInfo{Path: path, Size: st.Size()}
	info.Settings = readSettings(f)

	var all []Record
	var breaks []int // record indices where the continuation chain is provably broken
	prevOpen := false
	off := int64(RecordsOffset)
	for off < st.Size() {
		torn := off+PageSize > st.Size()

		var p *Page
		recovered := false

		if torn {
			// The tail page is shorter than one page: the write was torn. The
			// shadows live before the records area, so they survive truncation
			// and are the only place that can still hold a complete copy.
			rp, ok := recoverAt(f, uint64(off))
			if !ok {
				info.StoppedAt = off
				info.StopErr = ErrPartialTail
				break
			}
			p = rp.page
			recovered = true
			info.Recovered = append(info.Recovered, off)
		} else {
			buf := make([]byte, PageSize)
			n, rerr := f.ReadAt(buf, off)
			if rerr != nil && rerr != io.EOF {
				return nil, rerr
			}
			if n < PageSize {
				break
			}
			pp, derr := DecodePage(buf, uint64(off))
			if derr != nil {
				rp, ok := recoverAt(f, uint64(off))
				if !ok {
					info.StoppedAt = off
					info.StopErr = derr
					break
				}
				pp = rp.page
				recovered = true
				info.Recovered = append(info.Recovered, off)
			}
			p = pp
		}

		info.Pages = append(info.Pages, PageSummary{
			Offset:       off,
			CommittedLen: int(p.Header.CommittedLen),
			FirstRecOff:  p.Header.FirstRecOff,
			Rec:          p.Stats.Rec,
			RxTotal:      p.Stats.RxTotal,
			TxTotal:      p.Stats.TxTotal,
			FirstTs:      p.Stats.FirstTs,
			LastTs:       p.Stats.LastTs,
			Recovered:    recovered,
		})
		info.RxTotal += p.Stats.RxTotal
		info.TxTotal += p.Stats.TxTotal

		// A page that starts on a frame boundary cannot follow a page whose last
		// record claims the frame continues: a page in between was lost. Cut the
		// chain there instead of gluing an unrelated frame onto a half one.
		if p.Header.FirstRecOff == RecordsStart && prevOpen && len(p.Records) > 0 {
			breaks = append(breaks, len(all))
			info.BrokenChain = true
		}
		all = append(all, p.Records...)
		if n := len(p.Records); n > 0 {
			prevOpen = p.Records[n-1].Dir&DirMore != 0
		}

		if torn {
			break // a torn page is the last page by definition
		}
		off += PageSize
	}

	info.Records = len(all)
	info.Frames, info.Truncated = BuildFrames(all, breaks)

	// If the very first page opens with continuation segments, the frame they
	// belong to is missing its head (the file was truncated at the front).
	if len(info.Pages) > 0 && len(info.Frames) > 0 {
		fr := info.Pages[0].FirstRecOff
		if fr == NoFrameStart || fr > RecordsStart {
			info.Frames[0].Partial = true
		}
	}
	return info, nil
}

func readSettings(f *os.File) *Settings {
	var a, b *Slot
	for i, off := range []int64{SlotAOffset, SlotBOffset} {
		buf := make([]byte, SlotSize)
		if _, err := f.ReadAt(buf, off); err != nil {
			continue
		}
		s, err := DecodeSlot(buf)
		if err != nil {
			continue
		}
		if i == 0 {
			a = s
		} else {
			b = s
		}
	}
	chosen := ChooseSlot(a, b)
	if chosen == nil || len(chosen.Payload) == 0 {
		return nil
	}
	var set Settings
	if err := json.Unmarshal(chosen.Payload, &set); err != nil {
		return nil
	}
	return &set
}

// VerifyReport lists structural problems found by a full pass.
// Problems are hard errors; Warnings are expected after an unclean shutdown.
type VerifyReport struct {
	Pages        int
	Records      int
	Frames       int
	Recovered    []int64
	Problems     []string
	Warnings     []string
	StoppedAt    int64
	WastedMin    int
	WastedMax    int
	LastPageFree int
}

// Verify walks the file and reports every structural deviation it can prove.
func Verify(path string) (*VerifyReport, error) {
	info, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	rep := &VerifyReport{
		Pages:     len(info.Pages),
		Records:   info.Records,
		Frames:    len(info.Frames),
		Recovered: info.Recovered,
		StoppedAt: info.StoppedAt,
	}
	if info.StopErr != nil {
		if errors.Is(info.StopErr, ErrPartialTail) {
			rep.Warnings = append(rep.Warnings,
				"file ends with a torn page (expected after a crash; the last page was never committed)")
		} else {
			rep.Problems = append(rep.Problems, "unreadable page: "+info.StopErr.Error())
		}
	}
	if info.Truncated {
		rep.Warnings = append(rep.Warnings, "last frame is not terminated")
	}
	if info.BrokenChain {
		rep.Warnings = append(rep.Warnings,
			"a continuation chain was cut; one frame is incomplete")
	}
	if len(info.Frames) > 0 && info.Frames[0].Partial {
		rep.Warnings = append(rep.Warnings, "the first frame is missing its head")
	}
	for i := 0; i < len(info.Pages)-1; i++ {
		p := info.Pages[i]
		waste := CRCOffset - p.CommittedLen
		if waste > rep.WastedMax {
			rep.WastedMax = waste
		}
		if i == 0 || waste < rep.WastedMin {
			rep.WastedMin = waste
		}
	}
	if len(info.Pages) > 0 {
		// The last page is allowed to be partially filled.
		rep.LastPageFree = CRCOffset - info.Pages[len(info.Pages)-1].CommittedLen
	}
	if rep.WastedMax > 27 {
		rep.Problems = append(rep.Problems,
			fmt.Sprintf("a sealed page wastes %d bytes (limit 27)", rep.WastedMax))
	}
	return rep, nil
}
