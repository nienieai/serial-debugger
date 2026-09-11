package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// ProtocolVersion 是守护进程与客户端之间 IPC 协议的版本号。
//
// 守护进程是机器级单例（Global\serial-tool-daemon 互斥体），而 GUI / CLI /
// MCP / 独立软件是各自独立升级的可执行文件，因此「新客户端 + 旧守护进程」
// 是常态。若不核对版本，双方对方法集、参数形状、共享内存条目格式的理解
// 差异会静默落到数据层，表现为乱码或静默失败，极难定位。
//
// 任何破坏兼容性的改动（方法改名、参数增删、共享内存布局变化）都必须
// 递增此值，让客户端能够明确拒绝而不是带病工作。
const ProtocolVersion = 1

// UnknownMethodPrefix 是守护进程对未知方法返回的错误前缀。
// 客户端借此识别「对端版本过旧」，因此必须是共享常量而非两处字面量。
const UnknownMethodPrefix = "unknown method: "

// DaemonInfo 是 IPC 方法 "daemon.info" 的结果，
// 用于让客户端确认对端守护进程的身份与协议版本。
type DaemonInfo struct {
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocolVersion"`
	PID             uint32 `json:"pid"`
	StartTime       string `json:"startTime"`
	UptimeSec       int64  `json:"uptimeSec"`
}

// Compatible 报告对端守护进程是否与本客户端使用同一协议版本。
func (d DaemonInfo) Compatible() bool {
	return d.ProtocolVersion == ProtocolVersion
}

// DecodeDaemonInfo 把 IPC 的 map 结果还原为 DaemonInfo。
func DecodeDaemonInfo(m map[string]any) (DaemonInfo, error) {
	var d DaemonInfo
	if m == nil {
		return d, fmt.Errorf("空的 daemon.info 结果")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return d, err
	}
	return d, nil
}

// Request is an IPC request message.
type Request struct {
	ID       int64          `json:"id"`
	Method   string         `json:"method"`
	Params   map[string]any `json:"params,omitempty"`
	Source   string         `json:"source,omitempty"`   // "gui", "cli", "mcp"
	ClientId string         `json:"clientId,omitempty"` // persistent client identifier
}

// RegisterParams is sent by a persistent client to establish a 3-pipe session.
type RegisterParams struct {
	ClientId  string   `json:"clientId"`
	Source    string   `json:"source"`
	Subscribe []string `json:"subscribe"`
	RespPipe  string   `json:"respPipe"`
	SubPipe   string   `json:"subPipe"`
}

// Response is an IPC response message.
type Response struct {
	ID     int64  `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Event is a server-pushed event.
type Event struct {
	Event  string `json:"event"`
	Params any    `json:"params"`
}

// RawMsg is the unified wire message used for routing.
// Events have Event != ""; responses have ID != 0.
type RawMsg struct {
	ID       int64          `json:"id"`
	Result   map[string]any `json:"result,omitempty"`
	Error    string         `json:"error,omitempty"`
	Event    string         `json:"event,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
	ClientId string         `json:"clientId,omitempty"`
}

// ReadMessage reads a single newline-terminated JSON message.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	return r.ReadBytes('\n')
}

// WriteMessage serializes msg to JSON, appends '\n', and writes to w.
func WriteMessage(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// ParseRequest unmarshals data into a Request.
func ParseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	return &req, nil
}
