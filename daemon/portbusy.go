package main

// portBusyState 是「这个端口现在能不能被独占打开」的三态。
//
// 为什么要 unknown 这一档：非 Windows 的 tty 允许多次打开，「能打开」不代表
// 没被占用，试开判断没有意义；另外探测本身出错（设备刚被拔、驱动异常）也不能
// 当成「被占用」而标红。只有明确的 ACCESS_DENIED 才判 other。
type portBusyState int

const (
	portBusyUnknown portBusyState = iota
	portBusyFree
	portBusyOther
)

// probePortBusy 由平台文件实现（portbusy_windows.go / portbusy_other.go）：
// Windows 用 CreateFile 独占试开，其余平台返回 unknown。
//
// 注意：实现必须**不碰 DCB、不设波特率**——成功打开后立刻关闭，避免拉 DTR 把
// 板上 MCU 复位。这也是不能直接拿 serial.Open 来试的原因。
