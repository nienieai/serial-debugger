package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ProtocolVersion 是守护进程与客户端之间 IPC 协议的版本号。
//
// 守护进程是机器级单例（Global\serial-tool-daemon 互斥体），而 GUI / CLI /
// MCP / 独立软件是各自独立升级的可执行文件，因此「新客户端 + 旧守护进程」
// 是常态。若不核对版本，双方对方法集、参数形状、共享内存条目格式的理解
// 差异会静默落到数据层，表现为乱码或静默失败，极难定位。
//
// 用语义版本 MAJOR.MINOR.PATCH 而不是递增的整数，是为了让这个号本身带信息
// （此前只有裸整数 1，改一次就丢一次「这次改动有多严重」）：
//
//	MAJOR  同一个号里出现两套不兼容语义时的兜底，目前用不到
//	MINOR  方法增删或改名、参数形状变化、共享内存条目布局变化
//	PATCH  线路字节不变的修正（内部行为、日志、错误文案）
//
// 兼容判定见 Compatible：只看 MAJOR.MINOR，PATCH 可不同 —— 打补丁不必把
// 所有人的守护进程踢下线。
const ProtocolVersion = "0.2.0"

// protoMajorMinor 取 "MAJOR.MINOR" 前缀；不合法（少于两段或任一段为空）返回 false。
func protoMajorMinor(v string) (string, bool) {
	parts := strings.Split(v, ".")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + "." + parts[1], true
}

// ErrProtocolDecode 表示「daemon.info 的响应拿到了，但协议号解析不出来」。
//
// 几乎总是同一个成因：对端是老版本守护进程，回的是整数协议号（旧版是 1），
// 解成字符串就失败。守护进程因此要换代，而不是当成「没有守护进程在跑」——
// 后者会让客户端去拉起新实例，而旧实例还占着 Global 互斥体，结果是静默失败。
var ErrProtocolDecode = errors.New("daemon.info 的协议号无法解析")

// UnknownMethodPrefix 是守护进程对未知方法返回的错误前缀。
// 客户端借此识别「对端版本过旧」，因此必须是共享常量而非两处字面量。
const UnknownMethodPrefix = "unknown method: "

// DaemonInfo 是 IPC 方法 "daemon.info" 的结果，
// 用于让客户端确认对端守护进程的身份与协议版本。
type DaemonInfo struct {
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocolVersion"`
	PID             uint32 `json:"pid"`
	StartTime       string `json:"startTime"`
	UptimeSec       int64  `json:"uptimeSec"`
}

// Compatible 报告对端守护进程是否与本客户端兼容：MAJOR.MINOR 相同即可。
//
// 任何一侧的版本号解析不出来都按不兼容处理 —— 「看不懂对方的号」不能当成
// 「我们是同一个号」。
func (d DaemonInfo) Compatible() bool {
	theirs, okTheirs := protoMajorMinor(d.ProtocolVersion)
	ours, okOurs := protoMajorMinor(ProtocolVersion)
	return okTheirs && okOurs && theirs == ours
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
		return d, fmt.Errorf("%w: %v", ErrProtocolDecode, err)
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
