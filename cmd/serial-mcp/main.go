package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/nienieai/serial-debugger/version"
)

// ── JSON-RPC 2.0 types ──

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
}

type jsonrpcErrorResp struct {
	JSONRPC string   `json:"jsonrpc"`
	ID      any      `json:"id"`
	Error   rpcError `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ── MCP initialize types ──

type initializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    serverCap  `json:"capabilities"`
	ServerInfo      serverInfo `json:"serverInfo"`
}

type serverCap struct {
	Tools toolsCap `json:"tools"`
}

type toolsCap struct {
	ListChanged bool `json:"listChanged"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ── MCP server state ──

type mcpState int

const (
	stateUninitialized mcpState = iota
	statePendingInitialized
	stateReady
)

type mcpServer struct {
	state mcpState
}

func (s *mcpServer) writeResult(id any, result any) {
	writeMessage(os.Stdout, jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *mcpServer) writeError(id any, code int, message string) {
	writeMessage(os.Stdout, jsonrpcErrorResp{
		JSONRPC: "2.0",
		ID:      id,
		Error:   rpcError{Code: code, Message: message},
	})
}

func (s *mcpServer) requireReady(id any) bool {
	if s.state != stateReady {
		s.writeError(id, -32002, "Server not initialized: send initialize first")
		return false
	}
	return true
}

// ── Handlers ──

func (s *mcpServer) handleInitialize(req jsonrpcRequest) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, -32602, "Invalid params: "+err.Error())
		return
	}
	logMCP("initialize: client=%s/%s, protocol=%s",
		params.ClientInfo.Name, params.ClientInfo.Version, params.ProtocolVersion)

	result := initializeResult{
		ProtocolVersion: "2025-06-18",
		Capabilities: serverCap{
			Tools: toolsCap{ListChanged: false},
		},
		ServerInfo: serverInfo{
			Name:    "serial-tool-mcp",
			Version: version.Version,
		},
	}
	s.writeResult(req.ID, result)
	s.state = statePendingInitialized
}

func (s *mcpServer) handleInitialized() {
	logMCP("initialized notification received, ready for requests")
	s.state = stateReady
}

func (s *mcpServer) handleToolsList(id any) {
	type toolsListResult struct {
		Tools []toolDef `json:"tools"`
	}
	s.writeResult(id, toolsListResult{Tools: allTools})
}

func (s *mcpServer) handleToolsCall(req jsonrpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeResult(req.ID, errResult("Invalid tool call params: "+err.Error()))
		return
	}
	handler, ok := toolHandlers[params.Name]
	if !ok {
		s.writeResult(req.ID, errResult("Unknown tool: "+params.Name))
		return
	}
	logMCP("tools/call: %s", params.Name)
	result := handler(params.Arguments)
	s.writeResult(req.ID, result)
}

func (s *mcpServer) handleMessage(body []byte) {
	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(nil, -32700, "Parse error: "+err.Error())
		return
	}
	if req.JSONRPC != "2.0" {
		s.writeError(req.ID, -32600, "Invalid Request: jsonrpc must be \"2.0\"")
		return
	}

	isNotification := req.ID == nil

	switch req.Method {
	case "initialize":
		if isNotification {
			return
		}
		s.handleInitialize(req)

	case "notifications/initialized":
		if !isNotification {
			s.writeError(req.ID, -32600, "initialized must be a notification")
			return
		}
		s.handleInitialized()

	case "ping":
		if isNotification {
			return
		}
		s.writeResult(req.ID, map[string]any{})

	case "tools/list":
		if !s.requireReady(req.ID) {
			return
		}
		s.handleToolsList(req.ID)

	case "tools/call":
		if !s.requireReady(req.ID) {
			return
		}
		s.handleToolsCall(req)

	default:
		s.writeError(req.ID, -32601, "Method not found: "+req.Method)
	}
}

// ── newline-delimited framing (MCP stdio transport) ──
//
// Per the MCP specification the stdio transport delimits messages by
// newlines and messages MUST NOT contain embedded newlines:
// https://modelcontextprotocol.io/specification/2025-06-18/basic/transports
//
// The previous implementation used LSP-style "Content-Length: N\r\n\r\n"
// headers, which is not an MCP transport and made the server unusable from
// any spec-compliant client.

func readMessage(br *bufio.Reader) ([]byte, error) {
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			// Tolerate a final message without a trailing newline.
			if err == io.EOF {
				if body := bytes.TrimSpace(line); len(body) > 0 {
					return body, nil
				}
			}
			return nil, err
		}
		// Skip blank/whitespace-only lines between messages.
		if body := bytes.TrimSpace(line); len(body) > 0 {
			return body, nil
		}
	}
}

func writeMessage(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// ── Logging (stderr only) ──

func logMCP(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[MCP] "+format+"\n", args...)
}

// ── Entry point ──

func main() {
	log.SetOutput(os.Stderr)
	logMCP("serial-tool-mcp v" + version.Version + " starting")

	srv := &mcpServer{state: stateUninitialized}
	br := bufio.NewReader(os.Stdin)

	for {
		body, err := readMessage(br)
		if err != nil {
			if err == io.EOF {
				logMCP("stdin closed, exiting")
				return
			}
			logMCP("read error: %v", err)
			return
		}
		srv.handleMessage(body)
	}
}
