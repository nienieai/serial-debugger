package main

import (
	"fmt"
	"os"
	"strings"
)

// 守护进程控制台着色。
//
// 约束（与 CLI 那套共用同一份约定）：
//   - **只给控制台**上色：写进 daemon.log 的行永远不带转义序列，否则
//     `serial-cli logs` 与外部工具读到的就是乱码；
//   - 非 TTY 默认无色（管道、重定向、--silent 拉起时 Stdout 是空设备）；
//   - `--color=auto|always|never` 显式覆盖，`NO_COLOR` 环境变量按惯例优先关色
//     （但 `--color=always` 仍然强制开，方便在管道里抓彩色输出做验证）；
//   - Windows 需要先开 ENABLE_VIRTUAL_TERMINAL_PROCESSING，开不了就降级无色。
const (
	colorModeAuto   = "auto"
	colorModeAlways = "always"
	colorModeNever  = "never"

	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[90m"
)

var (
	colorMode    = colorModeAuto
	colorEnabled bool
)

// categoryColors 按类别给颜色：启动=青、连接=绿、串口=蓝、IPC/操作=灰、
// 关闭/警告=黄、错误=红、自动发送/探测=品红。
var categoryColors = map[string]string{
	"启动":   "\x1b[36m",
	"连接":   "\x1b[32m",
	"串口":   "\x1b[34m",
	"IPC":  "\x1b[90m",
	"操作":   "\x1b[90m",
	"关闭":   "\x1b[33m",
	"警告":   "\x1b[33m",
	"错误":   "\x1b[31m",
	"自动发送": "\x1b[35m",
	"探测":   "\x1b[35m",
}

// initColor 定下本次进程是否着色。
//
// 优先级：--color 命令行 > NO_COLOR > settings/daemon.ini 的 [log] color > auto。
// 必须在任何 logOp 之前调用（main 里紧随 daemon 设置加载之后）。
func initColor(args []string, defaultMode string) {
	colorMode = colorModeAuto
	switch defaultMode {
	case colorModeAuto, colorModeAlways, colorModeNever:
		colorMode = defaultMode
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if v, ok := strings.CutPrefix(a, "--color="); ok {
			colorMode = strings.ToLower(strings.TrimSpace(v))
		} else if a == "--color" && i+1 < len(args) {
			colorMode = strings.ToLower(strings.TrimSpace(args[i+1]))
			i++
		}
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok && colorMode != colorModeAlways {
		colorMode = colorModeNever
	}

	switch colorMode {
	case colorModeAlways:
		// 管道里也照打转义，方便测试/截图
		enableVirtualTerminal()
		colorEnabled = true
	case colorModeNever:
		colorEnabled = false
	default:
		colorEnabled = stdoutIsTTY() && enableVirtualTerminal()
	}
}

// formatLogLineColor 是控制台专用的彩色行；结构必须与 formatLogLine 完全相同
// （时间戳 + [类别] + 消息），这样彩色/无色两处肉眼可对照。
func formatLogLineColor(ts, category, msg string) string {
	cat := fmt.Sprintf("%-6s", category)
	if col, ok := categoryColors[category]; ok {
		cat = col + cat + ansiReset
	}
	return ansiDim + ts + ansiReset + " [" + cat + "] " + msg + "\n"
}
