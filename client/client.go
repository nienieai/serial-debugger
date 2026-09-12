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
	"strings"
	"sync"
	"time"

	"github.com/nienieai/serial-debugger/contract"
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

// handshakeTimeout 是等待守护进程回连各条管道的时间上限。
//
// 授权上是一个常量；做成变量是为了让测试能用很短的值，从而区分
// 「读了 daemonConn 拿到真实原因」与「一直干等到超时」两条路径。
var handshakeTimeout = 5 * time.Second

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
	// 端点名交给 pipe 包按平台构造：Windows 是命名管道路径，
	// 类 Unix 是文件系统套接字路径（写死 Windows 前缀会在 Linux 上
	// 于当前工作目录造出怪名字的套接字文件）。
	respName := pipe.Endpoint("st-" + clientId + "-resp")
	subName := pipe.Endpoint("st-" + clientId + "-sub")

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
		"method": contract.Register,
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
	//
	// 同时监听 daemonConn 上的第一条响应：守护进程回连客户端管道失败时，
	// 会把原因（如「连接客户端 resp 管道失败」）写在这条管道上
	// （daemon/ipc.go handleRegister 的三处 WriteMessage 错误分支）。
	// 此前客户端只等 respCh，从不读 daemonConn，于是真实原因被丢弃，
	// 5 秒后用户只看到一句没有信息量的超时。
	//
	// 成功注册时守护进程同样会回一条 {"registered":true}（ipc.go:532），
	// 这里把它消费掉，顺带当作「守护进程已接受注册」的确认。
	earlyCh := make(chan *protocol.RawMsg, 1)
	go func() {
		data, err := bufio.NewReader(daemonConn).ReadBytes('\n')
		if err != nil {
			close(earlyCh)
			return
		}
		var msg protocol.RawMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			close(earlyCh)
			return
		}
		earlyCh <- &msg
	}()

	// reasonFromMsg 把 daemonConn 上收到的第一条消息翻译成失败原因。
	reasonFromMsg := func(msg *protocol.RawMsg) (string, bool) {
		if msg == nil {
			return "守护进程在注册过程中关闭了连接", true
		}
		if msg.Error != "" {
			return msg.Error, true
		}
		// 成功的注册确认（{"registered":true}）不是失败。
		return "", false
	}

	// failFast 把 daemonConn 上的「提前失败」变成一条可读的错误。
	//
	// 单独监听它（而不是只在超时分支里顺手取一下）是必要的：守护进程回连失败时
	// 客户端要等的那条 resp/sub 管道**永远不会来**，所以只等超时的话，即使原因
	// 在几百微秒内就已到达，用户仍要干等满 5 秒才看到它。实测：原因是 0.5ms
	// 到达的，而连接要 2s（测试用超时）才断开。
	failFast := func() (string, bool) {
		select {
		case msg, ok := <-earlyCh:
			if !ok {
				return reasonFromMsg(nil)
			}
			return reasonFromMsg(msg)
		default:
			return "", false
		}
	}

	var respConn, subConn io.ReadWriteCloser
	select {
	case respConn = <-respCh:
	case err := <-respErrCh:
		daemonConn.Close()
		subLn.Close()
		return nil, fmt.Errorf("resp pipe: %w", err)
	case msg, ok := <-earlyCh:
		reason := "守护进程拒绝了注册请求"
		if !ok {
			reason = "守护进程在注册过程中关闭了连接"
		} else if r, failed := reasonFromMsg(msg); failed {
			reason = r
		}
		daemonConn.Close()
		respLn.Close()
		subLn.Close()
		return nil, fmt.Errorf("守护进程回连 resp 管道失败: %s", reason)
	case <-time.After(handshakeTimeout):
		reason := "守护进程未在 5 秒内回连 resp 管道"
		if e, ok := failFast(); ok {
			reason = "守护进程回连 resp 管道失败: " + e
		}
		daemonConn.Close()
		respLn.Close()
		subLn.Close()
		return nil, fmt.Errorf("%s", reason)
	}
	select {
	case subConn = <-subCh:
	case err := <-subErrCh:
		daemonConn.Close()
		respConn.Close()
		subLn.Close()
		return nil, fmt.Errorf("sub pipe: %w", err)
	case <-time.After(handshakeTimeout):
		reason := "守护进程未在 5 秒内回连 sub 管道"
		if e, ok := failFast(); ok {
			reason = "守护进程回连 sub 管道失败: " + e
		}
		daemonConn.Close()
		respConn.Close()
		subLn.Close()
		return nil, fmt.Errorf("%s", reason)
	}

	// 握手成功：确认 daemonConn 上那条注册响应已被消费。
	// 正常情况下上面那个 goroutine 早已把它放进 earlyCh；这里只在它尚未
	// 完成时收取，避免遗留协程在会话期间与后续写入争用这条管道。
	select {
	case <-earlyCh:
	default:
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

	// 建立会话后立刻核对协议版本。守护进程是机器级单例，很可能是另一次
	// 构建留下的；不核对就会带着不一致的方法集/数据格式继续工作。
	if err := c.verifyProtocol(); err != nil {
		c.Close()
		return nil, err
	}

	return c, nil
}

// verifyProtocol 确认对端守护进程与本客户端使用同一 IPC 协议版本。
func (c *DaemonClient) verifyProtocol() error {
	res, err := c.Call(contract.DaemonInfo, nil)
	if err != nil {
		if strings.HasPrefix(err.Error(), protocol.UnknownMethodPrefix) {
			return fmt.Errorf("守护进程版本过旧（不支持 %s）。请执行 serial-cli shutdown 后重试，或重启串口调试工具", "daemon.info")
		}
		return fmt.Errorf("读取守护进程版本失败: %w", err)
	}
	info, err := protocol.DecodeDaemonInfo(res)
	if err != nil {
		return fmt.Errorf("解析守护进程版本失败: %w", err)
	}
	if !info.Compatible() {
		return fmt.Errorf("IPC 协议版本不匹配：客户端为 %d，守护进程为 %s（协议 %d）。请执行 serial-cli shutdown 后重试",
			protocol.ProtocolVersion, info.Version, info.ProtocolVersion)
	}
	return nil
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
			_, err := c.Call(contract.Ping, nil)
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
func (c *DaemonClient) Call(method contract.Method, params map[string]any) (map[string]any, error) {
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
	_, err := c.Call(contract.Subscribe, map[string]any{"events": events})
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
	_, err := c.Call(contract.SendTrigger, map[string]any{"processId": processId, "raw": raw})
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
	result, err := c.Call(contract.SendRingName, map[string]any{"processId": processId})
	if err != nil {
		return "", err
	}
	name, _ := result["ringName"].(string)
	return name, nil
}

// ForwardCreate creates a port forwarding process between two serial ports.
func (c *DaemonClient) ForwardCreate(portA string, baudA int, portB string, baudB int) (map[string]any, error) {
	return c.Call(contract.ProcessCreate, map[string]any{
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
	return c.Call(contract.ProcessCreate, map[string]any{
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
	return c.Call(contract.ProcessCreate, map[string]any{
		"mode":      "forward",
		"port":      portA,
		"baud":      baudA,
		"dataBits":  dataBitsA,
		"stopBits":  stopBitsA,
		"parity":    parityA,
		"portB":     portB,
		"baudB":     baudB,
		"dataBitsB": dataBitsB,
		"stopBitsB": stopBitsB,
		"parityB":   parityB,
		"connect":   false,
	})
}

// WatchProcess declares that this client is viewing a specific process.
// The daemon tracks per-process viewer counts and broadcasts changes.
func (c *DaemonClient) WatchProcess(processId string) error {
	_, err := c.Call(contract.ProcessWatch, map[string]any{"processId": processId})
	return err
}

// UnwatchProcess stops watching a process (decrements viewer count).
func (c *DaemonClient) UnwatchProcess(processId string) error {
	_, err := c.Call(contract.ProcessUnwatch, map[string]any{"processId": processId})
	return err
}

// GetWatchedProcesses returns the list of process IDs this client is watching.
func (c *DaemonClient) GetWatchedProcesses() ([]string, error) {
	resp, err := c.Call(contract.ProcessWatched, nil)
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
	_, err := c.Call(contract.ProcessSetMode, map[string]any{"processId": processId, "mode": mode})
	return err
}

// SwitchPort switches a connected process to a different serial port.
func (c *DaemonClient) SwitchPort(processId string, port string, cfg map[string]any) error {
	params := map[string]any{"processId": processId, "port": port}
	if v, ok := cfg["baud"]; ok {
		params["baud"] = v
	}
	if v, ok := cfg["dataBits"]; ok {
		params["dataBits"] = v
	}
	if v, ok := cfg["stopBits"]; ok {
		params["stopBits"] = v
	}
	if v, ok := cfg["parity"]; ok {
		params["parity"] = v
	}
	_, err := c.Call(contract.ProcessSwitch, params)
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
	result, err := c.Call(contract.PortsProbe, params)
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
	_, err := c.Call(contract.AutosendStart, map[string]any{
		"processId":  processId,
		"intervalMs": intervalMs,
		"mode":       mode,
		"loop":       loop,
	})
	return err
}

// AutoSendStop stops auto-send on a process.
func (c *DaemonClient) AutoSendStop(processId string) error {
	_, err := c.Call(contract.AutosendStop, map[string]any{"processId": processId})
	return err
}

// AutoSendSetInterval updates the interval of a running auto-send.
func (c *DaemonClient) AutoSendSetInterval(processId string, intervalMs int) error {
	_, err := c.Call(contract.AutosendInterval, map[string]any{
		"processId":  processId,
		"intervalMs": intervalMs,
	})
	return err
}

// AutoSendStatus returns the auto-send status for a process.
func (c *DaemonClient) AutoSendStatus(processId string) (map[string]any, error) {
	result, err := c.Call(contract.AutosendStatus, map[string]any{"processId": processId})
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
	_, err := c.Call(contract.MultistrSave, map[string]any{"processId": processId})
	return err
}

// MultistrLoad tells the daemon to load entries from disk into sendq.
func (c *DaemonClient) MultistrLoad(processId string) ([]map[string]any, error) {
	result, err := c.Call(contract.MultistrLoad, map[string]any{"processId": processId})
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
	result, err := c.Call(contract.MultistrRead, map[string]any{"processId": processId})
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
	_, err := c.Call(contract.MultistrWrite, map[string]any{
		"processId": processId,
		"entries":   entries,
	})
	return err
}

// MultistrReload tells the daemon to re-read entries from sendq into cache.
func (c *DaemonClient) MultistrReload(processId string) error {
	_, err := c.Call(contract.MultistrReload, map[string]any{"processId": processId})
	return err
}

// ListHistoryFiles returns metadata for all .log files in the daemon's history directory.
func (c *DaemonClient) ListHistoryFiles() ([]map[string]any, error) {
	result, err := c.Call(contract.HistoryFiles, nil)
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
	return c.Call(contract.HistorySearch, map[string]any{
		"file":    file,
		"keyword": keyword,
		"limit":   limit,
		"offset":  offset,
	})
}

// SetHistoryEnabled toggles auto-save for history files.
func (c *DaemonClient) SetHistoryEnabled(enabled bool) error {
	_, err := c.Call(contract.HistoryEnable, map[string]any{"enabled": enabled})
	return err
}

// GetHistoryStatus returns whether history auto-save is enabled.
func (c *DaemonClient) GetHistoryStatus() (bool, error) {
	result, err := c.Call(contract.HistoryStatus, nil)
	if err != nil {
		return false, err
	}
	v, _ := result["enabled"].(bool)
	return v, nil
}

// AttachHistoryFile opens an existing history file for append and loads its
// content into the process ring buffer for display.
func (c *DaemonClient) AttachHistoryFile(processID, filename string) error {
	_, err := c.Call(contract.HistoryAttach, map[string]any{
		"processId": processID,
		"file":      filename,
	})
	return err
}

// NewHistoryFile creates a new history file for the process.
func (c *DaemonClient) NewHistoryFile(processID string) error {
	_, err := c.Call(contract.HistoryNew, map[string]any{"processId": processID})
	return err
}

// DetachHistoryFile closes the history file attached to the process.
func (c *DaemonClient) DetachHistoryFile(processID string) error {
	_, err := c.Call(contract.HistoryDetach, map[string]any{"processId": processID})
	return err
}

// ── one-shot (CLI CallOnce) ──

// CallOnce writes a request on a temporary pipe and reads the response,
// skipping events. The connection is closed after the response arrives.
func CallOnce(method contract.Method, params map[string]any, source string) (map[string]any, error) {
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

// StartDaemon starts the serial daemon if not already running, and additionally
// ensures the running daemon speaks this client's protocol version.
// Returns "started", "already_running" or "restarted".
func StartDaemon() (string, error) {
	info, err := DaemonInfo()
	if err == nil {
		if info.Compatible() {
			return "already_running", nil
		}
		// 版本不匹配：旧守护进程无法服务本客户端，直接换掉。
		if rerr := restartDaemon(); rerr != nil {
			return "", rerr
		}
		return "restarted", nil
	}
	if strings.HasPrefix(err.Error(), protocol.UnknownMethodPrefix) {
		// 旧版本守护进程不认识 daemon.info，同样需要换掉。
		if rerr := restartDaemon(); rerr != nil {
			return "", rerr
		}
		return "restarted", nil
	}
	return startDaemonProcess()
}

// DaemonInfo 通过一次性连接读取守护进程的身份与协议版本。
func DaemonInfo() (protocol.DaemonInfo, error) {
	res, err := CallOnce(contract.DaemonInfo, nil, "cli")
	if err != nil {
		return protocol.DaemonInfo{}, err
	}
	return protocol.DecodeDaemonInfo(res)
}

// restartDaemon 关闭正在运行的守护进程（可能是旧版本），等它真正退出后重启。
func restartDaemon() error {
	if _, err := CallOnce(contract.Shutdown, nil, "cli"); err != nil {
		return fmt.Errorf("关闭旧守护进程失败: %w", err)
	}
	waitDaemonExit(8 * time.Second)
	_, err := startDaemonProcess()
	return err
}

// waitDaemonExit 等待守护进程进程真正消失。
//
// 必须先等它退出：新实例靠 Global 互斥体判断单例，旧实例尚未释放锁时
// 新实例会直接以「已在运行」退出，重启就变成静默失败。
func waitDaemonExit(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsDaemonProcessRunning() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
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
		_, err := CallOnce(contract.Status, nil, "cli")
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
