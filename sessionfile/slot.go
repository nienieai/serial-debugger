package sessionfile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

var (
	ErrSlotMagic = errors.New("stlog: bad slot magic")
	ErrSlotVer   = errors.New("stlog: unsupported slot version")
	ErrSlotCRC   = errors.New("stlog: slot crc mismatch")
	ErrSlotEnd   = errors.New("stlog: slot end magic missing (truncated)")
	ErrSlotLen   = errors.New("stlog: slot payload too long")
)

// Slot is one complete settings snapshot.
type Slot struct {
	Seq     uint64
	Ts      uint64
	Payload []byte
}

const slotHeaderLen = 32

// EncodeSlot builds a full 4096-byte slot page.
func EncodeSlot(s *Slot) ([]byte, error) {
	if len(s.Payload) > MaxSlotPayload {
		return nil, fmt.Errorf("%w: %d", ErrSlotLen, len(s.Payload))
	}
	buf := make([]byte, SlotSize)
	binary.LittleEndian.PutUint32(buf[0:], SlotMagic)
	binary.LittleEndian.PutUint16(buf[4:], SlotVer)
	binary.LittleEndian.PutUint16(buf[6:], 0)
	binary.LittleEndian.PutUint64(buf[8:], s.Seq)
	binary.LittleEndian.PutUint64(buf[16:], s.Ts)
	binary.LittleEndian.PutUint16(buf[24:], uint16(len(s.Payload)))
	copy(buf[slotHeaderLen:], s.Payload)

	crcAt := slotHeaderLen + len(s.Payload)
	binary.LittleEndian.PutUint32(buf[crcAt:], crc32.ChecksumIEEE(buf[:crcAt]))
	binary.LittleEndian.PutUint32(buf[crcAt+4:], SlotEndMagic)
	return buf, nil
}

// DecodeSlot validates a slot page.
func DecodeSlot(buf []byte) (*Slot, error) {
	if len(buf) < SlotSize {
		return nil, fmt.Errorf("stlog: short slot buffer")
	}
	if binary.LittleEndian.Uint32(buf[0:]) != SlotMagic {
		return nil, ErrSlotMagic
	}
	if binary.LittleEndian.Uint16(buf[4:]) != SlotVer {
		return nil, ErrSlotVer
	}
	if binary.LittleEndian.Uint16(buf[6:]) != 0 {
		return nil, fmt.Errorf("stlog: unknown slot flags")
	}
	l := int(binary.LittleEndian.Uint16(buf[24:]))
	if l > MaxSlotPayload {
		return nil, fmt.Errorf("%w: %d", ErrSlotLen, l)
	}
	crcAt := slotHeaderLen + l
	if crcAt+8 > SlotSize {
		return nil, ErrSlotLen
	}
	end := binary.LittleEndian.Uint32(buf[crcAt+4:])
	if end != SlotEndMagic {
		return nil, ErrSlotEnd
	}
	want := binary.LittleEndian.Uint32(buf[crcAt:])
	if got := crc32.ChecksumIEEE(buf[:crcAt]); got != want {
		return nil, ErrSlotCRC
	}
	return &Slot{
		Seq:     binary.LittleEndian.Uint64(buf[8:]),
		Ts:      binary.LittleEndian.Uint64(buf[16:]),
		Payload: buf[slotHeaderLen:crcAt],
	}, nil
}

// ChooseSlot applies the deterministic selection rule: higher seq wins; on a
// tie the higher ts wins; still tied -> A. Returns nil when both are invalid.
func ChooseSlot(a, b *Slot) *Slot {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		return b
	case b == nil:
		return a
	}
	if a.Seq != b.Seq {
		if a.Seq > b.Seq {
			return a
		}
		return b
	}
	if a.Ts != b.Ts {
		if a.Ts > b.Ts {
			return a
		}
		return b
	}
	return a
}
