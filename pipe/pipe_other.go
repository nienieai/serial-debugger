//go:build !windows

package pipe

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

var lockFile *os.File

// socketDir 返回本用户的 IPC 端点目录。
//
// 优先 XDG_RUNTIME_DIR（权限通常为 0700，仅本人可访问）；否则回退到临时目录下
// 带 uid 的子目录。不能直接用 /tmp 根目录：那是全局可写的，别的用户既能抢占
// 套接字文件，也能窥探端点名。
func socketDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "serial-tool")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("serial-tool-%d", os.Getuid()))
}

func lockPath() string { return filepath.Join(socketDir(), "daemon.pid") }

// Addr 见 pipe.go 的说明，由各平台实现。
var Addr = filepath.Join(socketDir(), "daemon.sock")

// endpoint 把逻辑名映射为该平台的端点地址。
func endpoint(name string) string { return filepath.Join(socketDir(), name+".sock") }

func acquireLock() bool {
	if err := os.MkdirAll(socketDir(), 0o700); err != nil {
		return false
	}
	var err error
	lockFile, err = os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	return syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

func releaseLock() {
	if lockFile != nil {
		syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		lockFile.Close()
		os.Remove(lockPath())
	}
}

type pipeListener struct{ ln net.Listener }

func listenPipe(addr string) (Listener, error) {
	// Unix 套接字要求父目录存在
	if err := os.MkdirAll(filepath.Dir(addr), 0o700); err != nil {
		return nil, err
	}
	os.Remove(addr)
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	return &pipeListener{ln: ln}, nil
}

func (l *pipeListener) Accept() (io.ReadWriteCloser, error) {
	conn, err := l.ln.Accept()
	if err != nil {
		return nil, err
	}
	return conn.(io.ReadWriteCloser), nil
}

func (l *pipeListener) Close() error { return l.ln.Close() }

func clientPID(conn io.ReadWriteCloser) (uint32, error) {
	return 0, nil
}

// cancelPending is a no-op on Unix: closing the net.Conn already unblocks any
// goroutine parked in Read or Write.
func cancelPending(conn io.ReadWriteCloser) {}

func dialPipe(addr string) (io.ReadWriteCloser, error) {
	conn, err := net.Dial("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("daemon not running: %v", err)
	}
	return conn.(io.ReadWriteCloser), nil
}
