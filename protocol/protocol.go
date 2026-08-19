package protocol

import (
	"bufio"
	"encoding/json"
	"io"
)

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
