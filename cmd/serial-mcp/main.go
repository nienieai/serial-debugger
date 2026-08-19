package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"serial-tool-v3/version"
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
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    serverCap      `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
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
		ProtocolVersion string         `json:"protocolVersion"`
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

// ── Content-Length framing ──

func readMessage(br *bufio.Reader) ([]byte, error) {
	var contentLen int
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			contentLen, _ = strconv.Atoi(strings.TrimSpace(line[len("Content-Length:"):]))
		}
	}
	if contentLen <= 0 {
		return nil, fmt.Errorf("missing or invalid Content-Length header")
	}
	body := make([]byte, contentLen)
	_, err := io.ReadFull(br, body)
	return body, err
}

func writeMessage(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := w.Write([]byte(header)); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ── MCP header scanner (used by readMessage via bufio.Scanner) ──

func scanHeader(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.Index(data, []byte("\r\n")); i >= 0 {
		return i + 2, data[0:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
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
