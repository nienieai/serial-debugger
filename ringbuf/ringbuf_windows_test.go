//go:build windows

package ringbuf

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func uniqueRingName(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "st-ringtest-" + t.Name() + "-" + hex.EncodeToString(b)
}

// 建环 → 写 → 读，确认基本通路没有被头校验改动破坏。
func TestCreateSharedRingRoundTrip(t *testing.T) {
	name := uniqueRingName(t)
	rb, err := CreateSharedRing(name, 4096)
	if err != nil {
		t.Fatalf("CreateSharedRing: %v", err)
	}
	defer rb.Close()

	if rb.bufferSize != 4096 {
		t.Fatalf("bufferSize 应为 4096, got %d", rb.bufferSize)
	}

	payload := []byte("hello ring")
	if !rb.Write(payload) {
		t.Fatal("Write 失败")
	}
	got, ok := rb.Read(1024)
	if !ok {
		t.Fatal("Read 失败")
	}
	if string(got) != string(payload) {
		t.Fatalf("读回数据不一致: %q != %q", got, payload)
	}
}

// 同名重复创建必须报错。
//
// CreateFileMappingW 遇到已存在的同名对象会返回既有 section 的句柄、并把
// 错误码置为 ERROR_ALREADY_EXISTS；此前代码只在句柄为 0 时才看错误码，于是
// 静默复用了别人的内存。守护进程的进程号计数器每次重启都从 1 重新开始，
// 残留客户端可能仍映射着同名环，因此这条检查是真实场景下的防线。
func TestCreateSharedRingRejectsDuplicateName(t *testing.T) {
	name := uniqueRingName(t)

	first, err := CreateSharedRing(name, 4096)
	if err != nil {
		t.Fatalf("首次创建应成功: %v", err)
	}
	defer first.Close()

	second, err := CreateSharedRing(name, 4096)
	if err == nil {
		second.Close()
		t.Fatal("同名重复创建必须报错，否则两个进程会共享同一块内存")
	}
	if !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("错误信息应说明同名对象已存在, got: %v", err)
	}
}

// 非法大小必须在创建阶段就被拒绝，而不是留下一个头部荒谬的环。
func TestCreateSharedRingRejectsBadSize(t *testing.T) {
	for _, size := range []uint32{0, maxRingDataSize + 1} {
		name := uniqueRingName(t)
		rb, err := CreateSharedRing(name, size)
		if err == nil {
			rb.Close()
			t.Fatalf("ringDataSize=%d 应被拒绝", size)
		}
	}
}

// 打开路径要能正确识别头部，并按头里声明的大小重建视图。
func TestOpenSharedRingForWrite(t *testing.T) {
	name := uniqueRingName(t)

	creator, err := CreateSharedRing(name, 8192)
	if err != nil {
		t.Fatalf("CreateSharedRing: %v", err)
	}
	defer creator.Close()

	opened, err := OpenSharedRingForWrite(name)
	if err != nil {
		t.Fatalf("OpenSharedRingForWrite: %v", err)
	}
	defer opened.Close()

	if opened.bufferSize != 8192 {
		t.Fatalf("打开后 bufferSize 应为 8192, got %d", opened.bufferSize)
	}

	// 通过打开的视图写入，创建方应能读到 —— 证明二者确实指向同一块内存。
	payload := []byte("from-opened-view")
	if !opened.Write(payload) {
		t.Fatal("通过打开的视图写入失败")
	}
	got, ok := creator.Read(1024)
	if !ok {
		t.Fatal("创建方读取失败")
	}
	if string(got) != string(payload) {
		t.Fatalf("数据不一致: %q != %q", got, payload)
	}
}

// 被破坏的头部（版本不符）必须在打开时被拒绝，而不是照常返回一个环。
func TestOpenSharedRingForWriteRejectsBadVersion(t *testing.T) {
	name := uniqueRingName(t)

	creator, err := CreateSharedRing(name, 4096)
	if err != nil {
		t.Fatalf("CreateSharedRing: %v", err)
	}
	defer creator.Close()

	// 模拟「不兼容的另一版本」写下的头
	creator.data[headerVersionOff] = headerVersion + 7

	if rb, err := OpenSharedRingForWrite(name); err == nil {
		rb.Close()
		t.Fatal("版本不匹配的头部必须被拒绝")
	}
}
