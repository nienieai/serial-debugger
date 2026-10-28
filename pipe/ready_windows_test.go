//go:build windows

package pipe

import (
	"testing"
	"time"
)

// WaitReady 是客户端「等守护进程就绪」的依据：
//   - 管道还不存在 → 必须等到超时返回 false（不能谎报就绪）；
//   - Listen 之后 → 必须立刻返回 true。
func TestWaitReadyFollowsListen(t *testing.T) {
	addr := uniqueAddr(t)

	start := time.Now()
	if waitReadyAddr(addr, 150*time.Millisecond) {
		t.Fatal("管道还不存在时 WaitReady 不应返回 true")
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("应当等满超时，实际只等了 %v", elapsed)
	}

	ln, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	start = time.Now()
	if !waitReadyAddr(addr, 2*time.Second) {
		t.Fatal("Listen 之后 WaitReady 应立即返回 true")
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("就绪后应立刻返回，实际耗时 %v", elapsed)
	}
}

// 关闭监听后端点消失，WaitReady 必须重新变回 false（避免「假就绪」）。
func TestWaitReadyGoesFalseAfterClose(t *testing.T) {
	addr := uniqueAddr(t)
	ln, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if !waitReadyAddr(addr, time.Second) {
		t.Fatal("Listen 之后应为就绪")
	}
	ln.Close()
	time.Sleep(50 * time.Millisecond)
	if waitReadyAddr(addr, 120*time.Millisecond) {
		t.Fatal("监听关闭后不应再报就绪")
	}
}
