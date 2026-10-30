package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 来源标记用的是「长度字段最高位 + 包尾追加」这套兼容格式（见 historyOriginFlag），
// 最容易出错的是新老格式混读：老包必须照旧解析、新包必须带出来源、坏包必须被
// 跳过而不是 panic（历史文件可能被外部工具截断）。
func TestHistoryPacketOriginRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		origin string
	}{
		{"客户端来源", "gui-a1b2c3d4"},
		{"无来源", ""},
		{"引擎来源", "auto"},
	}
	for _, c := range cases {
		pkt := encodeHistoryPacket(1720000000123, "AABB", "tx", c.origin)
		wantLen := 11 + 4
		if c.origin != "" {
			wantLen += 1 + len(c.origin)
		}
		if len(pkt) != wantLen {
			t.Fatalf("%s: 包长=%d, want %d", c.name, len(pkt), wantLen)
		}
		entry, ok := decodeHistoryPacket(pkt)
		if !ok {
			t.Fatalf("%s: 解码失败", c.name)
		}
		if entry.Origin != c.origin {
			t.Fatalf("%s: 来源=%q, want %q", c.name, entry.Origin, c.origin)
		}
		if entry.Hex != "AABB" || entry.Direction != "tx" || entry.TsMs != 1720000000123 {
			t.Fatalf("%s: 字段错位: %+v", c.name, entry)
		}
	}
}

// 来源字符串用 1 字节记长度，超长必须截断而不是让长度字节回绕（否则包格式崩坏）。
func TestHistoryPacketOriginLengthCapped(t *testing.T) {
	long := strings.Repeat("x", maxOriginLen+40)
	entry, ok := decodeHistoryPacket(encodeHistoryPacket(1, "AA", "tx", long))
	if !ok {
		t.Fatal("解码失败")
	}
	if len(entry.Origin) != maxOriginLen {
		t.Fatalf("来源长度=%d, want %d", len(entry.Origin), maxOriginLen)
	}
}

// 截断的包一律判坏并跳过：尤其是「有来源但来源段不完整」这种，只有高位标记
// 能揭示后面还有字节，按老格式读会把包尾截断当成下一条包的头部，越读越乱。
func TestDecodeHistoryPacketRejectsTruncated(t *testing.T) {
	pkt := encodeHistoryPacket(1000, "AABB", "tx", "gui-a1b2c3d4")
	for _, cut := range []int{0, 5, 10, 11, len(pkt) - 1} {
		if _, ok := decodeHistoryPacket(pkt[:cut]); ok {
			t.Fatalf("截断到 %d 字节仍被当作有效包", cut)
		}
	}
}

// 混读：老包（无来源）与新包（带来源）拼在一个文件里，逐条读出来必须一一对齐。
func TestHistoryFileMixedOriginAndLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.log")
	var buf bytes.Buffer
	buf.Write(encodeHistoryPacket(1000, "AA", "rx", ""))
	buf.Write(encodeHistoryPacket(1001, "BB", "tx", "cli-deadbeef"))
	buf.Write(encodeHistoryPacket(1002, "sys.port_opened|COM1|115200", "system", ""))
	buf.Write(encodeHistoryPacket(1003, "CC", "tx", "auto"))
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开测试文件失败: %v", err)
	}
	defer f.Close()

	var got []HistoryEntry
	r := bufio.NewReader(f)
	for {
		entry, _, err := readHistoryEntry(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("读取第 %d 条失败: %v", len(got), err)
		}
		got = append(got, entry)
	}

	want := []struct{ hex, dir, origin string }{
		{"AA", "rx", ""},
		{"BB", "tx", "cli-deadbeef"},
		{"sys.port_opened|COM1|115200", "system", ""},
		{"CC", "tx", "auto"},
	}
	if len(got) != len(want) {
		t.Fatalf("读到 %d 条, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Hex != w.hex || got[i].Direction != w.dir || got[i].Origin != w.origin {
			t.Fatalf("第 %d 条=%+v, want %+v", i, got[i], w)
		}
	}
}

// 端到端：来源要能穿过环形缓冲与 GetHistoryPage 的 JSON 形状，
// 这样所有客户端（GUI 历史、CLI、MCP）读到的都是同一个字段。
func TestHistoryPageCarriesTxOrigin(t *testing.T) {
	pm, proc := newHistoryProc(t)

	proc.recordHistoryAt(5000, "AABB", "tx", "gui-a1b2c3d4")
	proc.recordHistoryAt(5001, "CCDD", "rx", "")

	entries := historyOf(t, pm.GetHistoryPage(proc.id, 0, 0, 0))
	if len(entries) != 2 {
		t.Fatalf("读到 %d 条, want 2", len(entries))
	}
	if entries[0].Origin != "gui-a1b2c3d4" {
		t.Fatalf("tx 来源=%q, want gui-a1b2c3d4", entries[0].Origin)
	}
	if entries[1].Origin != "" {
		t.Fatalf("rx 不应带来源, got %q", entries[1].Origin)
	}
}
