package client

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// earlyResult 是注册握手期间 daemonConn 上第一条消息的读取结果。
//
// 用「发送结果」而不是「关闭通道」表达失败是有意的：关闭通道会让 select
// 立刻返回，从而与诊断/清理动作竞争，掩盖真实错误。
type earlyResult struct {
	msg *protocol.RawMsg
	err error
}

// NewDaemonClient creates a 3-pipe persistent connection to the daemon.
func NewDaemonClient(source string) (*DaemonClient, error) {
	return NewDaemonClientWithEvents(source, nil)
}

// handshakeRetries 是注册握手失败后的重试次数（不含首次）。
//
// 为什么需要：在**机器有负载**时建立会话，注册握手会以可观概率失败。实测同一台
// i7-3632QM 上：空闲时串行 300 次仅 1 次（0.3%），而制造并发负载后 8 路并行
// × 60 轮 = 42/480（8.8%）。外部测试机在 Agent 持续占用下测得 6.3%~11%。
//
// 现场证据是矛盾的：守护进程侧显示该会话**已完成注册**（`addSession` 位于两条
// 回连管道都成功之后，且无「写注册确认失败」记录、会话也未被驱逐），客户端侧
// 却在读 `daemonConn` 时报错，于是报「注册未完成」。指向 pipe 层在负载下的
// 句柄/缓冲竞态，尚未定位。
//
// 注意失败在**时间上相关**：外部报告显示 3 次尝试后失败率仅从 7.8% 降到 6.3%，
// 若三次独立则应为 0.08% 量级——说明失败会成串发生（同一个持续状态下的几次
// 尝试一起失败）。因此重试只是缓解，真正解决办法仍待定位根因。
const handshakeRetries = 2

// NewDaemonClientWithEvents creates a 3-pipe client with a custom event subscription list.
//
// 只有**握手阶段**的失败才重试（见 handshakeRetries 的说明）；连不上守护进程
// 这类确定性失败立即返回，重试没有意义、还会拖慢并让错误文案失真
// （曾出现「注册握手失败（已尝试 3 次）: connect to daemon: pipe not available」，
// 而实际情况只是守护进程没运行）。
func NewDaemonClientWithEvents(source string, subscribe []string) (*DaemonClient, error) {
	var lastErr error
	for attempt := 0; attempt <= handshakeRetries; attempt++ {
		c, err := connectOnce(source, subscribe)
		if err == nil {
			return c, nil
		}
		lastErr = err
		if errors.Is(err, errConnectPhase) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("注册握手失败（已尝试 %d 次）: %w", handshakeRetries+1, lastErr)
}

// errConnectPhase 标记「还没走到握手阶段就失败了」——例如守护进程未运行、
// 回调管道建不出来。这类失败是确定性的，重试无益。
var errConnectPhase = errors.New("connect phase")

// connectError 是连接阶段的失败：errors.Is 能把它认成 errConnectPhase，
// 但 Error() 只返回真实原因——不把内部标记露给用户。
type connectError struct{ cause error }

func (e *connectError) Error() string { return e.cause.Error() }
func (e *connectError) Unwrap() error { return e.cause }
func (e *connectError) Is(target error) bool {
	// 让 errors.Is(err, errConnectPhase) 成立，同时不污染 Error()。
	// Unwrap 指回 cause，所以 errConnectPhase 必须由这里匹配。
	return target == errConnectPhase
}

// connectErr 把连接阶段的失败包成可分类、且文案干净的错误。
func connectErr(cause error) error { return &connectError{cause: cause} }

func connectOnce(source string, subscribe []string) (*DaemonClient, error) {
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
		return nil, connectErr(err)
	}
	subLn, err := pipe.Listen(subName)
	if err != nil {
		respLn.Close()
		return nil, connectErr(err)
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
		return nil, connectErr(err)
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
		return nil, connectErr(err)
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
	earlyCh := make(chan earlyResult, 1)
	go func() {
		data, err := bufio.NewReader(daemonConn).ReadBytes('\n')
		if err != nil {
			earlyCh <- earlyResult{err: err}
			return
		}
		var msg protocol.RawMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			earlyCh <- earlyResult{err: err}
			return
		}
		earlyCh <- earlyResult{msg: &msg}
	}()

	// reasonFromMsg 把 daemonConn 上收到的第一条消息翻译成失败原因。
	//
	// 关键：成功注册的确认 **不是失败**。守护进程在两条回连管道都成功之后才写
	// 这条确认（daemon/ipc.go:532，位于 addSession 之后），所以收到它就说明
	// 登记已完成——不能因为「respCh 还没就绪」就把它当失败。
	reasonFromMsg := func(msg *protocol.RawMsg) (string, bool) {
		if msg == nil {
			return "守护进程已接受注册，但注册确认未到达（连接在注册过程中被关闭）；" +
				"守护进程日志里该会话会表现为「已注册」后直接「已断开」", true
		}
		if msg.Error != "" {
			return msg.Error, true
		}
		return "", false
	}

	var respConn, subConn io.ReadWriteCloser
	var earlyRes earlyResult
	earlyDone := false

	// 统一的截止时间：必须让两条管道共享同一个 handshakeTimeout，不能在
	// respCh 分支里再开一个新的超时窗口，否则最坏情况要等 2×handshakeTimeout
	// （也会让「握手失败」的重试代价翻倍）。
	deadline := time.After(handshakeTimeout)

	for respConn == nil || subConn == nil {
		// 两条管道都已就绪 —— 成功。可能还剩下 earlyCh 的读取结果没取，
		// 交给调用方在返回前消费（见握手成功后的收尾）。
		if respConn != nil && subConn != nil {
			break
		}
		select {
		case c := <-respCh:
			respConn = c
		case c := <-subCh:
			subConn = c
		case err := <-respErrCh:
			daemonConn.Close()
			if subConn != nil {
				subConn.Close()
			}
			respLn.Close()
			subLn.Close()
			return nil, fmt.Errorf("resp pipe: %w", err)
		case err := <-subErrCh:
			daemonConn.Close()
			if respConn != nil {
				respConn.Close()
			}
			respLn.Close()
			subLn.Close()
			return nil, fmt.Errorf("sub pipe: %w", err)
		case r := <-earlyCh:
			earlyRes, earlyDone = r, true
			// 只有**错误**才中断握手。成功确认只说明守护进程已登记；
			// resp/sub 还在路上，必须继续等它们。
			if reason, failed := reasonFromMsg(r.msg); failed {
				daemonConn.Close()
				if respConn != nil {
					respConn.Close()
				}
				if subConn != nil {
					subConn.Close()
				}
				respLn.Close()
				subLn.Close()
				return nil, fmt.Errorf("注册未完成: %s", reason)
			}
			// 成功确认：无失败原因，继续等在途的 resp / sub 管道。
		case <-deadline:
			// 超时：把至今掌握的信息（含 earlyCh 上的结论）拼成可读错误。
			var missing []string
			if respConn == nil {
				missing = append(missing, "resp")
			}
			if subConn == nil {
				missing = append(missing, "sub")
			}
			// 非阻塞地收一下 earlyCh：可能刚好在这时到达。
			if !earlyDone {
				select {
				case r := <-earlyCh:
					earlyRes, earlyDone = r, true
				default:
				}
			}
			reason := fmt.Sprintf("守护进程未在 %v 内回连 %s 管道",
				handshakeTimeout, strings.Join(missing, " / "))
			if earlyDone {
				if e, failed := reasonFromMsg(earlyRes.msg); failed {
					reason = "注册未完成: " + e
				} else {
					reason = fmt.Sprintf(
						"守护进程已确认注册，但 %s 管道未在 %v 内就绪（本地 Accept 未完成）",
						strings.Join(missing, " / "), handshakeTimeout)
				}
			}
			daemonConn.Close()
			if respConn != nil {
				respConn.Close()
			}
			if subConn != nil {
				subConn.Close()
			}
			respLn.Close()
			subLn.Close()
			return nil, fmt.Errorf("%s", reason)
		}
	}

	// 握手成功：确认 daemonConn 上那条注册响应已被消费。
	// 它可能还没被 earlyCh 的协程读出来（守护进程写得晚），
	// 此时若直接进入会话期，该协程会与 readRespLoop 争用这条管道。
	if !earlyDone {
		select {
		case <-earlyCh:
		case <-time.After(100 * time.Millisecond):
		}
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
		// 老守护进程回的是整数协议号（旧版是 1），解码成字符串会失败。
		// 这不是「解析坏了」，而是两边代际不同，直接说清楚怎么办。
		return fmt.Errorf("读取守护进程协议号失败（可能是旧版守护进程，本客户端要求 IPC %s）: %w。请执行 serial-cli start 重启守护进程",
			protocol.ProtocolVersion, err)
	}
	if !info.Compatible() {
		return fmt.Errorf("IPC 协议不兼容：客户端 %s，守护进程 %s（协议 %s）。请执行 serial-cli start 重启守护进程",
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

// 心跳参数：间隔 2s、单次超时 1.5s、连续 3 次失败判掉线。
//
// 为什么能比原来的 5s 激进：正常关闭是守护进程主动广播 daemon-shutdown
// （即时生效），心跳只负责「意外退出」。最坏发现时间 ≈ 2s×3 + 1.5s ≈ 7.5s，
// 原来是 5s×3 + 10s ≈ 25s。
//
// 但间隔不能再短：守护进程处理大 history 请求或落盘 flush 时会有百毫秒级停顿，
// 而误判掉线的代价是清空所有会话页（app.js 的 handleDaemonOffline）。
const (
	heartbeatInterval = 2 * time.Second
	heartbeatTimeout  = 1500 * time.Millisecond
)

// startHeartbeat 每 2s ping 一次；连续 3 次失败判连接已死，回调 OnDisconnect。
func (c *DaemonClient) startHeartbeat() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ticker.C:
			_, err := c.callWithTimeout(contract.Ping, nil, heartbeatTimeout)
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
	return c.callWithTimeout(method, params, requestTimeoutFor(method))
}

// callWithTimeout 是 Call 的可指定超时版本。
//
// 心跳必须用它：通用超时是 10s，而心跳间隔只有 2s —— 一次 ping 卡满 10s，
// ticker 会连着丢好多个 tick，判定掉线的时间被拖到几十秒，比原来的 5s×3 还糟。
func (c *DaemonClient) callWithTimeout(method contract.Method, params map[string]any, timeout time.Duration) (map[string]any, error) {
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
	case <-time.After(timeout):
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, fmt.Errorf("request timeout: %s", method)
	case <-c.done:
		// 连接已经被 Close（心跳判死、StopDaemon、shutdown）。不在这里立刻返回
		// 的话，调用方要白等满整个超时 —— GUI 清理会话页、CLI 交互模式重连都会
		// 各卡 10 秒。
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, fmt.Errorf("connection closed")
	}
}

// requestTimeoutFor 返回某个方法允许等待的最长时间。
//
// 默认 10s；ports.probe 单独放宽：它在守护进程侧有总预算（默认 12s）加上打开
// 串口等开销，10s 的通用超时曾经先于守护进程放弃——客户端一走，守护进程还在
// 扫，那段窗口里端口被占，后续任何探测都会得到空结果（与「没有设备」同形）。
// 这里给足余量，确保「客户端放弃」不再先于「守护进程给出结论」。
func requestTimeoutFor(method contract.Method) time.Duration {
	if method == contract.PortsProbe {
		return 25 * time.Second
	}
	return 10 * time.Second
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

// Alive 用一次短 ping 判断这条连接还活着没有。
//
// CLI 交互模式用它决定要不要重连：整个 shell 会话只建一次连接，daemon 重启后
// 旧连接会死（心跳判死或对端关闭），靠这个判断自动恢复，不用退出重进。
// 超时给 1.5s（与心跳一致）：daemon 处理大 history 请求时会有百毫秒级停顿，
// 太短会误判；误判只是多建一次连接，代价很低但没必要。
func (c *DaemonClient) Alive(timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	_, err := c.callWithTimeout(contract.Ping, nil, timeout)
	return err == nil
}

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

// ProbeOutcome 是一次设备探测的结果。
//
// 关键：Results 为空**不等于**没有设备。Skipped 列出「本轮压根没探成」的端口
// 及原因（被会话占用 / 串口打不开 / 读不了 / 超出总预算没轮到），
// BudgetExhausted 表示到点即停、后面还有端口或波特率没试。此前这些情况
// 与「探测完成但没有规则命中」返回同一个空结果，会把在线设备报成「未检测到」。
type ProbeOutcome struct {
	Results         []map[string]any
	Skipped         []map[string]any
	Attempts        int
	ElapsedMs       int64
	BudgetExhausted bool
	Busy            bool
}

// ProbePorts triggers device probing on specified ports (or all available).
// budgetMs > 0 覆盖守护进程侧的总预算（默认 12s）。
func (c *DaemonClient) ProbePorts(ports []string, baudRates []int, rules []string, configPath string, budgetMs int) (*ProbeOutcome, error) {
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
	if budgetMs > 0 {
		params["budgetMs"] = budgetMs
	}
	result, err := c.Call(contract.PortsProbe, params)
	if err != nil {
		return nil, err
	}
	return probeOutcomeFromMap(result), nil
}

// probeOutcomeFromMap 把守护进程返回的原始 map 转成 ProbeOutcome。
func probeOutcomeFromMap(result map[string]any) *ProbeOutcome {
	out := &ProbeOutcome{}
	out.Results = mapSlice(result["results"])
	out.Skipped = mapSlice(result["skipped"])
	if v, ok := result["attempts"].(float64); ok {
		out.Attempts = int(v)
	}
	if v, ok := result["elapsedMs"].(float64); ok {
		out.ElapsedMs = int64(v)
	}
	out.BudgetExhausted, _ = result["budgetExhausted"].(bool)
	out.Busy, _ = result["busy"].(bool)
	return out
}

func mapSlice(v any) []map[string]any {
	items, _ := v.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
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
// entries 用 any 而不是 []map[string]any：调用方各自的条目类型不同
// （CLI 是做过严格校验的 queueEntry 结构体），只要可 JSON 序列化即可。
// 写死成 []map[string]any 会逼调用方做一次多余的形状转换，还会丢掉
// delay/note 这类字段。
func (c *DaemonClient) MultistrWrite(processId string, entries any) error {
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

// SetHistoryEnabled 修改「新进程默认是否把历史写入磁盘」。
//
// 已有的进程如果没被单独设置过会跟着变（老语义），显式按进程设置过的保持原样。
func (c *DaemonClient) SetHistoryEnabled(enabled bool) error {
	_, err := c.Call(contract.HistoryEnable, map[string]any{"enabled": enabled})
	return err
}

// GetHistoryStatus 返回新进程的默认落盘开关。
func (c *DaemonClient) GetHistoryStatus() (bool, error) {
	result, err := c.Call(contract.HistoryStatus, nil)
	if err != nil {
		return false, err
	}
	v, _ := result["enabled"].(bool)
	return v, nil
}

// SetHistoryEnabledFor 按进程（GUI 里就是「这个标签」）开关历史落盘。
//
// 开启会立即创建一个新的 history/*.log；关闭会关掉当前文件（已写入的内容保留）。
func (c *DaemonClient) SetHistoryEnabledFor(processID string, enabled bool) error {
	_, err := c.Call(contract.HistoryEnable, map[string]any{
		"enabled":   enabled,
		"processId": processID,
	})
	return err
}

// GetHistoryStatusFor 返回单个进程实际生效的落盘开关。
func (c *DaemonClient) GetHistoryStatusFor(processID string) (bool, error) {
	result, err := c.Call(contract.HistoryStatus, map[string]any{"processId": processID})
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
		// 同 pipe.dialPipe：不能断言「daemon not running」——连接失败也可能是
		// 端点残留、权限、或对端刚退出。让底层错误自己说话，别把方向带偏。
		return nil, err
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
	if errors.Is(err, protocol.ErrProtocolDecode) {
		// 守护进程在跑、也答了 daemon.info，但协议号解析不出来 —— 老版本（整数协议号）。
		// 这条必须走「重启」，不能落到 startDaemonProcess：旧实例还占着 Global 互斥体，
		// 新实例会以「已在运行」退出，变成静默失败。
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
	info, err := protocol.DecodeDaemonInfo(res)
	if err != nil {
		// 与 verifyProtocol 保持同一句人话：解码失败最常见的成因是「对端是老版本，
		// 协议号还是整数 1」，光打印 json 报错看不出该做什么。
		return protocol.DaemonInfo{}, fmt.Errorf("读取守护进程协议号失败（可能是旧版守护进程，本客户端要求 IPC %s）: %w。请执行 serial-cli start 重启守护进程",
			protocol.ProtocolVersion, err)
	}
	return info, nil
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

// WaitDaemonReady 阻塞到守护进程端点可连接为止（超时返回 false）。
//
// GUI 用它代替「每秒试连一次」：守护进程一就绪就能立刻连上，不必等下一次轮询。
func WaitDaemonReady(timeout time.Duration) bool {
	return pipe.WaitReady(timeout)
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

	// 等 OS 级「管道可用」这个就绪信号，而不是每 100ms 盲发一次 Status。
	// 这也修掉了「进程已经起来、管道还没建好就判失败」的竞态。
	if !pipe.WaitReady(6 * time.Second) {
		return "", fmt.Errorf("daemon started but its pipe never became ready (6s)")
	}
	// 就绪后再确认一次能真正通信；协议版本不匹配由 StartDaemon 的 DaemonInfo 负责。
	for i := 0; i < 20; i++ {
		if _, err := CallOnce(contract.Status, nil, "cli"); err == nil {
			return "started", nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "", fmt.Errorf("daemon pipe ready but not responsive")
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
