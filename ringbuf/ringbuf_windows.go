//go:build windows

package ringbuf

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	kernel32DLL          = syscall.NewLazyDLL("kernel32.dll")
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

	data := unsafe.Slice((*byte)(unsafe.Pointer(addr)), totalSize)
	rb := wrapMemory(data, true, addr, h)
	rb.initHeader(ringDataSize)
	rb.bufferSize = ringDataSize // set after initHeader writes to shared mem
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

	// Read the actual size from the mapped header
	bs := atomic.LoadUint32((*uint32)(unsafe.Pointer(addr + headerSizeOff)))
	totalSize := uintptr(headerTotal + bs)

	data := unsafe.Slice((*byte)(unsafe.Pointer(addr)), totalSize)
	rb := wrapMemory(data, true, addr, h)
	if !rb.verifyHeader() {
		rb.Close()
		return nil, fmt.Errorf("invalid ring buffer header in '%s'", name)
	}
	return rb, nil
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

	bs := atomic.LoadUint32((*uint32)(unsafe.Pointer(addr + headerSizeOff)))
	totalSize := uintptr(headerTotal + bs)

	data := unsafe.Slice((*byte)(unsafe.Pointer(addr)), totalSize)
	rb := wrapMemory(data, true, addr, h)
	if !rb.verifyHeader() {
		rb.Close()
		return nil, fmt.Errorf("invalid ring buffer header in '%s'", name)
	}
	return rb, nil
}

// Close unmaps the shared memory and closes the handle.
func (rb *RingBuffer) Close() error {
	if rb.autoUnmap && rb.mapAddr != 0 {
		procUnmapViewOfFile.Call(rb.mapAddr)
		rb.mapAddr = 0
	}
	if rb.mapHandle != 0 {
		syscall.CloseHandle(syscall.Handle(rb.mapHandle))
		rb.mapHandle = 0
	}
	return nil
}
