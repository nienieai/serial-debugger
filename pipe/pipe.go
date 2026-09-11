// Package pipe provides cross-platform named pipe IPC.
package pipe

import "io"

const Addr = `\\.\pipe\serial-tool-daemon`

// Listener is a cross-platform pipe listener.
type Listener interface {
	Accept() (io.ReadWriteCloser, error)
	Close() error
}

// AcquireLock acquires the singleton instance lock.
// Returns false if another instance is already running.
func AcquireLock() bool {
	return acquireLock()
}

// ReleaseLock releases the singleton instance lock.
func ReleaseLock() {
	releaseLock()
}

// Listen creates a pipe listener at the given address.
func Listen(addr string) (Listener, error) {
	return listenPipe(addr)
}

// Dial connects to a pipe server at the given address.
func Dial(addr string) (io.ReadWriteCloser, error) {
	return dialPipe(addr)
}

// ClientPID returns the process ID of the connected peer.
func ClientPID(conn io.ReadWriteCloser) (uint32, error) {
	return clientPID(conn)
}

// CancelPending aborts the I/O currently blocking on conn, so a goroutine
// parked in Read or Write returns immediately instead of waiting for the peer.
//
// This is needed because closing a handle does NOT reliably wake a synchronous
// Read/Write already in flight on Windows: the blocked call stays parked until
// the peer closes its end. Without cancelling first, tearing down a connection
// from another goroutine leaves the reader/writer goroutine stuck.
func CancelPending(conn io.ReadWriteCloser) {
	cancelPending(conn)
}
