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

	"serial-tool-v3/client"
	"serial-tool-v3/config"
	"serial-tool-v3/decode"
	"serial-tool-v3/version"

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

func (td *tabDecodeUnit) run() {
	for {
		select {
		case env := <-td.jobs:
			hex, _ := env.params["hex"].(string)
			td.encMu.RLock()
			enc := td.encoding
			td.encMu.RUnlock()
			env.params["segments"] = decode.Decode(hex, enc)
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

	// Try to establish a connection
	if !client.IsDaemonProcessRunning() {
		a.daemonMu.Lock()
		a.checking = false
		a.daemonMu.Unlock()
		return false
	}

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
		client.CallOnce("shutdown", nil, "gui")
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

func (a *App) call(method string, params map[string]any) (map[string]any, error) {
	a.daemonMu.Lock()
	ec := a.eventConn
	a.daemonMu.Unlock()
	if ec == nil {
		return nil, fmt.Errorf("not connected")
	}
	return ec.Call(method, params)
}

func (a *App) GetPorts() ([]map[string]any, error) {
	result, err := a.call("ports", nil)
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
	result, err := a.call("ports.refresh", nil)
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
	result, err := a.call("process.create", map[string]any{
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
	_, err := a.call("process.destroy", map[string]any{"processId": processID})
	return err
}

func (a *App) CreateIdleProcess() (string, error) {
	result, err := a.call("process.create", nil)
	if err != nil {
		return "", err
	}
	pid, _ := result["processId"].(string)
	return pid, nil
}

func (a *App) ConnectSession(processID string, cfg SerialConfig) error {
	_, err := a.call("process.connect", map[string]any{
		"processId": processID, "port": cfg.Port, "baud": cfg.Baud,
		"dataBits": cfg.DataBits, "stopBits": cfg.StopBits, "parity": cfg.Parity,
	})
	return err
}

func (a *App) DisconnectSession(processID string) error {
	_, err := a.call("process.disconnect", map[string]any{"processId": processID})
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
	_, err := a.call("session.send", map[string]any{
		"processId": processID, "data": data, "format": format,
	})
	return err
}

func (a *App) GetClients() []map[string]any {
	result, err := a.call("client.list", nil)
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
	result, err := a.call("process.list", nil)
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

func i18nDir() string {
	// Try exe directory first (production)
	if exe, err := os.Executable(); err == nil {
		d := filepath.Join(filepath.Dir(exe), "i18n")
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	// Fall back to working directory (development / wails dev)
	if wd, err := os.Getwd(); err == nil {
		d := filepath.Join(wd, "i18n")
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	return ""
}

// ListExternalI18n scans the i18n/ directory next to the exe and returns
// metadata for each external .json translation file found.
func (a *App) ListExternalI18n() []ExternalI18nInfo {
	dir := i18nDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []ExternalI18nInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		info := ExternalI18nInfo{
			Filename: e.Name(),
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

// LoadExternalI18n loads i18n/{lang}.json from the external directory.
// Returns nil if the file does not exist or is invalid.
func (a *App) LoadExternalI18n(lang string) map[string]string {
	dir := i18nDir()
	if dir == "" {
		return nil
	}
	path := filepath.Join(dir, lang+".json")
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
func (a *App) CreateExampleFiles(kind string) map[string]any {
	exe, err := os.Executable()
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	base := filepath.Dir(exe)

	switch kind {
	case "colors":
		dir := filepath.Join(base, "themes", "colors")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		sample := filepath.Join(dir, "sunset-warm.json")
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
		dir := filepath.Join(base, "themes", "icons", "example-outline")
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
				"trash": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/><line x1="10" x2="10" y1="11" y2="17"/><line x1="14" x2="14" y1="11" y2="17"/></svg>`,
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
	exe, err := os.Executable()
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	base := filepath.Dir(exe)
	var dir string
	switch kind {
	case "colors":
		dir = filepath.Join(base, "themes", "colors")
	case "icons":
		dir = filepath.Join(base, "themes", "icons")
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
	result, _ := a.call("threads", nil)
	return result
}

func (a *App) GetSessionHistory(processID string) map[string]any {
	result, _ := a.call("session.history", map[string]any{"processId": processID})
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
							entry["segments"] = decode.Decode(hexStr, enc)
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
	result, _ := a.call("session.stats", map[string]any{"processId": processID})
	return result
}

func (a *App) GetGoroutines() map[string]any {
	result, _ := a.call("goroutines", nil)
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
	_, err := ec.Call("session.clearhistory", map[string]any{"processId": processID})
	return err
}

// SetProcessMode switches the process mode between "single" and "forward".
func (a *App) SetProcessMode(processID string, mode string) error {
	_, err := a.call("process.setmode", map[string]any{"processId": processID, "mode": mode})
	return err
}

// ForwardConnect connects an idle forward-mode process to two serial ports.
func (a *App) ForwardConnect(processID string, cfgA, cfgB SerialConfig) error {
	_, err := a.call("process.connect", map[string]any{
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
	_, err := ec.Call("autosend.start", params)
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
