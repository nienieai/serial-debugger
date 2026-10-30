package sessionfile

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	ErrShortRecord = errors.New("stlog: truncated record")
	ErrBadDir      = errors.New("stlog: invalid dir byte")
	ErrBadLen      = errors.New("stlog: payload length out of range")
	ErrBadOrigin   = errors.New("stlog: invalid origin")
)

// Record is the on-disk unit: a 12-byte fixed header plus payload.
// A frame is one or more records; bit 7 of Dir marks "frame continues".
type Record struct {
	Ts        uint64
	Dir       uint8
	Origin    uint8
	Payload   []byte
	OriginStr string
}

// EncodedLen reports how many bytes this record occupies on disk.
func (r *Record) EncodedLen() int {
	n := RecordHeaderLen + len(r.Payload)
	if r.Origin == OriginInline {
		n += 1 + len(r.OriginStr)
	}
	return n
}

// EncodeRecord writes the record into dst and returns the bytes written.
func EncodeRecord(dst []byte, r *Record) int {
	binary.LittleEndian.PutUint64(dst[0:], r.Ts)
	dst[8] = r.Dir
	binary.LittleEndian.PutUint16(dst[9:], uint16(len(r.Payload)))
	dst[11] = r.Origin
	copy(dst[RecordHeaderLen:], r.Payload)
	n := RecordHeaderLen + len(r.Payload)
	if r.Origin == OriginInline {
		dst[n] = byte(len(r.OriginStr))
		copy(dst[n+1:], r.OriginStr)
		n += 1 + len(r.OriginStr)
	}
	return n
}

// DecodeRecords parses a contiguous run of records. It stops at the first
// malformed record and reports the error together with what it managed to read,
// mirroring the "read to the first bad record, do not guess" rule.
func DecodeRecords(b []byte) ([]Record, error) {
	var out []Record
	for len(b) > 0 {
		if len(b) < RecordHeaderLen {
			return out, fmt.Errorf("%w: %d trailing bytes", ErrShortRecord, len(b))
		}
		r := Record{
			Ts:     binary.LittleEndian.Uint64(b[0:]),
			Dir:    b[8],
			Origin: b[11],
		}
		l := int(binary.LittleEndian.Uint16(b[9:]))

		if !DirValid(r.Dir) {
			return out, fmt.Errorf("%w: %#x", ErrBadDir, r.Dir)
		}
		if l > MaxPayloadLen {
			return out, fmt.Errorf("%w: %d", ErrBadLen, l)
		}
		need := RecordHeaderLen + l
		if r.Origin == OriginInline {
			if len(b) < need+1 {
				return out, ErrShortRecord
			}
			ol := int(b[need])
			if ol > MaxOriginStr {
				return out, fmt.Errorf("%w: inline length %d", ErrBadOrigin, ol)
			}
			if len(b) < need+1+ol {
				return out, ErrShortRecord
			}
			r.OriginStr = string(b[need+1 : need+1+ol])
		} else if r.Origin == OriginNone || r.Origin <= 254 {
			// fine
		} else {
			return out, fmt.Errorf("%w: %d", ErrBadOrigin, r.Origin)
		}

		total := need
		if r.Origin == OriginInline {
			total += 1 + len(r.OriginStr)
		}
		if len(b) < total {
			return out, ErrShortRecord
		}
		r.Payload = b[RecordHeaderLen:need]
		out = append(out, r)
		b = b[total:]
	}
	return out, nil
}

// Frame is a set of records reassembled into one logical unit.
type Frame struct {
	Ts        uint64
	Dir       uint8
	Origin    uint8
	OriginStr string
	Payload   []byte
	// Partial is true when the frame is missing its head (the scan started in
	// the middle of a continuation chain) or its tail (file ended mid-frame).
	Partial bool
}

// BuildFrames stitches records into frames using the continuation bit.
// Continuation chains are single-linked: at most one frame can be open at any
// time, which is why no per-frame grouping is needed.
//
// breaks lists record indices where the chain must be cut even though the
// previous record claims a successor. The reader derives them from a
// contradiction the format makes visible: page P ends with dir bit7 set ("the
// frame continues") while page P+1 declares firstRecOff == 64 ("a frame starts
// here"). Both cannot be true, so a page was lost in between and the half frame
// must not swallow the next one.
//
// The scan is assumed to start at a frame boundary. A chain still open at the
// end of the input is reported as truncated.
func BuildFrames(recs []Record, breaks []int) (frames []Frame, truncated bool) {
	var cur *Frame
	bi := 0
	for i, r := range recs {
		for bi < len(breaks) && breaks[bi] == i {
			if cur != nil {
				cur.Partial = true
				frames = append(frames, *cur)
				cur = nil
			}
			bi++
		}
		if cur == nil {
			cur = &Frame{
				Ts:        r.Ts,
				Dir:       DirBase(r.Dir),
				Origin:    r.Origin,
				OriginStr: r.OriginStr,
			}
		}
		cur.Payload = append(cur.Payload, r.Payload...)
		if r.Dir&DirMore == 0 {
			frames = append(frames, *cur)
			cur = nil
		}
	}
	if cur != nil {
		cur.Partial = true
		frames = append(frames, *cur)
		truncated = true
	}
	return frames, truncated
}
