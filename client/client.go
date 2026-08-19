package client

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/nienieai/serial-debugger/pipe"
	"github.com/nienieai/serial-debugger/protocol"
	"github.com/nienieai/serial-debugger/ringbuf"
)

// DaemonClient is a persistent 3-pipe client connected to the serial daemon.
//
//	Pipe 1: \\.\pipe\serial-tool-daemon           — client→daemon (requests, one-way)
//	Pipe 2: \\.\pipe\st-{clientId}-resp           — daemon→client (responses)
//	Pipe 3: \\.\pipe\st-{clientId}-sub            — daemon→client (events)
type DaemonClient struct {
	clientId string
	source   string

	daemonConn io.ReadWriteCloser
	wMu        sync.Mutex

	respLn   pipe.Listener
	respConn io.ReadWriteCloser

	subLn   pipe.Listener
	subConn io.ReadWriteCloser

	reqID   int64
	pendMu  sync.Mutex
	pending map[int64]chan *protocol.RawMsg

	events       chan *protocol.RawMsg
	done         chan struct{}
	closeOnce    sync.Once
	OnDisconnect func() // called when heartbeat detects connection lost
}

func generateClientId(source string) string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%s-%s", source, hex.EncodeToString(b))
}

// NewDaemonClient creates a 3-pipe persistent connection to the daemon.
func NewDaemonClient(source string) (*DaemonClient, error) {
	return NewDaemonClientWithEvents(source, nil)
}

// NewDaemonClientWithEvents creates a 3-pipe client with a custom event subscription list.
func NewDaemonClientWithEvents(source string, subscribe []string) (*DaemonClient, error) {
	if subscribe == nil {
		subscribe = defaultEvents()
	}

	clientId := generateClientId(source)
	respName := `\\.\pipe\st-` + clientId + `-resp`
	subName := `\\.\pipe\st-` + clientId + `-sub`

	// 1. Create listeners (client acts as pipe server for resp and sub)
	respLn, err := pipe.Listen(respName)
	if err != nil {
		return nil, fmt.Errorf("create resp pipe: %w", err)
	}
	subLn, err := pipe.Listen(subName)
	if err != nil {
		respLn.Close()
		return nil, fmt.Errorf("create sub pipe: %w", err)
	}

	// 2. Start Accept goroutines so pipes exist when daemon dials
	respCh := make(chan io.ReadWriteCloser, 1)
	subCh := make(chan io.ReadWriteCloser, 1)
	respErrCh := make(chan error, 1)
	subErrCh := make(chan error, 1)

	go func() {
		c, err := respLn.Accept()
		if err != nil {
			respErrCh <- err
			return
		}
		respCh <- c
	}()
	go func() {
		c, err := subLn.Accept()
		if err != nil {
			subErrCh <- err
			return
		}
		subCh <- c
	}()

	// 3. Connect to daemon pipe and register
	daemonConn, err := pipe.Dial(pipe.Addr)
	if err != nil {
		respLn.Close()
		subLn.Close()
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}

	regReq := map[string]any{
		"id":     0,
		"method": "register",
		"params": map[string]any{
			"clientId":  clientId,
			"source":    source,
			"subscribe": subscribe,
			"respPipe":  respName,
			"subPipe":   subName,
		},
	}
	data, _ := json.Marshal(regReq)
	data = append(data, '\n')
	if _, err := daemonConn.Write(data); err != nil {
		daemonConn.Close()
		respLn.Close()
		subLn.Close()
		return nil, fmt.Errorf("send register: %w", err)
	}

	// 4. Wait for daemon to connect back on resp and sub
	var respConn, subConn io.ReadWriteCloser
	select {
	case respConn = <-respCh:
	case err := <-respErrCh:
		daemonConn.Close()
		subLn.Close()
		return nil, fmt.Errorf("resp pipe: %w", err)
	case <-time.After(5 * time.Second):
		daemonConn.Close()
		respLn.Close()
		subLn.Close()
		return nil, fmt.Errorf("timeout waiting for daemon to connect resp pipe")
	}
	select {
	case subConn = <-subCh:
	case err := <-subErrCh:
		daemonConn.Close()
		respConn.Close()
		subLn.Close()
		return nil, fmt.Errorf("sub pipe: %w", err)
	case <-time.After(5 * time.Second):
		daemonConn.Close()
		respConn.Close()
		subLn.Close()
		return nil, fmt.Errorf("timeout waiting for daemon to connect sub pipe")
	}

	c := &DaemonClient{
		clientId:   clientId,
		source:     source,
		daemonConn: daemonConn,
		respLn:     respLn,
		respConn:   respConn,
		subLn:      subLn,
		subConn:    subConn,
		reqID:      1,
		pending:    make(map[int64]chan *protocol.RawMsg),
		events:     make(chan *protocol.RawMsg, 4096),
		done:       make(chan struct{}),
	}

	go c.readRespLoop()
	go c.readSubLoop()
	go c.startHeartbeat()

	return c, nil
}

func defaultEvents() []string {
	return []string{"rx", "tx", "1", "2", "ports-changed", "ports-list", "daemon-shutdown", "process-changed", "send-error", "stats-count", "stats-rate", "clients-changed"}
}

func (c *DaemonClient) readRespLoop() {
	reader := bufio.NewReader(c.respConn)
	for {
		data, err := reader.ReadBytes('\n')
		if err != nil {
			select {
			case <-c.done:
			default:
				if c.OnDisconnect != nil {
					go c.OnDisconnect()
				}
			}
			return
		}
		var msg protocol.RawMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		c.pendMu.Lock()
		ch := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.pendMu.Unlock()
		if ch != nil {
			select {
			case ch <- &msg:
			default:
			}
		}
	}
}

func (c *DaemonClient) readSubLoop() {
	defer close(c.events)
	reader := bufio.NewReader(c.subConn)
	for {
		data, err := reader.ReadBytes('\n')
		if err != nil {
			select {
			case <-c.done:
			default:
				if c.OnDisconnect != nil {
					go c.OnDisconnect()
				}
			}
			return
		}
		var msg protocol.RawMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		select {
		case c.events <- &msg:
		case <-c.done:
			return
		default:
		}
	}
}

// startHeartbeat sends a ping every 5 s. After 3 consecutive failures the
// connection is considered dead and OnDisconnect is called (if set).
func (c *DaemonClient) startHeartbeat() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ticker.C:
			_, err := c.Call("ping", nil)
			if err != nil {
				failures++
				if failures >= 3 {
					c.Close()
					if c.OnDisconnect != nil {
						c.OnDisconnect()
					}
					return
				}
			} else {
				failures = 0
			}
		case <-c.done:
			return
		}
	}
}

// Call sends a request on the daemon pipe and waits for the response on the resp pipe.
func (c *DaemonClient) Call(method string, params map[string]any) (map[string]any, error) {
	c.wMu.Lock()
	id := c.reqID
	c.reqID++
	ch := make(chan *protocol.RawMsg, 1)

	c.pendMu.Lock()
	c.pending[id] = ch
	c.pendMu.Unlock()

	req := map[string]any{
		"id": id, "method": method, "params": params,
		"clientId": c.clientId, "source": c.source,
	}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	_, err := c.daemonConn.Write(data)
	c.wMu.Unlock()

	if err != nil {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, err
	}

	select {
	case msg := <-ch:
		if msg.Error != "" {
			return nil, fmt.Errorf("%s", msg.Error)
		}
		return msg.Result, nil
	case <-time.After(10 * time.Second):
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, fmt.Errorf("request timeout: %s", method)
	}
}

// Subscribe updates the event subscription list.
func (c *DaemonClient) Subscribe(events []string) error {
	_, err := c.Call("subscribe", map[string]any{"events": events})
	return err
}

// ReadEvent returns the event channel (fed from sub pipe).
func (c *DaemonClient) ReadEvent() <-chan *protocol.RawMsg {
	return c.events
}

// ClientId returns the unique client identifier.
func (c *DaemonClient) ClientId() string { return c.clientId }

// Close shuts down all 3 pipes.
func (c *DaemonClient) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.daemonConn.Close()
		c.respConn.Close()
		c.subConn.Close()
		c.respLn.Close()
		c.subLn.Close()
	})
	return nil
}

// ── shared memory send ──

// SendWrite writes raw data into a named send-queue ring buffer.
func SendWrite(ringName string, data []byte) error {
	rb, err := ringbuf.OpenSharedRingForWrite(ringName)
	if err != nil {
		return fmt.Errorf("open send queue: %w", err)
	}
	defer rb.Close()

	if !rb.Write(data) {
		return fmt.Errorf("send queue full")
	}
	return nil
}

// SendTrigger tells the daemon to read one entry from the send queue
// and push it through sendCh (broadcast + history).
// When raw is true, data is sent as-is; otherwise multistr header decoding is applied.
func (c *DaemonClient) SendTrigger(processId string, raw bool) error {
	_, err := c.Call("send.trigger", map[string]any{"processId": processId, "raw": raw})
	return err
}

// SendViaShm writes data to the send-queue shared memory and triggers
// the daemon to send one entry. Convenience for single-shot sends.
func (c *DaemonClient) SendViaShm(processId string, data string, format string) error {
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

	ringName, err := c.SendRingName(processId)
	if err != nil {
		return err
	}

	if err := SendWrite(ringName, raw); err != nil {
		return err
	}

	return c.SendTrigger(processId, true)
}

// SendRingName returns the send queue shared memory name for a process.
func (c *DaemonClient) SendRingName(processId string) (string, error) {
	result, err := c.Call("send.ringname", map[string]any{"processId": processId})
	if err != nil {
		return "", err
	}
	name, _ := result["ringName"].(string)
	return name, nil
}

// ForwardCreate creates a port forwarding process between two serial ports.
func (c *DaemonClient) ForwardCreate(portA string, baudA int, portB string, baudB int) (map[string]any, error) {
	return c.Call("process.create", map[string]any{
		"mode": "forward",
		"port": portA, "baud": baudA,
		"portB": portB, "baudB": baudB,
	})
}

// Declare registers a serial port configuration with the daemon without opening
// the port. Other clients can see the declared config via process.list / events.
// Returns the assigned processId; the process is idle and stored config is used
// as defaults when connect is called later.
func (c *DaemonClient) Declare(port string, baud int, dataBits int, stopBits string, parity string) (map[string]any, error) {
	return c.Call("process.create", map[string]any{
		"port":     port,
		"baud":     baud,
		"dataBits": dataBits,
		"stopBits": stopBits,
		"parity":   parity,
		"connect":  false,
	})
}

// DeclareForward registers a forward port pair configuration without opening ports.
func (c *DaemonClient) DeclareForward(portA string, baudA int, dataBitsA int, stopBitsA string, parityA string, portB string, baudB int, dataBitsB int, stopBitsB string, parityB string) (map[string]any, error) {
	return c.Call("process.create", map[string]any{
		"mode":       "forward",
		"port":       portA,
		"baud":       baudA,
		"dataBits":   dataBitsA,
		"stopBits":   stopBitsA,
		"parity":     parityA,
		"portB":      portB,
		"baudB":      baudB,
		"dataBitsB":  dataBitsB,
		"stopBitsB":  stopBitsB,
		"parityB":    parityB,
		"connect":    false,
	})
}

// WatchProcess declares that this client is viewing a specific process.
// The daemon tracks per-process viewer counts and broadcasts changes.
func (c *DaemonClient) WatchProcess(processId string) error {
	_, err := c.Call("process.watch", map[string]any{"processId": processId})
	return err
}

// UnwatchProcess stops watching a process (decrements viewer count).
func (c *DaemonClient) UnwatchProcess(processId string) error {
	_, err := c.Call("process.unwatch", map[string]any{"processId": processId})
	return err
}

// GetWatchedProcesses returns the list of process IDs this client is watching.
func (c *DaemonClient) GetWatchedProcesses() ([]string, error) {
	resp, err := c.Call("process.watched", nil)
	if err != nil {
		return nil, err
	}
	if ids, ok := resp["processIds"].([]any); ok {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			if s, ok := id.(string); ok {
				out = append(out, s)
			}
		}
		return out, nil
	}
	return []string{}, nil
}

// SetMode switches the process mode between "single" and "forward".
// Process must be idle (all ports disconnected).
func (c *DaemonClient) SetMode(processId string, mode string) error {
	_, err := c.Call("process.setmode", map[string]any{"processId": processId, "mode": mode})
	return err
}

// SwitchPort switches a connected process to a different serial port.
func (c *DaemonClient) SwitchPort(processId string, port string, cfg map[string]any) error {
	params := map[string]any{"processId": processId, "port": port}
	if v, ok := cfg["baud"]; ok { params["baud"] = v }
	if v, ok := cfg["dataBits"]; ok { params["dataBits"] = v }
	if v, ok := cfg["stopBits"]; ok { params["stopBits"] = v }
	if v, ok := cfg["parity"]; ok { params["parity"] = v }
	_, err := c.Call("process.switch", params)
	return err
}

// ProbePorts triggers device probing on specified ports (or all available).
func (c *DaemonClient) ProbePorts(ports []string, baudRates []int, rules []string, configPath string) ([]map[string]any, error) {
	params := map[string]any{}
	if len(ports) > 0 {
		params["ports"] = ports
	}
	if len(baudRates) > 0 {
		params["baudRates"] = baudRates
	}
	if len(rules) > 0 {
		params["rules"] = rules
	}
	if configPath != "" {
		params["configPath"] = configPath
	}
	result, err := c.Call("ports.probe", params)
	if err != nil {
		return nil, err
	}
	results, _ := result["results"].([]any)
	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// AutoSendStart starts auto-send on a process.

func (c *DaemonClient) AutoSendStart(processId string, intervalMs int, mode string, loop bool) error {
	_, err := c.Call("autosend.start", map[string]any{
		"processId":  processId,
		"intervalMs": intervalMs,
		"mode":       mode,
		"loop":       loop,
	})
	return err
}

// AutoSendStop stops auto-send on a process.
func (c *DaemonClient) AutoSendStop(processId string) error {
	_, err := c.Call("autosend.stop", map[string]any{"processId": processId})
	return err
}

// AutoSendSetInterval updates the interval of a running auto-send.
func (c *DaemonClient) AutoSendSetInterval(processId string, intervalMs int) error {
	_, err := c.Call("autosend.interval", map[string]any{
		"processId":  processId,
		"intervalMs": intervalMs,
	})
	return err
}

// AutoSendStatus returns the auto-send status for a process.
func (c *DaemonClient) AutoSendStatus(processId string) (map[string]any, error) {
	result, err := c.Call("autosend.status", map[string]any{"processId": processId})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// AutoSendStartWithData writes entries to the send queue, then starts auto-send.
func (c *DaemonClient) AutoSendStartWithData(processId string, intervalMs int, mode string, entries [][]byte) error {
	ringName, err := c.SendRingName(processId)
	if err != nil {
		return err
	}

	rb, err := ringbuf.OpenSharedRingForWrite(ringName)
	if err != nil {
		return fmt.Errorf("open send queue: %w", err)
	}
	defer rb.Close()

	for _, entry := range entries {
		if !rb.Write(entry) {
			return fmt.Errorf("send queue full (wrote %d entries)", len(entries))
		}
	}

	return c.AutoSendStart(processId, intervalMs, mode, false)
}

// MultistrSave tells the daemon to persist current sendq entries to disk.
func (c *DaemonClient) MultistrSave(processId string) error {
	_, err := c.Call("multistr.save", map[string]any{"processId": processId})
	return err
}

// MultistrLoad tells the daemon to load entries from disk into sendq.
func (c *DaemonClient) MultistrLoad(processId string) ([]map[string]any, error) {
	result, err := c.Call("multistr.load", map[string]any{"processId": processId})
	if err != nil {
		return nil, err
	}
	entries, _ := result["entries"].([]any)
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// MultistrRead reads the current sendq entries from the daemon.
func (c *DaemonClient) MultistrRead(processId string) ([]map[string]any, error) {
	result, err := c.Call("multistr.read", map[string]any{"processId": processId})
	if err != nil {
		return nil, err
	}
	entries, _ := result["entries"].([]any)
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// MultistrWrite writes entries to the sendq via IPC.
func (c *DaemonClient) MultistrWrite(processId string, entries []map[string]any) error {
	_, err := c.Call("multistr.write", map[string]any{
		"processId": processId,
		"entries":   entries,
	})
	return err
}

// MultistrReload tells the daemon to re-read entries from sendq into cache.
func (c *DaemonClient) MultistrReload(processId string) error {
	_, err := c.Call("multistr.reload", map[string]any{"processId": processId})
	return err
}

// ListHistoryFiles returns metadata for all .log files in the daemon's history directory.
func (c *DaemonClient) ListHistoryFiles() ([]map[string]any, error) {
	result, err := c.Call("history.files", nil)
	if err != nil {
		return nil, err
	}
	files, _ := result["files"].([]any)
	out := make([]map[string]any, 0, len(files))
	for _, f := range files {
		if m, ok := f.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// SearchHistory searches a history file for entries matching the keyword.
func (c *DaemonClient) SearchHistory(file, keyword string, limit int, offset int64) (map[string]any, error) {
	return c.Call("history.search", map[string]any{
		"file":    file,
		"keyword": keyword,
		"limit":   limit,
		"offset":  offset,
	})
}

// SetHistoryEnabled toggles auto-save for history files.
func (c *DaemonClient) SetHistoryEnabled(enabled bool) error {
	_, err := c.Call("history.enable", map[string]any{"enabled": enabled})
	return err
}

// GetHistoryStatus returns whether history auto-save is enabled.
func (c *DaemonClient) GetHistoryStatus() (bool, error) {
	result, err := c.Call("history.status", nil)
	if err != nil {
		return false, err
	}
	v, _ := result["enabled"].(bool)
	return v, nil
}

// AttachHistoryFile opens an existing history file for append and loads its
// content into the process ring buffer for display.
func (c *DaemonClient) AttachHistoryFile(processID, filename string) error {
	_, err := c.Call("history.attach", map[string]any{
		"processId": processID,
		"file":      filename,
	})
	return err
}

// NewHistoryFile creates a new history file for the process.
func (c *DaemonClient) NewHistoryFile(processID string) error {
	_, err := c.Call("history.new", map[string]any{"processId": processID})
	return err
}

// DetachHistoryFile closes the history file attached to the process.
func (c *DaemonClient) DetachHistoryFile(processID string) error {
	_, err := c.Call("history.detach", map[string]any{"processId": processID})
	return err
}

// ── one-shot (CLI CallOnce) ──

// CallOnce writes a request on a temporary pipe and reads the response,
// skipping events. The connection is closed after the response arrives.
func CallOnce(method string, params map[string]any, source string) (map[string]any, error) {
	conn, err := pipe.Dial(pipe.Addr)
	if err != nil {
		return nil, fmt.Errorf("daemon not running: %v", err)
	}
	defer conn.Close()

	req := map[string]any{"id": 1, "method": method, "params": params, "source": source}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	for {
		respData, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("connection lost: %v", err)
		}
		var msg protocol.RawMsg
		if err := json.Unmarshal(respData, &msg); err != nil {
			continue
		}
		if msg.Event != "" {
			continue
		}
		if msg.Error != "" {
			return nil, fmt.Errorf("%s", msg.Error)
		}
		return msg.Result, nil
	}
}

// ── daemon lifecycle (shared by all clients) ──

// StartDaemon starts the serial daemon if not already running.
// Returns "started" if newly started, "already_running" if already up.
func StartDaemon() (string, error) {
	if IsDaemonProcessRunning() {
		_, err := CallOnce("status", nil, "cli")
		if err == nil {
			return "already_running", nil
		}
	}
	return startDaemonProcess()
}

func startDaemonProcess() (string, error) {
	exePath := findDaemonExe()
	if exePath == "" {
		return "", fmt.Errorf("daemon executable not found")
	}

	cmd := exec.Command(exePath, "--silent")
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start daemon: %w", err)
	}

	for i := 0; i < 60; i++ {
		time.Sleep(100 * time.Millisecond)
		_, err := CallOnce("status", nil, "cli")
		if err == nil {
			return "started", nil
		}
	}
	return "", fmt.Errorf("daemon started but not responsive after 6s")
}

func findDaemonExe() string {
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		p := filepath.Join(dir, "serial-daemon.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if wd, err := os.Getwd(); err == nil {
		p := filepath.Join(wd, "serial-daemon.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		p = filepath.Join(wd, "build", "bin", "serial-daemon.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
