package main

import (
	"fmt"
	"sync"

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

	mu   sync.Mutex
	docs map[string]*stlogview.Store
	seq  int
}

func newLogView(a *App) *LogView {
	return &LogView{app: a, docs: make(map[string]*stlogview.Store)}
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

// Close 关掉一个文档（标签页销毁时调）。
func (v *LogView) Close(id string) error {
	v.mu.Lock()
	s := v.docs[id]
	delete(v.docs, id)
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
func (v *LogView) Configure(id string, charsPerLine, tsLineH2, dataLineH2 int, mode string, anchorIdx int, style stlogview.Style) (stlogview.ConfigureResult, error) {
	s, err := v.get(id)
	if err != nil {
		return stlogview.ConfigureResult{}, err
	}
	return s.Configure(charsPerLine, tsLineH2, dataLineH2, mode, anchorIdx, style), nil
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
