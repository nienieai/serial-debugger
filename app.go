package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nienieai/serial-debugger/client"
	"github.com/nienieai/serial-debugger/config"
	"github.com/nienieai/serial-debugger/contract"
	"github.com/nienieai/serial-debugger/decode"
	"github.com/nienieai/serial-debugger/pipe"
	"github.com/nienieai/serial-debugger/protocol"
	"github.com/nienieai/serial-debugger/sessionfile"
	"github.com/nienieai/serial-debugger/stlogview"
	"github.com/nienieai/serial-debugger/version"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type App struct {
	ctx context.Context

	// v3 的入口对象：wailsApp 用来发事件 / 弹对话框 / 退出，window 是主窗口。
	// 多窗口阶段（方案里的阶段 2/3）这两个会扩成窗口注册表，现在先固定单窗口。
	wailsApp *application.App
	window   *application.WebviewWindow

	// 窗口注册表：窗口名 → 窗口。
	winMu   sync.Mutex
	windows map[string]*application.WebviewWindow
	winSeq  int

	// 无边框窗口的最大化状态：窗口名 → 最大化之前的位置和大小。
	//
	// 为什么自己记：Wails 对 frameless 窗口的 ToggleMaximise 只把 WS_MAXIMIZE
	// 状态位置上了（IsMaximised 会跟着变 true），窗口尺寸却一动不动 —— 那个
	// ShowWindow(SW_MAXIMIZE) 在无边框窗口上不管用。这里干脆按屏幕工作区自己摆。
	restMu  sync.Mutex
	winRest map[string]winRect

	// 单实例：又启动了一份 GUI 时，v3 把通知交给我们（见 onSecondInstanceLaunch）。
	//
	// secondPending 是「要开的窗口」的队列（每项是那次启动的命令行参数，目前只用于
	// 日志），secondWorker 表示消费 goroutine 已在跑，secondReady 表示窗口系统已就绪
	// （attach 之后才为 true）。
	secondMu      sync.Mutex
	secondPending [][]string
	secondWorker  bool
	secondReady   bool

	// 会话归属：会话 ID → 窗口名。
	//
	// 每个窗口都是平等的「主窗口」，没有哪个窗口能看见别人的标签页。谁建的会话就
	// 归谁，窗口关掉时按这张表把**自己的**会话销毁（见 destroyWindowSessions）。
	//
	// 为什么放在 GUI 进程里而不是守护进程：会话本身在守护进程里是全局的，但
	// 「归哪个窗口」纯粹是界面概念；GUI 所有窗口都在同一个进程里，一张表就够了，
	// 不用动 IPC 协议（守护进程也不知道窗口这回事）。
	//
	// 留空表示「没人认领」：CLI / MCP 建的会话就落在这里，由第一个来同步的窗口
	// 认领，否则那些会话在 GUI 里永远不会显示。
	sessMu    sync.Mutex
	sessOwner map[string]string

	// 关窗确认：窗口名 → 上次发确认帧的时间（防连点 X 叠出多个确认窗口）。
	// forcedWindows 里的窗口表示用户已经确认过，下次 WindowClosing 直接放行。
	closeWinMu      sync.Mutex
	pendingCloseWin map[string]time.Time
	forcedWindows   map[string]bool

	// 对话框窗口（提示/确认/选择）注册表：id → 内容 + 窗口。
	// 见 dialog.go —— 提示类弹层已改成独立窗口（AttachModal 做 owned window）。
	dialogMu  sync.Mutex
	dialogSeq uint64
	dialogs   map[string]*dialogEntry

	// 数据帧的 per-window 通道：窗口名 → Stream 连接。
	//
	// 为什么不用事件：v3 的 window.EmitEvent 只是设 Sender 后走全局广播
	// （见 emitSession 的注释），做不了定向。Stream 是每窗口一条连接，
	// 天然定向，还自带背压和窗口关闭时的清理。
	streamMu    sync.Mutex
	streamConns map[string]*application.StreamConn
	frameDrops  int

	eventConn *client.DaemonClient
	stopping  bool
	checking  bool
	daemonMu  sync.Mutex

	// 关窗的「已确认」标记是 per-window 的（forcedWindows），这里不再需要
	// 全局的 forceQuit：QuitApp 直接走 Application.Quit，不经过窗口关闭判定。

	// Tab decode workers — one goroutine per tab.
	tabDecoders   map[string]*tabDecodeUnit // processId → worker
	tabDecodersMu sync.Mutex

	// 虚窗文档服务：每个 .stlog 标签页一个文档，按 id 分（见 logview.go）。
	logview *LogView

	// 启动参数里带的 .stlog / 工程目录（双击文件、命令行带上路径）。
	// 前端加载完自己来取一次，取走就清空 —— 用「拉」而不是「推」：推的时机
	// 永远赶不上页面就绪，早了就丢事件。
	openMu   sync.Mutex
	openPath string
}

// tabDecodeUnit runs a dedicated decode goroutine for one tab.
type tabDecodeUnit struct {
	a         *App
	processID string
	encoding  string
	encMu     sync.RWMutex
	jobs      chan rxTxEnvelope
	quit      chan struct{}
}

type rxTxEnvelope struct {
	event  string
	params map[string]any
}

// maxSegmentHexLen 是「还值得逐字符解 segments」的上限（十六进制字符数）。
//
// 一条 4 KB 的包解码出来是 ~3300 个 segment 对象、序列化约 70 KB；历史一次取
// 1 万条时就是 ~700 MB 的 JSON + JS 对象（实测），这是 GUI 内存暴涨与高 CPU 的
// 主因。大包本来也不需要「控制字符可视化」，超过这个长度就跳过，前端退回纯文本
// 渲染（live 数据走同一条规则）。
const maxSegmentHexLen = 256

// decodeSegments 只对足够小的包做逐字符解码；大包返回 nil。
func decodeSegments(hexStr, enc string) []decode.Segment {
	if len(hexStr) > maxSegmentHexLen {
		return nil
	}
	return decode.Decode(hexStr, enc)
}

func (td *tabDecodeUnit) run() {
	for {
		select {
		case env := <-td.jobs:
			hex, _ := env.params["hex"].(string)
			td.encMu.RLock()
			enc := td.encoding
			td.encMu.RUnlock()
			env.params["segments"] = decodeSegments(hex, enc)
			// 数据帧改成按会话定向（见 emitSession）。串口全速跑时每秒几千帧，
			// 走广播会把每个窗口都淹掉。
			td.a.emitSession(td.processID, env.event, env.params)
		case <-td.quit:
			return
		}
	}
}

func (td *tabDecodeUnit) setEncoding(enc string) {
	td.encMu.Lock()
	td.encoding = enc
	td.encMu.Unlock()
}

func NewApp() *App {
	a := &App{}
	a.logview = newLogView(a)
	return a
}

// ---- 启动参数里的文件 ----

// setStartupPath 记下「启动时要打开的那个路径」（由 main.go 解析命令行后调用）。
func (a *App) setStartupPath(p string) {
	a.openMu.Lock()
	a.openPath = p
	a.openMu.Unlock()
}

// TakeOpenPath 取走启动路径（一次）。前端在 wails-ready 之后调一次：
// 有值就开成只读页，没有就什么都不做。
func (a *App) TakeOpenPath() string {
	a.openMu.Lock()
	p := a.openPath
	a.openPath = ""
	a.openMu.Unlock()
	return p
}

// attach 把 v3 的 application / window 交给 service。
// 必须在 application.New 和 Window.NewWithOptions 之后、app.Run() 之前调用。
func (a *App) attach(wailsApp *application.App, win *application.WebviewWindow) {
	a.wailsApp = wailsApp
	a.window = win
	a.registerWindow(win.Name(), win)
	// 主窗口关闭时也要从窗口表里摘掉：多窗口之后 main 可能先于别的窗口关（比如
	// 把它唯一那个标签拖到别的窗口去）。不摘的话窗口表里会留着一个已销毁的窗口，
	// 拖标签的落点判定（tabBandWindowAt）还会把它的矩形当落点，会话就被转给
	// 一个不存在的窗口了。
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.unregisterWindow(win.Name())
	})

	// 窗口系统就绪。单实例通知可能**比这里更早**到（v3 在 application.New 内部
	// 就建好了收消息的窗口，见 single_instance.go:112），那时只能先攒着，
	// 这里补踢一次把它放出来。
	a.secondMu.Lock()
	a.secondReady = true
	a.secondMu.Unlock()
	a.kickSecondInstance()
}

// ---- 多窗口原型 ----
//
// 只验证三件事，验证完就扩成阶段 2 的正式实现：
//   1. 窗口注册表：按名字找回窗口
//   2. v3 的 service 方法能不能知道「是哪个窗口在调我」（ctx 里带 WindowKey）
//   3. window.EmitEvent 定向发事件（相比 app.Event.Emit 的广播）

func (a *App) registerWindow(name string, w *application.WebviewWindow) {
	if name == "" || w == nil {
		return
	}
	a.winMu.Lock()
	defer a.winMu.Unlock()
	if a.windows == nil {
		a.windows = make(map[string]*application.WebviewWindow)
	}
	a.windows[name] = w
}

func (a *App) unregisterWindow(name string) {
	a.winMu.Lock()
	delete(a.windows, name)
	// 主窗口没了就把指针清掉：OpenDialog / CloseMainWindow 会回退到它，
	// 拿一个已经销毁的窗口去 Position()/Close() 会出问题。
	if name == "main" {
		a.window = nil
	}
	a.winMu.Unlock()

	a.restMu.Lock()
	delete(a.winRest, name)
	a.restMu.Unlock()
}

func (a *App) windowByName(name string) *application.WebviewWindow {
	a.winMu.Lock()
	defer a.winMu.Unlock()
	return a.windows[name]
}

// ---- 会话归属 ----
//
// 目标模型：没有「主窗口」这个概念，每个窗口都是平等的；窗口里的标签页就是它
// 名下的会话，**关窗口 = 销毁这个窗口所有标签页背后的会话**。
//
// 归属表就三个操作：查、占、放。全部在一把锁里做，因为多个窗口会并发同步。

// sessWindow 返回会话当前的归属窗口（"" = 没人认领）。
func (a *App) sessWindow(sid string) string {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	return a.sessOwner[sid]
}

// sessClaim 占用一个会话。只有「没人认领」或「本来就是我的」才成功 ——
// 否则两个窗口会把同一个会话各建一个标签页，然后各自去关它。
func (a *App) sessClaim(sid, win string) bool {
	if sid == "" || win == "" {
		return false
	}
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	if a.sessOwner == nil {
		a.sessOwner = make(map[string]string)
	}
	cur := a.sessOwner[sid]
	if cur != "" && cur != win {
		return false
	}
	a.sessOwner[sid] = win
	return true
}

// sessSet 强行改归属（拖出标签转交给新窗口时用）。
func (a *App) sessSet(sid, win string) {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	if a.sessOwner == nil {
		a.sessOwner = make(map[string]string)
	}
	if win == "" {
		delete(a.sessOwner, sid)
		return
	}
	a.sessOwner[sid] = win
}

// sessOfWindow 列出某个窗口名下的全部会话。
func (a *App) sessOfWindow(win string) []string {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	out := make([]string, 0, 4)
	for sid, owner := range a.sessOwner {
		if owner == win {
			out = append(out, sid)
		}
	}
	return out
}

// sessDropWindow 摘掉某个窗口的全部归属并返回这些会话 ID（关窗时用）。
func (a *App) sessDropWindow(win string) []string {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	out := make([]string, 0, 4)
	for sid, owner := range a.sessOwner {
		if owner == win {
			out = append(out, sid)
			delete(a.sessOwner, sid)
		}
	}
	return out
}

// sessPrune 清掉已经不在守护进程里的会话的归属记录（守护进程重启后会话 ID
// 会重新从 0 开始，留着旧记录会把新会话误判成别人的）。
func (a *App) sessPrune(alive map[string]bool) {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	for sid := range a.sessOwner {
		if !alive[sid] {
			delete(a.sessOwner, sid)
		}
	}
}

// sessClearAll 清空全部窗口的会话归属。
//
// 只在**确认守护进程已经没了**时调用（用户点停止、收到 daemon-shutdown 广播、
// 心跳判定离线且重连失败）：会话是守护进程里的对象，它一走会话就全没了，归属表
// 再留着只会误导 —— 实测：手动停掉守护进程（界面已显示「守护进程未启用」）后
// 关窗口，仍会弹「会停止这个窗口里的会话（共 3 个）」，可实际一个都不剩。
func (a *App) sessClearAll() {
	a.sessMu.Lock()
	a.sessOwner = make(map[string]string)
	a.sessMu.Unlock()
}

// winNameFromCtx 取出「正在调用这个方法的窗口名」。
//
// 靠的是 v3 的约定：Service 方法第一个参数是 context.Context 时，框架会把
// 调用方的窗口塞进 ctx（application.WindowKey）。这是多窗口按来源分流的基础 ——
// 不用前端手动传窗口 ID。
func winNameFromCtx(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if w, ok := ctx.Value(application.WindowKey).(*application.WebviewWindow); ok && w != nil {
		return w.Name()
	}
	return ""
}

// WhoAmI 返回「调用这个方法的窗口名」。
func (a *App) WhoAmI(ctx context.Context) string {
	if name := winNameFromCtx(ctx); name != "" {
		return name
	}
	return "(unknown)"
}

// WindowControl 是无边框窗口右上角三个按钮的入口（minimise / toggle-maximise / close）。
//
// 为什么不用运行时的 window.wails.Window：那条路对无边框窗口实测不生效 ——
// Size / IsMaximised 这类读方法都正常，说明通道是通的，但 ToggleMaximise 调完
// 窗口纹丝不动。走服务方法还有个好处：Close 会经过 onWindowClosingFor 的
// 「这个窗口里的会话要不要一起销毁」确认，和点系统关闭按钮完全同一条路。
//
// 用 ctx 里的 WindowKey 认领调用方：多窗口时各点各的，不依赖主窗口单例。
func (a *App) WindowControl(ctx context.Context, action string) string {
	name := winNameFromCtx(ctx)
	w := a.windowByName(name)
	if w == nil {
		return "窗口不存在"
	}
	switch action {
	case "minimise":
		// Wails 的 Minimise 在无边框窗口上不落地，直接对原生窗口发 SW_MINIMIZE
		if !minimiseNative(uintptr(w.NativeWindow())) {
			w.Minimise()
		}
	case "toggle-maximise":
		a.toggleMaximise(w, name)
	case "close":
		w.Close()
	default:
		return "未知操作: " + action
	}
	return name
}

// winRect 是窗口在「最大化之前」的位置和大小（DIP）。
type winRect struct{ x, y, w, h int }

// toggleMaximise 在「铺满工作区」和「最大化之前的尺寸」之间来回切。
//
// 不用 Wails 自己的 Maximise：frameless 窗口上那个 ShowWindow(SW_MAXIMIZE) 只
// 把状态位置上了，尺寸一动不动。自己按屏幕工作区摆还有额外好处 —— 铺的是去掉
// 任务栏的区域，和原生最大化一致，也不会因为多显示器跑错屏幕。
func (a *App) toggleMaximise(w *application.WebviewWindow, name string) {
	a.restMu.Lock()
	defer a.restMu.Unlock()
	if a.winRest == nil {
		a.winRest = make(map[string]winRect)
	}
	if b, ok := a.winRest[name]; ok {
		delete(a.winRest, name)
		w.SetPosition(b.x, b.y)
		w.SetSize(b.w, b.h)
		return
	}
	x, y := w.Position()
	width, height := w.Size()
	// 先记原来的位置再铺满：铺满之后 Position/Size 就查不到原值了
	a.winRest[name] = winRect{x, y, width, height}
	scr, err := w.GetScreen()
	if err != nil || scr == nil {
		return
	}
	wa := scr.WorkArea
	w.SetPosition(wa.X, wa.Y)
	w.SetSize(wa.Width, wa.Height)
}

// IsMaximised 报告调用方窗口是否被本程序铺满了工作区（前端切「还原」图标用）。
func (a *App) IsMaximised(ctx context.Context) bool {
	name := winNameFromCtx(ctx)
	a.restMu.Lock()
	_, ok := a.winRest[name]
	a.restMu.Unlock()
	return ok
}

// OpenProbeWindow 打开（或聚焦）多窗口验证窗口。
func (a *App) OpenProbeWindow() string {
	const name = "probe"
	if w := a.windowByName(name); w != nil {
		w.Show()
		w.Focus()
		return name
	}
	w := a.wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      name,
		Title:     "多窗口原型",
		Width:     560,
		Height:    480,
		Frameless: true,
		Windows:   framelessChrome,
		URL:       "/?w=probe",
	})
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.unregisterWindow(name)
	})
	a.registerWindow(name, w)
	return name
}

// EmitTo 把事件定向发给某一个窗口（不是广播）。
func (a *App) EmitTo(target, event, msg string) string {
	w := a.windowByName(target)
	if w == nil {
		return "没有名为 " + target + " 的窗口"
	}
	w.EmitEvent(event, msg)
	return "已定向发给 " + target
}

// WindowNames 列出当前注册的窗口，供原型面板显示。
func (a *App) WindowNames() []string {
	a.winMu.Lock()
	defer a.winMu.Unlock()
	out := make([]string, 0, len(a.windows))
	for n := range a.windows {
		out = append(out, n)
	}
	return out
}

// emitSession 发一条会话数据帧 —— 走 Stream，按窗口定向。
//
// ⚠️ 这里**不能**用事件系统。v3 的 window.EmitEvent 只是把 event.Sender 设成
// 窗口名，然后照样走全局广播（webview_window.go:248-261 → EventManager.dispatch
// 遍历全局 wailsEventListeners）。实测：对一个窗口 EmitEvent，所有窗口都收到；
// 按「main 发一次 + 会话窗口发一次」写，每个窗口就收到两份。
//
// 所以数据帧改走 v3 的 Stream：每个窗口一条连接（HandleStream 的 handler 会
// 拿到 StreamConn，连 c.Window() 都能查到），天然定向、自带背压、窗口关闭时
// 连接跟着断 —— 正好是串口数据帧需要的形态。
func (a *App) emitSession(sid, name string, data ...any) {
	var payload any
	switch len(data) {
	case 0:
	case 1:
		payload = data[0]
	default:
		payload = data
	}
	frame := map[string]any{"e": name, "d": payload}

	// 数据帧只发给**这个会话所属的窗口**。
	//
	// 以前是固定发给 "main"：多窗口之后那样发，拖出去的窗口永远收不到自己那个
	// 会话的数据（main 反而收到别人的）。归属表就是路由表（见 sessWindow）。
	owner := a.sessWindow(sid)
	if owner == "" {
		owner = "main" // 兜底：无主会话（CLI 建的、还没被认领）仍给 main
	}
	a.sendFrame(owner, frame)
	// 调试面板窗口（?w=session）是同一个会话的另一个观察者，照旧收一份
	if sid != "" {
		a.sendFrame("session-"+sid, frame)
		// 虚窗文档也吃一份：串口监视器/转发监视器换成按像素取窗口之后，
		// 前端不再自己存 historyCache，这份得在 Go 里攒起来。
		if f, ok := liveFrameFrom(name, payload); ok {
			a.logview.appendSession(sid, f)
		}
	}
}

// liveFrameFrom 把一条数据帧的参数翻成虚窗文档的输入。
//
// 只认四个数据方向（rx/tx/1/2）与 system：别的帧（dialog-result、tab-drop-* 之类）
// 走的是同一个 Stream，不能混进日志文档。
func liveFrameFrom(name string, payload any) (stlogview.FrameIn, bool) {
	var dir uint8
	switch name {
	case "rx":
		dir = sessionfile.DirRx
	case "tx":
		dir = sessionfile.DirTx
	case "1":
		dir = sessionfile.DirP1
	case "2":
		dir = sessionfile.DirP2
	case "system", "sys":
		dir = sessionfile.DirSystem
	default:
		return stlogview.FrameIn{}, false
	}
	m, ok := payload.(map[string]any)
	if !ok {
		return stlogview.FrameIn{}, false
	}
	hexStr, _ := m["hex"].(string)
	if hexStr == "" {
		return stlogview.FrameIn{}, false
	}
	// 系统帧在协议和历史里存的是**文本**（daemon/history.go 的注释：系统帧存
	// text 而不是 hex），而 stlogview 的 FrameIn.Hex 一律是十六进制串。这里补
	// 一次转码，否则系统消息会被 hex.DecodeString 判成非法输入、解成空 payload，
	// 界面上只剩一行「Sys ~0B」没有正文。
	if dir == sessionfile.DirSystem {
		hexStr = hex.EncodeToString([]byte(hexStr))
	}
	f := stlogview.FrameIn{Dir: dir, Hex: hexStr}
	if v, ok := m["tsMs"].(int64); ok {
		f.Ts = v
	} else if v, ok := m["ts"].(int64); ok {
		f.Ts = v
	} else if v, ok := m["tsMs"].(float64); ok {
		f.Ts = int64(v)
	}
	if o, ok := m["origin"].(string); ok {
		f.Origin = o
	}
	return f, true
}

// sendFrame 把一帧交给指定窗口的 Stream 连接。
//
// 用 TrySend（非阻塞）而不是 Send：串口全速时如果某个窗口消费不过来，
// 宁可丢它的帧也不能把 daemon 的事件循环卡住 —— 主窗口也会跟着卡。
func (a *App) sendFrame(windowName string, frame map[string]any) {
	a.streamMu.Lock()
	conn := a.streamConns[windowName]
	a.streamMu.Unlock()
	if conn == nil {
		return
	}
	b, err := json.Marshal(frame)
	if err != nil {
		return
	}
	// ErrStreamFull 表示该窗口读得慢，丢这一帧即可。
	// 计数 + 每 100 次记一条：串口全速时这一行会刷屏，但没有它就看不出背压有没触发。
	if err := conn.TrySend(b); err != nil {
		a.streamMu.Lock()
		a.frameDrops++
		n := a.frameDrops
		a.streamMu.Unlock()
		if n%100 == 1 {
			log.Printf("[stream] %s 丢帧累计 %d（%v）", windowName, n, err)
		}
	}
}

func (a *App) setStreamConn(name string, c *application.StreamConn) {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.streamConns == nil {
		a.streamConns = make(map[string]*application.StreamConn)
	}
	a.streamConns[name] = c
}

func (a *App) clearStreamConn(name string, c *application.StreamConn) {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.streamConns[name] == c {
		delete(a.streamConns, name)
	}
}

// setupStreams 注册数据帧的 Stream。每个窗口连上来时开一个 handler goroutine，
// 把连接登记到「窗口名 → 连接」表里，供 emitSession 定向投递。
func (a *App) setupStreams() {
	if a.wailsApp == nil {
		return
	}
	a.wailsApp.HandleStream("frames", func(c *application.StreamConn) {
		name := ""
		if w := c.Window(); w != nil {
			name = w.Name()
		}
		a.setStreamConn(name, c)
		log.Printf("[stream] 数据帧通道已连接: %q", name)
		defer func() {
			a.clearStreamConn(name, c)
			_ = c.Close()
			log.Printf("[stream] 数据帧通道断开: %q", name)
		}()
		// 这个 goroutine 的生命周期就是这个连接的生命周期：一直读到对端关闭
		// （页面刷新、窗口关闭、应用退出都会让它返回）。
		for {
			if _, err := c.Receive(); err != nil {
				return
			}
		}
	})
}

// OpenSessionWindow 把一个会话开成独立窗口（多窗口原型）。
func (a *App) OpenSessionWindow(sessionID string) string {
	if sessionID == "" {
		return "会话 ID 为空"
	}
	name := "session-" + sessionID
	if w := a.windowByName(name); w != nil {
		w.Show()
		w.Focus()
		return name
	}
	w := a.wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      name,
		Title:     "会话 " + sessionID,
		Width:     720,
		Height:    560,
		Frameless: true,
		Windows:   framelessChrome,
		URL:       "/?w=session&sid=" + url.QueryEscape(sessionID),
	})
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.unregisterWindow(name)
	})
	a.registerWindow(name, w)
	return name
}

// nextWindowName 生成一个不重复的窗口名（win-1、win-2……）。
func (a *App) nextWindowName(prefix string) string {
	a.winMu.Lock()
	defer a.winMu.Unlock()
	for {
		a.winSeq++
		name := fmt.Sprintf("%s-%d", prefix, a.winSeq)
		if _, ok := a.windows[name]; !ok {
			return name
		}
	}
}

// createWindowNamed 按指定名字建一个「普通窗口」。
//
// 普通窗口 = 和 main 完全同等：一样跑整套界面、一样能开标签页、一样能再拖出窗口。
// 这正是「没有主窗口概念」的落地方式 —— main 只是第一个建出来的那个而已。
// 窗口默认尺寸。拖出来的窗口一开始就是这个尺寸（见 StartTearOffDrag）。
const (
	appWindowWidth  = 1100
	appWindowHeight = 720
	// 拖出标签时，光标停在**被拖出来的那个窗口**里的哪个位置。
	//
	// 要落在标签上：按下标签时鼠标在标签上，窗口跟过来以后光标也应该还在标签上，
	// 否则一拖出来光标就跑到标题栏/内容区，手感是「页面从手里飞了」。
	// 窗口内坐标 = 标签栏顶部 + 半个标签高度（无边框窗口没有系统标题栏）。
	// x 取第一个标签的左侧内缩，避免落在横向滚动箭头上。
	tearOffGrabX = 24
	tearOffGrabY = 47
)

// framelessChrome 是无边框窗口的 Windows 专有选项：去掉 Win11 的圆角与投影。
//
// 不设它的话系统仍会给无边框窗口留一圈 DWM 圆角和投影 —— 看起来像「边框没裁
// 干净」，最大化时四周还会露出一条桌面。标题栏交给前端画（见 index.html 的
// .win-ctrl），所以窗口本身用 Frameless。
var framelessChrome = application.WindowsWindow{DisableFramelessWindowDecorations: true}

func (a *App) createWindowNamed(name, title string, hidden bool, width, height int) (*application.WebviewWindow, error) {
	if a.wailsApp == nil {
		return nil, fmt.Errorf("窗口系统尚未就绪")
	}
	if width <= 0 {
		width = appWindowWidth
	}
	if height <= 0 {
		height = appWindowHeight
	}
	w := a.wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      name,
		Title:     title,
		Width:     width,
		Height:    height,
		MinWidth:  320,
		MinHeight: 200,
		Frameless: true,
		Windows:   framelessChrome,
		Hidden:    hidden, // 拖动中先隐藏，设好 no-activate 再显示，避免闪一下抢焦点
		URL:       "/",
	})
	if w == nil {
		return nil, fmt.Errorf("创建窗口失败")
	}
	// 关窗前先问一句「这个窗口里的会话要一起销毁」（见 allowWindowClose）。
	// 每个窗口一个闭包：v3 的 WindowEvent 不带窗口，只能按注册时绑定的名字区分。
	w.RegisterHook(events.Common.WindowClosing, a.onWindowClosingFor(name))
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.unregisterWindow(name)
	})
	w.OnWindowEvent(events.Common.WindowFilesDropped, a.onFilesDroppedFor(name))
	a.registerWindow(name, w)
	return w, nil
}

// NewWindow 开一个新的空窗口（菜单「窗口 → 新建窗口」用）。
//
// 新窗口没有任何会话，前端会按 autoCreateMode 自动补一个空闲会话 ——
// 和主窗口刚启动时的行为一致。
func (a *App) NewWindow() (string, error) {
	name := a.nextWindowName("win")
	w, err := a.createWindowNamed(name, "串口调试工具 v"+version.Version, false, appWindowWidth, appWindowHeight)
	if err != nil {
		return "", err
	}
	// 新窗口别精确盖在前一个窗口上：位置、大小完全重合时，用户看到的还是「刚才那
	// 一份」，会以为再启动一次没反应（实测连开三个窗口全落在 410,160）。
	// 按已经开着的窗口数错开一档（28px，8 档循环），第一档就是系统给的默认位置。
	//
	// 放在这里而不是 createWindowNamed：拖标签也走 createWindowNamed，那个窗口的
	// 位置是拖动过程中由光标决定的，不该在这里先挪一次。
	if n := a.countAppWindows() - 1; n > 0 {
		// Position() 在窗口还没真正建出来（Run 之前）返回 0,0，这时不挪。
		if x, y := w.Position(); x != 0 || y != 0 {
			w.SetPosition(x+28*(n%8), y+28*(n%8))
		}
	}
	w.Show()
	w.Focus()
	log.Printf("[window] 新窗口 %q", name)
	return name, nil
}

// ---- 单实例：再启动一次 GUI 就是在当前进程里再开一个窗口 ----
//
// 第二份进程根本不会走到这里：v3 在 application.New 里发现互斥体已被占用，发一条
// WM_COPYDATA 通知就 os.Exit 了（application.go:204-218），所以界面上只会多出一个
// 窗口，任务管理器里始终只有一个 serial-gui。

// onSecondInstanceLaunch 由 v3 在「又启动了一份 GUI」时回调。
//
// ⚠️ 这里**只入队，不能直接建窗口**：回调跑在 v3 的 COPYDATA 消费 goroutine 上
// （single_instance.go:77-101），而那条 channel 容量只有 1。消费侧一旦阻塞，
// 主线程的 wndProc 就卡在塞数据上；建窗口却要 InvokeSync 回主线程 —— 两边互等，
// 连点两下必死锁。入队后立刻返回，真正的窗口由 worker 去建。
func (a *App) onSecondInstanceLaunch(data application.SecondInstanceData) {
	log.Printf("[second-instance] 又启动了一次 GUI（args=%v，工作目录 %s）→ 在新窗口打开",
		data.Args, data.WorkingDir)
	a.secondMu.Lock()
	a.secondPending = append(a.secondPending, data.Args)
	a.secondMu.Unlock()
	a.kickSecondInstance()
}

// kickSecondInstance 在「窗口系统已就绪 + 队列非空 + 没有 worker」时启动一个 worker。
//
// 重复调用是安全的：它自己会判断要不要起，worker 也只会在队列取空时退出，
// 退出与入队之间由 secondMu 和 secondWorker 标记兜住。
func (a *App) kickSecondInstance() {
	a.secondMu.Lock()
	start := a.secondReady && len(a.secondPending) > 0 && !a.secondWorker
	if start {
		a.secondWorker = true
	}
	a.secondMu.Unlock()
	if start {
		go a.runSecondInstanceWorker()
	}
}

// runSecondInstanceWorker 按到达顺序把队列里的每次启动变成一个窗口。
//
// 走 NewWindow（而不是自己拼窗口参数）：它和菜单「新建窗口」是同一条路，
// 窗口的注册、关闭确认、文件拖放、按会话分流都跟着一起就位。
func (a *App) runSecondInstanceWorker() {
	for {
		a.secondMu.Lock()
		if len(a.secondPending) == 0 {
			a.secondWorker = false
			a.secondMu.Unlock()
			return
		}
		args := a.secondPending[0]
		a.secondPending = a.secondPending[1:]
		a.secondMu.Unlock()

		if _, err := a.NewWindow(); err != nil {
			log.Printf("[second-instance] 打开新窗口失败（args=%v）: %v", args, err)
		}
	}
}

// StartTearOffDrag 由前端在「多标签窗口里把标签纵向拖出标签栏」时调用：
// 建一个**完整尺寸**的窗口、把会话转交给它，然后由 Go 自己跟随鼠标直到左键松开。
//
// 为什么跟随不放在前端：窗口一旦被拖到光标下面，鼠标事件就派发给新窗口了，
// 源页面收不到 pointermove —— 实测只看得到前 4 次移动，窗口随即卡住。
// 所以这里用 GetCursorPos 轮询 + GetAsyncKeyState 判松开，全程不依赖前端事件。
//
// 转交顺序是关键：**先**把会话归属改成新窗口的名字，**再**建窗口。反过来的话
// 新窗口第一次同步时这个会话还归源窗口，它一个会话都看不到，于是按 autoCreate
// 又补一个空闲会话，用户会看到「拖出来一个窗口，里面是个空标签」。
//
// 窗口尺寸和正常窗口一致，不是小预览窗：拖出来的东西**当场就是一个窗口**，
// 光标停在它的标签上（见 tearOffGrabX/Y），松手就留在原地，不用再长大一次。
//
// 松手/中途换主意都交给 driveTabDrop（和「单标签窗口拖标签」同一套）：拖到别人
// 标签条上停一下就当场预览着插进去，拖出来又退回成窗口，松手才落地。
func (a *App) StartTearOffDrag(ctx context.Context, sessionID string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("会话 ID 为空")
	}
	from := winNameFromCtx(ctx)
	name := a.nextWindowName("win")
	a.sessSet(sessionID, name)
	w, err := a.createWindowNamed(name, "串口调试工具 v"+version.Version, true, appWindowWidth, appWindowHeight)
	if err != nil {
		a.sessSet(sessionID, from) // 窗口没建起来，归属退回去
		return "", err
	}
	// 拖动期间不能抢焦点，否则源窗口的 pointer capture 断掉，pointermove 停供，
	// 窗口只跟随一次就卡住（见 window_noactivate_windows.go）
	setNoActivate(w, true)
	w.Show()
	// 源窗口的标签**当场摘掉**，不等松手：拖出来的东西已经是一个窗口了，
	// 原来那一页再留在源窗口里就是重影。拖到一半又放回去的话，归属退回后
	// 源窗口重新同步会把标签补回来（见 driveTabDrop 的两态循环）。
	a.emit("tearoff-start", map[string]any{"sid": sessionID, "win": name, "from": from})
	go a.driveTabDrop(name, from, w, sessionID, true)
	return name, nil
}

// closeWindowIfEmpty 关掉一个已经空了的源窗口（它的最后一页被拖走、也没被拖回来）。
//
// 「空」按归属表算：这个窗口名下一张页都没有。这一步以前是前端在 tearoff-done
// 里直接 CloseCurrentWindow，但可反悔拖动要求窗口先活着等那一页回来（见 app.js
// 的 _parked），所以只能等松手时由这里按最终归属判定。
func (a *App) closeWindowIfEmpty(name string) {
	if name == "" || len(a.sessOfWindow(name)) > 0 {
		return
	}
	if w := a.windowByName(name); w != nil {
		log.Printf("[window] %q 的最后一页已经拖走，收掉这个空窗口", name)
		w.Close()
	}
}

// BeginWindowTabDrag 由前端在「单标签窗口里按下标签」时调用。
//
// 单标签窗口没有「把这一页拆出去」的意思：那一页就是这个窗口，所以拖标签
// 等于拖窗口本身 —— 移动交给系统原生拖窗（见 style.css 的 .is-window-drag）。
// 原生拖动期间前端拿不到事件（系统模态循环），所以落点只能由这里轮询。
//
// 落点与反悔全交给 driveTabDrop（和「标签拖出成窗」同一套）。
func (a *App) BeginWindowTabDrag(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("会话 ID 为空")
	}
	self := winNameFromCtx(ctx)
	w := a.windowByName(self)
	if w == nil {
		return fmt.Errorf("找不到窗口 %q", self)
	}
	go a.driveTabDrop(self, self, w, sessionID, false)
	return nil
}

// driveTabDrop 是「拖着一个窗口找落脚点」的唯一驱动循环，一直跑到左键松开。
//
// 两条入口共用：
//   - 单标签窗口里拖标签：窗口由系统的拖窗循环带着走（follow=false）
//   - 标签拖出成窗后继续拖：窗口由这里 SetPosition 跟着光标（follow=true）
//
// 中途在「跟着光标的窗口」和「并进别人标签栏的幽灵标签」两态之间来回切 —— 就是
// 浏览器的反悔：并进去以后把光标拖出标签条带一定范围，这一页当场退回成窗口；
// 再拖回去停一下，又并进去。**松手才落地**。
//
// owner 是「这一页原本属于哪个窗口」：tearoff-done 的 from 必须是它（不能是拖出来
// 的那扇窗，否则跟随窗会把自己手里那一页摘掉），收空窗时也按它判断。单标签窗口
// 拖标签时 owner == self。
func (a *App) driveTabDrop(self, owner string, w *application.WebviewWindow, sessionID string, follow bool) {
	log.Printf("[tabdrag] 开始盯落点：窗口 %q 拖的是会话 %s（跟随=%v）", self, sessionID, follow)
	startX, startY := w.Position()
	merged := "" // 已并入的目标窗口名；"" = 还没并入
	// 阻尼：光标要在目标窗口的标签栏带里**连续待够** tabMergeDwell 才并过去。
	// 只「扫过去」不算 —— main 占屏很大，把窗口从它上面拖过去太容易蹭到标签栏，
	// 一蹭就吞进去的话根本没法把窗口挪到别处。
	hoverTarget := ""
	var hoverSince time.Time
	for leftButtonDown() {
		x, y := cursorPos()
		if merged != "" {
			// 已并入：光标离开目标标签条带**超过余量**才算拖出（见
			// tabBandLeaveMargin）。贴着边缘抖一下不弹回去，否则手一抖就重来。
			if !a.nearTabBand(merged, x, y) {
				a.unmergeWindowFromTab(self, w, sessionID, merged)
				merged, hoverTarget, hoverSince = "", "", time.Time{}
			} else {
				a.sendDropMove(merged)
			}
			time.Sleep(16 * time.Millisecond)
			continue
		}
		if follow {
			// 拖出来的窗口由这里跟手（系统那边没有拖窗循环）
			w.SetPosition(x-tearOffGrabX, y-tearOffGrabY)
		} else if px, py := w.Position(); absInt(px-startX) < 4 && absInt(py-startY) < 4 {
			// 窗口还没挪窝 = 只是按了一下标签：这时候不做落点判断，否则
			// 「窗口正好压在别人标签栏上时点一下标签」会把页面甩走。
			time.Sleep(16 * time.Millisecond)
			continue
		}
		target := a.tabBandWindowAt(x, y, self)
		switch {
		case target == "":
			hoverTarget, hoverSince = "", time.Time{}
		case target != hoverTarget:
			hoverTarget, hoverSince = target, time.Now()
		case time.Since(hoverSince) >= tabMergeDwell:
			a.mergeWindowIntoTab(self, owner, w, sessionID, target)
			merged, hoverTarget, hoverSince = target, "", time.Time{}
			continue
		}
		time.Sleep(16 * time.Millisecond)
	}
	// 松手：已并入就落地（幽灵标签收尾 + 关掉跟随窗），没并入就让窗口留在原处。
	if merged != "" {
		a.sendDropMove(merged)
		a.sendFrame(merged, map[string]any{"e": "tab-drop-end", "d": map[string]any{"sid": sessionID}})
		w.Close()
		return
	}
	if follow {
		// 拖出来的窗口就地留下：尺寸本来就是整窗的，只把跑出屏幕的部分推回来
		setNoActivate(w, false)
		clampWindowToScreen(w, appWindowWidth, appWindowHeight)
		w.Focus()
		log.Printf("[tearoff] 会话 %s 就地成窗 %q", sessionID, self)
	}
	log.Printf("[tabdrag] 松手：窗口 %q 没有并进别人标签栏", self)
	a.closeWindowIfEmpty(owner)
}

// tabMergeDwell 是「拖着的窗口停在别人标签栏上多久才并进去」的阻尼。
const tabMergeDwell = 300 * time.Millisecond

// tabBandLeaveMargin 是「已并入之后又拖出来」的余量（物理像素）。
//
// 光标要离开标签条带这么多才算拖出：贴着边缘抖一下、或者只是往下挪了几像素调
// 位置，都不该把刚并进去的那一页弹回去。单向没有阻尼 —— 拖出去是用户明确的
// 反向动作，马上生效手感才对。
const tabBandLeaveMargin = 48

// nearTabBand 判断屏幕坐标 (x,y) 是否还在某个窗口标签条带的「含余量」范围里。
//
// 比严格的标签条带（tabBandHeight 那条线）四周外扩了 tabBandLeaveMargin，
// 专门给「已并入之后还能拖回去」用：在带内 = 还打算并着，出余量 = 反悔。
func (a *App) nearTabBand(name string, x, y int) bool {
	w := a.windowByName(name)
	if w == nil {
		return false
	}
	px, py := w.Position()
	pw, _ := w.Size()
	return x >= px-tabBandLeaveMargin && x < px+pw+tabBandLeaveMargin &&
		y >= py-tabBandLeaveMargin && y < py+tabBandHeight+tabBandLeaveMargin
}

// mergeWindowIntoTab 把拖着的窗口里那一页并到另一个窗口的标签栏（鼠标还按着，
// 所以这还不是「落地」，见 driveTabDrop 的两态循环与 unmergeWindowFromTab）。
//
// owner 是这一页原本的窗口：只有**它不等于目标**时才发 tearoff-done（那一页确实
// 离开了源窗口）。目标就是源窗口的场合（把标签拖回去）发了会和新标签的同步打架，
// 源窗口会把自己刚补回来的标签又摘掉。
//
// 顺序有讲究：先转归属（目标窗口同步时才看得到它），再藏本窗口，然后才通知目标
// 窗口 —— 关窗是异步的，紧跟着发的同步可能正好撞上销毁过程而失败。
func (a *App) mergeWindowIntoTab(self, owner string, w *application.WebviewWindow, sessionID, target string) {
	a.sessSet(sessionID, target)
	log.Printf("[tabdrag] 会话 %s 随窗口并入 %q 的标签栏（源 %q）", sessionID, target, owner)
	// **不能**在这里直接关窗：鼠标还按着，系统那边的拖动循环还在跑，窗口一没
	// 它就把这次拖动转给光标下面的那个窗口 —— 实测目标窗口被一路拖走了 330px。
	// 先藏起来（拖动循环还在这扇看不见的窗口上，不会波及别人），松手后再关。
	w.Hide()
	// tab-drop-begin：目标窗口收到后重同步建出这个标签，并把它当成「正在拖的
	// 标签」接着左右滑 —— 鼠标还没松，这一拖还没结束。
	a.sendFrame(target, map[string]any{"e": "tab-drop-begin", "d": map[string]any{
		"sid": sessionID,
		"x":   a.pointerRelX(target),
	}})
	if target != owner {
		a.emit("tearoff-done", map[string]any{"sid": sessionID, "win": target, "from": owner})
	}
	a.emit("sessions-changed", map[string]any{"reason": "move", "sid": sessionID, "to": target})
}

// unmergeWindowFromTab 是 mergeWindowIntoTab 的反向：把已经并进别人标签栏的那一页
// 当场退回成窗口 —— 鼠标还按着，用户把光标拖出了目标窗口的标签条带（见
// tabBandLeaveMargin），这就是浏览器的「反悔」。
//
// 归属先退回来（源窗口下次同步才能看到它），再把源窗口重新显出来：隐藏期间系统的
// 拖窗循环一直挂在这扇窗口上、也在跟着光标挪，显出来就继续跟手。
func (a *App) unmergeWindowFromTab(self string, w *application.WebviewWindow, sessionID, target string) {
	a.sessSet(sessionID, self)
	log.Printf("[tabdrag] 会话 %s 从 %q 的标签栏退回（源 %q）", sessionID, target, self)
	w.Show()
	a.sendFrame(target, map[string]any{"e": "tab-drop-cancel", "d": map[string]any{"sid": sessionID}})
	a.emit("sessions-changed", map[string]any{"reason": "move", "sid": sessionID, "to": self})
}

// pointerRelX 返回「光标相对某个窗口左边的横向距离」（物理像素）。
func (a *App) pointerRelX(win string) int {
	x, _ := cursorPos()
	w := a.windowByName(win)
	if w == nil {
		return x
	}
	px, _ := w.Position()
	return x - px
}

// sendDropMove 把当前光标位置（相对目标窗口左边的横向像素）转给目标窗口，
// 让那边接管拖动的幽灵标签跟着光标滑。
//
// 为什么要由 Go 转发：鼠标还按着，但指针已经在目标窗口上了，源窗口收不到
// pointermove，目标窗口自己也拿不到 —— 只有这里轮询得到光标。
func (a *App) sendDropMove(target string) {
	a.sendFrame(target, map[string]any{"e": "tab-drop-move", "d": map[string]any{"x": a.pointerRelX(target)}})
}

// clampWindowToScreen 把窗口左上角推回它所在屏幕的工作区里。
//
// 拖到屏幕右下角松手时，1100×720 会有一半跑到屏幕外 —— 用窗口自己的
// GetScreen，多屏也对。
func clampWindowToScreen(w *application.WebviewWindow, width, height int) {
	if w == nil {
		return
	}
	px, py := w.Position()
	maxX, maxY := 1<<30, 1<<30
	minX, minY := -1<<30, -1<<30
	if scr, err := w.GetScreen(); err == nil && scr != nil {
		wa := scr.WorkArea
		minX, minY = wa.X, wa.Y
		maxX = wa.X + wa.Width - width
		maxY = wa.Y + wa.Height - height
	}
	if px > maxX {
		px = maxX
	}
	if py > maxY {
		py = maxY
	}
	if px < minX {
		px = minX
	}
	if py < minY {
		py = minY
	}
	w.SetPosition(px, py)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// tabBandHeight 是「窗口顶部属于标签栏的那一带」的高度（菜单栏 + 标签栏）。
//
// 拖拽落点落在这条带子里 = 插到那排标签里；落在下面（内容区）不算。
// 68 px 是按现有布局量的：菜单栏 32 + 标签栏 36（单行标签；双行再加 18）。
// （无边框窗口没有系统标题栏，原来那 40 px 已经拿掉。）
// 这里没法做精确的 DOM 命中（松手时源页面拿不到坐标），所以用布局常量近似。
const tabBandHeight = 68

// tabBandWindowAt 返回「屏幕坐标 (x,y) 落在哪个窗口的标签栏带上」，没有则返回空。
//
// 拖标签的落点用它，而不是整个窗口矩形：拖到内容区只是路过，不该把页面塞进去。
//
// 用矩形判断，不用 WindowFromPoint：v3 没暴露 HWND（webview_window_windows.go
// 里的 hwnd 是私有字段），拿不到「这个 HWND 是哪个窗口」的映射。
// 多个窗口重叠时取面积最小的那个 —— 叠在上面的通常是小的那个。
func (a *App) tabBandWindowAt(x, y int, exclude string) string {
	a.winMu.Lock()
	defer a.winMu.Unlock()
	best := ""
	bestArea := 0
	for name, w := range a.windows {
		if name == exclude {
			continue
		}
		if name != "main" && !strings.HasPrefix(name, "win-") {
			continue // 对话框、调试窗口不算落点
		}
		px, py := w.Position()
		pw, ph := w.Size()
		if x < px || x >= px+pw || y < py || y >= py+tabBandHeight {
			continue
		}
		if area := pw * ph; best == "" || area < bestArea {
			best, bestArea = name, area
		}
	}
	return best
}

// 把会话挪到另一个窗口（供右键菜单等入口复用；拖拽走 driveTabDrop）。
func (a *App) MoveSessionToWindow(sessionID, target string) error {
	if sessionID == "" || target == "" {
		return fmt.Errorf("会话 ID 与目标窗口都不能为空")
	}
	if a.windowByName(target) == nil {
		return fmt.Errorf("窗口 %q 不存在", target)
	}
	a.sessSet(sessionID, target)
	log.Printf("[window] 会话 %s 移动到窗口 %q", sessionID, target)
	a.emit("sessions-changed", map[string]any{"reason": "move", "sid": sessionID, "to": target})
	return nil
}

// WindowList 列出当前所有「真正的应用窗口」（main / win-N），供界面做选择。
func (a *App) WindowList() []string {
	a.winMu.Lock()
	defer a.winMu.Unlock()
	out := make([]string, 0, len(a.windows))
	for name := range a.windows {
		if name == "main" || strings.HasPrefix(name, "win-") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// SetWindowPosition 供「拖出来的窗口跟随鼠标」使用（前端在 pointermove 里调用）。
//
// v3 的 startDrag 不是公开 API，拿不到原生拖窗循环，所以退一步：前端自己算
// 屏幕坐标，这里直接 SetPosition；调用频率由前端限流。
func (a *App) SetWindowPosition(name string, x, y int) {
	w := a.windowByName(name)
	if w == nil {
		log.Printf("[solo] SetWindowPosition 找不到窗口 %q", name)
		return
	}
	log.Printf("[solo] SetWindowPosition %q -> (%d,%d)", name, x, y)
	w.SetPosition(x, y)
}

// FirstSessionID 返回守护进程上一个会话的 ID（原型自动开窗用）。
// 优先挑已连接的 —— 只有连着的会话才有数据流，空闲会话开出来是空窗口。
func (a *App) FirstSessionID() string {
	sessions := a.GetSessions()
	for _, s := range sessions {
		if st, _ := s["status"].(string); st != "connected" {
			continue
		}
		if id, ok := s["id"].(string); ok && id != "" {
			return id
		}
	}
	for _, s := range sessions {
		if id, ok := s["id"].(string); ok && id != "" {
			return id
		}
	}
	return ""
}

// DebugEmitSession 手工注入一条会话数据帧。
//
// 用来在没有串口连接的情况下验证「数据帧按会话定向」这条链路 —— 真实环境里
// 它只在串口有数据时才跑得到。注入后应该只有 main 和 session-<sid> 收到，
// 其他窗口（比如 probe）一帧都不该收到。阶段 2 删掉。
func (a *App) DebugEmitSession(sid, text string) string {
	if sid == "" {
		return "会话 ID 为空"
	}
	a.emitSession(sid, "rx", map[string]any{
		"processId": sid,
		"text":      text,
		"hex":       "",
		"ts":        time.Now().UnixMilli(),
	})
	return "已注入到 " + sid
}

// emit 是 v2 runtime.EventsEmit 的替身。
//
//	v2: runtime.EventsEmit(a.ctx, name, data...)
//	v3: app.Event.Emit(name, data...)      —— 广播到所有窗口
//
// 多窗口阶段这里要按「这个事件该给谁」分流（数据帧只发给它所属的会话窗口，
// 见方案里的事件归属表）。现在只有一个窗口，先一律广播。
func (a *App) emit(name string, data ...any) {
	if a.wailsApp == nil {
		return
	}
	a.wailsApp.Event.Emit(name, data...)
}

// quit 是 v2 runtime.Quit 的替身。
func (a *App) quit() {
	if a.wailsApp == nil {
		return
	}
	a.wailsApp.Quit()
}

// Log 让前端把诊断信息打到 Go 侧 stderr。
//
// production 构建不开 DevTools（v3 的 inspector 要 debug 模式），前端 console
// 在发布版里等于黑洞；排查界面问题时用这个把关键状态带出来。
func (a *App) Log(msg string) {
	log.Printf("[js] %s", msg)
}

// onFilesDroppedFor 把原生拖放的文件路径转给前端。
// v3 没有前端侧的 OnFileDrop，原生 drop 只能在 Go 侧收。
//
// 多窗口之后事件依然是全局广播，所以带上来源窗口名 (win)：前端只处理发给
// 自己窗口的那次，否则往 win-2 拖一个 .stlog，**每个**窗口都会各开一个页签。
func (a *App) onFilesDroppedFor(name string) func(*application.WindowEvent) {
	return func(event *application.WindowEvent) {
		files := event.Context().DroppedFiles()
		if len(files) == 0 {
			return
		}
		target := ""
		if details := event.Context().DropTargetDetails(); details != nil {
			target = details.ElementID
		}
		a.emit("files-dropped", map[string]any{"files": files, "target": target, "win": name})
	}
}

// ServiceStartup 是 v3 的 service 生命周期钩子（v2 里叫 Startup）。
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx
	// 守护进程就绪通知：阻塞等 OS 级「管道可连接」，就绪后立刻推给前端，
	// 前端收到就能马上连，不必等 checkOffline 的 1s 轮询。
	go a.watchDaemonReady(ctx)
	return nil
}

// daemonReadyRepeat 是「守护进程就绪」事件的重发间隔（见 watchDaemonReady）。
//
// 1 秒而不是更长：手动启停 daemon 时，GUI 只能靠这个事件发现它起来了
// （GUI 自己点的「启动」走 App.StartDaemon，会主动连，不受这个间隔影响）。
// 实测间隔 3s 时，daemon 起来后 3.0s 才注册，看着就像「慢连接」。
// 代价可以忽略：在线时前端收到事件立刻 return，根本不调 Go。
const daemonReadyRepeat = 1 * time.Second

// watchDaemonReady 把「守护进程就绪」变成前端可订阅的事件。
//
// pipe.WaitReady 在 Windows 上就是 WaitNamedPipe：守护进程一 Listen 就返回，
// 不存在「进程起来了但管道还没建好」的误判。
//
// 就绪期间每 daemonReadyRepeat 重发一次：前端已经删掉离线轮询，上线完全依赖
// 这个事件，而 EventsEmit 在页面加载完成前发出会被丢弃（热重载、慢机器启动都
// 踩得到）——只发一次，丢一次就永久离线。这是 Go 侧的事件重发，不是客户端轮询。
func (a *App) watchDaemonReady(ctx context.Context) {
	for ctx.Err() == nil {
		if !pipe.WaitReady(1 * time.Second) {
			continue
		}
		// 端点从不可用变成可用：立刻通知，不用等下一个重发周期
		a.emit("daemon-ready", map[string]any{})
		for ctx.Err() == nil {
			// 端点消失（守护进程退出）就回外层，等它下次就绪时**立即**再通知
			if !pipe.WaitReady(300 * time.Millisecond) {
				break
			}
			time.Sleep(daemonReadyRepeat)
			a.emit("daemon-ready", map[string]any{})
		}
	}
}

// ServiceShutdown 是 v3 的 service 退出钩子（v2 里叫 Shutdown）。
func (a *App) ServiceShutdown() error {
	a.StopAllTabDecoders()
	a.CloseAllDialogs()
	if a.eventConn != nil {
		a.eventConn.Close()
		a.eventConn = nil
	}
	return nil
}

// ---- 关闭流程 ----
//
// 新模型下「关窗口 = 销毁这个窗口名下的所有会话」，所以关窗要问两件事：
//   1. 这个窗口还开着 N 个会话吗？开着就得确认（一关就断串口）
//   2. 按设置要不要顺手停守护进程？（原来的 shutdownDaemonMode 逻辑）
// 两件事合成**一个**确认框，避免连点两次「确定」。
//
// 窗口关闭事件是同步派发的，所以流程分两段：第一次关闭被拦下 → 定向发一条
// window-close-confirm 帧给这个窗口 → 前端弹窗 → 用户点确定后调
// ConfirmWindowClose() 再关一次，这次看到 forcedWindows 就放行。
//
// v3 的拦法（和 v2 完全不是一回事，迁移时踩过）：
//   - 必须用 win.RegisterHook(WindowClosing, ...) + event.Cancel()；
//   - 用 win.OnWindowEvent 挂监听器**拦不住**：v3 在窗口创建时自己挂了一个
//     OnWindowEvent(WindowClosing) 监听器，里面无条件 destroy + close
//     （webview_window.go:367-378），而 Cancel 只对 Hook 生效——
//     Hook 先跑，事件被 Cancel 后不再派发给监听器（events.go:151-160）。
//     症状：确认框根本不弹，点 X 直接退出。
//
// 确认帧为什么走 Stream 而不是事件：v3 的 Emit 是全局广播，一个窗口关窗会让
// **所有**窗口弹出确认框（实测的坑，和 emitSession 的注释同源）。

// onWindowClosingFor 生成某个窗口的 WindowClosing **Hook**。
//
// 注意 event.Cancel() 的语义是 **拦住**，不是放行。
func (a *App) onWindowClosingFor(name string) func(*application.WindowEvent) {
	return func(event *application.WindowEvent) {
		if a.allowWindowClose(name) {
			return
		}
		event.Cancel()
	}
}

// allowWindowClose 决定这次关窗要不要拦下来。
func (a *App) allowWindowClose(name string) bool {
	// 用户已经确认过（ConfirmWindowClose 第二次关闭）：直接放行
	a.closeWinMu.Lock()
	forced := a.forcedWindows[name]
	if forced {
		delete(a.forcedWindows, name)
		delete(a.pendingCloseWin, name)
	}
	a.closeWinMu.Unlock()
	if forced {
		return true
	}

	sids := a.sessOfWindow(name)
	cfg := a.LoadAppSettings()
	// 「要不要停守护进程」只对**最后一个窗口**有意义。
	//
	// decideClose 数的是守护进程上的客户端数，而所有窗口共用同一个 GUI 客户端 ——
	// 多窗口之后 clients 永远是 1，照它判定的话关掉任意一个窗口都会把守护进程停掉，
	// 别的窗口里的串口跟着全断（实测踩到：关 win-4，main 的会话也死了）。
	lastWindow := a.countAppWindows() <= 1
	action := closeProceed
	if lastWindow {
		clients := a.GetClients()
		action = decideClose(cfg.ShutdownDaemonMode, cfg.ShutdownDaemonWarn, clients != nil, len(clients))
	}
	// 这里的判定结果只在一处出现，不打日志的话「为什么没弹确认框」完全无从查起
	// （实测踩过：clients 多一个就会静默放行）。
	log.Printf("[close] window=%q sessions=%d lastWindow=%v mode=%s warn=%v -> action=%d",
		name, len(sids), lastWindow, cfg.ShutdownDaemonMode, cfg.ShutdownDaemonWarn, action)

	// 要停守护进程但用户没让「不再提醒」：停下来即可，但会话也要跟着没了，
	// 这时若窗口里还有会话，下面照样要确认。
	stopDaemon := action == closeStopDaemon || action == closeConfirm

	// 「关窗口时会话怎么处理」的默认动作（设置 → 标签页）。
	// transfer 只有在确实还有别的窗口时才成立，否则退化成 destroy。
	configured := cfg.WindowCloseAction
	if configured != "destroy" && configured != "transfer" {
		configured = "ask"
	}
	closeMode := configured
	canTransfer := a.pickTransferTarget(name) != ""
	degraded := false
	if closeMode == "transfer" && !canTransfer {
		closeMode = "destroy"
		degraded = true
	}

	// destroy / transfer 是用户在设置里明确选过的，不再追问（设置页那条文案本来
	// 就叫「直接销毁（会断串口）」）；只有 ask、以及「想转交却没地方转」这种
	// 退化情形才弹会话确认框。对话框里勾了「记住选择」写的就是这个设置。
	needConfirm := action == closeConfirm || windowCloseNeedsConfirm(configured, len(sids), degraded)

	if !needConfirm {
		if len(sids) > 0 {
			switch closeMode {
			case "transfer":
				a.transferWindowSessions(name)
			case "destroy":
				// 用户在设置里明确选了「直接销毁」，或者在确认框里勾了「记住选择」
				// 选过销毁：这里就是最终决定，真把会话销毁掉。
				// 以前这条分支只处理 transfer，destroy 全靠确认框走
				// ConfirmWindowClose；现在 destroy 不再弹框，漏掉这一步的话会话
				// 会变成守护进程上的孤儿 —— 标签没了、串口却一直开着（实测踩到）。
				n := a.destroyWindowSessions(name)
				log.Printf("[close] 窗口 %q 按设置直接销毁 %d 个会话", name, n)
			}
		}
		if stopDaemon {
			a.stopDaemonQuiet()
		}
		a.windowClosingCleanup(name)
		// 这里是「放行这次关闭」，不要再调 Close()（会二次触发关闭）；
		// 但最后一个窗口还是要显式退程序，否则窗口没了进程还在。
		if lastWindow {
			go func() {
				time.Sleep(300 * time.Millisecond)
				a.quit()
			}()
		}
		return true
	}

	// 同一轮里可能被点好几次 X：短时间内只发一次确认帧，否则会叠出多个确认窗口。
	// 用时间戳而不是布尔标记：前端万一没回调 CancelWindowClose（用户按 Esc、
	// 对话框进程崩了），标记也能自己过期，不至于之后再也关不掉这个窗口。
	a.closeWinMu.Lock()
	if a.pendingCloseWin == nil {
		a.pendingCloseWin = make(map[string]time.Time)
	}
	last := a.pendingCloseWin[name]
	if !last.IsZero() && time.Since(last) < 1500*time.Millisecond {
		a.closeWinMu.Unlock()
		return false
	}
	a.pendingCloseWin[name] = time.Now()
	a.closeWinMu.Unlock()

	a.sendFrame(name, map[string]any{"e": "window-close-confirm", "d": map[string]any{
		"sessions":    len(sids),
		"stopDaemon":  stopDaemon,
		"mode":        cfg.ShutdownDaemonMode,
		"closeAction": closeMode, // ask / destroy
		"canTransfer": canTransfer,
	}})
	return false
}

// ConfirmWindowClose 由前端在用户确认关闭窗口后调用。
//
// mode: destroy 销毁本窗口的会话 / transfer 把它们转到别的窗口 —— 后者不销毁
// 任何会话，串口连接不断，只是换了个窗口显示。
func (a *App) ConfirmWindowClose(ctx context.Context, mode string, stopDaemon bool) error {
	name := winNameFromCtx(ctx)
	if name == "" {
		return fmt.Errorf("无法确定发起调用的窗口")
	}
	n := 0
	transferredTo := ""
	if mode == "transfer" {
		transferredTo = a.transferWindowSessions(name)
	}
	if transferredTo == "" {
		// 没别的地方可去（或者用户选的就是销毁）：照旧销毁
		n = a.destroyWindowSessions(name)
	}
	if stopDaemon {
		a.stopDaemonQuiet()
	}
	// 关掉的是最后一个窗口 → 整个程序该退出。
	//
	// 不能指望 v3 的「最后一个窗口关闭就退」：实测 Windows 上关掉最后一个窗口后
	// 窗口没了、进程还在（fd 还占着、守护进程也没了，用户再启动就是两个进程）。
	// 所以这里自己 Quit，行为跟旧版「关窗口 = 退程序」一致。
	lastWindow := a.countAppWindows() <= 1
	a.closeWinMu.Lock()
	if a.forcedWindows == nil {
		a.forcedWindows = make(map[string]bool)
	}
	a.forcedWindows[name] = true
	delete(a.pendingCloseWin, name)
	a.closeWinMu.Unlock()
	log.Printf("[close] 窗口 %q 确认关闭：mode=%s 销毁会话 %d 个，转到窗口 %q，停守护进程=%v，最后一个窗口=%v",
		name, mode, n, transferredTo, stopDaemon, lastWindow)

	// 异步关：这个调用是从该窗口的 JS 里发出来的，同步 Close 会把正在回包的
	// IPC 连接一起带走，前端可能连回执都收不到。
	go func() {
		if w := a.windowByName(name); w != nil {
			w.Close()
		} else if name == "main" && a.window != nil {
			a.window.Close()
		}
		if lastWindow {
			// 留一点时间让窗口真正销毁（Quit 会强制关掉剩余窗口）
			time.Sleep(300 * time.Millisecond)
			a.quit()
		}
	}()
	return nil
}

// CancelWindowClose 由前端在用户点了「取消」后调用，清掉待确认标记。
func (a *App) CancelWindowClose(ctx context.Context) {
	name := winNameFromCtx(ctx)
	if name == "" {
		return
	}
	a.closeWinMu.Lock()
	delete(a.pendingCloseWin, name)
	a.closeWinMu.Unlock()
}

// countAppWindows 数一下还有几个「真正的应用窗口」（main / win-N）。
//
// 对话框窗口、调试用的 probe / session-* 窗口都不算：关一个对话框不能让主窗口
// 以为自己是最后一个窗口。
func (a *App) countAppWindows() int {
	a.winMu.Lock()
	defer a.winMu.Unlock()
	n := 0
	for name := range a.windows {
		if name == "main" || strings.HasPrefix(name, "win-") {
			n++
		}
	}
	return n
}

// windowClosingCleanup 在窗口真的关掉时收尾：把它的会话归属清掉（会话本身
// 已经在 destroyWindowSessions 里销毁了；走到这里的通常是没有会话的窗口）。
func (a *App) windowClosingCleanup(name string) {
	if sids := a.sessDropWindow(name); len(sids) > 0 {
		log.Printf("[close] 窗口 %q 关闭，仍有 %d 个会话未销毁: %v", name, len(sids), sids)
	}
}

// destroyWindowSessions 销毁某个窗口名下的全部会话 —— 这就是
// 「关窗口 = 销毁该窗口所有标签页的线程」的实现。
func (a *App) destroyWindowSessions(name string) int {
	sids := a.sessDropWindow(name)
	n := 0
	for _, sid := range sids {
		a.StopTabDecoder(sid)
		if err := a.CloseSession(sid); err != nil {
			log.Printf("[close] 销毁会话 %s 失败: %v", sid, err)
			continue
		}
		n++
	}
	if len(sids) > 0 {
		log.Printf("[close] 窗口 %q 销毁会话 %d/%d: %v", name, n, len(sids), sids)
	}
	return n
}

// pickTransferTarget 选一个「接盘」窗口：优先 main，否则编号最小的 win-N。
//
// 为什么优先 main：它是启动就在的那个，用户对它的位置最熟；转过去的标签
// 出现在最稳的窗口里，不会跟着某个临时窗口一起被关掉。
func (a *App) pickTransferTarget(from string) string {
	a.winMu.Lock()
	defer a.winMu.Unlock()
	if from != "main" {
		if _, ok := a.windows["main"]; ok {
			return "main"
		}
	}
	best := ""
	for name := range a.windows {
		if name == from || !strings.HasPrefix(name, "win-") {
			continue
		}
		if best == "" || name < best {
			best = name
		}
	}
	return best
}

// transferWindowSessions 把某个窗口名下的会话整体转给另一个窗口，返回目标窗口名。
//
// 和 destroyWindowSessions 的区别：**不销毁任何会话**，串口连接不断，只是换了个
// 窗口显示。转完之后两边都要重同步：目标窗口会为新会话建标签，源窗口（如果还活着）
// 会把自己的标签摘掉。
func (a *App) transferWindowSessions(from string) string {
	target := a.pickTransferTarget(from)
	if target == "" {
		log.Printf("[close] 窗口 %q 没有可转交的目标窗口", from)
		return ""
	}
	sids := a.sessDropWindow(from)
	for _, sid := range sids {
		a.sessSet(sid, target)
	}
	if len(sids) > 0 {
		log.Printf("[close] 窗口 %q 的 %d 个会话转到窗口 %q: %v", from, len(sids), target, sids)
	}
	a.emit("sessions-changed", map[string]any{"reason": "transfer", "from": from, "to": target})
	return target
}

// closeAction 是「关窗口时怎么处理守护进程」的判定结果。
type closeAction int

const (
	closeProceed    closeAction = iota // 直接放行，不动守护进程
	closeStopDaemon                    // 先停守护进程再放行
	closeConfirm                       // 拦住关闭，让前端弹确认框
)

// decideClose 是关窗时守护进程部分的判定核心，单独拆出来是为了能直接测各种组合。
//
// mode: never 从不 / last 最后一个客户端才关 / always 总是关。
// connected: 本次是否真的连着守护进程（没连上就没什么可关的）。
// clientCount: 守护进程上的客户端总数（含自己）。
//
// 注意 clientCount 数的是**全部**客户端，不是只数 gui：否则「只剩一个 GUI、
// 旁边还有个 CLI/MCP 连着」时关窗口会把它们一起断掉。当前 source 只有
// gui / cli / mcp 三种（protocol.go:91），这里不按 source 过滤，将来加 web
// 客户端也自动算进来。
func decideClose(mode string, warn, connected bool, clientCount int) closeAction {
	if mode != "last" && mode != "always" {
		return closeProceed // never（含旧配置缺这个键）
	}
	if !connected {
		return closeProceed
	}
	if mode == "last" && clientCount > 1 {
		return closeProceed // 还有别的客户端，它/它们还要用
	}
	if !warn {
		return closeStopDaemon
	}
	return closeConfirm
}

// windowCloseNeedsConfirm 决定「有会话时关这个窗口要不要弹确认框」。
//
// 和设置页的三档文案对齐（设置 → 标签页 → 窗口里的会话）：
//   - ask      每次都问
//   - destroy  用户明确选了「直接销毁（会断串口）」，不再追问
//   - transfer 不破坏任何东西，直接转交
//
// degraded = 设置是 transfer、但这会儿没有别的窗口可转（退化成销毁）。这种情形
// 必须问：用户期望的是「转走」，不是「悄悄把串口断掉」。
func windowCloseNeedsConfirm(configured string, sessions int, degraded bool) bool {
	if sessions <= 0 {
		return false
	}
	if degraded {
		return true
	}
	return configured != "destroy" && configured != "transfer"
}

// QuitApp 退出整个程序（所有窗口一起关），保留给「彻底退出」这类入口。
//
// 常规的「文件 → 退出」不走这里：那只是关掉当前窗口，其它窗口（如果有）应该
// 继续活着 —— 新模型下没有主窗口，关一个窗口不该带走别的。
func (a *App) QuitApp() {
	a.stopDaemonQuiet()
	a.quit()
}

// CloseCurrentWindow 由前端「文件 → 退出」调用：走和点窗口 X 完全一样的路径
// （照样弹关窗确认、照样销毁本窗口的会话）。
//
// 它故意**不**直接 Application.Quit：那会强制关窗，绕过 WindowClosing Hook。
func (a *App) CloseCurrentWindow(ctx context.Context) {
	name := winNameFromCtx(ctx)
	if name == "" {
		if a.window != nil {
			a.window.Close()
		}
		return
	}
	if w := a.windowByName(name); w != nil {
		w.Close()
		return
	}
	if a.window != nil {
		a.window.Close()
	}
}

// CloseMainWindow 兼容旧调用：关掉第一个窗口。
func (a *App) CloseMainWindow() {
	if a.window != nil {
		a.window.Close()
	}
}

// stopDaemonQuiet 与 StopDaemon 走同一条 IPC 路径，但不做那个 3 秒的 stopping
// 复位 —— 进程马上要退了，不需要再允许重连。
func (a *App) stopDaemonQuiet() {
	a.daemonMu.Lock()
	a.stopping = true
	ec := a.eventConn
	a.eventConn = nil
	a.daemonMu.Unlock()
	// 守护进程要停了：它上面的会话随之消失，归属表一并清掉（关窗口时不再按旧会话数弹框）
	a.sessClearAll()
	if ec == nil {
		return
	}
	client.CallOnce(contract.Shutdown, nil, "gui")
	ec.Close()
}

// onDaemonDisconnected is called by the heartbeat goroutine when the
// connection to the daemon is lost (3 consecutive ping failures).
// It only takes effect if ec is still the active connection, preventing
// a stale connection from killing a fresh reconnection.
func (a *App) onDaemonDisconnected(ec *client.DaemonClient) {
	a.daemonMu.Lock()
	if a.eventConn != ec {
		a.daemonMu.Unlock()
		return
	}
	a.eventConn = nil
	a.daemonMu.Unlock()

	// 判死之后先重连一次再报离线。
	//
	// 心跳可能误判（守护进程只是卡顿一下），而前端收到 daemon-offline 会销毁
	// 所有会话页、清掉历史缓存 —— 代价太大。重连成功就静默恢复：连接对象换了，
	// 前端无感。只有真的连不上才发离线事件。
	if nec, err := client.NewDaemonClient("gui"); err == nil {
		nec.OnDisconnect = func() { a.onDaemonDisconnected(nec) }
		a.daemonMu.Lock()
		if a.eventConn != nil {
			// 期间别的路径（CheckDaemonStatus / StartDaemon）已经连上，别抢
			a.daemonMu.Unlock()
			nec.Close()
			return
		}
		a.eventConn = nec
		a.daemonMu.Unlock()
		go a.pollDaemonEvents(nec)
		go nec.Subscribe(nil)
		// 告诉前端「又连上了」。这次重连是 Go 侧发起的，前端并不知道连接对象换了；
		// 而 daemon 重启前的 daemon-shutdown 广播已经让前端进了离线态（还清掉了
		// 会话页），不通知的话界面会一直停在离线，等下一次 daemon-ready 重发才醒。
		a.emit("daemon-ready", map[string]any{})
		return
	}

	// 重连也失败 = 守护进程真没了，会话跟着没了：先清归属再报离线，
	// 免得界面上还留着「这个窗口有 3 个会话」的假象（关窗口时会据此弹确认框）。
	a.sessClearAll()
	a.emit("daemon-offline", map[string]any{"reason": "heartbeat"})
}

// ---- daemon lifecycle ----

// CheckDaemonStatus returns true if the GUI is connected to a live daemon.
// It does NOT poll the daemon with IPC calls; connection health is tracked
// by the DaemonClient heartbeat. This method only establishes the initial
// connection or verifies the state after a disconnection event.
func (a *App) CheckDaemonStatus() bool {
	// Atomically check state and claim the right to connect.
	// This prevents two goroutines from both calling NewDaemonClient,
	// which would register a duplicate client entry with the daemon.
	a.daemonMu.Lock()
	if a.stopping {
		a.daemonMu.Unlock()
		return false
	}
	if a.checking || a.eventConn != nil {
		online := a.eventConn != nil
		a.daemonMu.Unlock()
		return online
	}
	a.checking = true
	a.daemonMu.Unlock()

	// 直接尝试建立连接。
	//
	// 这里刻意**不**先用 client.IsDaemonProcessRunning()（tasklist）做闸门：
	// 那是启发式检查——tasklist 被安全软件拦截、超时、CreateProcess 失败，
	// 或命令行输出不符合预期时都会返回 false，于是守护进程明明在等连接，
	// GUI 却被判成离线、连 IPC 都不试。实测该命令每次约 250ms。
	// 没有守护进程时 WaitNamedPipe 会立刻返回 ERROR_FILE_NOT_FOUND
	// （见 pipe.dialPipe），所以直接拨号并不慢，而且它是权威判据。
	ec, err := client.NewDaemonClient("gui")
	if err != nil {
		a.daemonMu.Lock()
		a.checking = false
		a.daemonMu.Unlock()
		return false
	}

	ec.OnDisconnect = func() { a.onDaemonDisconnected(ec) }

	a.daemonMu.Lock()
	// Double-check: another goroutine may have beaten us (can only happen
	// if StartDaemon was called while we were blocked in NewDaemonClient).
	if a.eventConn != nil {
		a.daemonMu.Unlock()
		ec.Close()
		a.checking = false
		return true
	}
	a.eventConn = ec
	a.checking = false
	a.daemonMu.Unlock()

	go a.pollDaemonEvents(ec)
	go ec.Subscribe(nil)
	return true
}

func (a *App) DaemonProcessRunning() bool {
	return client.IsDaemonProcessRunning()
}

func (a *App) StartDaemon() error {
	a.daemonMu.Lock()
	a.stopping = false
	cur := a.eventConn
	a.daemonMu.Unlock()

	// 有连接不等于连接活着：daemon 可能在 OnDisconnect 触发之前就没了 —— 用户点
	// 「停止守护进程」之后立刻点「启动」正落在这个窗口里。这里真 ping 一次确认，
	// 别拿着一条死管道去发请求，那正是「切换模式失败: The pipe is being closed.」
	if cur != nil {
		if cur.Alive(1500 * time.Millisecond) {
			return nil
		}
		a.daemonMu.Lock()
		if a.eventConn == cur {
			a.eventConn = nil
		}
		a.daemonMu.Unlock()
		cur.Close()
	}

	status, err := client.StartDaemon()
	if err != nil {
		return err
	}
	_ = status

	ec, err := client.NewDaemonClient("gui")
	if err != nil {
		return fmt.Errorf("started but cannot connect: %s", err.Error())
	}

	ec.OnDisconnect = func() { a.onDaemonDisconnected(ec) }

	a.daemonMu.Lock()
	old := a.eventConn
	a.eventConn = ec
	a.daemonMu.Unlock()
	if old != nil {
		old.Close()
	}

	go a.pollDaemonEvents(ec)
	go ec.Subscribe(nil)
	return nil
}

func (a *App) StopDaemon() error {
	a.daemonMu.Lock()
	a.stopping = true

	var ec *client.DaemonClient
	if a.eventConn != nil {
		ec = a.eventConn
		a.eventConn = nil
	}
	a.daemonMu.Unlock()
	// 用户主动停了守护进程：会话跟着消失，归属表一并清掉
	// （否则界面已显示「守护进程未启用」，关窗口还会弹「会停止 3 个会话」）
	a.sessClearAll()

	if ec != nil {
		client.CallOnce(contract.Shutdown, nil, "gui")
		ec.Close()
	}

	go func() {
		// Keep stopping=true briefly to prevent reconnection races,
		// then clear so the GUI can reconnect.
		time.Sleep(3 * time.Second)
		a.daemonMu.Lock()
		a.stopping = false
		a.daemonMu.Unlock()
	}()

	return nil
}

// ---- serial operations ----

type SerialConfig struct {
	Port     string `json:"port"`
	Baud     int    `json:"baud"`
	DataBits int    `json:"dataBits"`
	StopBits string `json:"stopBits"`
	Parity   string `json:"parity"`
}

func (a *App) call(method contract.Method, params map[string]any) (map[string]any, error) {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return nil, fmt.Errorf("not connected")
	}
	return ec.Call(method, params)
}

func (a *App) GetPorts() ([]map[string]any, error) {
	result, err := a.call(contract.Ports, nil)
	if err != nil {
		return nil, err
	}
	ports, _ := result["ports"].([]any)
	out := make([]map[string]any, 0, len(ports))
	for _, p := range ports {
		out = append(out, p.(map[string]any))
	}
	return out, nil
}

func (a *App) RefreshPorts() ([]map[string]any, error) {
	result, err := a.call(contract.PortsRefresh, nil)
	if err != nil {
		return nil, err
	}
	ports, _ := result["ports"].([]any)
	out := make([]map[string]any, 0, len(ports))
	for _, p := range ports {
		out = append(out, p.(map[string]any))
	}
	return out, nil
}

func (a *App) OpenSession(ctx context.Context, cfg SerialConfig) (string, error) {
	result, err := a.call(contract.ProcessCreate, map[string]any{
		"port": cfg.Port, "baud": cfg.Baud,
		"dataBits": cfg.DataBits, "stopBits": cfg.StopBits, "parity": cfg.Parity,
	})
	if err != nil {
		return "", err
	}
	pid, _ := result["processId"].(string)
	// 谁建的会话就归谁：窗口关掉时按这张表把自己的会话销毁
	a.sessClaim(pid, winNameFromCtx(ctx))
	return pid, nil
}

func (a *App) CloseSession(processID string) error {
	_, err := a.call(contract.ProcessDestroy, map[string]any{"processId": processID})
	if err == nil {
		// 会话没了，归属记录也跟着消失，免得下次守护进程复用这个 ID 时误判
		a.sessSet(processID, "")
	}
	return err
}

func (a *App) CreateIdleProcess(ctx context.Context) (string, error) {
	result, err := a.call(contract.ProcessCreate, nil)
	if err != nil {
		return "", err
	}
	pid, _ := result["processId"].(string)
	a.sessClaim(pid, winNameFromCtx(ctx))
	return pid, nil
}

func (a *App) ConnectSession(ctx context.Context, processID string, cfg SerialConfig) error {
	_, err := a.call(contract.ProcessConnect, map[string]any{
		"processId": processID, "port": cfg.Port, "baud": cfg.Baud,
		"dataBits": cfg.DataBits, "stopBits": cfg.StopBits, "parity": cfg.Parity,
	})
	if err == nil {
		// 连一个原本无主的会话（比如 CLI 建的空闲会话）：连上就归本窗口
		a.sessClaim(processID, winNameFromCtx(ctx))
	}
	return err
}

func (a *App) DisconnectSession(processID string) error {
	_, err := a.call(contract.ProcessDisconnect, map[string]any{"processId": processID})
	return err
}

// ForwardCreate creates a port forwarding process between two serial ports.
func (a *App) ForwardCreate(portA string, baudA int, portB string, baudB int) map[string]any {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return nil
	}
	result, _ := ec.ForwardCreate(portA, baudA, portB, baudB)
	return result
}

// SwitchPort switches a connected process to a different serial port.
func (a *App) SwitchPort(sessionId string, port string, cfg map[string]any) {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return
	}
	ec.SwitchPort(sessionId, port, cfg)
}

func (a *App) SendData(processID string, data string, format string) error {
	_, err := a.call(contract.SessionSend, map[string]any{
		"processId": processID, "data": data, "format": format,
	})
	return err
}

func (a *App) GetClients() []map[string]any {
	result, err := a.call(contract.ClientList, nil)
	if err != nil {
		return nil
	}
	clients, _ := result["clients"].([]any)
	out := make([]map[string]any, 0, len(clients))
	for _, c := range clients {
		if m, ok := c.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// GetDisplayDefaults 返回 daemon.ini 的 [display] 段：新标签页的显示设置初始值。
// 守护进程没在跑时返回 nil，由前端用内置默认值兜底（离线也得能建标签页）。
func (a *App) GetDisplayDefaults() map[string]any {
	result, err := a.call(contract.DisplayDefaults, nil)
	if err != nil {
		return nil
	}
	d, _ := result["display"].(map[string]any)
	return d
}

// SetSessionDisplay 把一条会话的显示设置写进它的工程文件。
// 普通串口会话没有工程目录，守护进程那边只更新内存、不落盘，也不算失败。
func (a *App) SetSessionDisplay(processID string, display map[string]any) error {
	_, err := a.call(contract.SessionSetDisplay, map[string]any{
		"processId": processID,
		"display":   display,
	})
	return err
}

// SaveDisplayDefaults 把新的默认值写进 daemon.ini 的 [display] 段。
//
// 只有守护进程能写 daemon.ini（单写者），所以走 IPC。IPC 只认字符串值
// （ini 里一切都是文本），这里统一转一遍；成功返回重新读出来的那份默认值，
// 前端拿它覆盖本地缓存。
func (a *App) SaveDisplayDefaults(display map[string]any) (map[string]any, error) {
	flat := make(map[string]any, len(display))
	for k, v := range display {
		flat[k] = fmt.Sprint(v)
	}
	result, err := a.call(contract.DisplaySetDefaults, map[string]any{"display": flat})
	if err != nil {
		return nil, err
	}
	back, _ := result["display"].(map[string]any)
	return back, nil
}

// GetSessions 返回守护进程上的**全部**会话。
//
// 这是给「不区分窗口」的调用方用的（FirstSessionID 这类探测）以及兜底路径；
// 界面上的标签同步必须用 GetSessionsForWindow —— 否则每个窗口都会看到所有标签页。
func (a *App) GetSessions() []map[string]any {
	return a.fetchSessions()
}

// GetSessionsForWindow 只返回「归调用窗口所有」的会话。
//
// 规则（对应「没有主窗口，每个窗口都是主窗口」这个模型）：
//   - 归属我的 → 给我
//   - 没人认领的 → 由第一个来同步、且**手上一个会话都没有**的窗口认领
//     （CLI / MCP 建的会话就靠这条进入界面；不让所有窗口都认领，否则同一个会话
//     会在每个窗口各长出一个标签页，然后被两个窗口各关一次）
//   - 归别人的 → 看不见
func (a *App) GetSessionsForWindow(ctx context.Context) []map[string]any {
	win := winNameFromCtx(ctx)
	all := a.fetchSessions()
	if win == "" {
		// 没有来源窗口（脚本/内部调用）：退化成全量，保持旧行为
		return all
	}
	if all == nil {
		// 这里什么都读不到：下面只剩「本窗口名下的」会返回，目标窗口可能因此
		// 一个标签都建不出来（见 mergeWindowIntoTab 里「先转归属再建窗口」的说明）。
		log.Printf("[win] GetSessionsForWindow %q: 守护进程没响应", win)
	}
	// all 为 nil 表示没连上守护进程。这时**不能**清归属：一次查询失败就把
	// 所有会话变成「无主」，别的窗口会立刻认领走，标签页就跑错窗口了。
	// 非 nil 的空切片才代表「守护进程上确实一个会话都没有」。
	if all != nil {
		alive := make(map[string]bool, len(all))
		for _, s := range all {
			if id, ok := s["id"].(string); ok && id != "" {
				alive[id] = true
			}
		}
		a.sessPrune(alive)
	}
	out := make([]map[string]any, 0, len(all))
	claimUnowned := len(a.sessOfWindow(win)) == 0
	for _, s := range all {
		sid, _ := s["id"].(string)
		if sid == "" {
			continue
		}
		owner := a.sessWindow(sid)
		switch {
		case owner == win:
			out = append(out, s)
		case owner == "" && claimUnowned:
			if a.sessClaim(sid, win) {
				log.Printf("[window] 会话 %s 无主，认领给窗口 %q", sid, win)
				out = append(out, s)
			}
		}
	}
	// 一行日志：出问题（标签该出现却没出现）时，先看这里拿到的是几个。
	log.Printf("[win] GetSessionsForWindow %q -> %d 个（守护进程上 %d 个）", win, len(out), len(all))
	return out
}

// fetchSessions 向守护进程要一份会话列表。
//
// 返回值语义很重要：连不上时返回 **nil**，连上了但一个会话都没有时返回**非 nil
// 的空切片** —— 上层靠这个区分「不知道」和「确实没有」（见 GetSessionsForWindow
// 里清理归属的逻辑）。
func (a *App) fetchSessions() []map[string]any {
	result, err := a.call(contract.ProcessList, nil)
	if err != nil {
		return nil
	}
	processes, _ := result["processes"].([]any)
	out := make([]map[string]any, 0, len(processes))
	for _, p := range processes {
		pmap := p.(map[string]any)
		status, _ := pmap["status"].(string)
		entry := map[string]any{"id": pmap["processId"], "status": status}
		// 线程名 / 名字来源 / 工程目录 / 显示设置：标签栏直接用这些
		// （见前端 _tabNameOf、_applyDaemonSessionToTab）。
		// 这几项以前漏在这份白名单外面，后果是所有标签都退化成
		// _tabNameOf 的最后一行兜底 "#<id>"，界面上显示成 "#0" 而不是「会话 #1」。
		if v, ok := pmap["name"]; ok {
			entry["name"] = v
		}
		if v, ok := pmap["nameCustom"]; ok {
			entry["nameCustom"] = v
		}
		if v, ok := pmap["sessionPath"]; ok {
			entry["sessionPath"] = v
		}
		if v, ok := pmap["display"]; ok {
			entry["display"] = v
		}
		if viewers, ok := pmap["viewers"]; ok {
			entry["viewers"] = viewers
		}
		if pmode, ok := pmap["mode"]; ok {
			entry["mode"] = pmode
		}
		if autoSend, ok := pmap["autoSend"]; ok {
			entry["autoSend"] = autoSend
		}
		if ringName, ok := pmap["sendRingName"]; ok {
			entry["sendRingName"] = ringName
		}
		if mode, _ := pmap["mode"].(string); mode == "forward" {
			entry["portName"] = pmap["portName"]
			entry["baud"] = pmap["baud"]
			entry["dataBits"] = pmap["dataBits"]
			entry["stopBits"] = pmap["stopBits"]
			entry["parity"] = pmap["parity"]
			entry["forwardPortB"] = pmap["forwardPortB"]
			entry["forwardBaudB"] = pmap["forwardBaudB"]
			entry["forwardDataBitsB"] = pmap["forwardDataBitsB"]
			entry["forwardStopBitsB"] = pmap["forwardStopBitsB"]
			entry["forwardParityB"] = pmap["forwardParityB"]
		} else if status == "connected" {
			entry["portName"] = pmap["portName"]
			entry["baud"] = pmap["baud"]
			entry["dataBits"] = pmap["dataBits"]
			entry["stopBits"] = pmap["stopBits"]
			entry["parity"] = pmap["parity"]
		}
		out = append(out, entry)
	}
	return out
}

func (a *App) WatchSession(processID string) {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec != nil {
		ec.WatchProcess(processID)
	}
}

// UnwatchSession stops watching a process.
func (a *App) UnwatchSession(processID string) {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec != nil {
		ec.UnwatchProcess(processID)
	}
}

// GetWatchedSessions returns process IDs this GUI is watching.
func (a *App) GetWatchedSessions() []string {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec != nil {
		ids, _ := ec.GetWatchedProcesses()
		return ids
	}
	return nil
}

// ExternalI18nInfo holds metadata for an external i18n file.
type ExternalI18nInfo struct {
	Filename string `json:"filename"`
	Lang     string `json:"lang"`
	Name     string `json:"name"`
	Dir      string `json:"dir"`
}

// ListExternalI18n 扫描各资源层的 i18n/ 目录，返回每份语言文件的元数据。
// 同名语言按优先级只取第一份（内置 resources-system > 外置 resources）。
func (a *App) ListExternalI18n() []ExternalI18nInfo {
	var result []ExternalI18nInfo
	for _, lang := range config.I18nLangs() {
		path, ok := config.I18nFile(lang)
		if !ok {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		info := ExternalI18nInfo{
			Filename: filepath.Base(path),
			Lang:     m["_lang"],
			Name:     m["_name"],
			Dir:      m["_dir"],
		}
		if info.Dir == "" {
			info.Dir = "ltr"
		}
		result = append(result, info)
	}
	return result
}

// LoadExternalI18n loads i18n/{lang}.json from the highest-priority resource layer.
// Returns nil if the file does not exist or is invalid.
func (a *App) LoadExternalI18n(lang string) map[string]string {
	path, ok := config.I18nFile(lang)
	if !ok {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

func (a *App) GetI18n(lang string) map[string]string {
	return config.GetI18nMap(lang)
}

func (a *App) GetTheme(id string) map[string]any {
	return config.LoadTheme(id)
}

func (a *App) ListThemes() []config.ThemeInfo {
	return config.ListThemeInfos()
}

func (a *App) ListExternalColorThemes() []config.ExternalThemeInfo {
	return config.ListExternalColorThemes()
}

func (a *App) GetExternalColorTheme(id string) map[string]any {
	return config.LoadExternalColorTheme(id)
}

func (a *App) ListExternalIconThemes() []config.ExternalThemeInfo {
	return config.ListExternalIconThemes()
}

func (a *App) GetExternalIconTheme(id string) map[string]any {
	return config.LoadExternalIconTheme(id)
}

// CreateExampleFiles creates example theme/i18n directories and sample files.
// kind: "colors", "icons", or "i18n".
//
// 一律写到外置资源层 <exe>\resources\（结构见 config/resources.go），不碰内置层
// resources-system；写完后设置页的「刷新」会重新扫描，能立刻看到示例。
func (a *App) CreateExampleFiles(kind string) map[string]any {
	base := config.UserResourcesDir()
	if base == "" {
		return map[string]any{"ok": false, "error": "resource dir not found"}
	}

	switch kind {
	case "colors":
		dir := filepath.Join(base, "themes", "example-sunset")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		sample := filepath.Join(dir, "theme.json")
		content := `{
  "_name": {"zh":"日落暖色","en":"Sunset Warm","_":"Sunset Warm"},
  "_modes": ["dark", "light"],
  "common": {
    "--accent": "#e08840",
    "--green": "#4aac6a",
    "--red": "#d05050",
    "--yellow": "#e0a030"
  },
  "dark": {
    "--fg": "#e8d8c8",
    "--bg": "#1e1814",
    "--bg2": "#26201a",
    "--bg3": "#2e2820",
    "--border": "#3a3028",
    "--input-bg": "#221c16",
    "--hover": "#302820",
    "--placeholder": "#706050",
    "--scrollbar-thumb": "#504030",
    "--fwd-p1": "#5ea0e0",
    "--fwd-p2": "#f0a060",
    "--fwd-p1-hover": "#4070b0",
    "--fwd-p2-hover": "#d08040",
    "--hex-escape": "#908070",
    "--badge-cli": "#f0a060",
    "--status-offline": "#7a7060",
    "--tab-close": "#a08060"
  },
  "light": {
    "--fg": "#3a2a1a",
    "--bg": "#fef8f2",
    "--bg2": "#faf0e4",
    "--bg3": "#f2e8da",
    "--border": "#d8c8b0",
    "--input-bg": "#fef8f2",
    "--hover": "#f0e0cc",
    "--placeholder": "#b0a090",
    "--scrollbar-thumb": "#c8b898",
    "--fwd-p1": "#3070c0",
    "--fwd-p2": "#d07030",
    "--fwd-p1-hover": "#2058a0",
    "--fwd-p2-hover": "#b05820",
    "--hex-escape": "#807060",
    "--badge-cli": "#d07030",
    "--status-offline": "#a09080",
    "--tab-close": "#806050"
  }
}`
		if err := os.WriteFile(sample, []byte(content), 0644); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		return map[string]any{"ok": true, "path": dir}

	case "icons":
		dir := filepath.Join(base, "icons", "example-outline")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		iconsJSON := `{
  "_name": {"zh":"线性图标","en":"Outline","_":"Outline"},
  "_fallback": ""
}`
		if err := os.WriteFile(filepath.Join(dir, "icons.json"), []byte(iconsJSON), 0644); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		svgs := map[string]string{
			"settings": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>`,
			"trash":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/><line x1="10" x2="10" y1="11" y2="17"/><line x1="14" x2="14" y1="11" y2="17"/></svg>`,
		}
		for k, v := range svgs {
			os.WriteFile(filepath.Join(dir, k+".svg"), []byte(v), 0644)
		}
		return map[string]any{"ok": true}
	case "i18n":
		dir := filepath.Join(base, "i18n")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		sample := filepath.Join(dir, "example-pt.json")
		content := `{
  "_lang": "pt",
  "_name": "Português",
  "_dir": "ltr",
  "menu.file": "Ficheiro",
  "menu.settings": "Configurações",
  "menu.tools": "Ferramentas",
  "menu.help": "Ajuda"
}`
		if err := os.WriteFile(sample, []byte(content), 0644); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		return map[string]any{"ok": true}
	default:
		return map[string]any{"ok": false, "error": "unknown kind: " + kind}
	}
}

// ── 守护进程日志（「守护进程 → 查看守护进程日志」标签页） ──

const (
	// daemonLogTailBytes 首次打开日志页时只取末尾这么多。文件轮转上限是 2 MB，
	// 一次全灌给前端既慢又没人看 —— 用户关心的是最近几屏。
	daemonLogTailBytes = 128 << 10
	// daemonLogMaxChunk 是单次增量读取上限，防止一次调用把几 MB 塞进 IPC。
	daemonLogMaxChunk = 1 << 20
)

// daemonLogPathLocal 是「守护进程与 GUI 同目录」这个约定下的推导。
//
// 用户态安装、systemd 服务、手工从别处启动都能打破这个约定，所以它只是**兜底**：
// 真正的路径来自 daemon.info 的 logPath（见 daemonLogPath）。
func daemonLogPathLocal() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "logs", "daemon.log")
}

// daemonLogPathCache 缓存解析出来的日志路径；空串表示还没解析过。
var (
	daemonLogPathMu    sync.Mutex
	daemonLogPathCache string
)

// daemonLogPath 返回守护进程日志文件路径，**优先用守护进程自己报的**。
//
// 守护进程装在别处时本地推导会指到一个不存在的文件，界面只能显示「日志文件
// 还没生成」—— 而它明明在写。daemon.info 从 0.9.0 起带回 logPath，问到了就用；
// 问不到（0.8.x 老守护进程、或它压根没在跑）退回 daemonLogPathLocal。
//
// force 只在「这一轮是从头读」（offset < 0，即刚打开标签或上次没读到）时给 true，
// 免得每 800 ms 的增量轮询都多打一次 daemon.info。
func (a *App) daemonLogPath(force bool) string {
	daemonLogPathMu.Lock()
	cached := daemonLogPathCache
	daemonLogPathMu.Unlock()
	if cached != "" && !force {
		return cached
	}

	best := daemonLogPathLocal()
	fromDaemon := false
	if res, err := a.call(contract.DaemonInfo, nil); err == nil {
		if s, _ := res["logPath"].(string); s != "" {
			best, fromDaemon = s, true
		}
	}
	if best == "" {
		return ""
	}
	daemonLogPathMu.Lock()
	changed := daemonLogPathCache != best
	daemonLogPathCache = best
	daemonLogPathMu.Unlock()
	if changed {
		log.Printf("[daemon] 日志文件: %s（来源：%s）", best, map[bool]string{true: "守护进程自报", false: "本地推导"}[fromDaemon])
	}
	return best
}

// ReadDaemonLog 读守护进程日志的增量，供日志标签页轮询。
//
// offset < 0 表示首次打开：只取末尾 maxBytes 字节，并丢掉被切开的半行。
// 返回的 next 是下一次该从哪里读，**一定落在行边界上** —— 半行和半个 UTF-8
// 字符都留到下一次，前端拿到的永远是完整的行。
//
// 只读文件、不碰守护进程：日志本来就是 daemon 自己写的，而「GUI 启动之前发生
// 了什么」也只有文件里有（实时事件流做不到这件事）。
func (a *App) ReadDaemonLog(offset int64, maxBytes int) (map[string]any, error) {
	// offset < 0 = 这一轮从头读（刚打开标签，或上次没读到文件）：顺手重新问一次
	// 守护进程日志路径，别的轮询走缓存，不多打 daemon.info。
	path := a.daemonLogPath(offset < 0)
	if path == "" {
		return nil, fmt.Errorf("取不到程序所在目录")
	}
	out := map[string]any{"path": path, "size": int64(0), "next": int64(0), "text": ""}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 记一下：下一轮（offset 仍 < 0）会重新问守护进程，换了实例也能跟上
			daemonLogPathMu.Lock()
			daemonLogPathCache = ""
			daemonLogPathMu.Unlock()
			// 还没写过日志（守护进程没启动过）。不是错误：前端继续轮询，它自己会出来。
			out["missing"] = true
			return out, nil
		}
		return nil, err
	}
	size := info.Size()
	out["size"] = size
	if maxBytes <= 0 {
		maxBytes = daemonLogTailBytes
	}
	if maxBytes > daemonLogMaxChunk {
		maxBytes = daemonLogMaxChunk
	}

	fromTail := offset < 0
	if fromTail {
		offset = size - int64(maxBytes)
		if offset < 0 {
			offset = 0
		}
	} else if offset > size {
		// 文件变小 = 守护进程启动时轮转过（daemon.log → daemon.log.1），从头读
		offset = 0
		out["rotated"] = true
	}
	n := size - offset
	if n <= 0 {
		out["next"] = offset
		return out, nil
	}
	if n > int64(maxBytes) {
		n = int64(maxBytes)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	if nn, rerr := f.ReadAt(buf, offset); nn > 0 {
		buf = buf[:nn]
	} else if rerr != nil {
		return nil, rerr
	}

	start := offset
	if fromTail && start > 0 {
		// 从尾部读的起点大概率落在行中间：丢掉第一段残行
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
			start += int64(i) + 1
		}
	}
	if start+int64(len(buf)) < size {
		// 后面还有数据：只交完整行，剩下的下次再读
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			buf = buf[:i+1]
		}
	}
	out["text"] = string(buf)
	out["next"] = start + int64(len(buf))
	return out, nil
}

func (a *App) ShowFolderInExplorer(kind string) map[string]any {
	// 设置目录与资源目录无关（在 exe 同级），单独处理，不去要 resources 路径。
	if kind == "settings" {
		dir, err := settingsDir()
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		_ = openFolder(dir)
		return map[string]any{"ok": true}
	}
	// 日志目录同样在 exe 同级（见 daemon/logfile.go），不走资源层
	if kind == "logs" {
		dir := filepath.Dir(a.daemonLogPath(true))
		if err := os.MkdirAll(dir, 0755); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		_ = openFolder(dir)
		return map[string]any{"ok": true}
	}

	base := config.UserResourcesDir()
	if base == "" {
		return map[string]any{"ok": false, "error": "resource dir not found"}
	}
	var dir string
	switch kind {
	case "colors":
		dir = filepath.Join(base, "themes")
	case "icons":
		dir = filepath.Join(base, "icons")
	case "i18n":
		dir = filepath.Join(base, "i18n")
	default:
		return map[string]any{"ok": false, "error": "unknown kind: " + kind}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	_ = openFolder(dir)
	return map[string]any{"ok": true}
}

func (a *App) GetBuiltinIcons() map[string]string {
	return config.GetBuiltinIcons()
}

func (a *App) GetVersion() string {
	return version.Version
}

// GetAppDir 返回 GUI 的 exe 所在目录（关于页「程序目录」显示用）。
func (a *App) GetAppDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// GetRenderEngine 返回当前平台真正用的 WebView 实现，关于页「渲染」一行显示它。
//
// 这行以前在前端写死成 'WebView2'，Linux 版上就是句假话 —— Wails v3 的 Linux
// 后端是 WebKitGTK。纯字符串判断，用 runtime.GOOS 就够，不必再拆平台文件。
func (a *App) GetRenderEngine() string {
	switch runtime.GOOS {
	case "windows":
		return "WebView2"
	case "linux":
		return "WebKitGTK"
	case "darwin":
		return "WKWebView"
	}
	return runtime.GOOS
}

// 关于页的两个外链。写成常量、由后端自己打开 —— 前端一旦能把任意字符串
// 塞进 openURL，就等于把 shell 交出去了。
const (
	aboutRepoURL   = "https://github.com/nienieai/serial-debugger"
	aboutIssuesURL = "https://github.com/nienieai/serial-debugger/issues"
)

// GetBuildInfo 返回构建信息（构建时间 / Git 提交 / Go 版本），关于页「构建」一行用。
//
// 取自 runtime/debug 的 VCS 信息（Go 1.18+ 在 git 仓库里 go build 会自动打进去），
// 不靠构建脚本传 ldflags —— 那种做法一旦脚本漏改，关于页显示的就是假的。
// 拿不到 VCS 信息时（不在仓库里构建）只回 Go 版本，前端按空值处理。
func (a *App) GetBuildInfo() map[string]string {
	out := map[string]string{"go": runtime.Version()}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return out
	}
	if bi.GoVersion != "" {
		out["go"] = bi.GoVersion
	}
	for _, d := range bi.Deps {
		switch d.Path {
		case "github.com/wailsapp/wails/v3":
			out["wails"] = d.Version
		case "go.bug.st/serial":
			out["serial"] = d.Version
		}
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev := s.Value
			if len(rev) > 7 {
				rev = rev[:7]
			}
			out["commit"] = rev
		case "vcs.time":
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				out["time"] = t.Local().Format("2006-01-02 15:04")
			} else {
				out["time"] = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				out["dirty"] = "1"
			}
		}
	}
	return out
}

// OpenProjectPage 打开项目主页。
func (a *App) OpenProjectPage() map[string]any {
	if err := openURL(aboutRepoURL); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// OpenIssuesPage 打开问题反馈页。
func (a *App) OpenIssuesPage() map[string]any {
	if err := openURL(aboutIssuesURL); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// GetProtocolVersion 返回本客户端使用的 IPC 协议版本（MAJOR.MINOR.PATCH）。
// 关于页要显示它，别再在 JS 里硬编码 —— 硬编码的那份必然和守护进程报的漂移。
func (a *App) GetProtocolVersion() string {
	return protocol.ProtocolVersion
}

func (a *App) GetThreads() map[string]any {
	result, _ := a.call(contract.Threads, nil)
	return result
}

// GetSessionHistory 读取一个进程的历史记录。
//
// limit <= 0 时回整环（老行为）；limit > 0 时只回一页：`beforeTsMs` 是调用方手上
// 最旧一条的毫秒时间戳，`sameTsSkip` 是它属于这一毫秒的条数。界面按页回补更早
// 的历史，避免每次都把 5MB 整环序列化过桥。
func (a *App) GetSessionHistory(processID string, limit int, beforeTsMs int64, sameTsSkip int) map[string]any {
	params := map[string]any{"processId": processID}
	if limit > 0 {
		params["limit"] = limit
	}
	if beforeTsMs > 0 {
		params["beforeTsMs"] = beforeTsMs
	}
	if sameTsSkip > 0 {
		params["sameTsSkip"] = sameTsSkip
	}
	result, _ := a.call(contract.SessionHistory, params)
	if result != nil {
		// Decode each history entry's hex into segments for the frontend.
		a.tabDecodersMu.Lock()
		td := a.tabDecoders[processID]
		a.tabDecodersMu.Unlock()
		enc := "utf-8"
		if td != nil {
			td.encMu.RLock()
			enc = td.encoding
			td.encMu.RUnlock()
		}
		if historyRaw, ok := result["history"]; ok {
			if history, ok := historyRaw.([]any); ok {
				for i, entryRaw := range history {
					entry, ok := entryRaw.(map[string]any)
					if ok {
						if hexStr, _ := entry["hex"].(string); hexStr != "" {
							// 大包不解码（见 maxSegmentHexLen）：否则 1 万条历史能把
							// 前端堆撑到几百 MB
							if segs := decodeSegments(hexStr, enc); segs != nil {
								entry["segments"] = segs
							}
						}
					}
					history[i] = entry
				}
				result["history"] = history
			}
		}
	}
	return result
}

func (a *App) GetSessionStats(processID string) map[string]any {
	result, _ := a.call(contract.SessionStats, map[string]any{"processId": processID})
	return result
}

func (a *App) GetGoroutines() map[string]any {
	result, _ := a.call(contract.Goroutines, nil)
	return result
}

// ---- shared memory send ----

// SendDataShm writes data to shared memory and triggers a single send.
func (a *App) SendDataShm(processID string, data string, format string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.SendViaShm(processID, data, format)
}

// ClearHistory clears the history ring buffer for a process.
func (a *App) ClearHistory(processID string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	_, err := ec.Call(contract.SessionClearHistory, map[string]any{"processId": processID})
	return err
}

// SetProcessMode switches the process mode between "single" and "forward".
func (a *App) SetProcessMode(processID string, mode string) error {
	_, err := a.call(contract.ProcessSetMode, map[string]any{"processId": processID, "mode": mode})
	return err
}

// ForwardConnect connects an idle forward-mode process to two serial ports.
func (a *App) ForwardConnect(processID string, cfgA, cfgB SerialConfig) error {
	_, err := a.call(contract.ProcessConnect, map[string]any{
		"processId": processID,
		"port":      cfgA.Port,
		"baud":      cfgA.Baud,
		"dataBits":  cfgA.DataBits,
		"stopBits":  cfgA.StopBits,
		"parity":    cfgA.Parity,
		"portB":     cfgB.Port,
		"baudB":     cfgB.Baud,
		"dataBitsB": cfgB.DataBits,
		"stopBitsB": cfgB.StopBits,
		"parityB":   cfgB.Parity,
	})
	return err
}

// WriteSendRing writes data to the send ring buffer without triggering.
// Used to update auto-send data on the fly.
func (a *App) WriteSendRing(processID string, data string, format string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	ringName, err := ec.SendRingName(processID)
	if err != nil {
		return err
	}
	var raw []byte
	if format == "hex" {
		raw, err = hex.DecodeString(data)
		if err != nil {
			return fmt.Errorf("invalid hex: %w", err)
		}
	} else {
		raw = []byte(data)
	}
	return client.SendWrite(ringName, raw)
}

// StartAutoSend writes data to shared memory and starts auto-send.
func (a *App) StartAutoSend(processID string, data string, format string, intervalMs int, mode string, delimiter string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}

	var raw []byte
	if format == "hex" {
		var err error
		raw, err = hex.DecodeString(data)
		if err != nil {
			return fmt.Errorf("invalid hex: %w", err)
		}
	} else {
		raw = []byte(data)
	}

	var entries [][]byte
	if mode == "queue" {
		if delimiter == "" {
			delimiter = "\n"
		}
		parts := bytes.Split(raw, []byte(delimiter))
		for _, part := range parts {
			if len(part) > 0 {
				entries = append(entries, part)
			}
		}
	} else {
		entries = [][]byte{raw}
	}

	if len(entries) == 0 {
		return fmt.Errorf("no data to send")
	}

	return ec.AutoSendStartWithData(processID, intervalMs, mode, entries)
}

// StopAutoSend stops auto-send on a process.
func (a *App) StopAutoSend(processID string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.AutoSendStop(processID)
}

// SetAutoSendInterval updates interval of a running auto-send.
func (a *App) SetAutoSendInterval(processID string, intervalMs int) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.AutoSendSetInterval(processID, intervalMs)
}

// GetAutoSendStatus returns auto-send status.
func (a *App) GetAutoSendStatus(processID string) map[string]any {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return nil
	}
	status, err := ec.AutoSendStatus(processID)
	if err != nil {
		return nil
	}
	return status
}

// ---- multi-string send ----

// WriteMultistrEntries serializes entries as JSON, writes to sendq via IPC.
func (a *App) WriteMultistrEntries(processID string, entriesJSON string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(entriesJSON), &entries); err != nil {
		return fmt.Errorf("invalid entries JSON: %w", err)
	}
	return ec.MultistrWrite(processID, entries)
}

// ReadMultistrEntries reads entries from sendq, returns as JSON string.
func (a *App) ReadMultistrEntries(processID string) string {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return "[]"
	}
	entries, err := ec.MultistrRead(processID)
	if err != nil {
		return "[]"
	}
	data, _ := json.Marshal(entries)
	return string(data)
}

// MultistrSave persists sendq entries to disk.
func (a *App) MultistrSave(processID string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.MultistrSave(processID)
}

// MultistrLoad loads entries from disk into sendq, returns as JSON.
func (a *App) MultistrLoad(processID string) string {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return "[]"
	}
	entries, err := ec.MultistrLoad(processID)
	if err != nil {
		return "[]"
	}
	data, _ := json.Marshal(entries)
	return string(data)
}

// StartQueueSend triggers daemon queue mode send.
func (a *App) StartQueueSend(processID string, loop bool, roundIntervalMs int) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	params := map[string]any{
		"processId":  processID,
		"mode":       "queue",
		"intervalMs": roundIntervalMs,
		"loop":       loop,
	}
	_, err := ec.Call(contract.AutosendStart, params)
	return err
}

// TriggerSend triggers a single send from sendq.
func (a *App) TriggerSend(processID string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.SendTrigger(processID, false)
}

// ---- devtools ----

// OpenDevTools 打开当前窗口的 WebView2 开发者工具。
//
// 需要 dev 构建（build.ps1 默认带 `devtools` tag）；production 构建里
// Wails 把 DevTools 关掉了（openDevTools 是空实现、AreDevToolsEnabled(false)），
// 调了也不会有反应。
func (a *App) OpenDevTools(ctx context.Context) {
	if a.wailsApp == nil {
		return
	}
	w := a.windowByName(winNameFromCtx(ctx))
	if w == nil {
		w = a.window
	}
	if w != nil {
		w.OpenDevTools()
		return
	}
	// 兜底：没有窗口对象时模拟 Ctrl+Shift+F12（老路子）
	openDevToolsWindow()
}

// ---- history files ----

// ListHistoryFiles returns all persistent history files from disk.
func (a *App) ListHistoryFiles() []map[string]any {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return nil
	}
	files, _ := ec.ListHistoryFiles()
	return files
}

// SearchHistory searches a history file for entries matching the keyword.
func (a *App) SearchHistory(file, keyword string, limit int, offset int64) map[string]any {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return nil
	}
	result, _ := ec.SearchHistory(file, keyword, limit, offset)
	return result
}

// DropProbe 是拖放预检的结果。Kind 取 "session" / "file" / "unknown"。
type DropProbe struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Err  string `json:"err,omitempty"`
}

// ProbeDroppedPaths 判断拖进来的路径能不能当会话打开，**不解析内容**。
//
// 前端拿不到 stat，分不清"目录"和"没有扩展名的文件"，所以判定放这里：
// 工程目录（有 manifest.json 或 pkg-*.stlog）和 .stlog 文件都算能开；
// 其余返回 unknown 带原因，由前端汇总成一个警告窗。
func (a *App) ProbeDroppedPaths(paths []string) []DropProbe {
	out := make([]DropProbe, 0, len(paths))
	for _, p := range paths {
		out = append(out, probeDropPath(p))
	}
	return out
}

func probeDropPath(p string) DropProbe {
	fi, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return DropProbe{Path: p, Kind: "unknown", Err: "路径不存在"}
		}
		return DropProbe{Path: p, Kind: "unknown", Err: err.Error()}
	}
	if fi.IsDir() {
		if !sessionfile.IsSessionDir(p) {
			return DropProbe{Path: p, Kind: "unknown", Err: "目录里没有 manifest.json，也没有 pkg-*.stlog"}
		}
		return DropProbe{Path: p, Kind: "session"}
	}
	if !strings.HasSuffix(strings.ToLower(fi.Name()), ".stlog") {
		return DropProbe{Path: p, Kind: "unknown", Err: "不是 .stlog 文件，也不是会话目录"}
	}
	if err := sessionfile.CheckHeader(p); err != nil {
		return DropProbe{Path: p, Kind: "unknown", Err: err.Error()}
	}
	return DropProbe{Path: p, Kind: "file"}
}

// OpenStlogSession 把一个**工程目录**交给守护进程，由它开一个文件会话进程。
//
// 单个 .stlog 不行：包文件没有设置文档、没有索引，也不会再追加，守护进程那边
// 直接拒绝（daemon/serial.go 的 OpenSession）。返回的 processId 之后走常规会话
// 接口读数据。
func (a *App) OpenStlogSession(ctx context.Context, path string) (map[string]any, error) {
	if path == "" {
		return nil, fmt.Errorf("路径为空")
	}
	result, err := a.call(contract.SessionOpen, map[string]any{"path": path})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("守护进程没有返回会话信息")
	}
	// 谁建的会话就归谁 —— 和 OpenSession / CreateIdleProcess / ConnectSession 一样。
	//
	// 少了这一句，推送工程只在**空窗口**里能用：会话入库时是无主的，而
	// GetSessionsForWindow 只在「本窗口名下一条都没有」时才认领无主会话
	// （app.go 的 claimUnowned）。窗口里但凡开着串口或挂着空闲标签，新建出来的
	// 工程会话就一直是「无主」，本窗口的标签同步永远看不到它，前端轮询 5 秒后报
	// 「守护进程没有把这个工程挂上来（进程可能创建失败）」—— 而守护进程其实建好了。
	if pid, _ := result["processId"].(string); pid != "" {
		a.sessClaim(pid, winNameFromCtx(ctx))
	}
	return result, nil
}

// OpenStlogFile 读取一个 .stlog 会话文件（v1.4 格式），返回给前端展示的摘要。
//
// 离线只读：不经过守护进程，也不修改文件。目前只做到"拖进来能看见内容"这一层，
// 分页/搜索/导出属于后续接入，见 docs/format/stlog-format.md 的 P1 阶段。
func (a *App) OpenStlogFile(path string) (map[string]any, error) {
	info, err := sessionfile.ReadSession(path)
	if err != nil {
		return nil, err
	}

	preview := make([]map[string]any, 0, 20)
	for i, f := range info.Frames {
		if i >= 20 {
			break
		}
		head := f.Payload
		if len(head) > 8 {
			head = head[:8]
		}
		preview = append(preview, map[string]any{
			"ts":      f.Ts,
			"dir":     f.Dir,
			"len":     len(f.Payload),
			"partial": f.Partial,
			"head":    strings.ToUpper(hex.EncodeToString(head)),
		})
	}

	out := map[string]any{
		"path":        info.Path,
		"size":        info.Size,
		"pages":       len(info.Pages),
		"records":     info.Records,
		"frames":      len(info.Frames),
		"rxTotal":     info.RxTotal,
		"txTotal":     info.TxTotal,
		"truncated":   info.Truncated,
		"brokenChain": info.BrokenChain,
		"recovered":   info.Recovered,
		"stoppedAt":   info.StoppedAt,
		"preview":     preview,
	}
	if info.Settings != nil {
		out["settings"] = info.Settings
	}
	if info.StopErr != nil {
		out["stopErr"] = info.StopErr.Error()
	}
	return out, nil
}

// maxStlogOrigins 是「来源汇总」的条数上限。一份录制正常只有几个客户端，
// 真出现几百个也不该把响应撑大。
const maxStlogOrigins = 32

// ReadStlogFrames 分页读 .stlog 的帧，给 GUI 的文件标签页渲染。
//
// 每次调用都重读整个文件：v1.4 明确不建 sidecar 索引（§2.12），单文件通常几 MB，
// 重读几十毫秒可以接受；真要读几百 MB 的录制，再上增量索引。
//
// limit 上限 1000 帧——一帧最大 4016 B，转成十六进制是 8 KB 字符串，一次给太多
// 会让过桥的 JSON 和 DOM 都爆掉。标签页首屏 500 帧，其余靠"加载更多"。
func (a *App) ReadStlogFrames(path string, offset, limit int) (map[string]any, error) {
	info, err := sessionfile.ReadSession(path)
	if err != nil {
		return nil, err
	}
	total := len(info.Frames)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	end := offset + limit
	if end > total {
		end = total
	}

	frames := make([]map[string]any, 0, end-offset)
	for i := offset; i < end; i++ {
		f := info.Frames[i]
		frames = append(frames, map[string]any{
			"index":   i,
			"ts":      f.Ts,
			"dir":     f.Dir,
			"len":     len(f.Payload),
			"hex":     strings.ToUpper(hex.EncodeToString(f.Payload)),
			"partial": f.Partial,
			"origin":  f.OriginStr,
		})
	}

	out := map[string]any{
		"path":        path,
		"size":        info.Size,
		"pages":       len(info.Pages),
		"records":     info.Records,
		"total":       total,
		"offset":      offset,
		"frames":      frames,
		"rxTotal":     info.RxTotal,
		"txTotal":     info.TxTotal,
		"bytesAB":     info.P1Total,
		"bytesBA":     info.P2Total,
		"truncated":   info.Truncated,
		"brokenChain": info.BrokenChain,
		"recovered":   info.Recovered,
		"stoppedAt":   info.StoppedAt,
	}
	// 首末帧时间：顶栏要显示「2023-11-15 06:13:20.000 ~ 06:13:20.019」。
	// 文件里存的是绝对毫秒时间戳，年份本来就在数据里，只是逐行显示时刻意省了。
	if total > 0 {
		out["firstTs"] = info.Frames[0].Ts
		out["lastTs"] = info.Frames[total-1].Ts
	}
	// 来源汇总：整份文件里出现过的 origin 字符串，按首次出现的顺序去重。
	// 顶栏拿它显示「来源 GUI · CLI」；没有来源的文件显示「无来源」——
	// 空着不说话，最容易让人以为来源功能没做（样本文件就是这样）。
	seen := make(map[string]bool, 8)
	origins := make([]string, 0, 8)
	for i := range info.Frames {
		s := info.Frames[i].OriginStr
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		origins = append(origins, s)
		if len(origins) >= maxStlogOrigins {
			break
		}
	}
	out["origins"] = origins
	// 录制模式：老文件没有 mode 字段，读回来是空串，一律按单端口处理。
	out["mode"] = sessionfile.ModeSingle
	out["portB"] = ""
	out["forward"] = false
	if info.Settings != nil {
		out["settings"] = info.Settings
		if info.Settings.Mode != "" {
			out["mode"] = info.Settings.Mode
		}
		out["portB"] = info.Settings.PortB
		out["forward"] = info.Settings.IsForward()
	}
	if info.StopErr != nil {
		out["stopErr"] = info.StopErr.Error()
	}
	return out, nil
}

// PickStlogFile 弹系统打开对话框选一个 .stlog，返回绝对路径。
//
// 用户取消时返回空串（不是错误）——前端据此静默作罢。单开一个方法而不是并到
// ReadStlogFrames 里：对话框是用户主动触发的动作，"新建会话 → 从文件打开"
// 和拖入共用同一条打开路径。
func (a *App) PickStlogFile() (string, error) {
	return a.wailsApp.Dialog.OpenFile().
		SetTitle("打开 .stlog 会话文件").
		AddFilter("会话文件 (*.stlog)", "*.stlog").
		AddFilter("所有文件 (*.*)", "*.*").
		PromptForSingleSelection()
}

// PickStlogDir 弹系统目录对话框选一个工程目录，返回绝对路径。
//
// 工程目录是 v2 的会话单位（manifest.json + pkg-*.stlog）；单个 .stlog 是包文件，
// 走 PickStlogFile。两个对话框分开是因为 Windows 的文件对话框选不了目录。
// 用户取消时返回空串（不是错误）。
func (a *App) PickStlogDir() (string, error) {
	return a.wailsApp.Dialog.OpenFile().
		SetTitle("打开工程目录").
		CanChooseFiles(false).
		CanChooseDirectories(true).
		PromptForSingleSelection()
}

// ExportStlogText 弹保存对话框，把 .stlog 导成"一行一帧"的纯文本。
//
// 格式由 sessionfile.WriteText 决定（CRLF、十六进制每 32 B 换行、系统帧按文本），
// 目标文件双击就能用记事本打开。用户取消时返回 {"canceled": true}，不算错误。
func (a *App) ExportStlogText(path string) (map[string]any, error) {
	info, err := sessionfile.ReadSession(path)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	// 注意：v3 的 SaveFileDialogStruct 没有 SetTitle（OpenFile 有），
	// 标题只能通过 SaveFileWithOptions 传 —— API 不对称，别照着 OpenFile 写。
	out, err := a.wailsApp.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:    "导出 .stlog 文本",
		Filename: base + ".txt",
		Filters: []application.FileFilter{
			{DisplayName: "文本文件 (*.txt)", Pattern: "*.txt"},
		},
	}).PromptForSingleSelection()
	if err != nil {
		return nil, err
	}
	if out == "" {
		return map[string]any{"canceled": true}, nil
	}
	f, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	n, werr := sessionfile.WriteText(f, info)
	cerr := f.Close()
	if werr != nil {
		return nil, werr
	}
	if cerr != nil {
		return nil, cerr
	}
	return map[string]any{"path": out, "bytes": n, "frames": len(info.Frames)}, nil
}

// SetHistoryEnabled toggles auto-save for history files.
func (a *App) SetHistoryEnabled(enabled bool) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.SetHistoryEnabled(enabled)
}

// GetHistoryStatus returns whether auto-save is enabled.
func (a *App) GetHistoryStatus() bool {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return false
	}
	v, _ := ec.GetHistoryStatus()
	return v
}

// SetHistoryEnabledFor 设置单个进程（标签）是否把历史写入磁盘。
//
// 标签级开关：同一窗口里可以有的标签落盘、有的不落盘。开启会立即建文件，
// 关闭会关掉当前文件（已写入内容保留）。
func (a *App) SetHistoryEnabledFor(processID string, enabled bool) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.SetHistoryEnabledFor(processID, enabled)
}

// GetHistoryStatusFor 返回单个进程实际生效的落盘开关。
func (a *App) GetHistoryStatusFor(processID string) bool {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return false
	}
	v, _ := ec.GetHistoryStatusFor(processID)
	return v
}

// RenameSession 改线程名（空串 = 退回自动名：端口名 / 会话号）。
//
// 名字归守护进程所有：GUI 不本地存一份，改完等 process-changed 回来重画标签，
// 这样 CLI / MCP 改的名字在同一块界面上立刻可见，不会出现两份真相。
func (a *App) RenameSession(processID, name string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.RenameSession(processID, name)
}

// RenameStlogFile 重命名一个本地会话文件（.stlog 目录或单文件），返回新路径。
//
// 只处理「本地只读标签」：已经被守护进程接管的工程不能改名 —— 那边还开着文件
// 句柄、会话路径记在内存里，改完两边就对不上了。
func (a *App) RenameStlogFile(path, newName string) (string, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return "", fmt.Errorf("名称不能为空")
	}
	if newName == "." || newName == ".." || strings.ContainsAny(newName, `\/:*?"<>|`) {
		return "", fmt.Errorf(`名称不能包含 \ / : * ? " < > |，也不能是 . 或 ..`)
	}
	old, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(old); err != nil {
		return "", fmt.Errorf("找不到文件: %w", err)
	}
	// 换掉最后一段，父目录不变
	newPath := filepath.Join(filepath.Dir(old), newName)
	if newPath == old {
		return old, nil
	}
	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("同名文件已存在: %s", filepath.Base(newPath))
	}
	if err := os.Rename(old, newPath); err != nil {
		return "", fmt.Errorf("重命名失败（文件可能正被占用）: %w", err)
	}
	return newPath, nil
}

// AttachHistoryFile opens a history file for a process and loads its content.
func (a *App) AttachHistoryFile(processID, filename string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.AttachHistoryFile(processID, filename)
}

// NewHistoryFile creates a new history file for a process.
func (a *App) NewHistoryFile(processID string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.NewHistoryFile(processID)
}

// DetachHistoryFile closes the history file attached to a process.
func (a *App) DetachHistoryFile(processID string) error {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return fmt.Errorf("not connected")
	}
	return ec.DetachHistoryFile(processID)
}

// ---- tab decoder management ----

// StartTabDecoder creates a decode worker goroutine for a tab.
func (a *App) StartTabDecoder(processID string) {
	a.tabDecodersMu.Lock()
	defer a.tabDecodersMu.Unlock()
	if a.tabDecoders == nil {
		a.tabDecoders = make(map[string]*tabDecodeUnit)
	}
	if _, ok := a.tabDecoders[processID]; ok {
		return // already started
	}
	td := &tabDecodeUnit{
		a:         a,
		processID: processID,
		encoding:  "utf-8",
		jobs:      make(chan rxTxEnvelope, 32),
		quit:      make(chan struct{}),
	}
	a.tabDecoders[processID] = td
	go td.run()
}

// StopTabDecoder stops and removes the decode worker for a tab.
func (a *App) StopTabDecoder(processID string) {
	a.tabDecodersMu.Lock()
	td := a.tabDecoders[processID]
	if td != nil {
		delete(a.tabDecoders, processID)
	}
	a.tabDecodersMu.Unlock()
	if td != nil {
		close(td.quit)
	}
}

// SetTabEncoding updates the encoding for a tab's decode worker.
func (a *App) SetTabEncoding(processID, encoding string) {
	a.tabDecodersMu.Lock()
	td := a.tabDecoders[processID]
	a.tabDecodersMu.Unlock()
	if td != nil {
		td.setEncoding(encoding)
	}
}

// BatchDecodeForTab re-decodes cached hex entries for a tab after encoding change.
func (a *App) BatchDecodeForTab(hexStrings []string, encoding string) [][]decode.Segment {
	return decode.BatchDecode(hexStrings, encoding)
}

// StopAllTabDecoders stops all tab decode workers.
func (a *App) StopAllTabDecoders() {
	a.tabDecodersMu.Lock()
	tabs := a.tabDecoders
	a.tabDecoders = nil
	a.tabDecodersMu.Unlock()
	for _, td := range tabs {
		close(td.quit)
	}
}

// ---- encoding ----

// EncodeTextForSend converts a text string to a hex-encoded byte sequence
// according to the given encoding ("utf-8", "ascii", "gb2312"). For ASCII
// mode, any character outside 0x00-0x7F triggers a silent fallback to UTF-8.
func (a *App) EncodeTextForSend(text string, encoding string) string {
	var raw []byte
	switch encoding {
	case "ascii":
		if isASCII(text) {
			raw = []byte(text)
		} else {
			raw = []byte(text) // fallback to UTF-8
		}
	case "gb2312":
		enc := simplifiedchinese.GBK.NewEncoder()
		s, _, err := transform.String(enc, text)
		if err != nil {
			raw = []byte(text) // fallback to UTF-8
		} else {
			raw = []byte(s)
		}
	default: // utf-8
		raw = []byte(text)
	}
	return strings.ToUpper(hex.EncodeToString(raw))
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 0x7F {
			return false
		}
	}
	return true
}

// ---- event polling ----

func (a *App) pollDaemonEvents(ec *client.DaemonClient) {
	events := ec.ReadEvent()
	for {
		select {
		case msg, ok := <-events:
			if !ok {
				return
			}
			// 守护进程正常关闭：它上面的会话已经全部销毁，归属表立刻清掉。
			// 只等心跳那条路（最长 ~6s）的话，这期间关窗口会按旧会话数弹确认框。
			if msg.Event == "daemon-shutdown" {
				a.sessClearAll()
			}
			// Route directional data events to per-tab decode workers.
			switch msg.Event {
			case "rx", "tx", "1", "2":
				procID := ""
				if msg.Params != nil {
					procID, _ = msg.Params["processId"].(string)
					a.tabDecodersMu.Lock()
					td := a.tabDecoders[procID]
					a.tabDecodersMu.Unlock()
					if td != nil {
						td.jobs <- rxTxEnvelope{event: msg.Event, params: msg.Params}
						continue
					}
				}
				// No worker for this tab — 按会话归属定向发，别广播：
				// 广播会把某个窗口的串口数据送到所有窗口的日志区里。
				if procID != "" {
					a.emitSession(procID, msg.Event, msg.Params)
				} else {
					a.emit(msg.Event, msg.Params)
				}
			default:
				a.emit(msg.Event, msg.Params)
			}
		case <-a.ctx.Done():
			return
		}
	}
}
