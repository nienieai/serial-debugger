//go:build !windows

package main

// probePortBusy 在非 Windows 上不判断：Linux/macOS 的 tty 允许多次打开，
// 「打得开」不等于「没人用」，要真判断得按设备节点扫 /proc/<pid>/fd（另一件事）。
// 返回 unknown 让界面保持原样，不产生误报的红标。
func probePortBusy(port string) portBusyState { return portBusyUnknown }
