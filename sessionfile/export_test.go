package sessionfile

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildExportSample 造一个含 rx/tx/系统帧各一条的文件并读回来。
func buildExportSample(t *testing.T) *FileInfo {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.stlog")
	w, err := CreateWriter(path, Settings{Label: "demo", Port: "COM9", Baud: 9600})
	if err != nil {
		t.Fatalf("CreateWriter: %v", err)
	}
	big := make([]byte, 40) // 40 B → 80 个十六进制字符，跨 2 行（每行 32 B）
	for i := range big {
		big[i] = byte(i + 1)
	}
	if err := w.Append(1_700_000_000_000, DirRx, OriginNone, "", big); err != nil {
		t.Fatalf("Append rx: %v", err)
	}
	if err := w.Append(1_700_000_000_001, DirTx, OriginInline, "gui-a1b2c3d4", []byte{0xAA, 0xBB}); err != nil {
		t.Fatalf("Append tx: %v", err)
	}
	if err := w.Append(1_700_000_000_002, DirSystem, OriginNone, "", []byte("sys.port_closed|COM9")); err != nil {
		t.Fatalf("Append sys: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	info, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return info
}

func TestExportTextLayout(t *testing.T) {
	info := buildExportSample(t)
	var buf bytes.Buffer
	n, err := WriteText(&buf, info)
	if err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if int(n) != buf.Len() {
		t.Errorf("返回字节数 = %d，实际写出 %d", n, buf.Len())
	}
	out := buf.String()

	// 行尾必须是 CRLF：这些文本的第一用途是丢进 Windows 记事本。
	if strings.Count(out, "\r\n") == 0 {
		t.Fatal("没有 CRLF 行尾")
	}
	if lf := strings.Count(out, "\n"); lf != strings.Count(out, "\r\n") {
		t.Errorf("存在裸 LF：\\n=%d, CRLF=%d", lf, strings.Count(out, "\r\n"))
	}

	lines := strings.Split(strings.TrimSuffix(out, exportLineEnding), exportLineEnding)
	// 头 6 行（含状态行时为 6）：版本/文件/设置/大小/状态/说明。
	headCount := 0
	for _, ln := range lines {
		if !strings.HasPrefix(ln, "#") {
			break
		}
		headCount++
	}
	if headCount < 5 {
		t.Fatalf("头部行数 = %d，至少应有 5 行", headCount)
	}
	if !strings.Contains(lines[1], "sample.stlog") {
		t.Errorf("第 2 行应含文件名: %q", lines[1])
	}
	if !strings.Contains(lines[2], "port=COM9") || !strings.Contains(lines[2], "baud=9600") ||
		!strings.Contains(lines[2], "label=demo") {
		t.Errorf("设置行不对: %q", lines[2])
	}

	data := lines[headCount:]
	// rx 40 B → 2 行；tx 2 B → 1 行；sys 1 行。
	if len(data) != 4 {
		t.Fatalf("数据行数 = %d，期望 4（rx 2 行 + tx 1 行 + sys 1 行）: %v", len(data), data)
	}
	if !strings.HasPrefix(data[0], "2023-11-15 ") {
		t.Errorf("时间戳列不对（应为本地时间的 YYYY-MM-DD）: %q", data[0])
	}
	if !strings.Contains(data[0], "rx") || !strings.Contains(data[0], "    40") {
		t.Errorf("rx 行缺少方向/长度: %q", data[0])
	}
	if !strings.HasSuffix(data[0], "0102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F20") {
		t.Errorf("rx 第一行应为前 32 B: %q", data[0])
	}
	// 续行：只有缩进 + 剩余 8 B。
	// 续行缩进 = 行首宽度（时间 23 + 2 + 方向 4 + 2 + 来源 12 + 2 + 长度 6 + 2）
	const testOrigin = "gui-a1b2c3d4"
	wantIndent := 23 + 2 + 4 + 2 + len(testOrigin) + 2 + 6 + 2
	if strings.TrimSpace(data[1]) != "2122232425262728" {
		t.Errorf("续行内容 = %q，期望 2122232425262728", strings.TrimSpace(data[1]))
	}
	if !strings.HasPrefix(data[1], strings.Repeat(" ", wantIndent)) {
		t.Errorf("续行缩进应为 %d 空格: %q", wantIndent, data[1])
	}
	// 来源列：tx 那条必须打出 OriginInline 的字符串，rx 留空
	if !strings.Contains(data[2], testOrigin) {
		t.Errorf("tx 行缺来源列: %q", data[2])
	}
	if strings.Contains(data[0], testOrigin) {
		t.Errorf("rx 行不该有来源: %q", data[0])
	}
	if !strings.Contains(data[2], "AABB") {
		t.Errorf("tx 行内容不对: %q", data[2])
	}
	// 系统帧按文本输出，不做十六进制化。
	if !strings.Contains(data[3], "sys.port_closed|COM9") {
		t.Errorf("系统帧应按文本输出: %q", data[3])
	}
	if strings.Contains(data[3], "7379732E") {
		t.Errorf("系统帧被十六进制化了: %q", data[3])
	}
}

// 空文件（只有头）也要能导出，且不产生空数据行。
func TestExportTextEmptyFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.stlog")
	w, err := CreateWriter(path, Settings{})
	if err != nil {
		t.Fatalf("CreateWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	info, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var buf bytes.Buffer
	if _, err := WriteText(&buf, info); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	for _, ln := range strings.Split(buf.String(), exportLineEnding) {
		if ln != "" && !strings.HasPrefix(ln, "#") {
			t.Fatalf("空文件不该有数据行: %q", ln)
		}
	}
}

// 样本目录里的真实文件也要能导出（防止字段取值与读取侧脱节）。
func TestExportTextRealSample(t *testing.T) {
	// 样本在工作区根目录（仓库外），有就跑、没有就跳过。
	path := filepath.Join("..", "..", "stlog-samples", "01-basic.stlog")
	if _, err := os.Stat(path); err != nil {
		t.Skip("没有样本文件，跳过")
	}
	info, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var buf bytes.Buffer
	if _, err := WriteText(&buf, info); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), exportLineEnding), exportLineEnding)
	data := 0
	for _, ln := range lines {
		if ln != "" && !strings.HasPrefix(ln, "#") {
			data++
		}
	}
	// 01-basic 是 20 帧、每帧 120 B → 每帧 4 行（32/32/32/24）。
	if want := len(info.Frames) * 4; data != want {
		t.Errorf("数据行数 = %d，期望 %d", data, want)
	}
}
