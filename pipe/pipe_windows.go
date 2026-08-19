//go:build windows

package pipe

import (
	"fmt"
	"io"
	"unsafe"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

var (
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procCreateNamedPipeW    = kernel32.NewProc("CreateNamedPipeW")
	procConnectNamedPipe    = kernel32.NewProc("ConnectNamedPipe")
	procDisconnectNamedPipe = kernel32.NewProc("DisconnectNamedPipe")
	procCreateMutexW        = kernel32.NewProc("CreateMutexW")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
	procWaitNamedPipeW      = kernel32.NewProc("WaitNamedPipeW")
	procCreateFileW         = kernel32.NewProc("CreateFileW")
)

const (
	pipeAccessDuplex       = 0x00000003
	pipeTypeByte           = 0x00000000
	pipeReadmodeByte       = 0x00000000
	pipeWait               = 0x00000000
	pipeUnlimitedInstances = 255
	genericRead            = 0x80000000
	genericWrite           = 0x40000000
	openExisting           = 3
	invalidHandleValue     = ^uintptr(0)
)

var singletonHandle windows.Handle

func acquireLock() bool {
	name, _ := windows.UTF16PtrFromString("Global\\serial-tool-daemon")
	h, _, lastErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	singletonHandle = windows.Handle(h)
	if h == 0 {
		return false
	}
	return lastErr != windows.ERROR_ALREADY_EXISTS
}

func releaseLock() {
	if singletonHandle != 0 {
		procCloseHandle.Call(uintptr(singletonHandle))
	}
}

type pipeListener struct {
	path       string
	mu         sync.Mutex
	closing    bool
	currHandle windows.Handle
}

func listenPipe(addr string) (Listener, error) {
	return &pipeListener{path: addr}, nil
}

func (l *pipeListener) Accept() (io.ReadWriteCloser, error) {
	name, _ := windows.UTF16PtrFromString(l.path)
	for {
		l.mu.Lock()
		if l.closing {
			l.mu.Unlock()
			return nil, fmt.Errorf("listener closed")
		}
		l.mu.Unlock()

		handle, _, _ := procCreateNamedPipeW.Call(
			uintptr(unsafe.Pointer(name)),
			pipeAccessDuplex,
			pipeTypeByte|pipeReadmodeByte|pipeWait,
			pipeUnlimitedInstances,
			65536, 65536, 0, 0,
		)
		if handle == invalidHandleValue || handle == 0 {
			return nil, fmt.Errorf("CreateNamedPipe failed: %v", windows.GetLastError())
		}

		h := windows.Handle(handle)
		l.mu.Lock()
		if l.closing {
			l.mu.Unlock()
			procCloseHandle.Call(handle)
			return nil, fmt.Errorf("listener closed")
		}
		l.currHandle = h
		l.mu.Unlock()

		ret, _, _ := procConnectNamedPipe.Call(handle, 0, 0)
		if ret == 0 {
			err := windows.GetLastError()
			if err != windows.ERROR_PIPE_CONNECTED {
				procCloseHandle.Call(handle)
				l.mu.Lock()
				l.currHandle = 0
				l.mu.Unlock()
				if err == windows.ERROR_NO_DATA {
					continue
				}
				return nil, fmt.Errorf("ConnectNamedPipe failed: %v", err)
			}
		}
		// Connection handed off — clear currHandle so Close() won't
		// double-close this handle after the caller is done with it.
		l.mu.Lock()
		l.currHandle = 0
		l.mu.Unlock()
		return &winPipeConn{handle: h}, nil
	}
}

func (l *pipeListener) Close() error {
	l.mu.Lock()
	l.closing = true
	if l.currHandle != 0 {
		procDisconnectNamedPipe.Call(uintptr(l.currHandle))
		procCloseHandle.Call(uintptr(l.currHandle))
		l.currHandle = 0
	}
	l.mu.Unlock()

	// Self-connect to unblock any Accept that already created a new pipe
	// instance but hasn't reached the closing check yet. Small initial
	// delay gives Accept time to reach ConnectNamedPipe, then 40 retries
	// (2 s total) to account for scheduling delays under load.
	time.Sleep(20 * time.Millisecond)
	name, _ := windows.UTF16PtrFromString(l.path)
	for i := 0; i < 40; i++ {
		handle, _, _ := procCreateFileW.Call(
			uintptr(unsafe.Pointer(name)),
			genericRead|genericWrite, 0, 0,
			openExisting, 0, 0,
		)
		if handle != 0 && handle != invalidHandleValue {
			procCloseHandle.Call(handle)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	return nil
}

type winPipeConn struct{ handle windows.Handle }

func (c *winPipeConn) Read(p []byte) (int, error) {
	done := uint32(0)
	err := windows.ReadFile(c.handle, p, &done, nil)
	if err != nil {
		if err == windows.ERROR_BROKEN_PIPE {
			return 0, io.EOF
		}
		return int(done), err
	}
	if done == 0 {
		return 0, io.EOF
	}
	return int(done), nil
}

func (c *winPipeConn) Write(p []byte) (int, error) {
	done := uint32(0)
	err := windows.WriteFile(c.handle, p, &done, nil)
	if err != nil {
		return int(done), err
	}
	return int(done), nil
}

func (c *winPipeConn) Close() error {
	procCloseHandle.Call(uintptr(c.handle))
	return nil
}

func clientPID(conn io.ReadWriteCloser) (uint32, error) {
	wc, ok := conn.(*winPipeConn)
	if !ok {
		return 0, fmt.Errorf("not a pipe connection")
	}
	procGetPID := kernel32.NewProc("GetNamedPipeClientProcessId")
	var pid uint32
	ret, _, _ := procGetPID.Call(uintptr(wc.handle), uintptr(unsafe.Pointer(&pid)))
	if ret == 0 {
		return 0, fmt.Errorf("GetNamedPipeClientProcessId failed: %v", windows.GetLastError())
	}
	return pid, nil
}

func dialPipe(addr string) (io.ReadWriteCloser, error) {
	name, _ := windows.UTF16PtrFromString(addr)
	ret, _, _ := procWaitNamedPipeW.Call(uintptr(unsafe.Pointer(name)), 5000)
	if ret == 0 {
		return nil, fmt.Errorf("pipe not available: %s", addr)
	}
	handle, _, _ := procCreateFileW.Call(
		uintptr(unsafe.Pointer(name)),
		genericRead|genericWrite, 0, 0,
		openExisting, 0, 0,
	)
	if handle == invalidHandleValue || handle == 0 {
		return nil, fmt.Errorf("CreateFile failed for pipe %s: %v", addr, windows.GetLastError())
	}
	return &winPipeConn{handle: windows.Handle(handle)}, nil
}
