// Package ringbuf provides a byte-level ring buffer with length-prefixed
// packet storage, modeled after embedded C ring buffer implementations.
// Supports shared memory via Windows file mapping (see ringbuf_windows.go).
package ringbuf

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	// Length prefix is always 2 bytes (uint16, little-endian).
	lenPrefixSize = 2

	// Shared memory header layout (must match ringbuf_windows.go).
	headerMagicOffset = 0
	headerMagicSize   = 4
	headerVersionOff  = 4
	headerSizeOff     = 8
	headerHeadOff     = 12
	headerTailOff     = 16
	headerCountOff    = 20
	headerTotal       = 24
)

var magic = [4]byte{'R', 'I', 'N', 'G'}

// headerVersion 是共享内存头的布局版本。
//
// 任何会改变头字段含义、偏移或数据区语义的改动都必须递增它。守护进程与
// 客户端是各自独立升级的二进制，新客户端可能映射到旧守护进程（或反之）
// 创建的内存；版本不一致时必须明确拒绝，否则会按错误的偏移解释数据，
// 表现为静默乱码，甚至在数据区大小变化时越界。
const headerVersion = 1

// maxRingDataSize 是头中声明的数据区大小的合理上界。
// 头可能被破坏或来自不兼容的构建；不校验就据此构造切片会越界访问。
const maxRingDataSize = 64 * 1024 * 1024

// validateHeaderBytes 校验魔数、布局版本与数据区大小。
// hdr 至少要有 headerTotal 字节。
func validateHeaderBytes(hdr []byte) error {
	if len(hdr) < headerTotal {
		return fmt.Errorf("头部长度不足: %d", len(hdr))
	}
	if hdr[headerMagicOffset] != magic[0] || hdr[headerMagicOffset+1] != magic[1] ||
		hdr[headerMagicOffset+2] != magic[2] || hdr[headerMagicOffset+3] != magic[3] {
		return fmt.Errorf("魔数不匹配")
	}
	if v := binary.LittleEndian.Uint32(hdr[headerVersionOff:]); v != headerVersion {
		return fmt.Errorf("布局版本不匹配: 期望 %d, 实际 %d", headerVersion, v)
	}
	bs := binary.LittleEndian.Uint32(hdr[headerSizeOff:])
	if bs == 0 || bs > maxRingDataSize {
		return fmt.Errorf("非法的数据区大小: %d", bs)
	}
	return nil
}

// RingBuffer is a byte-level ring buffer. It stores variable-length packets
// prefixed with a 2-byte little-endian length. The underlying []byte may
// be memory-mapped from a shared memory region.
type RingBuffer struct {
	data       []byte         // full memory region (header + ring data)
	dataStart  uint32         // offset to ring data (= headerTotal)
	bufferSize uint32         // size of ring data area
	autoUnmap  bool           // if true, unmap on Close (for CreateShared)
	mapAddr    unsafe.Pointer // MapViewOfFile base address; not Go heap memory
	mapHandle  uintptr
	// owner 表示本 RingBuffer 是共享内存对象的创建者。
	//
	// Windows 上对象由内核引用计数管理，最后一句柄关闭即释放，无需区分。
	// POSIX 共享内存则会一直留在 /dev/shm 里直到被显式 unlink，而客户端也会
	// 打开并 Close 同一个环 —— 若客户端也去 unlink，守护进程的环就没了。
	owner bool
	// shmFile 是 POSIX 共享内存对象路径，创建者 Close 时据此 unlink。
	// Windows 实现不使用（对象随句柄自动释放）。
	shmFile string

	mu sync.Mutex // protects concurrent read operations (Read/Peek/DrainAll/Reset)
}

// Header returns pointers into the shared header for atomic access.
func (rb *RingBuffer) headPtr() *uint32 {
	return (*uint32)(unsafe.Pointer(&rb.data[headerHeadOff]))
}
func (rb *RingBuffer) tailPtr() *uint32 {
	return (*uint32)(unsafe.Pointer(&rb.data[headerTailOff]))
}
func (rb *RingBuffer) countPtr() *uint32 {
	return (*uint32)(unsafe.Pointer(&rb.data[headerCountOff]))
}

// initHeader writes the initial header values. Only called by creator.
func (rb *RingBuffer) initHeader(bufSize uint32) {
	copy(rb.data[headerMagicOffset:], magic[:])
	binary.LittleEndian.PutUint32(rb.data[headerVersionOff:], headerVersion)
	binary.LittleEndian.PutUint32(rb.data[headerSizeOff:], bufSize)
	binary.LittleEndian.PutUint32(rb.data[headerHeadOff:], 0)
	binary.LittleEndian.PutUint32(rb.data[headerTailOff:], 0)
	binary.LittleEndian.PutUint32(rb.data[headerCountOff:], 0)
}

// wrapMemory wraps an already-validated mapping as a RingBuffer.
// The data slice must include the header, and its header must have passed
// validateHeaderBytes — the declared data-area size is read from it here.
func wrapMemory(data []byte, autoUnmap bool, mapAddr unsafe.Pointer, mapHandle uintptr) *RingBuffer {
	bs := atomic.LoadUint32((*uint32)(unsafe.Pointer(&data[headerSizeOff])))
	return &RingBuffer{
		data:       data,
		dataStart:  headerTotal,
		bufferSize: bs,
		autoUnmap:  autoUnmap,
		mapAddr:    mapAddr,
		mapHandle:  mapHandle,
	}
}

// ── Write ──

// Write stores data with a 2-byte little-endian length prefix.
// Returns false if insufficient space.
func (rb *RingBuffer) Write(pkt []byte) bool {
	if len(pkt) > 0xFFFF {
		return false // packet too large for 2-byte length
	}
	required := uint32(len(pkt) + lenPrefixSize)
	if required > rb.FreeSpace() {
		return false
	}

	head := atomic.LoadUint32(rb.headPtr())
	buf := rb.data[rb.dataStart : rb.dataStart+rb.bufferSize]

	// Write length prefix (little-endian)
	buf[head] = byte(len(pkt))
	head = (head + 1) % rb.bufferSize
	buf[head] = byte(len(pkt) >> 8)
	head = (head + 1) % rb.bufferSize

	// Write data
	for i := 0; i < len(pkt); i++ {
		buf[head] = pkt[i]
		head = (head + 1) % rb.bufferSize
	}

	atomic.StoreUint32(rb.headPtr(), head)
	atomic.AddUint32(rb.countPtr(), required)
	return true
}

// ── Read (public, locked) ──

// Read extracts one packet. Returns (nil, false) if no complete packet
// is available or maxLen is too small.
func (rb *RingBuffer) Read(maxLen uint32) ([]byte, bool) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.read(maxLen)
}

// read is the internal, unlocked version of Read.
func (rb *RingBuffer) read(maxLen uint32) ([]byte, bool) {
	if atomic.LoadUint32(rb.countPtr()) < lenPrefixSize {
		return nil, false
	}

	tail := atomic.LoadUint32(rb.tailPtr())
	buf := rb.data[rb.dataStart : rb.dataStart+rb.bufferSize]

	// Read length prefix (little-endian)
	lo := buf[tail]
	tail = (tail + 1) % rb.bufferSize
	hi := buf[tail]
	tail = (tail + 1) % rb.bufferSize
	pktLen := uint32(lo) | uint32(hi)<<8

	if pktLen > maxLen || atomic.LoadUint32(rb.countPtr()) < (lenPrefixSize+pktLen) {
		return nil, false
	}

	// Read data
	out := make([]byte, pktLen)
	for i := uint32(0); i < pktLen; i++ {
		out[i] = buf[tail]
		tail = (tail + 1) % rb.bufferSize
	}

	atomic.StoreUint32(rb.tailPtr(), tail)
	atomic.AddUint32(rb.countPtr(), -(lenPrefixSize + pktLen))
	return out, true
}

// ── Peek (public, locked) ──

// PeekPacketLength returns the length of the next packet's data
// without consuming it. Returns 0 if no complete packet.
func (rb *RingBuffer) PeekPacketLength() uint32 {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.peekPacketLength()
}

// peekPacketLength is the internal, unlocked version of PeekPacketLength.
func (rb *RingBuffer) peekPacketLength() uint32 {
	if atomic.LoadUint32(rb.countPtr()) < lenPrefixSize {
		return 0
	}
	tail := atomic.LoadUint32(rb.tailPtr())
	buf := rb.data[rb.dataStart : rb.dataStart+rb.bufferSize]
	lo := buf[tail]
	hi := buf[(tail+1)%rb.bufferSize]
	pktLen := uint32(lo) | uint32(hi)<<8
	if atomic.LoadUint32(rb.countPtr()) < (lenPrefixSize + pktLen) {
		return 0
	}
	return pktLen
}

// ── Status ──

func (rb *RingBuffer) FreeSpace() uint32 {
	return rb.bufferSize - atomic.LoadUint32(rb.countPtr())
}

func (rb *RingBuffer) UsedSpace() uint32 {
	return atomic.LoadUint32(rb.countPtr())
}

func (rb *RingBuffer) IsEmpty() bool {
	return atomic.LoadUint32(rb.countPtr()) == 0
}

func (rb *RingBuffer) IsFull() bool {
	return atomic.LoadUint32(rb.countPtr()) == rb.bufferSize
}

func (rb *RingBuffer) Capacity() uint32 {
	return rb.bufferSize
}

// Reset clears the buffer.
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	atomic.StoreUint32(rb.headPtr(), 0)
	atomic.StoreUint32(rb.tailPtr(), 0)
	atomic.StoreUint32(rb.countPtr(), 0)
}

// DrainAll reads all packets and returns them as a slice of byte slices.
// Holds the read lock for the entire drain to prevent concurrent reads.
func (rb *RingBuffer) DrainAll() [][]byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	var out [][]byte
	for {
		pktLen := rb.peekPacketLength()
		if pktLen == 0 {
			break
		}
		data, ok := rb.read(pktLen)
		if !ok {
			break
		}
		out = append(out, data)
	}
	return out
}

// Snapshot copies all packets without consuming them. The buffer state
// (tail, count) is unchanged after the call; callers get an independent
// copy that can be safely parsed without affecting other readers.
func (rb *RingBuffer) Snapshot() [][]byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	count := atomic.LoadUint32(rb.countPtr())
	if count < lenPrefixSize {
		return nil
	}

	tail := atomic.LoadUint32(rb.tailPtr())
	buf := rb.data[rb.dataStart : rb.dataStart+rb.bufferSize]
	remaining := count
	var out [][]byte

	for remaining >= lenPrefixSize {
		// Read length prefix
		lo := buf[tail]
		tail = (tail + 1) % rb.bufferSize
		hi := buf[tail]
		tail = (tail + 1) % rb.bufferSize
		pktLen := uint32(lo) | uint32(hi)<<8

		if lenPrefixSize+pktLen > remaining {
			break // corrupt entry, stop
		}

		// Copy packet data
		pkt := make([]byte, pktLen)
		for i := uint32(0); i < pktLen; i++ {
			pkt[i] = buf[tail]
			tail = (tail + 1) % rb.bufferSize
		}

		out = append(out, pkt)
		remaining -= lenPrefixSize + pktLen
	}
	return out
}

// OldestTimestampMs returns the timestamp (Unix milliseconds) of the oldest
// packet in the buffer, or 0 if empty. Does not consume any data.
func (rb *RingBuffer) OldestTimestampMs() int64 {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if atomic.LoadUint32(rb.countPtr()) < lenPrefixSize {
		return 0
	}

	tail := atomic.LoadUint32(rb.tailPtr())
	buf := rb.data[rb.dataStart : rb.dataStart+rb.bufferSize]

	// Skip length prefix (2 bytes)
	pktLen := uint32(buf[tail]) | uint32(buf[(tail+1)%rb.bufferSize])<<8
	if pktLen < 8 || atomic.LoadUint32(rb.countPtr()) < lenPrefixSize+pktLen {
		return 0
	}
	tail = (tail + 2) % rb.bufferSize

	// Read first 8 bytes of packet (timestamp LE int64)
	var tsBytes [8]byte
	for i := 0; i < 8; i++ {
		tsBytes[i] = buf[tail]
		tail = (tail + 1) % rb.bufferSize
	}
	return int64(binary.LittleEndian.Uint64(tsBytes[:]))
}
