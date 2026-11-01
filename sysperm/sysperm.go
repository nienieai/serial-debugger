// Package sysperm 体检并临时修复 Linux 上 USB 串口设备节点的访问权限。
//
// 分体版（解压即用的四个可执行）只做**临时授权**：一次 pkexec，对当前不可读写的
// /dev/ttyUSB*、/dev/ttyACM* 加 ACL（没有 setfacl 时退回 chmod 0666），不写任何
// 系统文件。拔插或重启后节点重建、权限失效，需要再点一次 —— 这是刻意的：
// 免安装的版本不替用户改系统。
//
// 聚合版（安装式）才允许落 /etc/udev/rules.d/60-serial-tool.rules 那条 uaccess
// 规则（见 build/linux/install.sh 的 --fix-permissions），一次授权后永不打扰。
// 哪个版本用哪条由发布形态决定，不在这一层判断。
//
// 只在 Linux 上真正实现；其它平台返回 Supported=false，界面据此不提横幅。
package sysperm

// Status 是权限体检结果，直接作为 GUI 绑定返回给前端。
type Status struct {
	// Supported 为 false 表示这个平台没有这套机制（Windows / macOS），
	// 前端看到就不提横幅，也不必解释为什么。
	Supported bool `json:"supported"`
	// Devices 是本机现有的 USB 串口节点，例如 /dev/ttyUSB0。
	Devices []string `json:"devices"`
	// Denied 是其中当前用户读不到的节点，也是授权动作唯一的目标。
	Denied []string `json:"denied"`
}

// NeedsFix 报告是否存在需要授权的节点。
func (s Status) NeedsFix() bool { return len(s.Denied) > 0 }

// FixResult 是一次临时授权的结果。Reason 是固定枚举，前端按它选文案 ——
// 不要把 stderr 原文直接甩给用户，pkexec 的报错里没有一句是人话。
//
//	""          成功（或本来就不需要授权）
//	cancel      没拿到授权（用户点了取消，或 polkit 直接拒绝）
//	no_pkexec   系统里没有 pkexec，弹不出授权窗口
//	no_agent    当前进程不在桌面会话里，系统授权窗口调不出来（典型：从 SSH 或
//	            终端启动的 GUI）。不是权限问题，换个启动方式就行
//	no_device   当前没有可授权的设备
//	unsupported 非 Linux
//	failed      其它失败，Detail 带原始错误
type FixResult struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}
