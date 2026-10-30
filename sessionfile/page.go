package sessionfile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

var (
	ErrBadMagic     = errors.New("stlog: bad page magic")
	ErrBadVer       = errors.New("stlog: unsupported page version")
	ErrBadFlags     = errors.New("stlog: unknown page flags")
	ErrBadOffset    = errors.New("stlog: pageOffset does not match file position")
	ErrBadCommitted = errors.New("stlog: committedLen out of range")
	ErrBadFirstRec  = errors.New("stlog: firstRecOff out of range")
	ErrBadCRC       = errors.New("stlog: page crc mismatch")
	ErrBadStats     = errors.New("stlog: bad stats block")
	ErrPartialTail  = errors.New("stlog: partial tail page (torn write)")
)

// PageHeader is the 20-byte header at the start of every 4096-byte page.
type PageHeader struct {
	Magic        uint32
	Ver          uint16
	Flags        uint16
	PageOffset   uint64
	CommittedLen uint16
	FirstRecOff  uint16
}

// Marshal writes the header into b (len(b) >= PageHeaderLen).
func (h *PageHeader) Marshal(b []byte) {
	binary.LittleEndian.PutUint32(b[0:], h.Magic)
	binary.LittleEndian.PutUint16(b[4:], h.Ver)
	binary.LittleEndian.PutUint16(b[6:], h.Flags)
	binary.LittleEndian.PutUint64(b[8:], h.PageOffset)
	binary.LittleEndian.PutUint16(b[16:], h.CommittedLen)
	binary.LittleEndian.PutUint16(b[18:], h.FirstRecOff)
}

// Unmarshal reads the header from b.
func (h *PageHeader) Unmarshal(b []byte) {
	h.Magic = binary.LittleEndian.Uint32(b[0:])
	h.Ver = binary.LittleEndian.Uint16(b[4:])
	h.Flags = binary.LittleEndian.Uint16(b[6:])
	h.PageOffset = binary.LittleEndian.Uint64(b[8:])
	h.CommittedLen = binary.LittleEndian.Uint16(b[16:])
	h.FirstRecOff = binary.LittleEndian.Uint16(b[18:])
}

// Stats is the 44-byte block that immediately follows the page header.
// It has no CRC of its own: it is only ever written as part of a whole page.
type Stats struct {
	Magic   uint32
	Ver     uint16
	Rsv     uint16
	RxTotal uint64
	TxTotal uint64
	FirstTs uint64
	LastTs  uint64
	Rec     uint32
}

// Marshal writes the stats block into b (len(b) >= StatsLen).
func (s *Stats) Marshal(b []byte) {
	binary.LittleEndian.PutUint32(b[0:], s.Magic)
	binary.LittleEndian.PutUint16(b[4:], s.Ver)
	binary.LittleEndian.PutUint16(b[6:], s.Rsv)
	binary.LittleEndian.PutUint64(b[8:], s.RxTotal)
	binary.LittleEndian.PutUint64(b[16:], s.TxTotal)
	binary.LittleEndian.PutUint64(b[24:], s.FirstTs)
	binary.LittleEndian.PutUint64(b[32:], s.LastTs)
	binary.LittleEndian.PutUint32(b[40:], s.Rec)
}

// Unmarshal reads the stats block from b.
func (s *Stats) Unmarshal(b []byte) {
	s.Magic = binary.LittleEndian.Uint32(b[0:])
	s.Ver = binary.LittleEndian.Uint16(b[4:])
	s.Rsv = binary.LittleEndian.Uint16(b[6:])
	s.RxTotal = binary.LittleEndian.Uint64(b[8:])
	s.TxTotal = binary.LittleEndian.Uint64(b[16:])
	s.FirstTs = binary.LittleEndian.Uint64(b[24:])
	s.LastTs = binary.LittleEndian.Uint64(b[32:])
	s.Rec = binary.LittleEndian.Uint32(b[40:])
}

// Page is a decoded, validated page.
type Page struct {
	Header  PageHeader
	Stats   Stats
	Records []Record
}

// DecodePage validates a raw 4096-byte page that is expected to live at
// expectOffset. CRC is checked *before* any header field is trusted, then the
// field ranges are checked (a correct CRC does not prove the fields are sane).
func DecodePage(buf []byte, expectOffset uint64) (*Page, error) {
	if len(buf) < PageSize {
		return nil, fmt.Errorf("stlog: short page buffer (%d bytes)", len(buf))
	}
	var h PageHeader
	h.Unmarshal(buf[:PageHeaderLen])

	if h.Magic != PageMagic {
		return nil, ErrBadMagic
	}
	if h.Ver != PageVer {
		return nil, ErrBadVer
	}
	if h.Flags != 0 {
		return nil, ErrBadFlags
	}
	if h.PageOffset != expectOffset {
		return nil, ErrBadOffset
	}

	want := binary.LittleEndian.Uint32(buf[CRCOffset:])
	if got := crc32.ChecksumIEEE(buf[:CRCOffset]); got != want {
		return nil, ErrBadCRC
	}

	// From here the bytes are intact, but may still hold nonsense values.
	if h.CommittedLen < RecordsStart || h.CommittedLen > CRCOffset {
		return nil, fmt.Errorf("%w: %d", ErrBadCommitted, h.CommittedLen)
	}
	if h.FirstRecOff != NoFrameStart &&
		(h.FirstRecOff < RecordsStart || h.FirstRecOff >= h.CommittedLen) {
		return nil, fmt.Errorf("%w: %d", ErrBadFirstRec, h.FirstRecOff)
	}

	var st Stats
	st.Unmarshal(buf[PageHeaderLen:RecordsStart])
	if st.Magic != StatsMagic {
		return nil, ErrBadStats
	}

	recs, err := DecodeRecords(buf[RecordsStart:h.CommittedLen])
	if err != nil {
		return nil, err
	}
	return &Page{Header: h, Stats: st, Records: recs}, nil
}

// EncodePage assembles a full 4096-byte page from its parts. Records must
// already be encoded contiguously; the caller passes the committed length.
func EncodePage(pageOffset uint64, committedLen uint16, firstRecOff uint16,
	st Stats, body []byte) []byte {
	buf := make([]byte, PageSize)
	h := PageHeader{
		Magic:        PageMagic,
		Ver:          PageVer,
		PageOffset:   pageOffset,
		CommittedLen: committedLen,
		FirstRecOff:  firstRecOff,
	}
	h.Marshal(buf[:PageHeaderLen])
	st.Marshal(buf[PageHeaderLen:RecordsStart])
	copy(buf[RecordsStart:], body[:committedLen-RecordsStart])

	crc := crc32.ChecksumIEEE(buf[:CRCOffset])
	binary.LittleEndian.PutUint32(buf[CRCOffset:], crc)
	return buf
}

// PageCRC recomputes the checksum of a raw page buffer.
func PageCRC(buf []byte) uint32 { return crc32.ChecksumIEEE(buf[:CRCOffset]) }

// StoredCRC reads the checksum recorded in the page tail.
func StoredCRC(buf []byte) uint32 { return binary.LittleEndian.Uint32(buf[CRCOffset:]) }

// SetPageCRC stamps the checksum into the page tail.
func SetPageCRC(buf []byte) {
	binary.LittleEndian.PutUint32(buf[CRCOffset:], crc32.ChecksumIEEE(buf[:CRCOffset]))
}
