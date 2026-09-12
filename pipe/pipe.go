// Package pipe provides cross-platform named pipe IPC.
package pipe

import "io"

// Addr 是守护进程监听的端点地址。具体取值由各平台文件定义：
//
//	Windows: \\.\pipe\serial-tool-daemon
//	Unix:    $XDG_RUNTIME_DIR/serial-tool/daemon.sock（无该变量时回退到
//	         $TMPDIR/serial-tool-<uid>/daemon.sock）
//
// 它必须是平台相关的：早期版本把它写死成 Windows 命名管道路径，在 Linux 上
// 会变成当前工作目录里一个名为 `\\.\pipe\serial-tool-daemon` 的套接字文件。

// Endpoint 把一个逻辑名映射为该平台的 IPC 端点地址。
//
// 客户端需要为每个会话建立两条回调端点（响应与事件），逻辑名形如
// "st-<clientId>-resp"。同样不能写死 Windows 前缀。
func Endpoint(name string) string { return endpoint(name) }

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
