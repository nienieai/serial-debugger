//go:build !windows

package ringbuf

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

// POSIX 共享内存实现（Linux / macOS / *BSD）。
//
// 与 Windows 版的对应关系：
//
//	CreateFileMappingW  -> open("/dev/shm/<name>", O_CREAT|O_EXCL)
//	MapViewOfFile       -> mmap(MAP_SHARED)
//	UnmapViewOfFile     -> munmap
//	CloseHandle         -> close(fd)
//	引用计数释放对象     -> 显式 unlink（因此需要 owner 标记，见 RingBuffer.owner）
//
// 名字语义与 Windows 保持一致：已存在时创建失败而不是静默复用，理由同
// CreateSharedRing 的注释——两个逻辑上无关的进程共享同一块环会导致互相覆盖。

const shmDir = "/dev/shm"

// shmPath 把逻辑环名映射为共享内存对象路径。
//
// 环名形如 "serial-tool-history-3a9cbcf8-1"，字符集本就安全，这里仍做一次
// 白名单清洗，避免调用方传入带斜杠或空字节的名字把路径带出 shmDir。
func shmPath(name string) string {
	var b strings.Builder
	b.WriteString(shmDir)
	b.WriteByte('/')
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// openRing 是创建与打开两条路径的公共实现。
func openRing(name string, ringDataSize uint32, create bool) (*RingBuffer, error) {
	if create && (ringDataSize == 0 || ringDataSize > maxRingDataSize) {
		return nil, fmt.Errorf("非法的环形缓冲区大小: %d", ringDataSize)
	}

	path := shmPath(name)
	flags := unix.O_RDWR
	if create {
		flags |= unix.O_CREAT | unix.O_EXCL
	}

	fd, err := unix.Open(path, flags, 0o600)
	if err != nil {
		switch {
		case errors.Is(err, unix.EEXIST):
			return nil, fmt.Errorf("共享内存 %q 已存在：可能有上一实例的客户端仍在运行，请关闭后重试", name)
		case errors.Is(err, unix.ENOENT):
			return nil, fmt.Errorf("OpenFileMapping failed: shared memory '%s' not found", name)
		default:
			return nil, fmt.Errorf("打开共享内存 %s 失败: %w", path, err)
		}
	}

	total := int(headerTotal) + int(ringDataSize)
	if create {
		if err := unix.Ftruncate(fd, int64(total)); err != nil {
			unix.Close(fd)
			_ = unix.Unlink(path)
			return nil, fmt.Errorf("设置共享内存大小失败: %w", err)
		}
	} else {
		// 打开已存在的对象时，真实长度只能从对象本身取：调用方并不知道
		// 创建时用的 ringDataSize（传来的是 0）。用 fstat 而不是信任头里
		// 声明的值来定 mmap 长度 —— 头是映射之后才校验的，不能先信。
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err != nil {
			unix.Close(fd)
			return nil, fmt.Errorf("读取共享内存大小失败: %w", err)
		}
		total = int(st.Size)
		if total < headerTotal {
			unix.Close(fd)
			return nil, fmt.Errorf("共享内存 %q 长度异常: %d 字节", name, total)
		}
	}

	data, err := unix.Mmap(fd, 0, total, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unix.Close(fd)
		if create {
			_ = unix.Unlink(path)
		}
		return nil, fmt.Errorf("映射共享内存失败: %w", err)
	}

	base := unsafe.Pointer(&data[0])

	if !create {
		// 与 Windows 版同源：先按头长度校验，再采用头里声明的大小。
		// 直接用未经校验的值会得到一个长度荒谬的切片。
		if verr := validateHeaderBytes(data[:headerTotal]); verr != nil {
			_ = unix.Munmap(data)
			unix.Close(fd)
			return nil, fmt.Errorf("共享内存 %q 头部无效: %w", name, verr)
		}
		bs := atomic.LoadUint32((*uint32)(unsafe.Add(base, headerSizeOff)))
		if int(headerTotal)+int(bs) > len(data) {
			_ = unix.Munmap(data)
			unix.Close(fd)
			return nil, fmt.Errorf("共享内存 %q 声明的数据区(%d)超出映射长度(%d)", name, bs, len(data))
		}
		return &RingBuffer{
			data:       data,
			dataStart:  headerTotal,
			bufferSize: bs,
			autoUnmap:  true,
			mapAddr:    base,
			mapHandle:  uintptr(fd),
			shmFile:    path,
		}, nil
	}

	rb := &RingBuffer{
		data:       data,
		dataStart:  headerTotal,
		bufferSize: ringDataSize,
		autoUnmap:  true,
		mapAddr:    base,
		mapHandle:  uintptr(fd),
		owner:      true,
		shmFile:    path,
	}
	rb.initHeader(ringDataSize)
	return rb, nil
}

// CreateSharedRing 创建命名共享内存环。语义与 Windows 版一致。
func CreateSharedRing(name string, ringDataSize uint32) (*RingBuffer, error) {
	return openRing(name, ringDataSize, true)
}

// OpenSharedRing 打开已存在的环。调用方需保证后续不写（见 Windows 版注释：
// 该路径从未接通，保留以维持 API 对称）。
func OpenSharedRing(name string) (*RingBuffer, error) {
	return openRing(name, 0, false)
}

// OpenSharedRingForWrite 打开已存在的环用于读写。
func OpenSharedRingForWrite(name string) (*RingBuffer, error) {
	return openRing(name, 0, false)
}

// Close 解除映射并关闭文件描述符；创建者还负责 unlink。
func (rb *RingBuffer) Close() error {
	if rb.autoUnmap && rb.mapAddr != nil && rb.data != nil {
		_ = unix.Munmap(rb.data)
		rb.mapAddr = nil
	}
	if rb.mapHandle != 0 {
		unix.Close(int(rb.mapHandle))
		rb.mapHandle = 0
	}
	if rb.owner {
		// 没有名字就无从再打开；创建者销毁环时一并清掉，否则 /dev/shm 会
		// 随着每次守护进程重启不断堆积（环名带实例令牌，永不复用）。
		_ = unix.Unlink(rb.shmFile)
		rb.owner = false
	}
	return nil
}
