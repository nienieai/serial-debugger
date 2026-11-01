package main

import (
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// guiInstanceBaseID 是单实例标识的前缀（完整标识 = 它 + exe 路径哈希，见 guiInstanceID）。
//
// 为什么不带版本号：同一份 exe 重复启动要落到同一个实例上，版本号一变就成了
// 「新旧两个 GUI 各开一份」，反而不是用户想要的。
const guiInstanceBaseID = "com.nienieai.serial-debugger.gui"

// guiInstanceID 返回「这份 exe 的单实例标识」。
//
// 按**可执行文件路径**分组，而不是整个程序只有一个标识：
//   - 开发版（build/bin/serial-gui.exe）与部署版（0.9.0-dev/serial-gui-dev.exe）
//     是两个独立实例，可以同时开着 —— 改 GUI 时不用先关掉部署版那份；
//   - 老版本（0.8.4 目录）与新版本各开各的，不会出现「启动新版却被切到旧版窗口」。
//
// 同一个路径重复启动仍然落到同一个实例（这正是本次需求），路径变了（移动/复制）
// 才另起一个 —— 复制两份就想要两个独立 GUI，也说得通。
func guiInstanceID() string {
	exe, err := os.Executable()
	if err != nil {
		// 拿不到自己的路径就退化成「全机一个实例」：宁可多开被拦，也别因此起不来。
		log.Printf("[second-instance] 取 exe 路径失败（%v），单实例标识退化为全局唯一", err)
		return guiInstanceBaseID
	}
	return instanceIDFor(exe)
}

// instanceIDFor 是 guiInstanceID 的纯函数部分（路径 → 标识），方便单测。
//
// 路径要归一化后再算：Windows 路径不区分大小写，`C:\A\gui.exe` 与 `c:\a\gui.exe`
// 必须是同一个标识，否则从快捷方式/命令行启动会得到两个实例。
func instanceIDFor(exePath string) string {
	if strings.TrimSpace(exePath) == "" {
		return guiInstanceBaseID
	}
	norm := strings.ToLower(filepath.Clean(exePath))
	h := fnv.New32a()
	// Write 对 fnv 永远不会返回错误（hash.Hash 的约定），忽略即可。
	_, _ = h.Write([]byte(norm))
	return fmt.Sprintf("%s-%08x", guiInstanceBaseID, h.Sum32())
}
