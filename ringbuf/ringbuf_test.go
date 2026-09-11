package ringbuf

import (
	"encoding/binary"
	"strings"
	"testing"
)

// newHeader 构造一个合法的共享内存头。
func newHeader(bufSize uint32) []byte {
	h := make([]byte, headerTotal)
	copy(h[headerMagicOffset:], magic[:])
	binary.LittleEndian.PutUint32(h[headerVersionOff:], headerVersion)
	binary.LittleEndian.PutUint32(h[headerSizeOff:], bufSize)
	return h
}

func TestValidateHeaderBytes(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func([]byte)
		wantErr bool
	}{
		{"合法头", func([]byte) {}, false},
		{"魔数错误", func(h []byte) { h[0] = 'X' }, true},
		{"布局版本过新", func(h []byte) {
			binary.LittleEndian.PutUint32(h[headerVersionOff:], headerVersion+1)
		}, true},
		{"布局版本过旧", func(h []byte) {
			binary.LittleEndian.PutUint32(h[headerVersionOff:], headerVersion-1)
		}, true},
		{"版本字段为零", func(h []byte) {
			binary.LittleEndian.PutUint32(h[headerVersionOff:], 0)
		}, true},
		{"数据区大小为 0", func(h []byte) {
			binary.LittleEndian.PutUint32(h[headerSizeOff:], 0)
		}, true},
		{"数据区大小超上界", func(h []byte) {
			binary.LittleEndian.PutUint32(h[headerSizeOff:], maxRingDataSize+1)
		}, true},
		{"数据区大小为 0xFFFFFFFF（损坏）", func(h []byte) {
			binary.LittleEndian.PutUint32(h[headerSizeOff:], 0xFFFFFFFF)
		}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHeader(4096)
			tc.mutate(h)
			err := validateHeaderBytes(h)
			if tc.wantErr && err == nil {
				t.Fatal("期望报错，但校验通过")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("期望通过，但报错: %v", err)
			}
		})
	}
}

// 头部长度不足时必须报错，而不是越界读取。
func TestValidateHeaderBytesShort(t *testing.T) {
	if err := validateHeaderBytes(make([]byte, headerTotal-1)); err == nil {
		t.Fatal("长度不足的头部应当报错")
	}
	if err := validateHeaderBytes(nil); err == nil {
		t.Fatal("nil 头部应当报错")
	}
}

// 版本字段此前只写不读，跨版本映射会静默按错误偏移解释数据。
// 这里锁住「版本必须是校验的一部分」这一行为。
func TestValidateHeaderBytesRejectsVersionMismatch(t *testing.T) {
	h := newHeader(1024)
	binary.LittleEndian.PutUint32(h[headerVersionOff:], 99)
	err := validateHeaderBytes(h)
	if err == nil {
		t.Fatal("版本不匹配必须被拒绝")
	}
	if !strings.Contains(err.Error(), "版本") {
		t.Fatalf("错误信息应指出是版本问题, got: %v", err)
	}
}
