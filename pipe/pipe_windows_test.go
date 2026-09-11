//go:build windows

package pipe

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"testing"
	"time"
)

// uniqueAddr 生成本次测试专用的管道名，避免并行或残留实例互相干扰。
func uniqueAddr(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`\\.\pipe\st-test-%s-%s`, t.Name(), hex.EncodeToString(b))
}

// Listen 返回时管道实例就必须已经存在。
//
// 否则调用方「Listen → go Accept() → 立刻通知对端 → 对端马上 Dial」
// 会在 Accept 的 goroutine 还没被调度到 CreateNamedPipe 时被对端连上，
// 对端拿到 ERROR_FILE_NOT_FOUND，表现为间歇性的管道不可用。
func TestListenCreatesInstanceImmediately(t *testing.T) {
	addr := uniqueAddr(t)

	ln, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	// 注意：这里刻意不启动任何 Accept goroutine。
	conn, err := Dial(addr)
	if err != nil {
		t.Fatalf("Listen 返回后立刻 Dial 应成功（实例必须已创建）: %v", err)
	}
	defer conn.Close()
}

// 复现客户端的三管道握手时序：Listen 之后只以 goroutine 形式进入 Accept，
// 随即被对端 Dial。旧实现下这个窗口会让对端拿到 ERROR_FILE_NOT_FOUND。
func TestDialRacesAcceptGoroutine(t *testing.T) {
	const iterations = 50

	for i := 0; i < iterations; i++ {
		addr := uniqueAddr(t)
		// 每轮换个名字，避免上一轮的监听器影响下一轮
		addr = fmt.Sprintf("%s-%d", addr, i)

		ln, err := Listen(addr)
		if err != nil {
			t.Fatalf("第 %d 轮 Listen: %v", i, err)
		}

		accepted := make(chan io.ReadWriteCloser, 1)
		go func() {
			c, err := ln.Accept()
			if err == nil {
				accepted <- c
			}
		}()

		// 不给 Accept 的 goroutine 任何调度保证，直接拨号。
		conn, err := Dial(addr)
		if err != nil {
			ln.Close()
			t.Fatalf("第 %d 轮 Dial 失败（Accept 尚未创建实例）: %v", i, err)
		}

		select {
		case c := <-accepted:
			// 收发一轮，确认连接真的可用
			payload := []byte("ping\n")
			if _, err := conn.Write(payload); err != nil {
				t.Fatalf("第 %d 轮写入失败: %v", i, err)
			}
			buf := make([]byte, len(payload))
			if _, err := io.ReadFull(c, buf); err != nil {
				t.Fatalf("第 %d 轮读取失败: %v", i, err)
			}
			if string(buf) != string(payload) {
				t.Fatalf("第 %d 轮数据不一致: %q != %q", i, buf, payload)
			}
			c.Close()
		case <-time.After(3 * time.Second):
			conn.Close()
			ln.Close()
			t.Fatalf("第 %d 轮 Accept 未在 3 秒内返回", i)
		}

		conn.Close()
		ln.Close()
	}
}

// dialPair 建立一个已连接的管道对，返回服务端与客户端两端。
func dialPair(t *testing.T) (server, client io.ReadWriteCloser) {
	t.Helper()
	addr := uniqueAddr(t)

	ln, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	accepted := make(chan io.ReadWriteCloser, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			accepted <- c
		}
	}()

	client, err = Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	select {
	case server = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("Accept 超时")
	}
	t.Cleanup(func() { server.Close() })
	return server, client
}

// CancelPending 必须唤醒阻塞中的 Read。
//
// 同步句柄上的 CloseHandle 并不保证做到这一点 —— 已发起的同步 Read 会一直
// 停在那里直到对端关闭。守护进程摘除慢客户端时要靠它让会话的读协程立刻退出，
// 否则该协程会一直挂到客户端进程结束。
func TestCancelPendingUnblocksRead(t *testing.T) {
	server, _ := dialPair(t)

	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 16)
		_, err := server.Read(buf)
		readDone <- err
	}()

	// 给读协程一点时间真正进入阻塞
	time.Sleep(200 * time.Millisecond)
	CancelPending(server)

	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("取消之后 Read 应返回错误")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CancelPending 未能唤醒阻塞中的 Read")
	}
}

// CancelPending 同样要唤醒阻塞中的 Write（对端不读时管道缓冲区写满即阻塞）。
func TestCancelPendingUnblocksWrite(t *testing.T) {
	server, _ := dialPair(t)

	// 刻意不读取 client 端，让服务端的写最终阻塞在满的管道缓冲区上。
	writeDone := make(chan struct{})
	go func() {
		chunk := make([]byte, 64*1024)
		for i := 0; i < 1024; i++ {
			if _, err := server.Write(chunk); err != nil {
				close(writeDone)
				return
			}
		}
		close(writeDone)
	}()

	// 让它写满缓冲区并阻塞
	time.Sleep(300 * time.Millisecond)
	CancelPending(server)

	select {
	case <-writeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("CancelPending 未能唤醒阻塞中的 Write")
	}
}

// Close 不能在存在在途 Read 的情况下阻塞。
//
// 同步句柄上的 CloseHandle 会等待在途 I/O 完成，而对端不发送数据就永远不
// 完成。守护进程的会话清理路径会在其它协程仍可能阻塞于该连接时关闭它，
// 因此 Close 必须先取消在途 I/O。
func TestCloseDoesNotBlockWithPendingRead(t *testing.T) {
	server, _ := dialPair(t)

	readDone := make(chan struct{})
	go func() {
		buf := make([]byte, 16)
		_, _ = server.Read(buf)
		close(readDone)
	}()

	// 让读协程真正进入阻塞
	time.Sleep(200 * time.Millisecond)

	closed := make(chan struct{})
	go func() {
		_ = server.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close 在存在在途 Read 时阻塞了")
	}

	select {
	case <-readDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Close 之后阻塞中的 Read 未返回")
	}

	// 重复关闭必须是空操作（摘除路径与 removeSession 可能都会关闭它）
	if err := server.Close(); err != nil {
		t.Fatalf("重复 Close 应无错误: %v", err)
	}
}

// Close 之后 Accept 必须立刻返回错误，而不是永久阻塞在 ConnectNamedPipe。
func TestCloseUnblocksAccept(t *testing.T) {
	addr := uniqueAddr(t)

	ln, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := ln.Accept()
		done <- err
	}()

	// 等 Accept 进入 ConnectNamedPipe 再关闭
	time.Sleep(100 * time.Millisecond)
	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Close 之后 Accept 应返回错误")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close 之后 Accept 仍然阻塞")
	}
}
