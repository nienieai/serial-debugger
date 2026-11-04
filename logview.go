package main

import (
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/nienieai/serial-debugger/sessionfile"
	"github.com/nienieai/serial-debugger/stlogview"
)

// LogView 是「虚窗文档」的服务壳。
//
// 为什么要这一层：stlogview.Store 是「一份文档」，而一个 GUI 进程里会同时开着
// 多个 .stlog 标签页；Wails 的服务是全局单例，一个服务实例只能是一个文档。
// 所以这里按 id 分文档：Open 返回 id，其余方法都带着 id 调。
//
// 服务名是 main.LogView（前端走 Call.ByName('main.LogView.<Method>')，不依赖
// 生成的 bindings）。
type LogView struct {
	app *App

	mu        sync.Mutex
	docs      map[string]*stlogview.Store
	bySession map[string]string // 会话 ID → 实时文档 id（数据帧按这个找文档）
	seq       int
}

func newLogView(a *App) *LogView {
	return &LogView{
		app:       a,
		docs:      make(map[string]*stlogview.Store),
		bySession: make(map[string]string),
	}
}

// OpenDoc 是 Open 的返回：文档 id + 首帧状态。
type OpenDoc struct {
	ID     string                 `json:"id"`
	Status stlogview.StatusResult `json:"status"`
}

// Open 打开一个 .stlog（或工程目录里的包文件），立即返回文档 id。
func (v *LogView) Open(path string) (OpenDoc, error) {
	if path == "" {
		return OpenDoc{}, fmt.Errorf("路径为空")
	}
	s := stlogview.NewStore()
	// 剪贴板走主程序的：WebView2 里 navigator.clipboard 要安全上下文和用户手势，
	// 脚本化验证时不可靠。
	s.SetClipboard(func(text string) {
		if v.app == nil || v.app.wailsApp == nil || v.app.wailsApp.Clipboard == nil {
			return
		}
		v.app.wailsApp.Clipboard.SetText(text)
	})
	st := s.Open(path)

	v.mu.Lock()
	v.seq++
	id := fmt.Sprintf("doc-%d", v.seq)
	v.docs[id] = s
	v.mu.Unlock()
	return OpenDoc{ID: id, Status: st}, nil
}

func (v *LogView) get(id string) (*stlogview.Store, error) {
	v.mu.Lock()
	s := v.docs[id]
	v.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("文档 %q 不存在", id)
	}
	return s, nil
}

// OpenLive 开一个实时文档（串口监视器 / 转发监视器）。
//
// 同一个会话重复开（比如标签页挪了窗口）会复用同一个文档 —— 数据帧是按会话喂进来
// 的，重建文档等于把已经在文档里的历史丢掉。
func (v *LogView) OpenLive(sessionID string) (OpenDoc, error) {
	if sessionID == "" {
		return OpenDoc{}, fmt.Errorf("会话 ID 为空")
	}
	v.mu.Lock()
	if id, ok := v.bySession[sessionID]; ok {
		if s := v.docs[id]; s != nil {
			st := s.Status()
			v.mu.Unlock()
			return OpenDoc{ID: id, Status: st}, nil
		}
	}
	v.mu.Unlock()

	s := stlogview.NewStore()
	s.SetClipboard(func(text string) {
		if v.app == nil || v.app.wailsApp == nil || v.app.wailsApp.Clipboard == nil {
			return
		}
		v.app.wailsApp.Clipboard.SetText(text)
	})
	s.MarkLive(0)

	v.mu.Lock()
	v.seq++
	id := fmt.Sprintf("doc-%d", v.seq)
	v.docs[id] = s
	v.bySession[sessionID] = id
	v.mu.Unlock()
	return OpenDoc{ID: id, Status: s.Status()}, nil
}

// appendSession 是数据帧的入口：有实时文档就追加一帧，没有就什么都不做。
//
// 由 App.emitSession 调用（每帧一次），所以这里不取 v.mu 的写锁 —— 用读锁拿
// 文档指针，追加本身在文档自己的锁里做。
func (v *LogView) appendSession(sessionID string, f stlogview.FrameIn) {
	v.mu.Lock()
	id := v.bySession[sessionID]
	s := v.docs[id]
	v.mu.Unlock()
	if s != nil {
		s.AppendFrame(f)
	}
}

// AppendFrames 批量追加（给前端补数据 / 测试用）。
func (v *LogView) AppendFrames(id string, frames []stlogview.FrameIn) (stlogview.LiveInfo, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.LiveInfo{}, err
	}
	return s.AppendFrames(frames), nil
}

// PrependFrames 往回补更早的历史。
func (v *LogView) PrependFrames(id string, frames []stlogview.FrameIn) (stlogview.LiveInfo, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.LiveInfo{}, err
	}
	return s.PrependFrames(frames), nil
}

// ResetLive 清空实时文档（界面上的「清空」）。
func (v *LogView) ResetLive(id string) (stlogview.LiveInfo, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.LiveInfo{}, err
	}
	return s.ResetLive(), nil
}

// LiveInfo 取实时文档概况（前端定时轮一次）。
func (v *LogView) LiveInfo(id string) (stlogview.LiveInfo, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.LiveInfo{}, err
	}
	return s.LiveInfo(), nil
}

// AppendSystem 把一条系统消息（sys.port_connected 之类）塞进实时文档。
//
// 它不是串口字节，但用户在会话页上要看得到「什么时候连上/断开」；按 dir=system
// 追加一条，payload 就是消息文本的 UTF-8 字节，前端按文本行渲染。
func (v *LogView) AppendSystem(id, text string) (stlogview.LiveInfo, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.LiveInfo{}, err
	}
	return s.AppendFrame(stlogview.FrameIn{
		Ts:  time.Now().UnixMilli(),
		Dir: sessionfile.DirSystem,
		Hex: hex.EncodeToString([]byte(text)),
	}), nil
}

// Close 关掉一个文档（标签页销毁时调）。
func (v *LogView) Close(id string) error {
	v.mu.Lock()
	s := v.docs[id]
	delete(v.docs, id)
	for sid, did := range v.bySession {
		if did == id {
			delete(v.bySession, sid)
		}
	}
	v.mu.Unlock()
	if s == nil {
		return nil
	}
	s.Close()
	return nil
}

// CloseAll 收掉所有文档（退出 / 主窗口关闭时兜底）。
func (v *LogView) CloseAll() {
	v.mu.Lock()
	docs := v.docs
	v.docs = make(map[string]*stlogview.Store)
	v.bySession = make(map[string]string)
	v.mu.Unlock()
	for _, s := range docs {
		s.Close()
	}
}

// Count 是当前打开的文档数（调试用）。
func (v *LogView) Count() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.docs)
}

// Status 是扫描进度（前端每 100~150 ms 轮一次）。
func (v *LogView) Status(id string) (stlogview.StatusResult, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.StatusResult{}, err
	}
	return s.Status(), nil
}

// Meta 是打开后的一次性元信息（文件名、大小、页数、来源、时间范围）。
func (v *LogView) Meta(id string) (stlogview.MetaResult, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.MetaResult{}, err
	}
	return s.Meta(), nil
}

// Configure 重算布局：宽度（每行几格）、两种行高、Hex/文本、显示样式
// （编码 / 控制字符 / 转义字节 / Hex 分隔与前缀）。行高按半像素传（前端量出来 ×2）。
//
// dirModes 是按方向的模式覆盖（下标 = sessionfile 方向号，空串 = 沿用 mode）：
// 转发页的 P1 / P2 各有独立的「字 / H」开关。
func (v *LogView) Configure(id string, charsPerLine, tsLineH2, dataLineH2 int, mode string, dirModes []string, anchorIdx int, style stlogview.Style) (stlogview.ConfigureResult, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.ConfigureResult{}, err
	}
	return s.Configure(charsPerLine, tsLineH2, dataLineH2, mode, dirModes, anchorIdx, style), nil
}

// SetOpt 切年份 / 0x 前缀。
func (v *LogView) SetOpt(id, name string, on bool) (bool, error) {
	s, err := v.get(id)
	if err != nil {
		return false, err
	}
	return s.SetOpt(name, on), nil
}

// Window 按像素取窗口（内容纵坐标 + 视口高 + 预取余量）。
func (v *LogView) Window(id string, contentY, viewportH, overscanPx int) (stlogview.WindowResult, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.WindowResult{}, err
	}
	return s.Window(contentY, viewportH, overscanPx), nil
}

// Ruler 给滚动条的两道：来源 / 发送方、落盘完整性。
func (v *LogView) Ruler(id string, nb int) (stlogview.RulerResult, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.RulerResult{}, err
	}
	return s.Ruler(nb), nil
}

// CopySelection 把选区（帧号 + 视觉行 + 格）换成字节并写剪贴板。
func (v *LogView) CopySelection(id string, req stlogview.CopyReq) (stlogview.CopyResult, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.CopyResult{}, err
	}
	return s.CopySelection(req), nil
}

// FrameDebug 是行高对账用的：一帧的原始字节 + Go 侧算出的格子/行数。
func (v *LogView) FrameDebug(id string, idx int) (string, error) {
	s, err := v.get(id)
	if err != nil {
		return "", err
	}
	return s.FrameDebug(idx), nil
}
