package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nienieai/serial-debugger/client"
	"github.com/nienieai/serial-debugger/config"
	"github.com/nienieai/serial-debugger/contract"
	"github.com/nienieai/serial-debugger/decode"
	"github.com/nienieai/serial-debugger/pipe"
	"github.com/nienieai/serial-debugger/sessionfile"
	"github.com/nienieai/serial-debugger/version"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type App struct {
	ctx       context.Context
	eventConn *client.DaemonClient
	stopping  bool
	checking  bool
	daemonMu  sync.Mutex

	// Tab decode workers — one goroutine per tab.
	tabDecoders   map[string]*tabDecodeUnit // processId → worker
	tabDecodersMu sync.Mutex
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
			runtime.EventsEmit(td.a.ctx, env.event, env.params)
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
	return &App{}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	// 守护进程就绪通知：阻塞等 OS 级「管道可连接」，就绪后立刻推给前端，
	// 前端收到就能马上连，不必等 checkOffline 的 1s 轮询。
	go a.watchDaemonReady(ctx)
}

// watchDaemonReady 把「守护进程就绪」变成前端可订阅的事件。
//
// pipe.WaitReady 在 Windows 上就是 WaitNamedPipe：守护进程一 Listen 就返回，
// 不存在「进程起来了但管道还没建好」的误判。就绪期间不重复通知，等端点消失
// （守护进程退出）后回到外层继续等下一次。
func (a *App) watchDaemonReady(ctx context.Context) {
	for ctx.Err() == nil {
		if !pipe.WaitReady(1 * time.Second) {
			continue
		}
		runtime.EventsEmit(ctx, "daemon-ready", map[string]any{})
		for ctx.Err() == nil && pipe.WaitReady(300*time.Millisecond) {
			time.Sleep(300 * time.Millisecond)
		}
	}
}

func (a *App) Shutdown(ctx context.Context) {
	a.StopAllTabDecoders()
	if a.eventConn != nil {
		a.eventConn.Close()
		a.eventConn = nil
	}
}

// onDaemonDisconnected is called by the heartbeat goroutine when the
// connection to the daemon is lost (3 consecutive ping failures).
// It only takes effect if ec is still the active connection, preventing
// a stale connection from killing a fresh reconnection.
func (a *App) onDaemonDisconnected(ec *client.DaemonClient) {
	a.daemonMu.Lock()
	if a.eventConn == ec {
		a.eventConn = nil
		a.daemonMu.Unlock()
		runtime.EventsEmit(a.ctx, "daemon-offline", map[string]any{"reason": "heartbeat"})
	} else {
		a.daemonMu.Unlock()
	}
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

	if a.eventConn != nil {
		a.daemonMu.Unlock()
		return nil
	}
	a.daemonMu.Unlock()

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

func (a *App) OpenSession(cfg SerialConfig) (string, error) {
	result, err := a.call(contract.ProcessCreate, map[string]any{
		"port": cfg.Port, "baud": cfg.Baud,
		"dataBits": cfg.DataBits, "stopBits": cfg.StopBits, "parity": cfg.Parity,
	})
	if err != nil {
		return "", err
	}
	pid, _ := result["processId"].(string)
	return pid, nil
}

func (a *App) CloseSession(processID string) error {
	_, err := a.call(contract.ProcessDestroy, map[string]any{"processId": processID})
	return err
}

func (a *App) CreateIdleProcess() (string, error) {
	result, err := a.call(contract.ProcessCreate, nil)
	if err != nil {
		return "", err
	}
	pid, _ := result["processId"].(string)
	return pid, nil
}

func (a *App) ConnectSession(processID string, cfg SerialConfig) error {
	_, err := a.call(contract.ProcessConnect, map[string]any{
		"processId": processID, "port": cfg.Port, "baud": cfg.Baud,
		"dataBits": cfg.DataBits, "stopBits": cfg.StopBits, "parity": cfg.Parity,
	})
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

func (a *App) GetSessions() []map[string]any {
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

// i18nDir 返回优先级最高的磁盘语言包目录：<exe>/resources-system/i18n 或
// <exe>/resources/i18n（见 config/resources.go）。
func i18nDir() string {
	return config.I18nDir()
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

// OpenDevTools toggles the WebView2 DevTools window.
// Requires the GUI to be built with `wails build -devtools`.
func (a *App) OpenDevTools() {
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

// OpenStlogSession 把一个 .stlog 会话（目录或 v1.4 单文件）推给守护进程，
// 由它开一个文件会话进程。返回的 processId 之后走常规会话接口读数据。
func (a *App) OpenStlogSession(path string) (map[string]any, error) {
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
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "打开 .stlog 会话文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "会话文件 (*.stlog)", Pattern: "*.stlog"},
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	})
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
	out, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出 .stlog 文本",
		DefaultFilename: base + ".txt",
		Filters: []runtime.FileFilter{
			{DisplayName: "文本文件 (*.txt)", Pattern: "*.txt"},
		},
	})
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
			// Route directional data events to per-tab decode workers.
			switch msg.Event {
			case "rx", "tx", "1", "2":
				if msg.Params != nil {
					procID, _ := msg.Params["processId"].(string)
					a.tabDecodersMu.Lock()
					td := a.tabDecoders[procID]
					a.tabDecodersMu.Unlock()
					if td != nil {
						td.jobs <- rxTxEnvelope{event: msg.Event, params: msg.Params}
						continue
					}
				}
				// No worker for this tab — emit without segments.
				runtime.EventsEmit(a.ctx, msg.Event, msg.Params)
			default:
				runtime.EventsEmit(a.ctx, msg.Event, msg.Params)
			}
		case <-a.ctx.Done():
			return
		}
	}
}
