package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nienieai/serial-debugger/client"
	"github.com/nienieai/serial-debugger/config"
)

var mcpLang = "zh"

func init() {
	if s := os.Getenv("LANG"); s != "" && config.HasLang(s) {
		mcpLang = s
	}
}

type toolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]schemaProperty `json:"properties,omitempty"`
	Required   []string                  `json:"required,omitempty"`
}

type schemaProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type toolCallResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}


var allTools = []toolDef{
	{
		Name:        "serial_start_daemon",
		Description: "Start the serial daemon if not already running. Returns status when daemon is already running. The daemon is started in background (silent) mode.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{}},
	},
	{
		Name:        "serial_list_ports",
		Description: "List available serial (COM) ports from the daemon cache. Does NOT trigger a hardware rescan.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{}},
	},
	{
		Name:        "serial_refresh_ports",
		Description: "Force a hardware rescan and return the updated list of serial (COM) ports.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{}},
	},
	{
		Name:        "serial_create",
		Description: "Create a serial process. mode: single (default) or forward. Without port: idle. With port: create+connect with dedup. Forward mode requires portB.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"mode": {Type: "string", Description: "Process mode: single (default) or forward", Enum: []string{"single", "forward"}},
			"port": {Type: "string", Description: "Serial port name (e.g. COM3). Omit for idle. In forward mode, this is port A."},
			"baud": {Type: "integer", Description: "Baud rate (default 115200)."},
			"dataBits": {Type: "integer", Description: "Data bits (default: 8)"},
			"stopBits": {Type: "string", Description: "Stop bits (default: 1)", Enum: []string{"1", "1.5", "2"}},
			"parity": {Type: "string", Description: "Parity (default: none)", Enum: []string{"none", "odd", "even", "mark", "space"}},
			"portB": {Type: "string", Description: "Second port for forward mode (e.g. COM4)."},
			"baudB": {Type: "integer", Description: "Baud rate for port B (default 115200)."},
			"dataBitsB": {Type: "integer", Description: "Data bits for port B (default: 8)"},
			"stopBitsB": {Type: "string", Description: "Stop bits for port B (default: 1)", Enum: []string{"1", "1.5", "2"}},
			"parityB": {Type: "string", Description: "Parity for port B (default: none)", Enum: []string{"none", "odd", "even", "mark", "space"}},
		}},
	},
	{
		Name:        "serial_open",
		Description: "Open a serial port. Shortcut for serial_create with port. Returns processId.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"port": {Type: "string", Description: "Serial port name (e.g. COM3)"},
			"baud": {Type: "integer", Description: "Baud rate (default 115200)."},
			"dataBits": {Type: "integer", Description: "Data bits (default: 8)"},
			"stopBits": {Type: "string", Description: "Stop bits (default: 1)", Enum: []string{"1", "1.5", "2"}},
			"parity": {Type: "string", Description: "Parity (default: none)", Enum: []string{"none", "odd", "even", "mark", "space"}},
		}, Required: []string{"port"}},
	},
	{
		Name:        "serial_connect",
		Description: "Connect an idle process to a serial port. Process must be idle.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID from serial_create."},
			"port": {Type: "string", Description: "Serial port name (e.g. COM3)."},
			"baud": {Type: "integer", Description: "Baud rate (default 115200)."},
			"dataBits": {Type: "integer", Description: "Data bits (default: 8)"},
			"stopBits": {Type: "string", Description: "Stop bits (default: 1)", Enum: []string{"1", "1.5", "2"}},
			"parity": {Type: "string", Description: "Parity (default: none)", Enum: []string{"none", "odd", "even", "mark", "space"}},
		}, Required: []string{"processId", "port"}},
	},
	{
		Name:        "serial_disconnect",
		Description: "Disconnect a process from its serial port. Port closed, process kept as idle.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID to disconnect."},
		}},
	},
	{
		Name:        "serial_switch",
		Description: "Switch a connected process to a different serial port without destroying the process. Preserves history and send queue.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID to switch"},
			"port": {Type: "string", Description: "New serial port name (e.g. COM4)"},
			"baud": {Type: "integer", Description: "Baud rate (default 115200)"},
		}, Required: []string{"processId", "port"}},
	},
	{
		Name:        "serial_forward_create",
		Description: "Create a port forwarding process that copies data bidirectionally between two serial ports. PC acts as passive monitor.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"portA": {Type: "string", Description: "First serial port (e.g. COM13)"},
			"portB": {Type: "string", Description: "Second serial port (e.g. COM14)"},
			"baudA": {Type: "integer", Description: "Baud rate for port A (default 115200)"},
			"baudB": {Type: "integer", Description: "Baud rate for port B (default 115200)"},
		}, Required: []string{"portA", "portB"}},
	},
	{
		Name:        "serial_declare",
		Description: "Declare a serial port configuration without opening the port. Stores full serial config (baud, dataBits, stopBits, parity) on an idle process visible to ALL connected clients via process-changed events. Use serial_connect later to activate. Forward mode supported for port pairs.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"mode": {Type: "string", Description: "Process mode: single (default) or forward", Enum: []string{"single", "forward"}},
			"port": {Type: "string", Description: "Serial port name (e.g. COM3). Required unless creating empty idle process."},
			"baud": {Type: "integer", Description: "Baud rate (default 115200)."},
			"dataBits": {Type: "integer", Description: "Data bits (default: 8)"},
			"stopBits": {Type: "string", Description: "Stop bits (default: 1)", Enum: []string{"1", "1.5", "2"}},
			"parity": {Type: "string", Description: "Parity (default: none)", Enum: []string{"none", "odd", "even", "mark", "space"}},
			"portB": {Type: "string", Description: "Second port for forward mode (e.g. COM4)."},
			"baudB": {Type: "integer", Description: "Baud rate for port B (default 115200)."},
			"dataBitsB": {Type: "integer", Description: "Data bits for port B (default: 8)"},
			"stopBitsB": {Type: "string", Description: "Stop bits for port B (default: 1)", Enum: []string{"1", "1.5", "2"}},
			"parityB": {Type: "string", Description: "Parity for port B (default: none)", Enum: []string{"none", "odd", "even", "mark", "space"}},
		}, Required: []string{"port"}},
	},
	{
		Name:        "serial_set_mode",
		Description: "Switch a process between single-port and forward mode. Process must be idle (all ports disconnected).",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID to modify."},
			"mode": {Type: "string", Description: "Target mode", Enum: []string{"single", "forward"}},
		}, Required: []string{"processId", "mode"}},
	},
	{
		Name:        "serial_close",
		Description: "Close and destroy a serial process. If connected, disconnects first.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID to close."},
		}},
	},
	{
		Name:        "serial_send",
		Description: "Send data over a connected serial process. Supports text and hex.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"data": {Type: "string", Description: "Data to send."},
			"processId": {Type: "string", Description: "Process ID. If omitted, uses first connected."},
			"format": {Type: "string", Description: "Data format (default: text)", Enum: []string{"text", "hex"}},
		}, Required: []string{"data"}},
	},
	{
		Name:        "serial_sessions",
		Description: "List all processes (idle and connected) with their configuration and status.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{}},
	},
	{
		Name:        "serial_history",
		Description: "Read data history for a serial process from shared memory.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID."},
		}},
	},
	{
		Name:        "serial_status",
		Description: "Check daemon running status.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{}},
	},
	{
		Name:        "serial_monitor",
		Description: "Persistent daemon client that listens for events.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"timeout": {Type: "integer", Description: "Listen duration in seconds (default: 30)."},
			"events": {Type: "string", Description: "Comma-separated event names to listen for."},
		}},
	},
	{
		Name:        "serial_shutdown",
		Description: "Gracefully shutdown the daemon.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{}},
	},
	{
		Name:        "serial_stats",
		Description: "Get I/O statistics for a serial process.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID."},
		}},
	},
	{
		Name:        "serial_port_watch",
		Description: "Poll for port changes over a time window.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"interval": {Type: "integer", Description: "Poll interval in ms (default: 1000)."},
			"duration": {Type: "integer", Description: "Total watch duration in ms (default: 10000)."},
		}},
	},
	{
		Name:        "serial_autosend_start",
		Description: "Start auto-send on a serial process.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID."},
			"intervalMs": {Type: "integer", Description: "Send interval (single) or round interval (queue loop)."},
			"mode": {Type: "string", Description: "Mode: single or queue", Enum: []string{"single", "queue"}},
			"loop": {Type: "boolean", Description: "Loop continuously (queue mode only)."},
		}, Required: []string{"processId", "intervalMs", "mode"}},
	},
	{
		Name:        "serial_autosend_stop",
		Description: "Stop auto-send on a serial process.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID."},
		}},
	},
	{
		Name:        "serial_autosend_status",
		Description: "Get auto-send status for a serial process.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID."},
		}},
	},
	{
		Name:        "serial_sendqueue",
		Description: "Write multi-string entries to the send queue. Each entry: {enabled, hex, content, delay, note}.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID."},
			"entries": {Type: "array", Description: "Array of entry objects with fields: enabled, hex, content, delay, note."},
		}, Required: []string{"processId", "entries"}},
	},
	{
		Name:        "serial_multistr_save",
		Description: "Persist current send queue entries to disk for the process.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID."},
		}, Required: []string{"processId"}},
	},
	{
		Name:        "serial_multistr_load",
		Description: "Load entries from disk into the send queue for the process.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID."},
		}, Required: []string{"processId"}},
	},
	{
		Name:        "serial_multistr_status",
		Description: "Get multi-string send status (entries count, loop state, round count).",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"processId": {Type: "string", Description: "Process ID."},
		}, Required: []string{"processId"}},
	},
	{
		Name:        "serial_probe_ports",
		Description: "Probe serial ports to detect device types. Sends probe frames defined in probe.toml and matches responses to identify connected devices (Modbus RTU, MCU control boards, etc.). Occupied ports are skipped automatically.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]schemaProperty{
			"ports":      {Type: "array", Description: "Specific port names to probe (e.g. [\"COM3\"]). Omit to probe all available."},
			"baudRates":  {Type: "array", Description: "Baud rates to try (e.g. [9600, 115200]). Omit to use probe.toml defaults."},
			"rules":      {Type: "array", Description: "Rule names to apply. Omit to use all rules from probe.toml."},
			"configPath": {Type: "string", Description: "Path to probe.toml config file. Omit to auto-discover."},
		}},
	},
}
type toolHandler func(params json.RawMessage) *toolCallResult

var toolHandlers = map[string]toolHandler{
	"serial_start_daemon":  handleStartDaemon,
	"serial_list_ports":    handleListPorts,
	"serial_refresh_ports": handleRefreshPorts,
	"serial_create":        handleCreate,
	"serial_open":          handleOpen,
	"serial_connect":       handleConnect,
	"serial_disconnect":    handleDisconnect,
	"serial_switch":        handleSwitchPort,
	"serial_forward_create": handleForwardCreate,
	"serial_declare":       handleDeclare,
	"serial_close":         handleClose,
	"serial_send":          handleSend,
	"serial_sessions":      handleSessions,
	"serial_history":       handleHistory,
	"serial_status":        handleStatus,
	"serial_monitor":       handleMonitor,
	"serial_shutdown":      handleShutdown,
	"serial_stats":         handleStats,
	"serial_port_watch":       handlePortWatch,
	"serial_autosend_start":   handleAutoSendStart,
	"serial_autosend_stop":    handleAutoSendStop,
	"serial_autosend_status":  handleAutoSendStatus,
	"serial_sendqueue":        handleSendQueue,
	"serial_multistr_save":    handleMultistrSave,
	"serial_multistr_load":    handleMultistrLoad,
	"serial_multistr_status":  handleMultistrStatus,
	"serial_probe_ports":      handleProbePorts,
	"serial_set_mode":         handleSetMode,
}

func okResult(v any) *toolCallResult {
	b, _ := json.MarshalIndent(v, "", "  ")
	return &toolCallResult{
		Content: []toolContent{{Type: "text", Text: string(b)}},
	}
}

func errResult(msg string) *toolCallResult {
	return &toolCallResult{
		Content: []toolContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}

func firstConnectedID() string {
	result, err := client.CallOnce("process.list", nil, "mcp")
	if err != nil {
		return ""
	}
	processes, ok := result["processes"].([]any)
	if !ok {
		return ""
	}
	for _, p := range processes {
		pmap, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if status, _ := pmap["status"].(string); status == "connected" {
			id, _ := pmap["processId"].(string)
			return id
		}
	}
	return ""
}

func mcpToIPCParams(raw json.RawMessage) map[string]any {
	var m map[string]any
	json.Unmarshal(raw, &m)
	if v, ok := m["baud"]; ok {
		if f, ok2 := v.(float64); ok2 {
			m["baud"] = int(f)
		}
	}
	if v, ok := m["dataBits"]; ok {
		if f, ok2 := v.(float64); ok2 {
			m["dataBits"] = int(f)
		}
	}
	return m
}

// ---- Tool handlers ----

func handleStartDaemon(_ json.RawMessage) *toolCallResult {
	status, err := client.StartDaemon()
	if err != nil {
		return errResult(fmt.Sprintf("启动失败: %v", err))
	}
	if status == "already_running" {
		return okResult(map[string]any{
			"status":  "already_running",
			"message": "守护进程已在运行中",
		})
	}
	return okResult(map[string]any{
		"status":  "started",
		"message": "守护进程已启动",
	})
}

func handleListPorts(_ json.RawMessage) *toolCallResult {
	result, err := client.CallOnce("ports", nil, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to list ports: %v", err))
	}
	return okResult(result)
}

func handleRefreshPorts(_ json.RawMessage) *toolCallResult {
	result, err := client.CallOnce("ports.refresh", nil, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to refresh ports: %v", err))
	}
	return okResult(result)
}

func handleCreate(raw json.RawMessage) *toolCallResult {
	params := mcpToIPCParams(raw)
	result, err := client.CallOnce("process.create", params, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to create process: %v", err))
	}
	return okResult(result)
}

func handleOpen(raw json.RawMessage) *toolCallResult {
	params := mcpToIPCParams(raw)
	if _, ok := params["port"]; !ok {
		return errResult("Missing required parameter: port")
	}
	result, err := client.CallOnce("process.create", params, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to open process: %v", err))
	}
	return okResult(result)
}

func handleConnect(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
		Port      string `json:"port"`
		Baud      int    `json:"baud"`
		DataBits  int    `json:"dataBits"`
		StopBits  string `json:"stopBits"`
		Parity    string `json:"parity"`
	}
	json.Unmarshal(raw, &p)
	if p.ProcessID == "" || p.Port == "" {
		return errResult("Missing required parameters: processId and port")
	}
	result, err := client.CallOnce("process.connect", map[string]any{
		"processId": p.ProcessID,
		"port":      p.Port,
		"baud":      p.Baud,
		"dataBits":  p.DataBits,
		"stopBits":  p.StopBits,
		"parity":    p.Parity,
	}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to connect: %v", err))
	}
	return okResult(result)
}


func handleSwitchPort(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
		Port      string `json:"port"`
		Baud      int    `json:"baud"`
	}
	json.Unmarshal(raw, &p)
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" { return errResult("No connected process") }
	}
	if p.Port == "" { return errResult("port is required") }
	if p.Baud <= 0 { p.Baud = 115200 }
	result, err := client.CallOnce("process.switch", map[string]any{
		"processId": p.ProcessID, "port": p.Port, "baud": p.Baud,
	}, "mcp")
	if err != nil { return errResult(fmt.Sprintf("Failed to switch: %v", err)) }
	return okResult(result)
}

func handleForwardCreate(raw json.RawMessage) *toolCallResult {
	var p struct {
		PortA string `json:"portA"`
		PortB string `json:"portB"`
		BaudA int    `json:"baudA"`
		BaudB int    `json:"baudB"`
	}
	json.Unmarshal(raw, &p)
	if p.PortA == "" || p.PortB == "" { return errResult("portA and portB are required") }
	if p.BaudA <= 0 { p.BaudA = 115200 }
	if p.BaudB <= 0 { p.BaudB = 115200 }
	result, err := client.CallOnce("forward.create", map[string]any{
		"portA": p.PortA, "baudA": p.BaudA, "portB": p.PortB, "baudB": p.BaudB,
	}, "mcp")
	if err != nil { return errResult(fmt.Sprintf("Failed to create forward: %v", err)) }
	return okResult(result)
}

func handleDeclare(raw json.RawMessage) *toolCallResult {
	params := mcpToIPCParams(raw)
	if _, ok := params["port"]; !ok {
		return errResult("Missing required parameter: port")
	}
	params["connect"] = false
	result, err := client.CallOnce("process.create", params, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to declare: %v", err))
	}
	return okResult(result)
}

func handleSetMode(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
		Mode      string `json:"mode"`
	}
	json.Unmarshal(raw, &p)
	if p.ProcessID == "" {
		return errResult("processId is required")
	}
	if p.Mode != "single" && p.Mode != "forward" {
		return errResult("mode must be 'single' or 'forward'")
	}
	result, err := client.CallOnce("process.setmode", map[string]any{
		"processId": p.ProcessID, "mode": p.Mode,
	}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to set mode: %v", err))
	}
	return okResult(result)
}

func handleDisconnect(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
	}
	json.Unmarshal(raw, &p)
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("process.disconnect", map[string]any{"processId": p.ProcessID}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to disconnect: %v", err))
	}
	return okResult(result)
}

func handleClose(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
	}
	json.Unmarshal(raw, &p)
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("process.destroy", map[string]any{"processId": p.ProcessID}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to destroy process: %v", err))
	}
	return okResult(result)
}

func handleSend(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
		Data      string `json:"data"`
		Format    string `json:"format"`
	}
	json.Unmarshal(raw, &p)
	if p.Data == "" {
		return errResult("Missing required parameter: data")
	}
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" {
			return errResult("No connected process")
		}
	}
	if p.Format == "hex" {
		p.Data = strings.ReplaceAll(p.Data, " ", "")
	}

	// Write data to shared memory, then trigger send
	ringResult, err := client.CallOnce("send.ringname", map[string]any{"processId": p.ProcessID}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to get ring name: %v", err))
	}
	ringName, _ := ringResult["ringName"].(string)
	if ringName == "" {
		return errResult("Failed to get send queue name")
	}

	var sendBytes []byte
	if p.Format == "hex" {
		sendBytes, err = hex.DecodeString(p.Data)
		if err != nil {
			return errResult(fmt.Sprintf("Invalid hex: %v", err))
		}
	} else {
		sendBytes = []byte(p.Data)
	}

	if err := client.SendWrite(ringName, sendBytes); err != nil {
		return errResult(fmt.Sprintf("Failed to write send queue: %v", err))
	}

	result, err := client.CallOnce("send.trigger", map[string]any{"processId": p.ProcessID, "raw": true}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to trigger send: %v", err))
	}
	return okResult(result)
}

func handleSessions(_ json.RawMessage) *toolCallResult {
	result, err := client.CallOnce("process.list", nil, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to list processes: %v", err))
	}
	return okResult(result)
}

func handleHistory(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(raw, &p)
	pid := p.ProcessID
	if pid == "" {
		pid = p.SessionID
	}
	if pid == "" {
		pid = firstConnectedID()
		if pid == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("session.history", map[string]any{"processId": pid}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to read history: %v", err))
	}
	// Translate sys.* key-format entries in history
	if entries, ok := result["history"].([]any); ok {
		for i, e := range entries {
			if entry, ok := e.(map[string]any); ok {
				if dir, _ := entry["direction"].(string); dir == "system" {
					if hex, _ := entry["hex"].(string); hex != "" {
						if t := config.FormatSysMsg(mcpLang, hex); t != hex {
							entry["hex"] = t
							entries[i] = entry
						}
					}
				}
			}
		}
	}
	return okResult(result)
}

func handleStatus(_ json.RawMessage) *toolCallResult {
	result, err := client.CallOnce("status", nil, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("%v", err))
	}
	return okResult(result)
}

func handleMonitor(raw json.RawMessage) *toolCallResult {
	var p struct {
		Timeout float64 `json:"timeout"`
	}
	json.Unmarshal(raw, &p)
	timeout := time.Duration(p.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if timeout > 60*time.Second {
		timeout = 60 * time.Second
	}

	dc, err := client.NewDaemonClientWithEvents("mcp", []string{"rx", "tx"})
	if err != nil {
		return errResult(fmt.Sprintf("Failed to connect to daemon: %v", err))
	}
	defer dc.Close()

	timer := time.NewTimer(timeout)
	events := dc.ReadEvent()
	var collected []string

	for {
		select {
		case <-timer.C:
			if len(collected) == 0 {
				return errResult("No events received during monitoring period")
			}
			result := map[string]any{
				"eventCount": len(collected),
				"events":     collected,
			}
			b, _ := json.MarshalIndent(result, "", "  ")
			return &toolCallResult{
				Content: []toolContent{{Type: "text", Text: string(b)}},
			}
		case msg, ok := <-events:
			if !ok {
				return errResult("Connection to daemon lost")
			}
			timer.Reset(timeout)
			collected = append(collected,
				fmt.Sprintf("[%s] %s", msg.Event, string(toJSON(msg.Params))))
		}
	}
}

func handleShutdown(_ json.RawMessage) *toolCallResult {
	result, err := client.CallOnce("shutdown", nil, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to shutdown daemon: %v", err))
	}
	return okResult(result)
}

func handleStats(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(raw, &p)
	pid := p.ProcessID
	if pid == "" {
		pid = p.SessionID
	}
	if pid == "" {
		pid = firstConnectedID()
		if pid == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("session.stats", map[string]any{"processId": pid}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to get stats: %v", err))
	}
	return okResult(result)
}

func handlePortWatch(raw json.RawMessage) *toolCallResult {
	var p struct {
		Timeout float64 `json:"timeout"`
	}
	json.Unmarshal(raw, &p)
	timeout := time.Duration(p.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 300*time.Second {
		timeout = 300 * time.Second
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	lastResult, err := client.CallOnce("ports", nil, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to get ports: %v", err))
	}
	lastPorts, _ := lastResult["ports"]

	for {
		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				return okResult(map[string]any{
					"changed":     false,
					"ports":       lastPorts,
					"description": fmt.Sprintf("No port changes detected during %.0fs watch period", timeout.Seconds()),
				})
			}
			currentResult, err := client.CallOnce("ports", nil, "mcp")
			if err != nil {
				continue
			}
			currentPorts, _ := currentResult["ports"]
			if portsJSONChanged(lastPorts, currentPorts) {
				return okResult(map[string]any{
					"changed":     true,
					"ports":       currentPorts,
					"description": "Port change detected",
				})
			}
			lastPorts = currentPorts
		}
	}
}

func portsJSONChanged(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) != string(bj)
}

func handleAutoSendStart(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID  string `json:"processId"`
		IntervalMs int    `json:"intervalMs"`
		Mode       string `json:"mode"`
		Loop       bool   `json:"loop"`
	}
	json.Unmarshal(raw, &p)
	if p.Mode == "" {
		p.Mode = "single"
	}
	if p.IntervalMs <= 0 {
		p.IntervalMs = 1000
	}
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("autosend.start", map[string]any{
		"processId":  p.ProcessID,
		"intervalMs": p.IntervalMs,
		"mode":       p.Mode,
		"loop":       p.Loop,
	}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to start auto-send: %v", err))
	}
	return okResult(result)
}

func handleAutoSendStop(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
	}
	json.Unmarshal(raw, &p)
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("autosend.stop", map[string]any{"processId": p.ProcessID}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to stop auto-send: %v", err))
	}
	return okResult(result)
}

func handleAutoSendStatus(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
	}
	json.Unmarshal(raw, &p)
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("autosend.status", map[string]any{"processId": p.ProcessID}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to get auto-send status: %v", err))
	}
	return okResult(result)
}

func handleSendQueue(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string           `json:"processId"`
		Entries   []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return errResult("Invalid params: " + err.Error())
	}
	if len(p.Entries) == 0 {
		return errResult("entries array must not be empty")
	}
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("multistr.write", map[string]any{
		"processId": p.ProcessID,
		"entries":   p.Entries,
	}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to write entries: %v", err))
	}
	return okResult(result)
}

func handleMultistrSave(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
	}
	json.Unmarshal(raw, &p)
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("multistr.save", map[string]any{"processId": p.ProcessID}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to save entries: %v", err))
	}
	return okResult(result)
}

func handleMultistrLoad(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
	}
	json.Unmarshal(raw, &p)
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("multistr.load", map[string]any{"processId": p.ProcessID}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to load entries: %v", err))
	}
	return okResult(result)
}

func handleMultistrStatus(raw json.RawMessage) *toolCallResult {
	var p struct {
		ProcessID string `json:"processId"`
	}
	json.Unmarshal(raw, &p)
	if p.ProcessID == "" {
		p.ProcessID = firstConnectedID()
		if p.ProcessID == "" {
			return errResult("No connected process")
		}
	}
	result, err := client.CallOnce("autosend.status", map[string]any{"processId": p.ProcessID}, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to get status: %v", err))
	}
	return okResult(result)
}

func handleProbePorts(raw json.RawMessage) *toolCallResult {
	var p struct {
		Ports      []string `json:"ports"`
		BaudRates  []int    `json:"baudRates"`
		Rules      []string `json:"rules"`
		ConfigPath string   `json:"configPath"`
	}
	json.Unmarshal(raw, &p)

	params := map[string]any{}
	if len(p.Ports) > 0 {
		params["ports"] = p.Ports
	}
	if len(p.BaudRates) > 0 {
		params["baudRates"] = p.BaudRates
	}
	if len(p.Rules) > 0 {
		params["rules"] = p.Rules
	}
	if p.ConfigPath != "" {
		params["configPath"] = p.ConfigPath
	}

	result, err := client.CallOnce("ports.probe", params, "mcp")
	if err != nil {
		return errResult(fmt.Sprintf("Failed to probe ports: %v", err))
	}
	return okResult(result)
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if s == "" || s == "null" {
		return "{}"
	}
	return s
}
