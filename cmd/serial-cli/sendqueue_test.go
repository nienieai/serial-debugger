package main

import (
	"strings"
	"testing"
)

// TestParseQueueEntriesAcceptsValid 确认推荐格式能被接受。
func TestParseQueueEntriesAcceptsValid(t *testing.T) {
	got, err := parseQueueEntries([]byte(`[{"content":"ONE","hex":false,"enabled":true}]`))
	if err != nil {
		t.Fatalf("合法输入被拒: %v", err)
	}
	if len(got) != 1 || got[0].Content != "ONE" {
		t.Fatalf("解析结果不对: %+v", got)
	}
}

// TestParseQueueEntriesStripsBOM 确认带 UTF-8 BOM 的文件能读。
//
// 为什么重要：PowerShell 5.1 的 `Set-Content -Encoding UTF8` 会写 BOM，
// 而 Go 的 json 解析器会报 `invalid character 'ï' looking for beginning of value`
// ——这是最常见的跨工具互操作失败，用户完全看不出是自己文件的编码问题。
func TestParseQueueEntriesStripsBOM(t *testing.T) {
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`[{"content":"X"}]`)...)
	got, err := parseQueueEntries(raw)
	if err != nil {
		t.Fatalf("带 BOM 的输入被拒: %v", err)
	}
	if len(got) != 1 || got[0].Content != "X" {
		t.Fatalf("解析结果不对: %+v", got)
	}
}

// TestParseQueueEntriesRejectsUnknownFields 是本文件的核心回归。
//
// 为什么重要：此前 CLI 把文件解析成 []map[string]any 再走 IPC，未知字段会被
// 静默丢两次（CLI 的 map 忽略一次、守护进程的 struct 再忽略一次），于是
// {"data":"ONE"} 会返回 success:true 而实际装载的是**空内容**——用户直到发现
// 什么都发不出去才知道字段名写错了。DisallowUnknownFields 让它在装载时就失败。
func TestParseQueueEntriesRejectsUnknownFields(t *testing.T) {
	for _, bad := range []string{
		`[{"text":"ONE"}]`,
		`[{"value":"ONE"}]`,
		`[{"content":"ONE","enabledd":true}]`,
		`[{"content":"ONE","hexadecimal":true}]`,
	} {
		if _, err := parseQueueEntries([]byte(bad)); err == nil {
			t.Errorf("未知字段应被拒绝，但通过了: %s", bad)
		} else if !strings.Contains(err.Error(), "content") {
			// 报错里应指出接受的字段名，否则用户还是不知道该写什么
			t.Errorf("错误信息应提到接受的字段名，实际: %v", err)
		}
	}
}

// TestParseQueueEntriesAcceptsDataAlias 确认 "data" 作为 content 的别名被兼容。
//
// 单独留这个别名是为了让最常见的一种手误仍能工作，同时不牺牲严格性
// （其他拼错仍旧报错）。
func TestParseQueueEntriesAcceptsDataAlias(t *testing.T) {
	got, err := parseQueueEntries([]byte(`[{"data":"ONE"}]`))
	if err != nil {
		t.Fatalf("data 别名应被接受: %v", err)
	}
	if len(got) != 1 || got[0].Content != "ONE" {
		t.Fatalf("别名未回填到 content: %+v", got)
	}
}

// TestParseQueueEntriesRejectsEmptyContent 确认空内容条目被拒。
//
// 为什么重要：空条目发不出任何字节，却一路 success；autosend 在 single 模式下
// 会静默空转，现象与「发送路径坏掉」无法区分。
func TestParseQueueEntriesRejectsEmptyContent(t *testing.T) {
	for _, bad := range []string{
		`[{"content":""}]`,
		`[{"hex":false,"enabled":true}]`,
		`[{"content":"OK"},{"content":""}]`,
	} {
		if _, err := parseQueueEntries([]byte(bad)); err == nil {
			t.Errorf("空内容应被拒绝，但通过了: %s", bad)
		}
	}
}

// TestParseQueueEntriesRejectsTrailingGarbage 确认数组之后的垃圾内容被拒。
func TestParseQueueEntriesRejectsTrailingGarbage(t *testing.T) {
	if _, err := parseQueueEntries([]byte(`[{"content":"A"}] {}`)); err == nil {
		t.Error("数组之后的尾随内容应被拒绝")
	}
}

// TestParseQueueEntriesAcceptsEmptyArray 确认空数组仍可装载。
//
// 清空队列是合法操作，不能因为「空」就报错——会被拒的是**空内容的条目**，
// 不是**空数组**。
func TestParseQueueEntriesAcceptsEmptyArray(t *testing.T) {
	got, err := parseQueueEntries([]byte(`[]`))
	if err != nil {
		t.Fatalf("空数组应被接受: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("空数组应解析出 0 条，实际 %d", len(got))
	}
}
