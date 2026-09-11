//go:build windows

package ringbuf

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	kernel32DLL           = syscall.NewLazyDLL("kernel32.dll")
	procCreateFileMapping = kernel32DLL.NewProc("CreateFileMappingW")
	procOpenFileMapping   = kernel32DLL.NewProc("OpenFileMappingW")
	procMapViewOfFile     = kernel32DLL.NewProc("MapViewOfFile")
	procUnmapViewOfFile   = kernel32DLL.NewProc("UnmapViewOfFile")
)

const (
	fileMapAllAccess   = 0x000F001F
	fileMapRead        = 0x0004
	pageReadWrite      = 0x04
	invalidHandleValue = ^uintptr(0)
)

// CreateSharedRing creates a named shared-memory ring buffer.
// The total mapping size is headerTotal + ringDataSize.
// name is the shared memory object name, e.g. "serial-tool-history-1".
func CreateSharedRing(name string, ringDataSize uint32) (*RingBuffer, error) {
	if ringDataSize == 0 || ringDataSize > maxRingDataSize {
		return nil, fmt.Errorf("非法的环形缓冲区大小: %d", ringDataSize)
	}
	totalSize := uint32(headerTotal) + ringDataSize
	wname, _ := syscall.UTF16PtrFromString(name)

	h, _, err := procCreateFileMapping.Call(
		invalidHandleValue, // paging file backed
		0,                  // default security
		pageReadWrite,
		0,                  // high size
		uintptr(totalSize), // low size
		uintptr(unsafe.Pointer(wname)),
	)
	if h == 0 {
		return nil, fmt.Errorf("CreateFileMapping failed: %v", err)
	}
	// 同名对象已存在时 CreateFileMapping 会「成功」并返回既有 section 的句柄，
	// 错误码为 ERROR_ALREADY_EXISTS。此时若继续使用，两个逻辑上无关的进程
	// （例如上一实例残留的客户端与本次守护进程）就会共享同一块内存，互相
	// 覆盖彼此的环形缓冲区。必须报错而不是静默复用。
	if err == syscall.ERROR_ALREADY_EXISTS {
		syscall.CloseHandle(syscall.Handle(h))
		return nil, fmt.Errorf("共享内存 %q 已存在：可能有上一实例的客户端仍在运行，请关闭后重试", name)
	}

	addr, _, err := procMapViewOfFile.Call(
		h,
		fileMapAllAccess,
		0, 0,
		uintptr(totalSize),
	)
	if addr == 0 {
		syscall.CloseHandle(syscall.Handle(h))
		return nil, fmt.Errorf("MapViewOfFile failed: %v", err)
	}

	// MapViewOfFile returns a raw address. Converting it to unsafe.Pointer is
	// safe here: the region is a file mapping, not Go heap memory, so the GC
	// never moves it, and its lifetime is bounded by UnmapViewOfFile.
	// go vet's unsafeptr check is deliberately conservative about uintptr
	// results from syscall wrappers and still reports these conversions; the
	// conversion is done once per mapping and kept in mapAddr.
	base := unsafe.Pointer(addr)
	data := unsafe.Slice((*byte)(base), totalSize)
	// 创建路径上大小是已知的，直接构造，不做「先读头再回填」——
	// 那样会依赖 initHeader 的调用顺序，容易在后续改动中被破坏。
	rb := &RingBuffer{
		data:       data,
		dataStart:  headerTotal,
		bufferSize: ringDataSize,
		autoUnmap:  true,
		mapAddr:    base,
		mapHandle:  h,
	}
	rb.initHeader(ringDataSize)
	return rb, nil
}

// OpenSharedRing opens an existing named shared-memory ring buffer for reading.
func OpenSharedRing(name string) (*RingBuffer, error) {
	wname, _ := syscall.UTF16PtrFromString(name)

	h, _, err := procOpenFileMapping.Call(
		uintptr(fileMapRead),
		0,
		uintptr(unsafe.Pointer(wname)),
	)
	if h == 0 {
		return nil, fmt.Errorf("OpenFileMapping failed: shared memory '%s' not found", name)
	}

	addr, _, err := procMapViewOfFile.Call(
		h,
		uintptr(fileMapRead),
		0, 0,
		0, // map entire section
	)
	if addr == 0 {
		syscall.CloseHandle(syscall.Handle(h))
		return nil, fmt.Errorf("MapViewOfFile failed: %v", err)
	}

	base := unsafe.Pointer(addr)
	// 先只按头部长度建立视图并校验，校验通过后才按头里声明的大小重建视图。
	// 直接用未经校验的 bufSize 构造切片会得到一个长度荒谬的切片。
	header := unsafe.Slice((*byte)(base), uintptr(headerTotal))
	if verr := validateHeaderBytes(header); verr != nil {
		procUnmapViewOfFile.Call(addr)
		syscall.CloseHandle(syscall.Handle(h))
		return nil, fmt.Errorf("共享内存 %q 头部无效: %w", name, verr)
	}
	bs := atomic.LoadUint32((*uint32)(unsafe.Add(base, headerSizeOff)))
	totalSize := uintptr(headerTotal) + uintptr(bs)

	data := unsafe.Slice((*byte)(base), totalSize)
	return wrapMemory(data, true, base, h), nil
}

// OpenSharedRingForWrite opens an existing named shared-memory ring buffer
// for both reading and writing. Used by clients that need to push data
// into a send queue that the daemon will consume.
func OpenSharedRingForWrite(name string) (*RingBuffer, error) {
	wname, _ := syscall.UTF16PtrFromString(name)

	h, _, err := procOpenFileMapping.Call(
		uintptr(fileMapAllAccess),
		0,
		uintptr(unsafe.Pointer(wname)),
	)
	if h == 0 {
		return nil, fmt.Errorf("OpenFileMapping failed: shared memory '%s' not found", name)
	}

	addr, _, err := procMapViewOfFile.Call(
		h,
		uintptr(fileMapAllAccess),
		0, 0,
		0, // map entire section
	)
	if addr == 0 {
		syscall.CloseHandle(syscall.Handle(h))
		return nil, fmt.Errorf("MapViewOfFile failed: %v", err)
	}

	base := unsafe.Pointer(addr)
	// 同 OpenSharedRing：先校验头部，再按声明大小重建视图。
	header := unsafe.Slice((*byte)(base), uintptr(headerTotal))
	if verr := validateHeaderBytes(header); verr != nil {
		procUnmapViewOfFile.Call(addr)
		syscall.CloseHandle(syscall.Handle(h))
		return nil, fmt.Errorf("共享内存 %q 头部无效: %w", name, verr)
	}
	bs := atomic.LoadUint32((*uint32)(unsafe.Add(base, headerSizeOff)))
	totalSize := uintptr(headerTotal) + uintptr(bs)

	data := unsafe.Slice((*byte)(base), totalSize)
	return wrapMemory(data, true, base, h), nil
}

// Close unmaps the shared memory and closes the handle.
func (rb *RingBuffer) Close() error {
	if rb.autoUnmap && rb.mapAddr != nil {
		procUnmapViewOfFile.Call(uintptr(rb.mapAddr))
		rb.mapAddr = nil
	}
	if rb.mapHandle != 0 {
		syscall.CloseHandle(syscall.Handle(rb.mapHandle))
		rb.mapHandle = 0
	}
	return nil
}
