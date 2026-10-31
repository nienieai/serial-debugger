package main

import (
	"strings"
	"testing"
)

func TestMergeDisplaySectionAppends(t *testing.T) {
	body := "# 注释\n[log]\nlanguage = zh\n"
	out := mergeDisplaySection(body, map[string]string{
		"encoding": "gb2312",
		"hex_case": "lower",
	})
	if !strings.Contains(out, "[display]") {
		t.Fatalf("应追加 [display] 段:\n%s", out)
	}
	if !strings.Contains(out, "encoding = gb2312") || !strings.Contains(out, "hex_case = lower") {
		t.Fatalf("键值没写进去:\n%s", out)
	}
	// 原有内容必须原样保留
	if !strings.Contains(out, "[log]\nlanguage = zh") {
		t.Fatalf("原有段被改动了:\n%s", out)
	}
}

func TestMergeDisplaySectionUpdatesInPlace(t *testing.T) {
	body := strings.Join([]string{
		"[log]",
		"language = zh",
		"",
		"[display]",
		"# 手工注释要留着",
		"encoding = utf-8",
		"hex_sep = space",
		"",
		"[history]",
		"auto_save = false",
		"",
	}, "\n")
	out := mergeDisplaySection(body, map[string]string{
		"encoding":   "ascii",
		"hex_sep":    "comma",
		"cr_visible": "false",
	})

	if !strings.Contains(out, "encoding = ascii") || !strings.Contains(out, "hex_sep = comma") {
		t.Fatalf("段内已有的键应就地改值:\n%s", out)
	}
	if !strings.Contains(out, "cr_visible = false") {
		t.Fatalf("缺的键应补进段内:\n%s", out)
	}
	if !strings.Contains(out, "# 手工注释要留着") {
		t.Fatalf("段内注释被删了:\n%s", out)
	}
	if !strings.Contains(out, "[history]\nauto_save = false") {
		t.Fatalf("后面那一段被改动了:\n%s", out)
	}
	// 补的键必须在 [history] 之前
	if strings.Index(out, "cr_visible = false") > strings.Index(out, "[history]") {
		t.Fatalf("新键补到了段外:\n%s", out)
	}
	// 不能出现两份 [display]
	if strings.Count(out, "[display]") != 1 {
		t.Fatalf("[display] 段数量不对:\n%s", out)
	}
}

func TestMergeDisplaySectionKeepsCRLF(t *testing.T) {
	body := "[log]\r\nlanguage = zh\r\n"
	out := mergeDisplaySection(body, map[string]string{"encoding": "ascii"})
	if strings.Contains(out, "\n") && !strings.Contains(out, "\r\n") {
		t.Fatalf("CRLF 文件写回后应保持 CRLF:\n%q", out)
	}
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Fatalf("混进了裸 LF:\n%q", out)
	}
}

func TestMergeDisplaySectionIgnoresUnknownKeys(t *testing.T) {
	out := mergeDisplaySection("[display]\nencoding = utf-8\n", map[string]string{
		"encoding": "ascii",
		"bogus":    "x",
	})
	if strings.Contains(out, "bogus") {
		t.Fatalf("不认识的键不该写进 daemon.ini:\n%s", out)
	}
	if !strings.Contains(out, "encoding = ascii") {
		t.Fatalf("认识的键应改写:\n%s", out)
	}
}
