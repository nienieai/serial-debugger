//go:build windows

package main

import "golang.org/x/sys/windows"

// probePortBusy 用「能不能独占打开」判断端口是否被别的程序占着。
//
// 参数与 go.bug.st/serial 的 CreateFile 对齐（GENERIC_READ|GENERIC_WRITE、
// 共享模式 0、OPEN_EXISTING、FILE_FLAG_OVERLAPPED），成功时立刻 CloseHandle：
// 不碰 DCB、不设波特率，所以不会拉 DTR、不会复位板子。
//
// 判定依据与 serial_windows.go 一致：ERROR_ACCESS_DENIED 在这里就是「已经被别人打开」。
// 其它错误（设备刚拔掉等）一律 unknown，不标红。
func probePortBusy(port string) portBusyState {
	name, err := windows.UTF16PtrFromString(`\\.\` + port)
	if err != nil {
		return portBusyUnknown
	}
	h, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED,
		0)
	if err != nil {
		if err == windows.ERROR_ACCESS_DENIED {
			return portBusyOther
		}
		return portBusyUnknown
	}
	windows.CloseHandle(h)
	return portBusyFree
}
