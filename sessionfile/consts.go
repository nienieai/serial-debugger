package sessionfile

// Fixed geometry of the v1.4 on-disk format. Nothing here is configurable:
// changing any of these is a format break and requires bumping a version field.
const (
	PageSize        = 4096
	PageHeaderLen   = 20
	StatsLen        = 44
	RecordsStart    = PageHeaderLen + StatsLen // 64
	CRCOffset       = PageSize - 4             // 4092: CRC occupies [4092,4096)
	RecordHeaderLen = 12
	MaxRecordLen    = CRCOffset - RecordsStart       // 4028
	MaxPayloadLen   = MaxRecordLen - RecordHeaderLen // 4016
	MinPageRemain   = RecordHeaderLen + 16           // 28: admission gate
	PageVer         = 1
)

// File layout: two setting slots, two shadow pages, then fixed-offset records.
const (
	SlotSize      = 4096
	SlotAOffset   = 0
	SlotBOffset   = 4096
	ShadowAOffset = 8192
	ShadowBOffset = 12288
	RecordsOffset = 16384 // 0x4000, 4 KiB aligned
	HeaderSize    = RecordsOffset
)

// Slot constants.
//
// SlotPayloadVer 记的是**payload 的编码方式**，不是字段清单：v3 起 payload
// 是 TLV 表（见 payload.go），加字段只要分配新 key，不用再动这个数。
const (
	SlotVer        = 1
	SlotPayloadVer = 3
	MaxSlotPayload = 4000
)

// Magic values, stored little-endian as uint32.
const (
	PageMagic    uint32 = 0x47504C53 // "SLPG"
	StatsMagic   uint32 = 0x54415453 // "STAT"
	SlotMagic    uint32 = 0x474F4C53 // "SLOG"
	SlotEndMagic uint32 = 0x474F4C45 // "ELOG"
)

// NoFrameStart marks a page that contains no frame-starting record.
const NoFrameStart uint16 = 0xFFFF

// Directions. Bit 7 (DirMore) means "this frame continues in a later record".
const (
	DirSystem uint8 = 0
	DirRx     uint8 = 1
	DirTx     uint8 = 2
	DirP1     uint8 = 3
	DirP2     uint8 = 4
	DirMore   uint8 = 0x80
)

// Origin encoding inside a record.
const (
	OriginNone   uint8 = 0   // no source attribution
	OriginInline uint8 = 255 // followed by len byte + UTF-8 string
	MaxOriginStr       = 64
)

// DirBase strips the continuation bit.
func DirBase(d uint8) uint8 { return d & 0x7F }

// DirValid reports whether the low 7 bits name a known direction.
func DirValid(d uint8) bool {
	b := DirBase(d)
	return b <= DirP2
}

// OtherShadow returns the other shadow slot offset.
func OtherShadow(off int64) int64 {
	if off == ShadowAOffset {
		return ShadowBOffset
	}
	return ShadowAOffset
}
