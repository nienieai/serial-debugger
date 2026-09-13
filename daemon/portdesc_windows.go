//go:build windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

// portNameRE 从 FriendlyName 里抠出 (COMxx)。
var portNameRE = regexp.MustCompile(`\((COM\d+)\)`)

// loadPortDescriptions 取「COM 口 → 设备描述」的映射。
//
// 首选**直接读注册表**，不再依赖起 PowerShell 子进程查 WMI：
// 旧实现在机器繁忙时会失败（子进程起不来 / WMI 查询超时 10s），返回空 map，
// 而 buildPortList 对空描述的回退是 `desc = p`（端口名本身），于是表现为
// 「refresh 的 description 偶发退化成端口名」——外部报告连续几轮观察到该现象，
// 且同一会话早期正常、后来退化。读注册表没有子进程也没有 WMI，毫秒级完成。
func loadPortDescriptions() map[string]string {
	if m := loadPortDescriptionsFromRegistry(); len(m) > 0 {
		return m
	}
	// 注册表不可用时才退回 WMI（保留原实现作为兜底）。
	return loadPortDescriptionsFromWMI()
}

// serialCapableClasses 是可能出现 COM 口的枚举类。
//
// 只遍历这些类而不是整棵 Enum 树：整树含显示适配器、磁盘、音频等大量无关项，
// 全走一遍要多花一个数量级的时间，而这是每次刷新端口列表都要做的事。
// 实测本机这些类合计约 40 个实例、耗时在毫秒级。
var serialCapableClasses = []string{
	"USB",      // USB 转串口（CH340/CH343/FTDI/CP210x…）
	"BTHENUM",  // 蓝牙串口（SPP）
	"ROOT",     // 虚拟串口（com0com 等）
	"ACPI",     // 主板自带串口
	"FTDIBUS",  // FTDI 总线
	"SERENUM",  // 串口枚举器
	"LPTENUM",  // 并口转串口
	"MODEM",    // 调制解调器串口
}

// loadPortDescriptionsFromRegistry 取「COM 口 → 设备描述」的映射。
//
// 首选**直接读注册表**，不再依赖起 PowerShell 子进程查 WMI：
// 旧实现在机器繁忙时会失败（子进程起不来 / WMI 查询超时 10s），返回空 map，
// 而 buildPortList 对空描述的回退是 `desc = p`（端口名本身），于是表现为
// 「refresh 的 description 偶发退化成端口名」——外部报告连续几轮观察到该现象，
// 且同一会话早期正常、后来退化。读注册表没有子进程也没有 WMI，毫秒级完成。
func loadPortDescriptionsFromRegistry() map[string]string {
	result := make(map[string]string)
	for _, class := range serialCapableClasses {
		// 枚举树是三层：Enum\<类>\<实例容器>\<实际实例>，值只在最里层，
		// 例如 Enum\USB\VID_1A86&PID_55D3\56CC036433\FriendlyName。
		walkEnum(`SYSTEM\CurrentControlSet\Enum\`+class, 0, result)
	}
	return result
}

// walkEnum 在给定子树里收集「FriendlyName 里带 (COMxx)」或「PortName 是 COMx」的项。
// 深度上限 2 层（类 → 实例容器 → 实例），够到实际实例即可。
func walkEnum(path string, depth int, out map[string]string) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path,
		registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return
	}

	friendly, _, _ := k.GetStringValue("FriendlyName")
	portName, _, _ := k.GetStringValue("PortName")

	if com := portNameRE.FindStringSubmatch(friendly); len(com) == 2 {
		if d := strings.TrimSpace(portNameRE.ReplaceAllString(friendly, "")); d != "" {
			out[com[1]] = d
		}
	} else if strings.HasPrefix(portName, "COM") && friendly != "" {
		if d := strings.TrimSpace(portNameRE.ReplaceAllString(friendly, "")); d != "" {
			out[portName] = d
		}
	}

	if depth >= 2 {
		k.Close()
		return
	}
	subs, err := k.ReadSubKeyNames(-1)
	k.Close()
	if err != nil {
		return
	}
	for _, s := range subs {
		walkEnum(path+`\`+s, depth+1, out)
	}
}

// loadPortDescriptionsFromWMI 是旧的兜底实现：起 PowerShell 查 WMI。
//
// 保留它是因为注册表路径在极端情况下可能读不到（权限、异常的机器配置）。
// 注意它的可靠性受机器负载影响，不要再把它作为首选。
func loadPortDescriptionsFromWMI() map[string]string {
	result := make(map[string]string)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Write PowerShell output to a temp UTF-8 file to avoid code-page garbling
	tmpFile := os.TempDir() + "\\serial_ports_" + fmt.Sprint(time.Now().UnixNano()) + ".txt"
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		"Get-CimInstance -ClassName Win32_PnPEntity | "+
			"Where-Object { $_.Name -match '\\(COM[0-9]+\\)' } | "+
			"ForEach-Object { $_.Name } | "+
			"Out-File -FilePath '"+tmpFile+"' -Encoding UTF8")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var output []byte
	if runErr := cmd.Run(); runErr == nil {
		data, ferr := os.ReadFile(tmpFile)
		os.Remove(tmpFile)
		if ferr == nil {
			output = data
		}
	}

	if len(output) == 0 {
		// Fallback: try wmic
		ctx2, cancel2 := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel2()
		cmd2 := exec.CommandContext(ctx2, "wmic", "path", "Win32_PnPEntity",
			"where", "Name like '%(COM%'", "get", "Name")
		cmd2.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		output, _ = cmd2.Output()
	}

	// PowerShell 5.1's `Out-File -Encoding UTF8` writes a UTF-8 BOM and
	// os.ReadFile hands it back verbatim. Left in place it lands inside a
	// port description; because the results are collected into a map, which
	// port carries the stray U+FEFF varies between runs, and it breaks
	// regex/equality matching against descriptions.
	output = bytes.TrimPrefix(output, []byte{0xEF, 0xBB, 0xBF})

	re := regexp.MustCompile(`^\s*(.*?)\s*\((COM\d+)\)\s*$`)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "Name" {
			continue
		}
		if m := re.FindStringSubmatch(line); len(m) == 3 {
			result[m[2]] = strings.TrimSpace(m[1])
		}
	}
	return result
}
