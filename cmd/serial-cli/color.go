package main

import (
	"fmt"
	"os"
	"strings"
)

// CLI 输出着色。
//
// 约定与守护进程一致：
//   - 只有**面向人**的行才上色：错误行、check/start 的状态行、`logs` 读出来的
//     日志行；所有 JSON 结果（status/ports/sessions/history…）原样输出，
//     管道里也绝不会有转义；
//   - 非 TTY 自动无色，`--color=auto|always|never` 可覆盖，`NO_COLOR` 优先关；
//   - Windows 需要先开 VT（失败就降级无色）。
//
// MCP 是 stdio JSON-RPC，完全不经过这里。
const (
	colorModeAuto   = "auto"
	colorModeAlways = "always"
	colorModeNever  = "never"

	ansiReset   = "\x1b[0m"
	ansiDim     = "\x1b[90m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiCyan    = "\x1b[36m"
	ansiMagenta = "\x1b[35m"
)

var (
	colorMode = colorModeAuto
	colorOut  bool // 标准输出（人读的文本）是否着色
	colorErr  bool // 标准错误是否着色
)

// categoryColors 与守护进程控制台保持同一套配色。
var categoryColors = map[string]string{
	"启动":   ansiCyan,
	"连接":   ansiGreen,
	"串口":   "\x1b[34m",
	"IPC":  ansiDim,
	"操作":   ansiDim,
	"关闭":   ansiYellow,
	"警告":   ansiYellow,
	"错误":   ansiRed,
	"自动发送": ansiMagenta,
	"探测":   ansiMagenta,
}

// stripColorFlag 摘掉 --color / --color=X 并定下本次是否着色。
// 返回去掉该 flag 后的参数表，交给后面的命令解析。
//
// defaultMode 来自 settings/cli.ini 的 [general] color；命令行与 NO_COLOR
// 都能覆盖它。
func stripColorFlag(args []string, defaultMode string) []string {
	colorMode = colorModeAuto
	switch defaultMode {
	case colorModeAuto, colorModeAlways, colorModeNever:
		colorMode = defaultMode
	}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case strings.HasPrefix(args[i], "--color="):
			colorMode = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(args[i], "--color=")))
		case args[i] == "--color" && i+1 < len(args):
			i++
			colorMode = strings.ToLower(strings.TrimSpace(args[i]))
		default:
			out = append(out, args[i])
		}
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok && colorMode != colorModeAlways {
		colorMode = colorModeNever
	}

	switch colorMode {
	case colorModeAlways:
		colorOut, colorErr = true, true
	case colorModeNever:
		colorOut, colorErr = false, false
	default:
		vt := enableVirtualTerminal()
		colorOut = vt && isTerminal(os.Stdout)
		colorErr = vt && isTerminal(os.Stderr)
	}
	return out
}

// isTerminal 判断给定流是不是终端；管道/重定向时为 false。
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func paint(s, color string, enabled bool) string {
	if !enabled || color == "" {
		return s
	}
	return color + s + ansiReset
}

// paintOut / paintErr 分别按对应的流是否支持着色来处理。
func paintOut(s, color string) string { return paint(s, color, colorOut) }
func paintErr(s, color string) string { return paint(s, color, colorErr) }

// printErr 打一行红色错误到 stderr；非 TTY 时与普通 Fprintln 完全一样。
func printErr(format string, args ...any) {
	msg := strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	fmt.Fprintln(os.Stderr, paintErr(msg, ansiRed))
}

// printWarn 打一行黄色提示（用法错误、注意事项等）。
func printWarn(format string, args ...any) {
	msg := strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	fmt.Fprintln(os.Stderr, paintErr(msg, ansiYellow))
}

// colorizeLogLine 给 `serial-cli logs` 的一行上色：时间戳弱化、类别按类别着色。
// 行格式固定为 "HH:MM:SS.mmm [类别    ] 消息"（见 daemon/logfile.go）。
func colorizeLogLine(line string) string {
	if !colorOut {
		return line
	}
	lb := strings.IndexByte(line, '[')
	if lb < 0 {
		return line
	}
	rb := strings.IndexByte(line[lb:], ']')
	if rb < 0 {
		return line
	}
	rb += lb
	cat := strings.TrimSpace(line[lb+1 : rb])
	col, ok := categoryColors[cat]
	if !ok {
		return line
	}
	ts := strings.TrimRight(line[:lb], " ")
	rest := line[rb+1:]
	return ansiDim + ts + ansiReset + " [" + col + line[lb+1:rb] + ansiReset + "]" + rest
}
