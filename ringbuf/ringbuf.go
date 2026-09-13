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

// WriteEvict 写入一个包；空间不足时先挤掉最旧的包，直到装得下。
//
// 为什么需要它：Write 在环满时返回 false，而守护进程的记录路径此前忽略了这个
// 返回值 —— 于是一旦写满 5MB，历史就**永久停在那一刻**，之后收到的数据既不进
// 环也不被任何人发现（磁盘文件仍在写，但界面上「往回翻」永远翻到同一段）。
// 环形缓冲的正确语义是保留**最新**的一段，所以这里挤掉旧的。
//
// 调用方必须与 Write 走同一把互斥（守护进程是 ringBufMu）：本方法不加锁，
// 因为 Write 也不加，而跨进程读者从未接通（见 ARCHITECTURE 关于 OpenSharedRing
// 的说明）。单个包超过整个环容量时返回 false。
func (rb *RingBuffer) WriteEvict(pkt []byte) bool {
	if len(pkt) > 0xFFFF {
		return false
	}
	required := uint32(len(pkt) + lenPrefixSize)
	if required > rb.bufferSize {
		return false
	}
	for rb.FreeSpace() < required {
		if !rb.discardOldest() {
			return false
		}
	}
	return rb.Write(pkt)
}

// discardOldest 丢掉最旧的一个包。不加锁，理由同 WriteEvict。
func (rb *RingBuffer) discardOldest() bool {
	if atomic.LoadUint32(rb.countPtr()) < lenPrefixSize {
		return false
	}
	tail := atomic.LoadUint32(rb.tailPtr())
	buf := rb.data[rb.dataStart : rb.dataStart+rb.bufferSize]
	var pktLen uint32
	if tail+lenPrefixSize <= rb.bufferSize {
		pktLen = uint32(buf[tail]) | uint32(buf[tail+1])<<8
	} else {
		pktLen = uint32(buf[tail]) | uint32(buf[(tail+1)%rb.bufferSize])<<8
	}
	if atomic.LoadUint32(rb.countPtr()) < lenPrefixSize+pktLen {
		return false // 尾部不完整，宁可不动也不要破坏计数
	}
	if next := tail + lenPrefixSize + pktLen; next >= rb.bufferSize {
		tail = next - rb.bufferSize
	} else {
		tail = next
	}
	atomic.StoreUint32(rb.tailPtr(), tail)
	atomic.AddUint32(rb.countPtr(), -(lenPrefixSize + pktLen))
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
//
// 一次性取整环的版本，保留给需要全部数据的调用方；守护进程的历史读取走
// SnapshotPage（只复制一页，见下），因为整环 5MB 逐包复制的开销远大于
// 一次性取回对界面的价值。
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

// PageResult 是一次分页快照的结果。
type PageResult struct {
	// Packets 是本次选中的包，按时间升序。
	Packets [][]byte
	// Older 是比 Packets 更早、但**仍在环里**的包数量。
	Older int
	// HasMore 等价于 Older > 0，单独给出来是因为调用方真正要问的就是
	// 「环里还有没有更早的」——这决定了要不要继续往磁盘翻。
	HasMore bool
	// OldestMs 是环里最旧包的毫秒时间戳，环为空时为 0。
	OldestMs int64
}

// SnapshotPage 是 Snapshot 的分页版本：只复制「调用方还没有的那一段」里的
// 最后 limit 个包，用于前端有界缓存向上回补。
//
// 游标语义（beforeMs + sameTsSkip）：
//
//	beforeMs <= 0       取最新的 limit 个（首次加载）
//	beforeMs > 0        只考虑 ts < beforeMs 的包，外加 ts == beforeMs 这一毫秒组里
//	                    除**最新** sameTsSkip 个之外的部分
//
// 为什么要 sameTsSkip：毫秒时间戳会撞车（同一毫秒内到达多帧是常态），只用
// `ts < beforeMs` 做游标会把边界那一毫秒剩下的包永久丢掉，而只用 `<` 之外的
// 任何近似判断又会让同一批包被反复返回。调用方报出「我手上有几条是 beforeMs
// 这一毫秒的」，两个方向就都对上了。
//
// 不走 Snapshot 再切片：整环可能有两万个包、5MB，逐包复制一遍是这条路径上
// 唯一的实际开销；这里全程只记位置（4 字节/包），最后才复制选中的那几个。
func (rb *RingBuffer) SnapshotPage(limit int, beforeMs int64, sameTsSkip int) PageResult {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	var res PageResult

	count := atomic.LoadUint32(rb.countPtr())
	if count < lenPrefixSize {
		return res
	}

	tail := atomic.LoadUint32(rb.tailPtr())
	buf := rb.data[rb.dataStart : rb.dataStart+rb.bufferSize]
	remaining := count

	var offsets []uint32   // 严格早于游标的包
	var eqOffsets []uint32 // 与游标同一毫秒的包
	first := true

	for remaining >= lenPrefixSize {
		start := tail

		// 快路径：包不跨过缓冲区末端时，长度前缀与时间戳都能直接按切片读。
		// 整环 19 万个包，逐字节 + 取模的写法会把这条路径压到十几毫秒。
		var pktLen uint32
		if start+lenPrefixSize <= rb.bufferSize {
			pktLen = uint32(buf[start]) | uint32(buf[start+1])<<8
		} else {
			pktLen = uint32(buf[start]) | uint32(buf[(start+1)%rb.bufferSize])<<8
		}

		if lenPrefixSize+pktLen > remaining {
			break // 损坏条目，与 Snapshot 同样停下
		}
		remaining -= lenPrefixSize + pktLen

		var tsMs int64
		if pktLen >= 8 {
			if off := start + lenPrefixSize; off+8 <= rb.bufferSize {
				tsMs = int64(binary.LittleEndian.Uint64(buf[off:]))
			} else {
				var b [8]byte
				for i := uint32(0); i < 8; i++ {
					b[i] = buf[(off+i)%rb.bufferSize]
				}
				tsMs = int64(binary.LittleEndian.Uint64(b[:]))
			}
		}
		if first {
			res.OldestMs = tsMs
			first = false
		}

		// 环是按时间升序的：撞到比游标新的一侧就可以停，后面只会更新。
		if beforeMs > 0 && tsMs > beforeMs {
			break
		}
		if beforeMs > 0 && tsMs == beforeMs {
			eqOffsets = append(eqOffsets, start)
		} else {
			offsets = append(offsets, start)
		}

		// 单次减法代替取模：step < bufferSize，最多溢出一轮
		if next := start + lenPrefixSize + pktLen; next >= rb.bufferSize {
			tail = next - rb.bufferSize
		} else {
			tail = next
		}
	}

	// 同一毫秒组在时间上整体晚于 offsets，接在后面。
	// 留下的是这一组里**最旧**的 keep 条：调用方手上是最新的 sameTsSkip 条。
	keep := len(eqOffsets) - sameTsSkip
	if keep < 0 {
		keep = 0 // 调用方持有的比环里留着的还多（被挤掉了），这一组就都别给了
	}
	all := offsets
	all = append(all, eqOffsets[:keep]...)

	if limit > 0 && len(all) > limit {
		res.Older = len(all) - limit
		all = all[len(all)-limit:]
	}
	res.HasMore = res.Older > 0

	if len(all) == 0 {
		return res
	}
	res.Packets = make([][]byte, 0, len(all))
	for _, o := range all {
		var pktLen uint32
		if o+lenPrefixSize <= rb.bufferSize {
			pktLen = uint32(buf[o]) | uint32(buf[o+1])<<8
		} else {
			pktLen = uint32(buf[o]) | uint32(buf[(o+1)%rb.bufferSize])<<8
		}
		pkt := make([]byte, pktLen)
		if off := o + lenPrefixSize; off+pktLen <= rb.bufferSize {
			copy(pkt, buf[off:off+pktLen]) // 快路径：整包连续
		} else {
			for i := uint32(0); i < pktLen; i++ {
				pkt[i] = buf[(off+i)%rb.bufferSize]
			}
		}
		res.Packets = append(res.Packets, pkt)
	}
	return res
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
