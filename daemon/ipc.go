package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.bug.st/serial"

	"github.com/nienieai/serial-debugger/pipe"
	"github.com/nienieai/serial-debugger/protocol"
)

// ---- port info ----

type portInfo struct {
	Port        string `json:"port"`
	Description string `json:"description"`
}

// ---- client session (3-pipe) ----

type clientSession struct {
	clientId        string
	source          string
	daemonConn      io.ReadWriteCloser // client→daemon (requests)
	respConn        io.ReadWriteCloser // daemon→client (responses)
	subConn         io.ReadWriteCloser // daemon→client (events)
	pid             uint32
	connectTime     time.Time
	reqCount        int
	subMu           sync.Mutex // serializes writes to subConn
	subs            map[string]bool
	watchedProcesses map[string]bool // set of process IDs this client is viewing
}

func (sess *clientSession) writeResp(v any) error {
	return protocol.WriteMessage(sess.respConn, v)
}

func (sess *clientSession) writeSub(v any) error {
	sess.subMu.Lock()
	defer sess.subMu.Unlock()
	return protocol.WriteMessage(sess.subConn, v)
}

// ---- ipc server ----

type IpcServer struct {
	pm           *ProcessManager
	sessions     map[string]*clientSession // clientId → session
	sessMu       sync.RWMutex
	knownPIDs    map[uint32]string // PID → source
	portDescs    map[string]string
	portDescMu   sync.RWMutex
	cachedPorts  []portInfo
	portsMu      sync.RWMutex
	shutdownCh   chan struct{}
	shutdownOnce sync.Once

	hwProbeInterval  time.Duration
	hwProbeStop      chan struct{}
	hwProbeLastPorts []string
}

func NewIpcServer(pm *ProcessManager) *IpcServer {
	s := &IpcServer{
		pm:              pm,
		sessions:        make(map[string]*clientSession),
		knownPIDs:       make(map[uint32]string),
		shutdownCh:      make(chan struct{}),
		hwProbeStop:     make(chan struct{}),
		hwProbeInterval: 2 * time.Second,
	}
	pm.broadcast = s.broadcastEvent
	pm.pushEvent = s.pushClientEvent
	pm.onProcessChanged = s.broadcastProcessChanged
	s.cachedPorts = s.buildPortList()
	go s.loadPortDescsBg()
	s.startHwProbe()
	return s
}

func (s *IpcServer) Shutdown() {
	s.shutdownOnce.Do(func() {
		close(s.hwProbeStop)
		close(s.shutdownCh)
	})
}

func (s *IpcServer) Done() <-chan struct{} { return s.shutdownCh }

// ---- port list ----

func (s *IpcServer) loadPortDescsBg() {
	descs := loadPortDescriptions()
	s.portDescMu.Lock()
	s.portDescs = descs
	s.portDescMu.Unlock()
	s.reloadCache()
	// Always broadcast after descriptions load so clients re-fetch the full list.
	s.broadcastPortsChanged()
}

func (s *IpcServer) getPortDescs() map[string]string {
	s.portDescMu.RLock()
	descs := s.portDescs
	s.portDescMu.RUnlock()
	if descs == nil {
		return make(map[string]string)
	}
	return descs
}

func (s *IpcServer) buildPortList() []portInfo {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil
	}
	descs := s.getPortDescs()
	result := make([]portInfo, 0, len(ports))
	for _, p := range ports {
		desc := descs[p]
		if desc == "" {
			desc = p
		}
		result = append(result, portInfo{Port: p, Description: desc})
	}
	return result
}

func (s *IpcServer) reloadCache() {
	newPorts := s.buildPortList()
	s.portsMu.RLock()
	oldPorts := s.cachedPorts
	s.portsMu.RUnlock()
	changed := portsChanged(oldPorts, newPorts)
	s.portsMu.Lock()
	s.cachedPorts = newPorts
	s.portsMu.Unlock()
	if changed {
		s.broadcastPortsChanged()
	}
}

func portsChanged(a, b []portInfo) bool {
	if len(a) != len(b) {
		return true
	}
	set := make(map[string]bool, len(a))
	for _, p := range a {
		set[p.Port] = true
	}
	for _, p := range b {
		if !set[p.Port] {
			return true
		}
	}
	return false
}

func portsChangedStr(a, b []string) bool {
	if len(a) != len(b) {
		return true
	}
	set := make(map[string]bool, len(a))
	for _, p := range a {
		set[p] = true
	}
	for _, p := range b {
		if !set[p] {
			return true
		}
	}
	return false
}

func (s *IpcServer) startHwProbe() {
	go func() {
		ticker := time.NewTicker(s.hwProbeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ports, err := serial.GetPortsList()
				if err != nil {
					continue
				}
				last := s.hwProbeLastPorts
				if portsChangedStr(last, ports) {
					s.hwProbeLastPorts = ports
					s.reloadCache()
				}
			case <-s.hwProbeStop:
				return
			}
		}
	}()
}

// ---- broadcasting (via sub pipes) ----

func (s *IpcServer) broadcastDaemonShutdown() {
	s.broadcastFiltered("daemon-shutdown", map[string]any{"reason": "shutdown"})
}

func (s *IpcServer) broadcastPortsChanged() {
	s.broadcastFiltered("ports-changed", map[string]any{})
}

// trackCallerViewer adds the calling session as a viewer of a process (CLI/MCP on-demand).
func (s *IpcServer) trackCallerViewer(sess *clientSession, processId string) {
	if sess == nil {
		return
	}
	if sess.watchedProcesses == nil {
		sess.watchedProcesses = make(map[string]bool)
	}
	if !sess.watchedProcesses[processId] {
		sess.watchedProcesses[processId] = true
		s.pm.AddViewer(processId, sess.source)
	}
}

// autoWatchGUI makes all GUI sessions watch a newly created process.
func (s *IpcServer) autoWatchGUI(processId string) {
	s.sessMu.RLock()
	defer s.sessMu.RUnlock()
	for _, sess := range s.sessions {
		if sess.source == "gui" {
			if sess.watchedProcesses == nil {
				sess.watchedProcesses = make(map[string]bool)
			}
			if !sess.watchedProcesses[processId] {
				sess.watchedProcesses[processId] = true
				s.pm.AddViewer(processId, "gui")
			}
		}
	}
}

func (s *IpcServer) broadcastProcessChanged() {
	procs := s.pm.List()
	s.broadcastFiltered("process-changed", map[string]any{"processes": procs})
}

func (s *IpcServer) broadcastFiltered(eventName string, params any) {
	s.sessMu.RLock()
	defer s.sessMu.RUnlock()
	evt := protocol.Event{Event: eventName, Params: params}
	for _, sess := range s.sessions {
		if sess.subs[eventName] {
			sess.writeSub(evt)
		}
	}
}

func (s *IpcServer) broadcastEvent(msg RxTxMessage) {
	s.broadcastFiltered(msg.Direction, msg)
}

func (s *IpcServer) pushClientEvent(evt protocol.Event) {
	s.broadcastFiltered(evt.Event, evt.Params)
}

// ---- session management ----

func (s *IpcServer) addSession(sess *clientSession) {
	s.sessMu.Lock()
	// Remove any existing session from the same PID (stale entry left by
	// heartbeat-triggered reconnect). Without this, a brief window exists
	// where both old and new client entries coexist in the session map.
	if sess.pid > 0 {
		for cid, old := range s.sessions {
			if old.pid == sess.pid {
				delete(s.sessions, cid)
				old.daemonConn.Close()
				old.respConn.Close()
				old.subConn.Close()
				break
			}
		}
	}
	s.sessions[sess.clientId] = sess
	if sess.pid > 0 {
		s.knownPIDs[sess.pid] = sess.source
	}
	s.sessMu.Unlock()

	label := sourceLabel(sess.source)
	if sess.pid > 0 {
		logOp("连接", "%s:%s 已注册 (PID: %d)", label, sess.clientId, sess.pid)
	} else {
		logOp("连接", "%s:%s 已注册", label, sess.clientId)
	}
	s.broadcastClientsChanged()
}

func (s *IpcServer) removeSession(clientId string) {
	s.sessMu.Lock()
	sess, ok := s.sessions[clientId]
	if ok {
		delete(s.sessions, clientId)
		// Check if any remaining sessions share this PID
		hasOther := false
		if sess.pid > 0 {
			for _, o := range s.sessions {
				if o.pid == sess.pid {
					hasOther = true
					break
				}
			}
		}
		if !hasOther {
			delete(s.knownPIDs, sess.pid)
		}
	}
	n := len(s.sessions)
	s.sessMu.Unlock()

	if sess != nil {
		// Clean up viewer tracking: remove this session from all watched processes
		for pid := range sess.watchedProcesses {
			s.pm.RemoveViewer(pid, sess.source)
		}
		if len(sess.watchedProcesses) > 0 {
			s.broadcastProcessChanged()
		}
		label := sourceLabel(sess.source)
		logOp("连接", "%s:%s 已断开 (活跃会话: %d)", label, clientId, n)
		sess.daemonConn.Close()
		sess.respConn.Close()
		sess.subConn.Close()
	}
	s.broadcastClientsChanged()
}

// ---- main handler (dispatches register vs call-once) ----

func (s *IpcServer) Handle(conn io.ReadWriteCloser) {
	reader := bufio.NewReader(conn)

	// Read first message
	data, err := protocol.ReadMessage(reader)
	if err != nil {
		conn.Close()
		return
	}
	req, err := protocol.ParseRequest(data)
	if err != nil {
		protocol.WriteMessage(conn, protocol.Response{ID: 0, Error: "parse error: " + err.Error()})
		conn.Close()
		return
	}

	if req.Method == "register" {
		s.handleRegister(conn, reader, req)
	} else {
		s.handleCallOnce(conn, data, req)
	}
}

// handleRegister processes a REGISTER message, establishes 3-pipe session.
func (s *IpcServer) handleRegister(daemonConn io.ReadWriteCloser, reader *bufio.Reader, req *protocol.Request) {
	var p protocol.RegisterParams
	if err := convParams(req.Params, &p); err != nil {
		protocol.WriteMessage(daemonConn, protocol.Response{ID: req.ID, Error: "invalid register params: " + err.Error()})
		daemonConn.Close()
		return
	}

	// Dial client's resp and sub pipes (client created them, we connect)
	respConn, err := pipe.Dial(p.RespPipe)
	if err != nil {
		protocol.WriteMessage(daemonConn, protocol.Response{ID: req.ID, Error: "cannot connect resp pipe: " + err.Error()})
		daemonConn.Close()
		return
	}
	subConn, err := pipe.Dial(p.SubPipe)
	if err != nil {
		respConn.Close()
		protocol.WriteMessage(daemonConn, protocol.Response{ID: req.ID, Error: "cannot connect sub pipe: " + err.Error()})
		daemonConn.Close()
		return
	}

	pid, _ := pipe.ClientPID(daemonConn)

	sess := &clientSession{
		clientId:    p.ClientId,
		source:      p.Source,
		daemonConn:  daemonConn,
		respConn:    respConn,
		subConn:     subConn,
		pid:         pid,
		connectTime: time.Now(),
		subs:        make(map[string]bool),
	}
	for _, e := range p.Subscribe {
		sess.subs[e] = true
	}

	s.addSession(sess)

	// GUI auto-watches all existing processes
	if p.Source == "gui" {
		sess.watchedProcesses = make(map[string]bool)
		procs := s.pm.List()
		for _, proc := range procs {
			if pid, ok := proc["processId"].(string); ok {
				sess.watchedProcesses[pid] = true
				s.pm.AddViewer(pid, "gui")
			}
		}
	}

	// Send confirmation on daemon pipe
	protocol.WriteMessage(daemonConn, protocol.Response{ID: req.ID, Result: map[string]any{"registered": true, "clientId": p.ClientId}})

	// Push initial state after channel established
	s.pushPortListTo(sess)
	s.pushProcessListTo(sess)
	s.pushClientListTo(sess)

	// Enter request loop: read from daemonConn, respond on respConn
	for {
		data, err := protocol.ReadMessage(reader)
		if err != nil {
			s.removeSession(p.ClientId)
			return
		}
		r, err := protocol.ParseRequest(data)
		if err != nil {
			sess.writeResp(protocol.Response{ID: 0, Error: "parse error: " + err.Error()})
			continue
		}
		sess.reqCount++
		if sess.reqCount == 1 {
			label := sourceLabel(sess.source)
			logOp("连接", "%s:%s 开始通信 (PID: %d)", label, sess.clientId, sess.pid)
		}

		t0 := time.Now()
		resp := s.dispatchForSession(sess, r)
		dt := time.Since(t0)

		if err := sess.writeResp(resp); err != nil {
			s.removeSession(p.ClientId)
			return
		}
		logIPCForSession(sess, r, resp, dt)
	}
}

// handleCallOnce processes a single request-response on a temporary pipe.
func (s *IpcServer) handleCallOnce(conn io.ReadWriteCloser, firstData []byte, req *protocol.Request) {
	defer conn.Close()

	t0 := time.Now()
	resp := s.dispatchForSession(nil, req)
	dt := time.Since(t0)

	protocol.WriteMessage(conn, resp)

	src := req.Source
	if req.Method != "status" && req.Method != "ping" {
		label := sourceLabel(src)
		result := "ok"
		if resp.Error != "" {
			result = "err: " + resp.Error
		}
		logOp("IPC", "%s call-once %s → %s (%v)", label, req.Method, result, dt.Round(time.Microsecond))
	}
}

func (s *IpcServer) pushPortListTo(sess *clientSession) {
	if !sess.subs["ports-list"] {
		return
	}
	s.portsMu.RLock()
	ports := s.cachedPorts
	s.portsMu.RUnlock()
	evt := protocol.Event{Event: "ports-list", Params: map[string]any{"ports": ports}}
	sess.writeSub(evt)
}

func (s *IpcServer) pushProcessListTo(sess *clientSession) {
	if !sess.subs["process-changed"] {
		return
	}
	evt := protocol.Event{Event: "process-changed", Params: map[string]any{"processes": s.pm.List()}}
	sess.writeSub(evt)
}

func (s *IpcServer) pushSubscribedState(sess *clientSession, events []any) {
	for _, e := range events {
		if es, ok := e.(string); ok {
			s.pushStateFor(sess, es)
		}
	}
}

func (s *IpcServer) pushAllSubscribedState(sess *clientSession) {
	for e := range sess.subs {
		s.pushStateFor(sess, e)
	}
}

func (s *IpcServer) pushStateFor(sess *clientSession, event string) {
	switch event {
	case "ports-list", "ports-changed":
		s.pushPortListTo(sess)
	case "process-changed":
		s.pushProcessListTo(sess)
	case "stats-count":
		s.pushStatsCountTo(sess)
	case "clients-changed":
		s.pushClientListTo(sess)
	}
}

func (s *IpcServer) broadcastClientsChanged() {
	s.sessMu.RLock()
	clients := make([]map[string]any, 0, len(s.sessions))
	for _, cs := range s.sessions {
		subList := make([]string, 0, len(cs.subs))
		for e := range cs.subs {
			subList = append(subList, e)
		}
		clients = append(clients, map[string]any{
			"clientId":    cs.clientId,
			"source":      cs.source,
			"pid":         cs.pid,
			"connectTime": cs.connectTime.Format("15:04:05"),
			"reqCount":    cs.reqCount,
			"subs":        subList,
		})
	}
	s.sessMu.RUnlock()
	s.broadcastFiltered("clients-changed", map[string]any{"clients": clients})
}

func (s *IpcServer) pushClientListTo(sess *clientSession) {
	if !sess.subs["clients-changed"] {
		return
	}
	s.sessMu.RLock()
	defer s.sessMu.RUnlock()
	clients := make([]map[string]any, 0, len(s.sessions))
	for _, cs := range s.sessions {
		subList := make([]string, 0, len(cs.subs))
		for e := range cs.subs {
			subList = append(subList, e)
		}
		clients = append(clients, map[string]any{
			"clientId":    cs.clientId,
			"source":      cs.source,
			"pid":         cs.pid,
			"connectTime": cs.connectTime.Format("15:04:05"),
			"reqCount":    cs.reqCount,
			"subs":        subList,
		})
	}
	sess.writeSub(protocol.Event{
		Event:  "clients-changed",
		Params: map[string]any{"clients": clients},
	})
}

func (s *IpcServer) pushStatsCountTo(sess *clientSession) {
	if !sess.subs["stats-count"] {
		return
	}
	s.pm.mu.RLock()
	defer s.pm.mu.RUnlock()
	for _, p := range s.pm.processes {
		if p.status == "connected" {
			sess.writeSub(protocol.Event{
				Event: "stats-count",
				Params: map[string]any{
					"processId":    p.id,
					"bytesRead":    p.bytesRead.Load(),
					"bytesWritten": p.bytesWritten.Load(),
					"bytesPort1":   p.bytesPort1.Load(),
					"bytesPort2":   p.bytesPort2.Load(),
				},
			})
		}
	}
}

func sourceLabel(s string) string {
	switch s {
	case "gui":
		return "GUI"
	case "cli":
		return "CLI"
	case "mcp":
		return "MCP"
	default:
		return s
	}
}

func convParams(params map[string]any, v any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// ---- dispatch (session-aware) ----

func (s *IpcServer) dispatchForSession(sess *clientSession, req *protocol.Request) protocol.Response {
	src := req.Source
	if sess != nil {
		src = sess.source
	}
	label := sourceLabel(src)

	switch req.Method {
	case "register":
		return protocol.Response{ID: req.ID, Error: "already registered"}

	case "subscribe":
		events, _ := req.Params["events"].([]any)
		if sess != nil {
			for _, e := range events {
				if es, ok := e.(string); ok {
					sess.subs[es] = true
				}
			}
			// Push immediate state: specific events if provided, all subscribed if empty
			if len(events) == 0 {
				s.pushAllSubscribedState(sess)
			} else {
				s.pushSubscribedState(sess, events)
			}
			return protocol.Response{ID: req.ID, Result: map[string]any{"subscribed": len(sess.subs)}}
		}
		return protocol.Response{ID: req.ID, Error: "subscribe requires persistent session"}

	case "ping":
		return protocol.Response{ID: req.ID, Result: map[string]string{"pong": "ok"}}

	case "status":
		return protocol.Response{ID: req.ID, Result: map[string]string{"status": "ok"}}

	case "ports.refresh":
		s.reloadCache()
		logOp("操作", "%s 刷新串口列表", label)
		fallthrough
	case "ports":
		s.portsMu.RLock()
		ports := s.cachedPorts
		s.portsMu.RUnlock()
		return protocol.Response{ID: req.ID, Result: map[string]any{"ports": ports}}

	case "ports.probe":
		return s.handlePortsProbe(req, label)

	case "process.list":
		return protocol.Response{ID: req.ID, Result: map[string]any{"processes": s.pm.List()}}

	case "client.list":
		s.sessMu.RLock()
		clients := make([]map[string]any, 0, len(s.sessions))
		for _, sess := range s.sessions {
			subList := make([]string, 0, len(sess.subs))
			for e := range sess.subs {
				subList = append(subList, e)
			}
			clients = append(clients, map[string]any{
				"clientId":    sess.clientId,
				"source":      sess.source,
				"pid":         sess.pid,
				"connectTime": sess.connectTime.Format("15:04:05"),
				"reqCount":    sess.reqCount,
				"subs":        subList,
			})
		}
		s.sessMu.RUnlock()
		return protocol.Response{ID: req.ID, Result: map[string]any{"clients": clients}}

	case "threads":
		logOp("操作", "%s 查看线程详情", label)
		return protocol.Response{ID: req.ID, Result: map[string]any{
			"goroutines": s.pm.GoroutineCount(),
			"sessions":   s.pm.ListConnected(),
		}}

	case "goroutines":
		logOp("操作", "%s 查看调用栈", label)
		return protocol.Response{ID: req.ID, Result: map[string]any{
			"goroutines": s.pm.GoroutineCount(),
			"stack":      s.pm.GoroutineStacks(),
		}}

	case "process.create":
		mode, _ := req.Params["mode"].(string)
		if mode == "" {
			mode = "single"
		}
		connect := true
		if v, ok := req.Params["connect"]; ok {
			if b, ok := v.(bool); ok {
				connect = b
			}
		}
		if mode == "forward" {
			var p struct {
				Port       string `json:"port"`
				Baud       int    `json:"baud"`
				DataBits   int    `json:"dataBits"`
				StopBits   string `json:"stopBits"`
				Parity     string `json:"parity"`
				PortB      string `json:"portB"`
				BaudB      int    `json:"baudB"`
				DataBitsB  int    `json:"dataBitsB"`
				StopBitsB  string `json:"stopBitsB"`
				ParityB    string `json:"parityB"`
			}
			if err := convParams(req.Params, &p); err != nil {
				return protocol.Response{ID: req.ID, Error: "invalid params: " + err.Error()}
			}
			if p.Baud <= 0 { p.Baud = 115200 }
			if p.DataBits <= 0 { p.DataBits = 8 }
			if p.StopBits == "" { p.StopBits = "1" }
			if p.Parity == "" { p.Parity = "none" }
			if p.BaudB <= 0 { p.BaudB = 115200 }
			if p.DataBitsB <= 0 { p.DataBitsB = 8 }
			if p.StopBitsB == "" { p.StopBitsB = "1" }
			if p.ParityB == "" { p.ParityB = "none" }
			cfgA := SerialConfig{Port: p.Port, Baud: p.Baud, DataBits: p.DataBits, StopBits: p.StopBits, Parity: p.Parity}
			cfgB := SerialConfig{Port: p.PortB, Baud: p.BaudB, DataBits: p.DataBitsB, StopBits: p.StopBitsB, Parity: p.ParityB}
			proc, err := s.pm.Create("forward", p.Port, cfgA, &cfgB, connect)
			if err != nil {
				logOp("错误", "%s 创建转发进程失败: %v", label, err)
				return protocol.Response{ID: req.ID, Error: err.Error()}
			}
			s.autoWatchGUI(proc.id)
			s.trackCallerViewer(sess, proc.id)
			connected := proc.status == "connected"
			if connected {
				logOp("串口", "%s 创建转发 %s ↔ %s → #%s", label, p.Port, p.PortB, proc.id)
			} else {
				logOp("操作", "%s 声明转发 %s ↔ %s → #%s", label, p.Port, p.PortB, proc.id)
			}
			return protocol.Response{ID: req.ID, Result: map[string]any{
				"processId": proc.id,
				"connected": connected,
				"success":   true,
			}}
		}
		// mode == "single"
		var cfg SerialConfig
		if err := convParams(req.Params, &cfg); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params: " + err.Error()}
		}
		proc, err := s.pm.Create("single", cfg.Port, cfg, nil, connect)
		if err != nil {
			logOp("错误", "%s 创建进程失败: %v", label, err)
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		s.autoWatchGUI(proc.id)
		s.trackCallerViewer(sess, proc.id)
		connected := proc.status == "connected"
		if connected {
			logOp("串口", "%s 打开 %s @ %d baud — 进程 #%s, 当前 %d 个进程 (%d 已连接)",
				label, cfg.Port, cfg.Baud, proc.id, s.pm.Count(), s.pm.ConnectedCount())
		} else if cfg.Port != "" {
			logOp("操作", "%s 声明 %s @ %d baud → #%s, 当前 %d 个进程",
				label, cfg.Port, cfg.Baud, proc.id, s.pm.Count())
		} else {
			logOp("操作", "%s 创建空闲进程 #%s, 当前 %d 个进程", label, proc.id, s.pm.Count())
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{
			"processId": proc.id,
			"connected": connected,
			"success":   true,
		}}

	case "process.destroy":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		proc := s.pm.Get(p.ProcessID)
		portName := ""
		if proc != nil && proc.status == "connected" {
			portName = proc.config.Port
		}
		if err := s.pm.Destroy(p.ProcessID); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		if portName != "" {
			logOp("串口", "%s 关闭 %s, 剩余 %d 个进程 (%d 已连接)",
				label, portName, s.pm.Count(), s.pm.ConnectedCount())
		} else {
			logOp("操作", "%s 销毁进程 #%s", label, p.ProcessID)
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "process.connect":
		var p struct {
			ProcessID string `json:"processId"`
			Port      string `json:"port"`
			Baud      int    `json:"baud"`
			DataBits  int    `json:"dataBits"`
			StopBits  string `json:"stopBits"`
			Parity    string `json:"parity"`
			PortB     string `json:"portB"`
			BaudB     int    `json:"baudB"`
			DataBitsB int    `json:"dataBitsB"`
			StopBitsB string `json:"stopBitsB"`
			ParityB   string `json:"parityB"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if p.PortB != "" {
			// Forward mode: connect both ports
			if p.Baud <= 0 { p.Baud = 115200 }
			if p.DataBits <= 0 { p.DataBits = 8 }
			if p.StopBits == "" { p.StopBits = "1" }
			if p.Parity == "" { p.Parity = "none" }
			if p.BaudB <= 0 { p.BaudB = 115200 }
			if p.DataBitsB <= 0 { p.DataBitsB = 8 }
			if p.StopBitsB == "" { p.StopBitsB = "1" }
			if p.ParityB == "" { p.ParityB = "none" }
			cfgA := SerialConfig{Port: p.Port, Baud: p.Baud, DataBits: p.DataBits, StopBits: p.StopBits, Parity: p.Parity}
			cfgB := SerialConfig{Port: p.PortB, Baud: p.BaudB, DataBits: p.DataBitsB, StopBits: p.StopBitsB, Parity: p.ParityB}
			if err := s.pm.ConnectForward(p.ProcessID, cfgA, cfgB); err != nil {
				return protocol.Response{ID: req.ID, Error: err.Error()}
			}
			s.trackCallerViewer(sess, p.ProcessID)
			logOp("串口", "%s 连接转发 #%s → %s ↔ %s", label, p.ProcessID, p.Port, p.PortB)
		} else {
			cfg := SerialConfig{
				Port: p.Port, Baud: p.Baud, DataBits: p.DataBits,
				StopBits: p.StopBits, Parity: p.Parity,
			}
			if err := s.pm.Connect(p.ProcessID, p.Port, cfg); err != nil {
				return protocol.Response{ID: req.ID, Error: err.Error()}
			}
			s.trackCallerViewer(sess, p.ProcessID)
			logOp("串口", "%s 连接进程 #%s → %s @ %d", label, p.ProcessID, p.Port, p.Baud)
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "process.disconnect":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if err := s.pm.Disconnect(p.ProcessID); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		logOp("操作", "%s 断开进程 #%s", label, p.ProcessID)
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "process.switch":
		var p struct {
			ProcessID string `json:"processId"`
			Port      string `json:"port"`
			Baud      int    `json:"baud"`
			DataBits  int    `json:"dataBits"`
			StopBits  string `json:"stopBits"`
			Parity    string `json:"parity"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		cfg := SerialConfig{
			Port: p.Port, Baud: p.Baud, DataBits: p.DataBits,
			StopBits: p.StopBits, Parity: p.Parity,
		}
		if err := s.pm.SwitchPort(p.ProcessID, p.Port, cfg); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		logOp("串口", "%s 切换进程 #%s → %s @ %d", label, p.ProcessID, p.Port, p.Baud)
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "process.setmode":
		var p struct {
			ProcessID string `json:"processId"`
			Mode      string `json:"mode"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if err := s.pm.SetMode(p.ProcessID, p.Mode); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		logOp("操作", "%s 设置进程 #%s 模式 → %s", label, p.ProcessID, p.Mode)
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "process.watch":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil || p.ProcessID == "" {
			return protocol.Response{ID: req.ID, Error: "invalid params: processId required"}
		}
		if sess != nil {
			// Add to watched set (additive, no unwatch)
			if sess.watchedProcesses == nil {
				sess.watchedProcesses = make(map[string]bool)
			}
			if !sess.watchedProcesses[p.ProcessID] {
				sess.watchedProcesses[p.ProcessID] = true
				s.pm.AddViewer(p.ProcessID, sess.source)
			}
		}
		s.broadcastProcessChanged()
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "process.unwatch":
		var pu struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &pu); err != nil || pu.ProcessID == "" {
			return protocol.Response{ID: req.ID, Error: "invalid params: processId required"}
		}
		if sess != nil && sess.watchedProcesses != nil && sess.watchedProcesses[pu.ProcessID] {
			delete(sess.watchedProcesses, pu.ProcessID)
			s.pm.RemoveViewer(pu.ProcessID, sess.source)
			s.broadcastProcessChanged()
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "process.watched":
		var watched []string
		if sess != nil {
			for pid := range sess.watchedProcesses {
				watched = append(watched, pid)
			}
		}
		if watched == nil {
			watched = []string{}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"processIds": watched}}

	case "forward.create":
		var p struct {
			PortA     string `json:"portA"`
			BaudA     int    `json:"baudA"`
			DataBitsA int    `json:"dataBitsA"`
			StopBitsA string `json:"stopBitsA"`
			ParityA   string `json:"parityA"`
			PortB     string `json:"portB"`
			BaudB     int    `json:"baudB"`
			DataBitsB int    `json:"dataBitsB"`
			StopBitsB string `json:"stopBitsB"`
			ParityB   string `json:"parityB"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if p.BaudA <= 0 { p.BaudA = 115200 }
		if p.DataBitsA <= 0 { p.DataBitsA = 8 }
		if p.StopBitsA == "" { p.StopBitsA = "1" }
		if p.ParityA == "" { p.ParityA = "none" }
		if p.BaudB <= 0 { p.BaudB = 115200 }
		if p.DataBitsB <= 0 { p.DataBitsB = 8 }
		if p.StopBitsB == "" { p.StopBitsB = "1" }
		if p.ParityB == "" { p.ParityB = "none" }
		cfgA := SerialConfig{Port: p.PortA, Baud: p.BaudA, DataBits: p.DataBitsA, StopBits: p.StopBitsA, Parity: p.ParityA}
		cfgB := SerialConfig{Port: p.PortB, Baud: p.BaudB, DataBits: p.DataBitsB, StopBits: p.StopBitsB, Parity: p.ParityB}
		proc, err := s.pm.ForwardCreate(cfgA, cfgB)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		logOp("串口", "%s 创建转发 %s \u2194 %s \u2192 #%s", label, p.PortA, p.PortB, proc.id)
		return protocol.Response{ID: req.ID, Result: map[string]any{"processId": proc.id, "success": true}}

	case "session.send":
		var p struct {
			ProcessID string `json:"processId"`
			Data      string `json:"data"`
			Format    string `json:"format"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if err := s.pm.Send(p.ProcessID, p.Data, p.Format); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		s.trackCallerViewer(sess, p.ProcessID)
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "session.clearhistory":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if err := s.pm.ClearHistory(p.ProcessID); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "session.history":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		result := s.pm.GetHistory(p.ProcessID)
		if result == nil {
			return protocol.Response{ID: req.ID, Error: "process not found"}
		}
		logOp("操作", "%s 读取历史记录 (进程 %s)", label, p.ProcessID)
		return protocol.Response{ID: req.ID, Result: result}

	case "session.stats":
		var ps struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &ps); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		stats, err := s.pm.GetStats(ps.ProcessID)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"stats": stats}}

	case "send.trigger":
		var p struct {
			ProcessID string `json:"processId"`
			Raw       bool   `json:"raw"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if err := s.pm.SendTrigger(p.ProcessID, p.Raw); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "autosend.start":
		var p struct {
			ProcessID  string `json:"processId"`
			IntervalMs int    `json:"intervalMs"`
			Mode       string `json:"mode"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if p.Mode == "" {
			p.Mode = "single"
		}
		if p.IntervalMs <= 0 {
			p.IntervalMs = 1000
		}
		loop := false
		if v, ok := req.Params["loop"]; ok {
			if b, ok := v.(bool); ok {
				loop = b
			}
		}
		if err := s.pm.AutoSendStart(p.ProcessID, p.IntervalMs, p.Mode, loop); err != nil {
			logOp("错误", "%s 启动自动发送失败: %v", label, err)
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		logOp("自动发送", "%s 启动自动发送 进程#%s 间隔%dms 模式=%s loop=%v", label, p.ProcessID, p.IntervalMs, p.Mode, loop)
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "autosend.stop":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if err := s.pm.AutoSendStop(p.ProcessID); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		logOp("自动发送", "%s 停止自动发送 进程#%s", label, p.ProcessID)
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "autosend.status":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		status, err := s.pm.AutoSendStatus(p.ProcessID)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"status": status}}

	case "autosend.interval":
		var p struct {
			ProcessID  string `json:"processId"`
			IntervalMs int    `json:"intervalMs"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if err := s.pm.AutoSendSetInterval(p.ProcessID, p.IntervalMs); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "send.ringname":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		name := s.pm.GetSendRingName(p.ProcessID)
		if name == "" {
			return protocol.Response{ID: req.ID, Error: "process not found"}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"ringName": name}}

	case "multistr.save":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if err := s.pm.MultistrSave(p.ProcessID); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "multistr.load":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		entries, err := s.pm.MultistrLoad(p.ProcessID)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		if entries == nil {
			entries = []MultistrEntry{}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"entries": entries}}

	case "multistr.reload":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		entries, err := s.pm.MultistrReload(p.ProcessID)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		if entries == nil {
			entries = []MultistrEntry{}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"entries": entries}}

	case "multistr.read":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		entries, err := s.pm.MultistrReadEntries(p.ProcessID)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		if entries == nil {
			entries = []MultistrEntry{}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"entries": entries}}

	case "multistr.write":
		var p struct {
			ProcessID string           `json:"processId"`
			Entries   []MultistrEntry  `json:"entries"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params: " + err.Error()}
		}
		if err := s.pm.MultistrWriteEntries(p.ProcessID, p.Entries); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "shutdown":
		logOp("关闭", "%s 请求关闭守护进程", label)
		// 1. Disconnect all serial ports then destroy processes
		s.pm.DestroyAll()
		// 2. Broadcast shutdown 3 times consecutively
		for i := 0; i < 3; i++ {
			s.broadcastDaemonShutdown()
		}
		// 3. Close channels and exit immediately
		s.Shutdown()
		go func() {
			time.Sleep(100 * time.Millisecond)
			os.Exit(0)
		}()
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "history.files":
		files, err := listHistoryFiles(historyDir())
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		if files == nil {
			files = []HistoryFileInfo{}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{"files": files}}

	case "history.search":
		var p struct {
			File    string `json:"file"`
			Keyword string `json:"keyword"`
			Limit   int    `json:"limit"`
			Offset  int64  `json:"offset"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		if p.Limit <= 0 {
			p.Limit = 100
		}
		path := filepath.Join(historyDir(), filepath.Base(p.File))
		results, next, err := searchHistoryFile(path, p.Keyword, p.Limit, p.Offset)
		if err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		return protocol.Response{ID: req.ID, Result: map[string]any{
			"results":   results,
			"nextOffset": next,
			"hasMore":   len(results) == p.Limit,
		}}

	case "history.enable":
		var p struct {
			Enabled bool `json:"enabled"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		setHistoryEnabled(p.Enabled)
		logOp("操作", "%s 历史自动保存: %v", label, p.Enabled)
		return protocol.Response{ID: req.ID, Result: map[string]any{
			"enabled": p.Enabled,
		}}

	case "history.status":
		return protocol.Response{ID: req.ID, Result: map[string]any{
			"enabled": isHistoryEnabled(),
		}}

	case "history.attach":
		var p struct {
			ProcessID string `json:"processId"`
			File      string `json:"file"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		proc := s.pm.Get(p.ProcessID)
		if proc == nil {
			return protocol.Response{ID: req.ID, Error: "process not found"}
		}
		if err := proc.attachHistoryFile(p.File); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		logOp("操作", "%s 进程 #%s 已附加历史文件 %s", label, p.ProcessID, p.File)
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "history.new":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		proc := s.pm.Get(p.ProcessID)
		if proc == nil {
			return protocol.Response{ID: req.ID, Error: "process not found"}
		}
		if err := proc.newHistoryFile(); err != nil {
			return protocol.Response{ID: req.ID, Error: err.Error()}
		}
		logOp("操作", "%s 进程 #%s 新建历史文件", label, p.ProcessID)
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	case "history.detach":
		var p struct {
			ProcessID string `json:"processId"`
		}
		if err := convParams(req.Params, &p); err != nil {
			return protocol.Response{ID: req.ID, Error: "invalid params"}
		}
		proc := s.pm.Get(p.ProcessID)
		if proc == nil {
			return protocol.Response{ID: req.ID, Error: "process not found"}
		}
		closeHistoryFile(proc.historyFile)
		proc.historyFile = nil
		logOp("操作", "%s 进程 #%s 已分离历史文件", label, p.ProcessID)
		return protocol.Response{ID: req.ID, Result: map[string]any{"success": true}}

	default:
		return protocol.Response{ID: req.ID, Error: "unknown method: " + req.Method}
	}
}

func (s *IpcServer) handlePortsProbe(req *protocol.Request, label string) protocol.Response {
	var params struct {
		Ports      []string `json:"ports"`
		BaudRates  []int    `json:"baudRates"`
		Rules      []string `json:"rules"`
		ConfigPath string   `json:"configPath"`
	}
	if err := convParams(req.Params, &params); err != nil {
		return protocol.Response{ID: req.ID, Error: "invalid params: " + err.Error()}
	}

	configPath, err := findProbeConfig(params.ConfigPath)
	if err != nil {
		return protocol.Response{ID: req.ID, Error: err.Error()}
	}

	cfg, err := LoadProbeConfig(configPath)
	if err != nil {
		return protocol.Response{ID: req.ID, Error: "加载探针配置失败: " + err.Error()}
	}

	// 收集已占用端口
	occupied := make(map[string]bool)
	s.pm.mu.RLock()
	for port := range s.pm.portMap {
		occupied[port] = true
	}
	s.pm.mu.RUnlock()

	// 获取要探测的端口列表
	ports := params.Ports
	if len(ports) == 0 {
		list, err := serial.GetPortsList()
		if err != nil {
			return protocol.Response{ID: req.ID, Error: "获取端口列表失败: " + err.Error()}
		}
		ports = list
	}

	logOp("操作", "%s 开始设备探测 (端口: %d, 规则: %s)", label, len(ports), configPath)

	results := ProbePorts(ports, occupied, cfg, params.BaudRates, params.Rules)
	return protocol.Response{ID: req.ID, Result: map[string]any{"results": results}}
}

func logIPCForSession(sess *clientSession, req *protocol.Request, resp protocol.Response, dt time.Duration) {
	if req.Method == "status" || req.Method == "ping" ||
		req.Method == "session.send" || req.Method == "send.trigger" ||
		req.Method == "autosend.status" || req.Method == "ports.probe" {
		return
	}
	label := sourceLabel(sess.source)
	result := "ok"
	if resp.Error != "" {
		result = "err: " + resp.Error
	}
	summary := req.Method
	switch req.Method {
	case "process.create":
		port, _ := req.Params["port"].(string)
		baud, _ := req.Params["baud"].(float64)
		if port != "" {
			if resp.Result != nil {
				if rid, ok := resp.Result.(map[string]any)["processId"]; ok {
					summary = fmt.Sprintf("create %s@%d → #%s", port, int(baud), rid)
				}
			}
		} else {
			if resp.Result != nil {
				if rid, ok := resp.Result.(map[string]any)["processId"]; ok {
					summary = fmt.Sprintf("create idle → #%s", rid)
				}
			}
		}
	case "process.destroy":
		if pid, ok := req.Params["processId"]; ok {
			summary = fmt.Sprintf("destroy #%v", pid)
		}
	case "process.connect":
		port, _ := req.Params["port"].(string)
		pid, _ := req.Params["processId"].(string)
		summary = fmt.Sprintf("connect #%s → %s", pid, port)
	case "process.disconnect":
		if pid, ok := req.Params["processId"]; ok {
			summary = fmt.Sprintf("disconnect #%v", pid)
		}
	case "process.switch":
		if pid, ok := req.Params["processId"]; ok {
			port, _ := req.Params["port"].(string)
			summary = fmt.Sprintf("switch #%v → %s", pid, port)
		}
	case "session.send":
		fmtStr, _ := req.Params["format"].(string)
		data, _ := req.Params["data"].(string)
		size := len(data)
		if fmtStr == "hex" {
			size = len(data) / 2
		}
		pid, _ := req.Params["processId"]
		summary = fmt.Sprintf("send #%v %s %dB", pid, fmtStr, size)
	case "session.history":
		pid, _ := req.Params["processId"]
		summary = fmt.Sprintf("history #%v", pid)
	case "ports.refresh":
		summary = "refresh ports"
	case "shutdown":
		summary = "shutdown"
	}
	logOp("IPC", "%s:%s r%d %s → %s (%v)",
		label, sess.clientId, sess.reqCount, summary, result, dt.Round(time.Microsecond))
}
