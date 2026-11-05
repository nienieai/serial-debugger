package main

// 提示 / 确认 / 选择改成**独立窗口**。
//
// 背景：v3 迁移的初衷就是多窗口，但提示类弹层一直还是页内 overlay
// （frontend/app.js 的 #alertOverlay）。页内弹层有两个实际问题：
//   1. 它挡在 WebView 里，弹着的期间整个界面不能交互（尤其退出确认这种时机）；
//   2. 它不在窗口管理器里，多显示器/切窗口时容易"藏"在主窗口后面找不着。
//
// v3 的 WebviewWindow.AttachModal 在三个平台都有实现：Windows 走
// GWLP_HWNDPARENT（owned window：始终浮在主窗口之上、跟随移动/最小化）。
// 注意它**不阻塞**主窗口输入 —— 这反而是好事，弹窗卡死不会拖着整个界面。
//
// 结果回传走已有的 stream 帧通道（sendFrame），不新增 IPC 协议。

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// DialogButton 是对话框上的一个按钮。
type DialogButton struct {
	Key   string `json:"key"`   // 回传给前端的结果标识（ok / cancel / c0 / c1 …）
	Label string `json:"label"` // 已由主窗口翻译好的文案
	Kind  string `json:"kind"`  // primary / default
}

// DialogSpec 描述一次提示 / 确认 / 选择。
type DialogSpec struct {
	Kind         string         `json:"kind"` // alert / confirm / choice
	Title        string         `json:"title"`
	Message      string         `json:"message"`
	Mono         bool           `json:"mono"` // 等宽 + 保留换行（多行摘要用）
	Buttons      []DialogButton `json:"buttons"`
	DontAsk      bool           `json:"dontAsk"` // 第一勾选框（「记住选择」/「不再提醒」都走它）
	DontAskLabel string         `json:"dontAskLabel"`
	// DontAsk2 / DontAsk2Label 是第二个勾选框。
	//
	// 关窗口时同时要记住两件互不相干的事：窗口里的会话怎么办（TabCloseAction），
	// 以及下次还要不要提醒停守护进程（ShutdownDaemonWarn）。它们写的是两个不同的
	// 设置，一个勾选框表达不了，所以对话框要能摆两个。
	DontAsk2      bool              `json:"dontAsk2"`
	DontAsk2Label string            `json:"dontAsk2Label"`
	Vars          map[string]string `json:"vars"` // 主窗口的 CSS 变量快照，用来对齐主题
}

type dialogEntry struct {
	spec     DialogSpec
	win      *application.WebviewWindow
	resolved bool
	// owner 是发起这次弹窗的窗口名。结果帧必须发回**它**：多窗口之后窗口各有一份
	// app.js，发给别的窗口等于把 Promise 挂死（见 finishDialog）。
	owner string
}

const (
	dialogWidth = 520
	dialogMinH  = 210
	dialogMaxH  = 620
)

// OpenDialog 建一个对话框窗口，挂在**发起调用的那个窗口**上。
//
// 返回的 id 由前端用来等待结果：前端收到 dialog-result 帧后按 id 找到自己的
// Promise 并 resolve。
func (a *App) OpenDialog(ctx context.Context, spec DialogSpec) (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("窗口系统尚未就绪")
	}
	if len(spec.Buttons) == 0 {
		return "", fmt.Errorf("对话框至少要有一个按钮")
	}

	// 多窗口之后每个窗口都能弹框，对话框要跟着「谁弹的」走：
	// owned window 挂错父窗口的话，弹窗可能跑到另一个窗口所在的屏幕上。
	ownerName := winNameFromCtx(ctx)
	owner := a.windowByName(ownerName)
	if owner == nil {
		owner = a.window
		ownerName = "main"
	}

	id := fmt.Sprintf("dlg%d", atomic.AddUint64(&a.dialogSeq, 1))
	h := dialogHeight(spec.Message)

	w := a.wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:          "dialog-" + id,
		Title:         spec.Title,
		Width:         dialogWidth,
		Height:        h,
		DisableResize: true,
		// 和主窗口一致：无边框、标题栏由页面自己画（dialog.html 的 .o-dialog-bar），
		// 边界靠那 1px 的边框。系统标题栏会跟主窗口的观感断开一截。
		Frameless: true,
		Windows:   framelessChrome,
		URL:       "/dialog.html?id=" + id,
	})
	if w == nil {
		return "", fmt.Errorf("创建对话框窗口失败")
	}

	entry := &dialogEntry{spec: spec, win: w, owner: ownerName}
	a.dialogMu.Lock()
	if a.dialogs == nil {
		a.dialogs = make(map[string]*dialogEntry)
	}
	a.dialogs[id] = entry
	a.dialogMu.Unlock()

	// 用户点右上角 X 等同于「取消」：关闭事件里自己结算一次，
	// 避免前端那个 Promise 永远挂着。
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.finishDialog(id, "cancel", false, false)
		a.unregisterWindow("dialog-" + id)
	})

	// 固定尺寸的对话框不该有最大化/最小化按钮
	w.SetMaximiseButtonState(application.ButtonHidden)
	w.SetMinimiseButtonState(application.ButtonHidden)

	// 居中到发起窗口上方，而不是居中到屏幕 —— 那个窗口可能已经挪到副屏了。
	if owner != nil {
		w.SetPosition(a.dialogCenter(owner, dialogWidth, h))
	}

	// owned window：浮在父窗口之上、跟着父窗口移动。
	if owner != nil {
		owner.AttachModal(w)
	}

	a.registerWindow("dialog-"+id, w)
	w.Show()
	w.Focus()
	log.Printf("[dialog] 打开 %s（%s，%d 个按钮，父窗口 %q）", id, spec.Kind, len(spec.Buttons), ownerName)
	return id, nil
}

// GetDialog 供对话框页面取自己的内容。
func (a *App) GetDialog(id string) (DialogSpec, error) {
	a.dialogMu.Lock()
	defer a.dialogMu.Unlock()
	e := a.dialogs[id]
	if e == nil {
		return DialogSpec{}, fmt.Errorf("对话框不存在（可能已关闭）")
	}
	return e.spec, nil
}

// ResolveDialog 由对话框页面调用：把结果送回主窗口并关掉自己。
func (a *App) ResolveDialog(id, action string, dontAsk, dontAsk2 bool) error {
	e := a.finishDialog(id, action, dontAsk, dontAsk2)
	if e == nil || e.win == nil {
		return nil // 已经结算过（例如同时点了按钮又点了 X）
	}
	win := e.win
	// 延后一点再关：这是从对话框自己的 IPC 调用里触发的，
	// 立刻销毁有可能让本次调用的回包无处可去。
	go func() {
		time.Sleep(30 * time.Millisecond)
		win.Close()
	}()
	return nil
}

// finishDialog 幂等地结算一次：只有第一次有效。
//
// 返回被结算的条目（已被移出注册表），调用方如果还要关窗口就得靠它——
// 早先版本这里只返回 bool，调用方回头再查 map 已经是 nil，窗口就关不掉了。
func (a *App) finishDialog(id, action string, dontAsk, dontAsk2 bool) *dialogEntry {
	a.dialogMu.Lock()
	e := a.dialogs[id]
	if e == nil || e.resolved {
		a.dialogMu.Unlock()
		return nil
	}
	e.resolved = true
	delete(a.dialogs, id)
	a.dialogMu.Unlock()

	// 结果发回**发起弹窗的那个窗口**：多窗口下每个窗口各有一份 app.js，
	// 发给 main 会让别的窗口的 Promise 永远挂着（之前只有 main 时没这个问题）。
	target := e.owner
	if target == "" {
		target = "main"
	}
	a.sendFrame(target, map[string]any{
		"e": "dialog-result",
		"d": map[string]any{"id": id, "action": action, "dontAsk": dontAsk, "dontAsk2": dontAsk2},
	})
	log.Printf("[dialog] 结算 %s -> %s（勾选1=%v 勾选2=%v，发往 %q）", id, action, dontAsk, dontAsk2, target)
	return e
}

// dialogCenter 让对话框居中于父窗口。
func (a *App) dialogCenter(owner *application.WebviewWindow, w, h int) (int, int) {
	mx, my := owner.Position()
	mw, mh := owner.Size()
	x := mx + (mw-w)/2
	y := my + (mh-h)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// dialogHeight 按正文长度估个高度。前端渲染完还会按实际内容再调一次
// ResizeDialog，所以这里只要别差得太离谱。
func dialogHeight(msg string) int {
	rows := len([]rune(msg)) / 40
	// 184 = 自绘标题栏 34 + 正文一行 20 + 按钮行 30 + 内边距 32 + 行间距 24 + 边框 2，
	// 基线取整到「一行消息正好不滚动」。前端渲染完还会按实际内容再调一次。
	h := 184 + rows*20
	if h < dialogMinH {
		h = dialogMinH
	}
	if h > dialogMaxH {
		h = dialogMaxH
	}
	return h
}

// ResizeDialog 由对话框页面在渲染完成后调用，把窗口高度贴合内容。
func (a *App) ResizeDialog(id string, height int) {
	a.dialogMu.Lock()
	e := a.dialogs[id]
	a.dialogMu.Unlock()
	if e == nil || e.win == nil {
		return
	}
	if height < dialogMinH {
		height = dialogMinH
	}
	if height > dialogMaxH {
		height = dialogMaxH
	}
	e.win.SetSize(dialogWidth, height)
}

// CloseAllDialogs 在主窗口退出时兜底清场（owned window 通常会被系统一起关，
// 这里只是防止某些平台上残留窗口卡住退出）。
func (a *App) CloseAllDialogs() {
	a.dialogMu.Lock()
	wins := make([]*application.WebviewWindow, 0, len(a.dialogs))
	for _, e := range a.dialogs {
		if e.win != nil {
			wins = append(wins, e.win)
		}
	}
	a.dialogs = nil
	a.dialogMu.Unlock()
	for _, w := range wins {
		w.Close()
	}
}
