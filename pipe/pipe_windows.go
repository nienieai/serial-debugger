//go:build windows

package pipe

import (
	"fmt"
	"io"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procCreateNamedPipeW = kernel32.NewProc("CreateNamedPipeW")
	procConnectNamedPipe = kernel32.NewProc("ConnectNamedPipe")
	procCreateMutexW     = kernel32.NewProc("CreateMutexW")
	procCloseHandle      = kernel32.NewProc("CloseHandle")
	procWaitNamedPipeW   = kernel32.NewProc("WaitNamedPipeW")
	procCreateFileW      = kernel32.NewProc("CreateFileW")
	procCancelIoEx       = kernel32.NewProc("CancelIoEx")
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

// callErrno 取出 Proc.Call 第三个返回值中的 Win32 错误码。
//
// 必须用 Proc.Call 的第三个返回值，不能改用 windows.GetLastError()：
// 后者是另一次系统调用，此时 goroutine 可能已经被调度到别的 OS 线程，
// 读到的是那个线程的 last-error（通常是 0），于是真正的错误码丢失。
// x/sys/windows 的 Proc.Call 文档明确要求由调用方直接使用该返回值。
func callErrno(callErr error) windows.Errno {
	errno, _ := callErr.(windows.Errno)
	return errno
}

func acquireLock() bool {
	name, _ := windows.UTF16PtrFromString("Global\\serial-tool-daemon")
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	singletonHandle = windows.Handle(h)
	if h == 0 {
		return false
	}
	return callErrno(callErr) != windows.ERROR_ALREADY_EXISTS
}

func releaseLock() {
	if singletonHandle != 0 {
		procCloseHandle.Call(uintptr(singletonHandle))
	}
}

type pipeListener struct {
	path string
	mu   sync.Mutex
	// closing 后不再接受新连接。
	closing bool
	// currHandle 是当前已有 ConnectNamedPipe 在途的实例，Close 需要取消它的 I/O。
	currHandle windows.Handle
	// nextHandle 是预建好、留给下一次 Accept 的实例。
	nextHandle windows.Handle
}

// createPipeInstance 创建一个命名管道实例（尚未 ConnectNamedPipe）。
// 实例一经创建，管道名即存在且可被客户端 CreateFile 连上；随后的
// ConnectNamedPipe 会立刻返回 ERROR_PIPE_CONNECTED。
func createPipeInstance(addr string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(addr)
	if err != nil {
		return 0, err
	}
	handle, _, callErr := procCreateNamedPipeW.Call(
		uintptr(unsafe.Pointer(name)),
		pipeAccessDuplex,
		pipeTypeByte|pipeReadmodeByte|pipeWait,
		pipeUnlimitedInstances,
		65536, 65536, 0, 0,
	)
	if handle == invalidHandleValue || handle == 0 {
		return 0, fmt.Errorf("CreateNamedPipe failed: %v", callErr)
	}
	return windows.Handle(handle), nil
}

func listenPipe(addr string) (Listener, error) {
	l := &pipeListener{path: addr}
	// 必须在 Listen 返回前就把实例建好。调用方拿到 Listener 后通常是
	// 「go l.Accept()」再立刻向对端发消息，对端随即回调 Dial：如果实例
	// 要等 Accept 的 goroutine 被调度后才由 CreateNamedPipe 创建，对端
	// 会在这个窗口里拿到 ERROR_FILE_NOT_FOUND（WaitNamedPipe 对不存在的
	// 管道名立即返回失败），表现为间歇性的「管道不可用」。
	// Unix 实现的 net.Listen 同样是在 Listen 阶段就完成绑定，此处对齐。
	h, err := createPipeInstance(addr)
	if err != nil {
		return nil, err
	}
	l.nextHandle = h
	return l, nil
}

func (l *pipeListener) Accept() (io.ReadWriteCloser, error) {
	for {
		l.mu.Lock()
		if l.closing {
			l.mu.Unlock()
			return nil, fmt.Errorf("listener closed")
		}
		handle := l.nextHandle
		l.nextHandle = 0
		l.mu.Unlock()

		if handle == 0 {
			// 正常路径上不会走到这里（Listen 预建 + 每次 Accept 补建），
			// 仅作为预建失败后的兜底。
			h, err := createPipeInstance(l.path)
			if err != nil {
				return nil, err
			}
			handle = h
		}

		l.mu.Lock()
		if l.closing {
			// Close 已经执行过：此时 handle 还没登记到 currHandle，
			// Close 不会去关它，必须由这里释放。
			l.mu.Unlock()
			procCloseHandle.Call(uintptr(handle))
			return nil, fmt.Errorf("listener closed")
		}
		l.currHandle = handle
		l.mu.Unlock()

		// 在阻塞于 ConnectNamedPipe 之前先补建下一个实例，让管道名在整个
		// 生命周期内始终有实例存在，杜绝两次 Accept 之间的空窗。
		if next, err := createPipeInstance(l.path); err == nil {
			l.mu.Lock()
			if l.closing {
				procCloseHandle.Call(uintptr(next))
			} else {
				l.nextHandle = next
			}
			l.mu.Unlock()
		}

		// ERROR_PIPE_CONNECTED 表示客户端在本实例创建之后、ConnectNamedPipe
		// 之前就连上来了——连接已经建立，属于正常结果而非错误。预建实例
		// 之后这种情况是常态，因此这里必须正确区分。
		ret, _, callErr := procConnectNamedPipe.Call(uintptr(handle), 0)
		if ret == 0 {
			errno := callErrno(callErr)
			if errno != windows.ERROR_PIPE_CONNECTED {
				procCloseHandle.Call(uintptr(handle))
				l.mu.Lock()
				l.currHandle = 0
				l.mu.Unlock()
				if errno == windows.ERROR_NO_DATA {
					continue
				}
				return nil, fmt.Errorf("ConnectNamedPipe failed: %v", callErr)
			}
		}
		// Connection handed off — clear currHandle so Close() won't
		// double-close this handle after the caller is done with it.
		l.mu.Lock()
		l.currHandle = 0
		l.mu.Unlock()
		return &winPipeConn{handle: handle}, nil
	}
}

func (l *pipeListener) Close() error {
	l.mu.Lock()
	l.closing = true
	if l.nextHandle != 0 {
		// 预建实例上没有在途 I/O，CloseHandle 不会阻塞。
		procCloseHandle.Call(uintptr(l.nextHandle))
		l.nextHandle = 0
	}
	if l.currHandle != 0 {
		// ConnectNamedPipe is a *synchronous* pending I/O on this handle.
		// Both DisconnectNamedPipe and CloseHandle block until that I/O
		// completes — and it never completes without a client, so either
		// call deadlocks the listener forever. CancelIoEx aborts pending
		// I/O on a handle from another thread; Accept then observes the
		// error, closes the handle it created and returns.
		//
		// No self-connect fallback is needed: Accept checks l.closing and
		// assigns l.currHandle inside a single critical section, so either
		// Accept wins the race (Close sees the handle and cancels it) or
		// Close wins (Accept sees closing and closes its own handle).
		procCancelIoEx.Call(uintptr(l.currHandle), 0)
	}
	l.mu.Unlock()
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
	ret, _, callErr := procGetPID.Call(uintptr(wc.handle), uintptr(unsafe.Pointer(&pid)))
	if ret == 0 {
		return 0, fmt.Errorf("GetNamedPipeClientProcessId failed: %v", callErr)
	}
	return pid, nil
}

func dialPipe(addr string) (io.ReadWriteCloser, error) {
	name, _ := windows.UTF16PtrFromString(addr)
	// WaitNamedPipe 在管道名不存在时立即失败（ERROR_FILE_NOT_FOUND），
	// 名字存在但实例都被占用时才等到超时；两种情况的错误码都要报出来，
	// 否则只能看到笼统的「管道不可用」。
	ret, _, callErr := procWaitNamedPipeW.Call(uintptr(unsafe.Pointer(name)), 5000)
	if ret == 0 {
		return nil, fmt.Errorf("pipe not available: %s: %v", addr, callErr)
	}
	handle, _, callErr := procCreateFileW.Call(
		uintptr(unsafe.Pointer(name)),
		genericRead|genericWrite, 0, 0,
		openExisting, 0, 0,
	)
	if handle == invalidHandleValue || handle == 0 {
		return nil, fmt.Errorf("CreateFile failed for pipe %s: %v", addr, callErr)
	}
	return &winPipeConn{handle: windows.Handle(handle)}, nil
}
