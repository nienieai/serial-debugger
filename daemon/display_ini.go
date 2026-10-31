package main

import (
	"os"
	"strings"

	"github.com/nienieai/serial-debugger/ini"
)

// saveDisplaySection 把显示设置的默认值写进 daemon.ini 的 [display] 段。
//
// ini 包只支持读，所以这里按行处理，而不是整文件重写：
//   - 已有 [display] 段：段内认识的键就地改值，缺的键补到段尾
//   - 没有 [display] 段：在文件末尾追加一段（带注释）
//
// 段外的内容（[log]、[history]、用户自己的注释）一个字节都不动 —— 这个文件
// 是人手写的，整文件重排会把它变成机器产物。
func saveDisplaySection(exeDir string, kv map[string]string) error {
	path := ini.Path(exeDir, "daemon.ini")
	body := ""
	if b, err := os.ReadFile(path); err == nil {
		body = string(b)
	}
	out := mergeDisplaySection(body, kv)
	return os.WriteFile(path, []byte(out), 0644)
}

// mergeDisplaySection 是 saveDisplaySection 的纯文本部分，单独拆出来好测。
func mergeDisplaySection(body string, kv map[string]string) string {
	crlf := strings.Contains(body, "\r\n")
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")

	// 找 [display] 段的行范围 [start, end)
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if strings.EqualFold(t, "[display]") {
				start = i
			}
			continue
		}
		if strings.HasPrefix(t, "[") {
			end = i
			break
		}
	}

	if start < 0 {
		// 没有这一段：追加到末尾（保留原来的结尾空行习惯）
		var sb strings.Builder
		sb.WriteString(strings.TrimRight(strings.Join(lines, "\n"), "\n"))
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("# 新会话的显示设置默认值（编码 / Hex / 控制字符 / 转义）。\n")
		sb.WriteString("# 单个标签页可以各自改，工程会话会把它写进工程目录的 settings.json。\n")
		sb.WriteString("[display]\n")
		for _, k := range []string{"encoding", "hex_case", "hex_prefix", "hex_sep",
			"cr_visible", "hex_escape_mode", "hex_escape_format", "copy_hex_escapes"} {
			if v, ok := kv[k]; ok {
				sb.WriteString(k + " = " + v + "\n")
			}
		}
		return finishLines(sb.String(), crlf)
	}

	// 段内改值
	seen := map[string]bool{}
	for i := start + 1; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
			continue
		}
		eq := strings.IndexByte(t, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(t[:eq])
		v, ok := kv[key]
		if !ok {
			continue
		}
		// 保留原有缩进与键的写法，只换等号右边
		indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
		lines[i] = indent + key + " = " + v
		seen[key] = true
	}
	// 缺的键补到段尾
	var add []string
	for _, k := range []string{"encoding", "hex_case", "hex_prefix", "hex_sep",
		"cr_visible", "hex_escape_mode", "hex_escape_format", "copy_hex_escapes"} {
		if v, ok := kv[k]; ok && !seen[k] {
			add = append(add, k+" = "+v)
		}
	}
	if len(add) > 0 {
		merged := make([]string, 0, len(lines)+len(add))
		merged = append(merged, lines[:end]...)
		merged = append(merged, add...)
		merged = append(merged, lines[end:]...)
		lines = merged
	}
	return finishLines(strings.Join(lines, "\n"), crlf)
}

func finishLines(s string, crlf bool) string {
	s = strings.TrimRight(s, "\n") + "\n"
	if crlf {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}
