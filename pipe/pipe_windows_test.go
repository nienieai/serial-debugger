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
