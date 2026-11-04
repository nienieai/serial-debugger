//go:build windows

package main

// 收窄无边框窗口右侧的「调整大小」热区。
//
// 系统默认的边框热区是 SM_CXSIZEFRAME + SM_CXPADDEDBORDER（100% DPI 下 8px），
// 整条压在窗口最右边的滚动条上：鼠标一进滚动条右半边就变成窗口缩放手柄，
// 拖滑块反而是把窗口拉宽/拉窄。
//
// Wails 没有暴露热区宽度，只能子类化窗口过程拦 WM_NCHITTEST：原过程判成
// 「右边框 / 右上角 / 右下角」时，再看光标离右边缘还剩几像素 —— 超过阈值的
// 交还给客户区（HTCLIENT），滚动条就点得到了。上/下/左边保持系统默认。
//
// 为什么是 5 物理像素而不是 0：右侧总得留一点能拖窗口的地方，全抹掉的话
// 用户只能从另外三边和左上/左下角调整大小。

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	wmNCHitTest   = 0x0084
	htClient      = 1
	htRight       = 11
	htTopRight    = 14
	htBottomRight = 17
)

// narrowRightEdge 是右侧热区保留的宽度（物理像素，和 WM_NCHITTEST 的坐标同单位）。
const narrowRightEdge = 5

var (
	procCallWindowProcW = modUser32NoActivate.NewProc("CallWindowProcW")
	procGetWindowRect   = modUser32NoActivate.NewProc("GetWindowRect")

	// gwlpWndProc 是 GWLP_WNDPROC (-4)：换窗口过程用的索引。
	gwlpWndProc = ^uintptr(3)

	resizeMu    sync.Mutex
	resizeProcs = map[uintptr]uintptr{} // hwnd → 原来的窗口过程
)

type winRECT struct{ left, top, right, bottom int32 }

// narrowEdgeProc 是我们的窗口过程：先把消息交给原过程，只在 WM_NCHITTEST
// 判成右边框、且光标离右边缘还有一段距离时，把它改回客户区。
func narrowEdgeProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	resizeMu.Lock()
	orig := resizeProcs[hwnd]
	resizeMu.Unlock()
	if orig == 0 {
		return 0
	}
	ret, _, _ := procCallWindowProcW.Call(orig, hwnd, msg, wparam, lparam)
	if msg != wmNCHitTest || (ret != htRight && ret != htTopRight && ret != htBottomRight) {
		return ret
	}
	var rc winRECT
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	// lparam 的低 16 位是光标屏幕 x（有符号）
	x := int32(int16(uint16(lparam & 0xffff)))
	if int(rc.right)-int(x) > narrowRightEdge {
		return htClient
	}
	return ret
}

// installNarrowResizeEdge 给一个窗口装上收窄热区的钩子。重复调用是安全的。
func installNarrowResizeEdge(w *application.WebviewWindow) {
	if w == nil {
		return
	}
	hwnd := uintptr(w.NativeWindow())
	if hwnd == 0 {
		return
	}
	resizeMu.Lock()
	_, done := resizeProcs[hwnd]
	resizeMu.Unlock()
	if done {
		return
	}
	cb := syscall.NewCallback(narrowEdgeProc)
	old, _, _ := procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, cb)
	if old == 0 {
		return
	}
	resizeMu.Lock()
	resizeProcs[hwnd] = old
	resizeMu.Unlock()
}

// forgetNarrowResizeEdge 在窗口销毁前把钩子摘掉（把原过程还回去）。
//
// 不还也能用：HWND 一失效系统就不会再调这个回调了；但那样 map 会随窗口开关
// 一直涨，而且万一 HWND 被复用，新窗口会命中一条过期的原过程指针。
func forgetNarrowResizeEdge(w *application.WebviewWindow) {
	if w == nil {
		return
	}
	hwnd := uintptr(w.NativeWindow())
	if hwnd == 0 {
		return
	}
	resizeMu.Lock()
	orig := resizeProcs[hwnd]
	delete(resizeProcs, hwnd)
	resizeMu.Unlock()
	if orig != 0 {
		procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, orig)
	}
}
