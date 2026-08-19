//go:build !windows

package pipe

import (
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
)

var lockFile *os.File

func acquireLock() bool {
	var err error
	lockFile, err = os.OpenFile("/tmp/serial-tool-daemon.pid", os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return false
	}
	return syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

func releaseLock() {
	if lockFile != nil {
		syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		lockFile.Close()
		os.Remove("/tmp/serial-tool-daemon.pid")
	}
}

type pipeListener struct{ ln net.Listener }

func listenPipe(addr string) (Listener, error) {
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

func dialPipe(addr string) (io.ReadWriteCloser, error) {
	conn, err := net.Dial("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("daemon not running: %v", err)
	}
	return conn.(io.ReadWriteCloser), nil
}
