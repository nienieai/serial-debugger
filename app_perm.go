package main

import "github.com/nienieai/serial-debugger/sysperm"

// GetSerialPermission 体检本机 USB 串口节点的访问权限（Linux 才有意义）。
//
// 非 Linux 返回 supported=false：界面据此不再提横幅，也不必解释「为什么没有」。
// 检测只读文件系统事实（access(2)），不落任何状态文件 —— 授权成功后自然就不报了。
func (a *App) GetSerialPermission() sysperm.Status {
	return sysperm.Probe()
}

// FixSerialPermission 对当前读不到的串口节点做一次临时授权（pkexec 弹一次密码框）。
//
// 刻意只做临时授权：分体版是解压即用的，替用户往 /etc/udev/rules.d 里写规则属于
// 改系统，得用户自己决定（build/linux/install.sh --fix-permissions）。所以拔插或
// 重启后节点重建、权限失效，横幅会再出现一次。
func (a *App) FixSerialPermission() sysperm.FixResult {
	return sysperm.FixTemp()
}
