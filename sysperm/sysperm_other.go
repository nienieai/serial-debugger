//go:build !linux

package sysperm

// Probe 在非 Linux 上恒返回 Supported=false：Windows 的 COM 口不需要提权
// （除非被别的进程独占，那是占用不是权限），macOS 的 /dev/cu.* 归当前用户。
// 前端看到 not supported 就不提横幅，也不留一行点不动的按钮。
func Probe() Status {
	return Status{Supported: false, Devices: []string{}, Denied: []string{}}
}

// FixTemp 在非 Linux 上是空动作。调用点仍然存在（CLI 子命令、GUI 绑定），
// 所以这里要给一个明确的 Reason，而不是静默成功。
func FixTemp() FixResult {
	return FixResult{Reason: "unsupported", Detail: "linux only"}
}
