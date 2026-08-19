package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.bug.st/serial"
	"serial-tool-v3/protocol"
	"serial-tool-v3/ringbuf"
)

// ---- types ----

type SerialConfig struct {
	Port        string `json:"port"`
	Baud        int    `json:"baud"`
	DataBits    int    `json:"dataBits"`
	StopBits    string `json:"stopBits"`
	Parity      string `json:"parity"`
	FlowControl string `json:"flowControl"`
}

type RxTxMessage struct {
	ProcessID string `json:"processId"`
	Data      string `json:"data"`
	Hex       string `json:"hex"`
	Direction string `json:"direction"`
	Timestamp string `json:"timestamp"`
}

type HistoryEntry struct {
	Timestamp string `json:"timestamp"`
	Hex       string `json:"hex"`
	Direction string `json:"direction"`
}

type SendErrorInfo struct {
	ProcessID string `json:"processId"`
	Error     string `json:"error"`
	Data      string `json:"data"`
	Timestamp string `json:"timestamp"`
}

type ProcessStats struct {
	ProcessID    string  `json:"processId"`
	StartTime    string  `json:"startTime"`
	UptimeSec    int64   `json:"uptimeSec"`
	BytesRead    int64   `json:"bytesRead"`
	BytesWritten int64   `json:"bytesWritten"`
	BytesPort1   int64   `json:"bytesPort1"`
	BytesPort2   int64   `json:"bytesPort2"`
	ReadRateBps  float64 `json:"readRateBps"`
	WriteRateBps float64 `json:"writeRateBps"`
	ReadErrors   int64   `json:"readErrors"`
	WriteErrors  int64   `json:"writeErrors"`
	LastSample   string  `json:"lastSample"`
}

// ---- send job ----

type sendJob struct {
	raw    []byte
	format string
}

// ---- stats sample ----

type statsSample struct {
	readRateBps  float64
	writeRateBps float64
	lastSample   time.Time
}

// ---- auto-send status ----

type AutoSendStatus struct {
	Enabled       bool   `json:"enabled"`
	Mode          string `json:"mode"`
	IntervalMs    int    `json:"intervalMs"`
	SendCount     int64  `json:"sendCount"`
	ErrorCount    int64  `json:"errorCount"`
	LastSend      string `json:"lastSend"`
	QueueCount    int32  `json:"queueCount"`
	LoopActive    bool   `json:"loopActive"`
	MultistrState string `json:"multistrState"`
	EntriesCount  int    `json:"entriesCount"`
	RoundCount    int64  `json:"roundCount"`
}

// ---- IPC client ----

type IpcClient interface {
	writeJSON(v any) error
}

// ---- process ----

type Process struct {
	id     string
	status string // "idle" | "connected"
	mode   string // "single" | "forward"
	config SerialConfig
	port   serial.Port

	historyRing     *ringbuf.RingBuffer // shared memory ring buffer
	historyName     string              // shared memory name for clients
	historyFile     *os.File            // persistent history log file
	historyFileName string              // current disk file name (for client paging)
	ringBufMu       sync.Mutex          // serializes writes to ring buffer
	stopCh      chan struct{}
	stopOnce    sync.Once // ensures stopIO runs only once

	sendCh chan sendJob

	bytesRead    atomic.Int64
	bytesWritten atomic.Int64
	bytesPort1   atomic.Int64 // bytes from port A in forward mode (A→B)
	bytesPort2   atomic.Int64 // bytes from port B in forward mode (B→A)
	readErrors   atomic.Int64
	writeErrors  atomic.Int64
	startTime    time.Time
	statsStop    chan struct{}
	statsSample  atomic.Value // *statsSample

	// send queue (shared memory)
	sendRing     *ringbuf.RingBuffer
	sendRingName string
	sendRingMu   sync.Mutex

	// auto-send
	autoSendEnabled    bool
	autoSendMode       string
	autoSendIntervalMs int
	autoSendStopCh     chan struct{}
	autoSendOnce       sync.Once
	autoSendSendCount  atomic.Int64
	autoSendErrorCount atomic.Int64
	autoSendLastSend   time.Time
	autoSendMu         sync.RWMutex

	// multi-string send (queue mode upgrade)
	multistrCache      []MultistrEntry // cached entries read from sendq
	multistrMu         sync.RWMutex
	multistrState      string // "idle" | "sending" | "looping"
	multistrLoopStopCh chan struct{}
	multistrLoopOnce   sync.Once
	multistrCurrIndex  int
	multistrRoundCount atomic.Int64
	multistrDirty      bool // true if sendq has been updated since last cache

	// serializes all port.Write calls (writeLoop + autoSendLoop)
	portWriteMu sync.Mutex

	// port forwarding
	forwardPortB    serial.Port
	forwardConfigB  SerialConfig
	forwardStopCh   chan struct{}
	forwardStopOnce sync.Once

	// per-source viewer counts: {"gui": N, "cli": N, "mcp": N}
	viewers map[string]int

	// callbacks set by startIO (used by autoSendLoop for broadcast + history)
	broadcastFn func(RxTxMessage)
	pushEventFn func(protocol.Event)
}

func (p *Process) viewerSummary() map[string]int {
	if p.viewers == nil {
		return map[string]int{"gui": 0, "cli": 0, "mcp": 0}
	}
	out := make(map[string]int, 3)
	for _, s := range []string{"gui", "cli", "mcp"} {
		out[s] = p.viewers[s]
	}
	return out
}

func (p *Process) recordHistory(hexData, direction string) {
	ts := time.Now().UnixMilli()
	// Serialize: | timestamp(8B) | direction(1B) | hexLen(2B) | hexData(N) |
	pktLen := 8 + 1 + 2 + len(hexData)
	pkt := make([]byte, pktLen)
	binary.LittleEndian.PutUint64(pkt[0:8], uint64(ts))
	pkt[8] = dirToByte(direction)
	binary.LittleEndian.PutUint16(pkt[9:11], uint16(len(hexData)))
	copy(pkt[11:], hexData)

	p.ringBufMu.Lock()
	if p.historyRing != nil {
		p.historyRing.Write(pkt)
	}
	p.ringBufMu.Unlock()

	// Lazy-create history file on first real data (not system events).
	if p.historyFile == nil && direction != "system" && isHistoryEnabled() {
		p.newHistoryFile()
	}
	if p.historyFile != nil {
		appendHistoryPacket(p.historyFile, pkt)
	}
}

func dirToByte(d string) byte {
	switch d {
	case "rx":
		return 1
	case "tx":
		return 2
	case "1":
		return 3
	case "2":
		return 4
	default:
		return 0 // system
	}
}

func byteToDir(b byte) string {
	switch b {
	case 1:
		return "rx"
	case 2:
		return "tx"
	case 3:
		return "1"
	case 4:
		return "2"
	default:
		return "system"
	}
}

// recordSystemEvent stores a system event in the history ring buffer.
// The message is stored as an i18n key + args (pipe-separated) so the
// frontend can translate it on display, supporting live language switching.
// Format: "sys.port_connected|COM13|115200"
func (p *Process) recordSystemEvent(key string, args ...any) {
	var s string
	if len(args) == 0 {
		s = key
	} else {
		var b strings.Builder
		b.WriteString(key)
		for _, a := range args {
			b.WriteByte('|')
			b.WriteString(fmt.Sprint(a))
		}
		s = b.String()
	}
	p.recordHistory(s, "system")
}

// writePortSilent writes raw bytes to the serial port without broadcasting
// events or recording history. Used by auto-send.
func (p *Process) writePortSilent(data []byte) error {
	p.portWriteMu.Lock()
	defer p.portWriteMu.Unlock()
	_, err := p.port.Write(data)
	return err
}

// startAutoSend starts the auto-send goroutine.
func (p *Process) startAutoSend(intervalMs int, mode string, loop bool) error {
	p.autoSendMu.Lock()
	defer p.autoSendMu.Unlock()

	if p.autoSendEnabled {
		return fmt.Errorf("auto-send already running")
	}
	if mode != "single" && mode != "queue" {
		return fmt.Errorf("invalid mode: %s (use single or queue)", mode)
	}
	if intervalMs < 5 {
		return fmt.Errorf("interval must be >= 5 ms")
	}

	p.autoSendEnabled = true
	p.autoSendMode = mode
	p.autoSendIntervalMs = intervalMs
	p.autoSendStopCh = make(chan struct{})
	p.autoSendOnce = sync.Once{}

	// For queue mode, load entries from sendq into cache before starting
	if mode == "queue" {
		p.multistrMu.Lock()
		p.multistrCache = ReadAllEntries(p.sendRing)
		p.multistrDirty = false
		if loop {
			p.multistrState = "looping"
		} else {
			p.multistrState = "sending"
		}
		p.multistrCurrIndex = 0
		p.multistrRoundCount.Store(0)
		p.multistrLoopStopCh = make(chan struct{})
		p.multistrLoopOnce = sync.Once{}
		p.multistrMu.Unlock()

		// Auto-persist
		SaveEntriesToFile(p.id, p.multistrCache)
	}

	go p.autoSendLoop()
	return nil
}

// stopAutoSend stops the auto-send goroutine.
func (p *Process) stopAutoSend() {
	p.autoSendOnce.Do(func() {
		p.autoSendMu.Lock()
		p.autoSendEnabled = false
		ch := p.autoSendStopCh
		p.autoSendMu.Unlock()
		if ch != nil {
			close(ch)
		}
	})
	// Also stop queue loop if running
	p.multistrLoopOnce.Do(func() {
		p.multistrMu.Lock()
		if p.multistrLoopStopCh != nil {
			close(p.multistrLoopStopCh)
		}
		p.multistrMu.Unlock()
	})
}

// getAutoSendStatus returns the current auto-send state.
func (p *Process) getAutoSendStatus() AutoSendStatus {
	p.autoSendMu.RLock()
	defer p.autoSendMu.RUnlock()

	lastSend := ""
	if !p.autoSendLastSend.IsZero() {
		lastSend = p.autoSendLastSend.Format("15:04:05.000")
	}

	queueCount := int32(0)
	if p.sendRing != nil {
		queueCount = int32(p.sendRing.UsedSpace())
	}

	p.multistrMu.RLock()
	entriesCount := len(p.multistrCache)
	state := p.multistrState
	roundCount := p.multistrRoundCount.Load()
	p.multistrMu.RUnlock()

	loopActive := p.autoSendEnabled && p.autoSendMode == "queue" && state == "looping"

	return AutoSendStatus{
		Enabled:       p.autoSendEnabled,
		Mode:          p.autoSendMode,
		IntervalMs:    p.autoSendIntervalMs,
		SendCount:     p.autoSendSendCount.Load(),
		ErrorCount:    p.autoSendErrorCount.Load(),
		LastSend:      lastSend,
		QueueCount:    queueCount,
		LoopActive:    loopActive,
		MultistrState: state,
		EntriesCount:  entriesCount,
		RoundCount:    roundCount,
	}
}

// autoSendLoop is the goroutine that drives periodic auto-send.
// single mode: fixed-rate repeat of one entry (unchanged).
// queue mode: iterate multistrCache entries, send enabled ones with per-entry delay.
func (p *Process) autoSendLoop() {
	nextWake := time.Now()
	var lastSent []byte

	// Check if we're in queue mode
	p.autoSendMu.RLock()
	mode := p.autoSendMode
	p.autoSendMu.RUnlock()

	if mode == "queue" {
		p.queueSendLoop()
		return
	}

	// ── single mode (unchanged) ──
	for {
		delay := time.Until(nextWake)
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)

		select {
		case <-timer.C:
			now := time.Now()
			p.autoSendMu.RLock()
			enabled := p.autoSendEnabled
			intervalMs := p.autoSendIntervalMs
			p.autoSendMu.RUnlock()

			nextWake = now.Add(time.Duration(intervalMs) * time.Millisecond)

			if !enabled {
				return
			}

			var data []byte
			var ok bool

			p.sendRingMu.Lock()
			// Drain all entries, keep only the LAST, re-write it
			var last []byte
			for {
				d, has := p.sendRing.Read(65535)
				if !has {
					break
				}
				last = d
			}
			if last != nil {
				// Extract content from multistr entry if needed
				content := DecodeEntryContentOnly(last)
				p.sendRing.Write(content)
				lastSent = content
				data = content
				ok = true
			} else if lastSent != nil {
				data = lastSent
				ok = true
			}
			p.sendRingMu.Unlock()

			if !ok {
				continue
			}

			if err := p.writePortSilent(data); err != nil {
				p.autoSendErrorCount.Add(1)
				hexData := strings.ToUpper(hex.EncodeToString(data))
				if p.pushEventFn != nil {
					p.pushEventFn(protocol.Event{
						Event: "send-error",
						Params: SendErrorInfo{
							ProcessID: p.id,
							Error:     err.Error(),
							Data:      hexData,
							Timestamp: time.Now().Format("15:04:05.000"),
						},
					})
				}
				continue
			}
			p.bytesWritten.Add(int64(len(data)))
			p.autoSendSendCount.Add(1)
			p.autoSendMu.Lock()
			p.autoSendLastSend = time.Now()
			p.autoSendMu.Unlock()

			hexData := strings.ToUpper(hex.EncodeToString(data))
			if p.broadcastFn != nil {
				p.broadcastFn(RxTxMessage{
					ProcessID: p.id,
					Data:      string(data),
					Hex:       hexData,
					Direction: "tx",
					Timestamp: time.Now().Format("15:04:05.000"),
				})
			}
			p.recordHistory(hexData, "tx")
			if p.pushEventFn != nil {
				p.pushEventFn(protocol.Event{
					Event: "stats-count",
					Params: map[string]any{
						"processId":    p.id,
						"bytesRead":    p.bytesRead.Load(),
						"bytesWritten": p.bytesWritten.Load(),
					},
				})
			}

		case <-p.autoSendStopCh:
			return
		}
	}
}

// queueSendLoop sends multistr entries with per-entry delays.
// loop=true: continuously cycles through enabled entries.
// loop=false: sends all enabled entries once then stops.
func (p *Process) queueSendLoop() {
	for {
		// Refresh cache from sendq if dirty
		p.multistrMu.Lock()
		if p.multistrDirty {
			p.multistrCache = ReadAllEntries(p.sendRing)
			p.multistrDirty = false
		}
		state := p.multistrState
		p.multistrMu.Unlock()

		if state != "sending" && state != "looping" {
			p.stopAutoSend()
			return
		}

		// Send one round: iterate all enabled entries
		p.sendOneRound()

		p.multistrMu.RLock()
		state = p.multistrState
		p.multistrMu.RUnlock()

		if state != "looping" {
			// One-shot complete
			p.multistrMu.Lock()
			p.multistrState = "idle"
			p.multistrMu.Unlock()
			p.stopAutoSend()
			p.broadcastMultistrChanged()
			return
		}

		// Loop: wait round interval before next round
		p.multistrRoundCount.Add(1)
		p.broadcastMultistrChanged()

		p.autoSendMu.RLock()
		roundInterval := p.autoSendIntervalMs
		p.autoSendMu.RUnlock()

		if roundInterval > 0 {
			timer := time.NewTimer(time.Duration(roundInterval) * time.Millisecond)
			select {
			case <-timer.C:
			case <-p.multistrLoopStopCh:
				return
			case <-p.autoSendStopCh:
				return
			}
		}
	}
}

// sendOneRound sends all enabled entries from the cache once.
func (p *Process) sendOneRound() {
	p.multistrMu.RLock()
	cache := make([]MultistrEntry, len(p.multistrCache))
	copy(cache, p.multistrCache)
	p.multistrMu.RUnlock()

	for i, entry := range cache {
		// Check if loop was stopped mid-round
		select {
		case <-p.autoSendStopCh:
			return
		default:
		}

		// Re-read enabled flag (may have changed via reload)
		p.multistrMu.RLock()
		if i < len(p.multistrCache) {
			entry = p.multistrCache[i]
		}
		p.multistrMu.RUnlock()

		if !entry.Enabled || entry.Content == "" {
			continue
		}

		raw, err := EntryToRaw(entry)
		if err != nil {
			p.autoSendErrorCount.Add(1)
			if p.pushEventFn != nil {
				p.pushEventFn(protocol.Event{
					Event: "send-error",
					Params: SendErrorInfo{
						ProcessID: p.id,
						Error:     err.Error(),
						Timestamp: time.Now().Format("15:04:05.000"),
					},
				})
			}
			continue
		}

		p.multistrMu.Lock()
		p.multistrCurrIndex = i
		p.multistrMu.Unlock()

		if err := p.writePortSilent(raw); err != nil {
			p.autoSendErrorCount.Add(1)
			hexData := strings.ToUpper(hex.EncodeToString(raw))
			if p.pushEventFn != nil {
				p.pushEventFn(protocol.Event{
					Event: "send-error",
					Params: SendErrorInfo{
						ProcessID: p.id,
						Error:     err.Error(),
						Data:      hexData,
						Timestamp: time.Now().Format("15:04:05.000"),
					},
				})
			}
			continue
		}

		p.bytesWritten.Add(int64(len(raw)))
		p.autoSendSendCount.Add(1)
		p.autoSendMu.Lock()
		p.autoSendLastSend = time.Now()
		p.autoSendMu.Unlock()

		hexData := strings.ToUpper(hex.EncodeToString(raw))
		if p.broadcastFn != nil {
			p.broadcastFn(RxTxMessage{
				ProcessID: p.id,
				Data:      string(raw),
				Hex:       hexData,
				Direction: "tx",
				Timestamp: time.Now().Format("15:04:05.000"),
			})
		}
		p.recordHistory(hexData, "tx")
		if p.pushEventFn != nil {
			p.pushEventFn(protocol.Event{
				Event: "stats-count",
				Params: map[string]any{
					"processId":    p.id,
					"bytesRead":    p.bytesRead.Load(),
					"bytesWritten": p.bytesWritten.Load(),
				},
			})
		}

		// Wait per-entry delay (interruptible)
		delay := entry.Delay
		if delay < 1 {
			p.autoSendMu.RLock()
			delay = p.autoSendIntervalMs
			p.autoSendMu.RUnlock()
			if delay < 1 {
				delay = 100
			}
		}
		timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
		select {
		case <-timer.C:
		case <-p.autoSendStopCh:
			return
		}
	}
}

// broadcastMultistrChanged pushes a multistr-changed event.
func (p *Process) broadcastMultistrChanged() {
	if p.pushEventFn == nil {
		return
	}
	p.multistrMu.RLock()
	p.pushEventFn(protocol.Event{
		Event: "multistr-changed",
		Params: map[string]any{
			"processId":    p.id,
			"entriesCount": len(p.multistrCache),
			"enabledCount": countEnabled(p.multistrCache),
			"state":        p.multistrState,
			"currentIndex": p.multistrCurrIndex,
			"roundCount":   p.multistrRoundCount.Load(),
		},
	})
	p.multistrMu.RUnlock()
}

func countEnabled(entries []MultistrEntry) int {
	n := 0
	for _, e := range entries {
		if e.Enabled {
			n++
		}
	}
	return n
}

// setAutoSendInterval updates the interval while auto-send is running.
// The new value takes effect on the next tick.
func (p *Process) setAutoSendInterval(intervalMs int) error {
	if intervalMs < 5 {
		return fmt.Errorf("interval must be >= 5 ms")
	}
	p.autoSendMu.Lock()
	p.autoSendIntervalMs = intervalMs
	p.autoSendMu.Unlock()
	return nil
}

// ---- port forwarding ----

func (p *Process) startForward() {
	p.forwardStopCh = make(chan struct{})
	p.forwardStopOnce = sync.Once{}
	go p.forwardLoop("1", p.port, p.forwardPortB, &p.bytesPort1)
	go p.forwardLoop("2", p.forwardPortB, p.port, &p.bytesPort2)
}

func (p *Process) stopForward() {
	p.forwardStopOnce.Do(func() {
		close(p.forwardStopCh)
	})
}

func (p *Process) forwardLoop(direction string, src, dst serial.Port, portBytes *atomic.Int64) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-p.forwardStopCh:
			return
		default:
		}
		n, err := src.Read(buf)
		if err != nil {
			select {
			case <-p.stopCh:
				return
			default:
			}
			if !strings.Contains(err.Error(), "timeout") {
				p.readErrors.Add(1)
				return
			}
			continue
		}
		if n > 0 {
			src.SetReadTimeout(5 * time.Millisecond)
			for {
				n2, err2 := src.Read(buf[n:])
				if err2 != nil || n2 == 0 {
					break
				}
				n += n2
				if n >= len(buf) {
					break
				}
			}
			src.SetReadTimeout(serial.NoTimeout)

			data := make([]byte, n)
			copy(data, buf[:n])
			hexData := strings.ToUpper(hex.EncodeToString(data))
			p.bytesRead.Add(int64(n))
			portBytes.Add(int64(n))

			// Write to destination
			if _, werr := dst.Write(data); werr != nil {
				p.writeErrors.Add(1)
				if p.pushEventFn != nil {
					p.pushEventFn(protocol.Event{
						Event: "send-error",
						Params: SendErrorInfo{
							ProcessID: p.id,
							Error:     werr.Error(),
							Data:      hexData,
							Timestamp: time.Now().Format("15:04:05.000"),
						},
					})
				}
				continue
			}
			p.bytesWritten.Add(int64(n))

			if p.broadcastFn != nil {
				p.broadcastFn(RxTxMessage{
					ProcessID: p.id,
					Data:      string(data),
					Hex:       hexData,
					Direction: direction,
					Timestamp: time.Now().Format("15:04:05.000"),
				})
			}
			p.recordHistory(hexData, direction)

			if p.pushEventFn != nil {
				p.pushEventFn(protocol.Event{
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
}

func (p *Process) readLoop(broadcast func(RxTxMessage)) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-p.stopCh:
			return
		default:
		}
		p.port.SetReadTimeout(100 * time.Millisecond)
		n, err := p.port.Read(buf)
		if err != nil {
			select {
			case <-p.stopCh:
				return
			default:
			}
			if !strings.Contains(err.Error(), "timeout") {
				p.readErrors.Add(1)
				return
			}
			continue
		}
		if n > 0 {
			// 短超时累积读取：串口数据到达速度远快于应用层处理，
			// 首字节触发 Read 返回后立即再读几次，将后续字节合并为单包
			p.port.SetReadTimeout(5 * time.Millisecond)
			for {
				n2, err2 := p.port.Read(buf[n:])
				if err2 != nil || n2 == 0 {
					break
				}
				n += n2
				if n >= len(buf) {
					break
				}
			}
			p.port.SetReadTimeout(serial.NoTimeout)

			p.bytesRead.Add(int64(n))
			data := make([]byte, n)
			copy(data, buf[:n])
			hexData := strings.ToUpper(hex.EncodeToString(data))
			broadcast(RxTxMessage{
				ProcessID: p.id,
				Data:      string(data),
				Hex:       hexData,
				Direction: "rx",
				Timestamp: time.Now().Format("15:04:05.000"),
			})
			p.recordHistory(hexData, "rx")

			if p.pushEventFn != nil {
				p.pushEventFn(protocol.Event{
					Event: "stats-count",
					Params: map[string]any{
						"processId":    p.id,
						"bytesRead":    p.bytesRead.Load(),
						"bytesWritten": p.bytesWritten.Load(),
					},
				})
			}
		}
	}
}

func (p *Process) writeLoop(broadcast func(RxTxMessage), pushEvent func(protocol.Event)) {
	for job := range p.sendCh {
		type writeResult struct {
			n   int
			err error
		}
		ch := make(chan writeResult, 1)
		go func() {
			p.portWriteMu.Lock()
			n, err := p.port.Write(job.raw)
			p.portWriteMu.Unlock()
			select {
			case ch <- writeResult{n, err}:
			default:
			}
		}()

		select {
		case wr := <-ch:
			if wr.err != nil {
				p.writeErrors.Add(1)
				hexData := strings.ToUpper(hex.EncodeToString(job.raw))
				pushEvent(protocol.Event{
					Event: "send-error",
					Params: SendErrorInfo{
						ProcessID: p.id,
						Error:     wr.err.Error(),
						Data:      hexData,
						Timestamp: time.Now().Format("15:04:05.000"),
					},
				})
				p.recordSystemEvent("sys.send_error", wr.err.Error())
				continue
			}
			p.bytesWritten.Add(int64(len(job.raw)))
			hexData := strings.ToUpper(hex.EncodeToString(job.raw))
			msg := RxTxMessage{
				ProcessID: p.id,
				Data:      string(job.raw),
				Hex:       hexData,
				Direction: "tx",
				Timestamp: time.Now().Format("15:04:05.000"),
			}
			broadcast(msg)
			p.recordHistory(hexData, "tx")
			if pushEvent != nil {
				pushEvent(protocol.Event{
					Event: "stats-count",
					Params: map[string]any{
						"processId":    p.id,
						"bytesRead":    p.bytesRead.Load(),
						"bytesWritten": p.bytesWritten.Load(),
					},
				})
			}

		case <-time.After(3 * time.Second):
			p.writeErrors.Add(1)
			hexData := strings.ToUpper(hex.EncodeToString(job.raw))
			pushEvent(protocol.Event{
				Event: "send-error",
				Params: SendErrorInfo{
					ProcessID: p.id,
					Error:     fmt.Sprintf("write timeout on %s", p.config.Port),
					Data:      hexData,
					Timestamp: time.Now().Format("15:04:05.000"),
				},
			})
			p.recordSystemEvent("sys.write_timeout", p.config.Port)
			p.stopIO()
			return
		}
	}
}

func (p *Process) statsLoop(broadcast func(RxTxMessage), pushEvent func(protocol.Event)) {
	if p.mode == "forward" {
		p.recordSystemEvent("sys.stats_started", p.config.Port+" ↔ "+p.forwardConfigB.Port)
	} else {
		p.recordSystemEvent("sys.stats_started", p.config.Port)
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastBytesRead := int64(0)
	lastBytesWritten := int64(0)
	lastBytesPort1 := int64(0)
	lastBytesPort2 := int64(0)

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			curRead := p.bytesRead.Load()
			curWritten := p.bytesWritten.Load()
			curP1 := p.bytesPort1.Load()
			curP2 := p.bytesPort2.Load()

			readRate := float64(curRead - lastBytesRead)
			writeRate := float64(curWritten - lastBytesWritten)
			p1Rate := float64(curP1 - lastBytesPort1)
			p2Rate := float64(curP2 - lastBytesPort2)

			sample := &statsSample{
				readRateBps:  readRate,
				writeRateBps: writeRate,
				lastSample:   now,
			}
			p.statsSample.Store(sample)

			lastBytesRead = curRead
			lastBytesWritten = curWritten
			lastBytesPort1 = curP1
			lastBytesPort2 = curP2

			if p.mode == "forward" {
				pushEvent(protocol.Event{
					Event: "stats-rate",
					Params: map[string]any{
						"processId":    p.id,
						"readRateBps":  p2Rate,
						"writeRateBps": p1Rate,
						"bytesPort1":   curP1,
						"bytesPort2":   curP2,
					},
				})
			} else {
				pushEvent(protocol.Event{
					Event: "stats-rate",
					Params: map[string]any{
						"processId":    p.id,
						"readRateBps":  readRate,
						"writeRateBps": writeRate,
						"bytesPort1":   curP1,
						"bytesPort2":   curP2,
					},
				})
			}

		case <-p.statsStop:
			return
		}
	}
}

// ---- serial port helper ----

func openSerialPort(config *SerialConfig) (serial.Port, error) {
	if config.Baud <= 0 {
		config.Baud = 115200
	}
	if config.DataBits <= 0 {
		config.DataBits = 8
	}
	if config.StopBits == "" {
		config.StopBits = "1"
	}
	if config.Parity == "" {
		config.Parity = "none"
	}

	mode := &serial.Mode{
		BaudRate: config.Baud,
		DataBits: config.DataBits,
		StopBits: toStopBits(config.StopBits),
		Parity:   toParity(config.Parity),
	}

	type openResult struct {
		p   serial.Port
		err error
	}
	openCh := make(chan openResult, 1)
	go func() {
		p, err := serial.Open(config.Port, mode)
		select {
			case openCh <- openResult{p, err}:
			default:
				if p != nil {
					p.Close()
				}
			}
	}()
	select {
	case r := <-openCh:
		if r.err != nil {
			return nil, r.err
		}
		// 清除硬件接收缓冲区中的残留数据
		r.p.ResetInputBuffer()
		if config.FlowControl != "" && config.FlowControl != "none" {
			if err := setFlowControl(r.p, config.FlowControl); err != nil {
				r.p.Close()
				return nil, fmt.Errorf("setFlowControl: %w", err)
			}
		}
		return r.p, nil
	case <-time.After(8 * time.Second):
		return nil, fmt.Errorf("open timeout on %s", config.Port)
	}
}

func (p *Process) startIO(broadcast func(RxTxMessage), pushEvent func(protocol.Event)) {
	p.broadcastFn = broadcast
	p.pushEventFn = pushEvent
	go p.readLoop(broadcast)
	go p.writeLoop(broadcast, pushEvent)
	go p.statsLoop(broadcast, pushEvent)
}

func (p *Process) stopIO() {
	p.stopOnce.Do(func() {
		p.stopAutoSend()
		close(p.stopCh)
		close(p.statsStop)
		close(p.sendCh)
		p.port.Close()
	})
}

// closeSendRing closes the send queue shared memory ring buffer.
func (p *Process) closeSendRing() {
	if p.sendRing != nil {
		p.sendRing.Close()
		p.sendRing = nil
	}
}

func (p *Process) openHistoryFile() {
	if !isHistoryEnabled() {
		return
	}
	f, _, err := newHistoryFile(historyDir(), p.id)
	if err != nil {
		logOp("错误", "打开历史文件失败 (进程 #%s): %v", p.id, err)
		return
	}
	p.historyFile = f
}

func (p *Process) attachHistoryFile(filename string) error {
	f, path, err := attachHistoryFile(historyDir(), filename)
	if err != nil {
		return err
	}
	p.historyFile = f
	p.historyFileName = filepath.Base(path)
	n, err := loadHistoryIntoRing(p.historyRing, path)
	if err != nil {
		p.historyFile.Close()
		p.historyFile = nil
		p.historyFileName = ""
		return fmt.Errorf("加载历史文件失败: %w", err)
	}
	if n > 0 {
		logOp("历史", "进程 #%s 已加载 %s (%d 条)", p.id, filepath.Base(path), n)
	}
	return nil
}

func (p *Process) newHistoryFile() error {
	f, path, err := newHistoryFile(historyDir(), p.id)
	if err != nil {
		return err
	}
	p.historyFile = f
	p.historyFileName = filepath.Base(path)
	return nil
}

// closeHistory closes the shared memory ring buffer and the history file.
func (p *Process) closeHistory() {
	if p.historyRing != nil {
		p.historyRing.Close()
		p.historyRing = nil
	}
	closeHistoryFile(p.historyFile)
	p.historyFile = nil
}

// ---- process manager ----

type ProcessManager struct {
	processes        map[string]*Process
	portMap          map[string]string
	mu               sync.RWMutex
	counter          atomic.Int64
	broadcast        func(RxTxMessage)
	pushEvent        func(protocol.Event)
	onProcessChanged func() // 进程状态变更回调
}

func NewProcessManager() *ProcessManager {
	return &ProcessManager{
		processes: make(map[string]*Process),
		portMap:   make(map[string]string),
	}
}

func toStopBits(v string) serial.StopBits {
	switch v {
	case "1.5":
		return serial.OnePointFiveStopBits
	case "2":
		return serial.TwoStopBits
	default:
		return serial.OneStopBit
	}
}

func toParity(v string) serial.Parity {
	switch v {
	case "odd":
		return serial.OddParity
	case "even":
		return serial.EvenParity
	case "mark":
		return serial.MarkParity
	case "space":
		return serial.SpaceParity
	default:
		return serial.NoParity
	}
}

// Create creates a process. mode defaults to "single" if empty.
// If port is "", creates an idle process (all modes get rings).
// If port is non-empty and mode="single", creates + connects with port dedup.
// If port is non-empty and mode="forward", requires cfgB.Port non-empty, opens both ports.
func (pm *ProcessManager) Create(mode, port string, cfg SerialConfig, cfgB *SerialConfig, connect bool) (*Process, error) {
	if mode == "" {
		mode = "single"
	}

	if port != "" {
		// Declare-only: store config but don't open ports
		if !connect {
			if mode == "forward" {
				if cfgB == nil || cfgB.Port == "" {
					return nil, fmt.Errorf("forward mode requires both portA and portB")
				}
				if port == cfgB.Port {
					return nil, fmt.Errorf("cannot forward between the same port")
				}
			}

			id := fmt.Sprintf("%d", pm.counter.Add(1))
			sharedName := fmt.Sprintf("serial-tool-history-%s", id)
			ring, err := ringbuf.CreateSharedRing(sharedName, 5*1024*1024)
			if err != nil {
				return nil, fmt.Errorf("create shared memory: %w", err)
			}

			sendName := fmt.Sprintf("serial-tool-sendq-%s", id)
			sendQ, err := ringbuf.CreateSharedRing(sendName, 1*1024*1024)
			if err != nil {
				ring.Close()
				return nil, fmt.Errorf("create send queue shared memory: %w", err)
			}

			proc := &Process{
				id:           id,
				status:       "idle",
				mode:         mode,
				config:       cfg,
				historyRing:  ring,
				historyName:  sharedName,
				sendRing:     sendQ,
				sendRingName: sendName,
				startTime:    time.Now(),
			}

			if mode == "forward" {
				proc.forwardConfigB = *cfgB
			}

			pm.mu.Lock()
			pm.processes[id] = proc
			pm.mu.Unlock()


			if mode == "forward" {
				proc.recordSystemEvent("sys.forward_declared", port, cfgB.Port)
			} else {
				proc.recordSystemEvent("sys.port_declared", port, cfg.Baud)
			}
			if pm.onProcessChanged != nil {
				pm.onProcessChanged()
			}
			return proc, nil
		}

		// connect == true: open ports immediately (existing behaviour)
		if mode == "forward" {
			if cfgB == nil || cfgB.Port == "" {
				return nil, fmt.Errorf("forward mode requires both portA and portB")
			}
			if port == cfgB.Port {
				return nil, fmt.Errorf("cannot forward between the same port")
			}

			pm.mu.RLock()
			_, occupiedA := pm.portMap[port]
			_, occupiedB := pm.portMap[cfgB.Port]
			pm.mu.RUnlock()
			if occupiedA {
				return nil, fmt.Errorf("port %s already connected", port)
			}
			if occupiedB {
				return nil, fmt.Errorf("port %s already connected", cfgB.Port)
			}

			portA, err := openSerialPort(&cfg)
			if err != nil {
				return nil, fmt.Errorf("open port A (%s): %w", port, err)
			}
			portB, err := openSerialPort(cfgB)
			if err != nil {
				portA.Close()
				return nil, fmt.Errorf("open port B (%s): %w", cfgB.Port, err)
			}

			id := fmt.Sprintf("%d", pm.counter.Add(1))
			sharedName := fmt.Sprintf("serial-tool-history-%s", id)
			ring, err := ringbuf.CreateSharedRing(sharedName, 5*1024*1024)
			if err != nil {
				portA.Close()
				portB.Close()
				return nil, fmt.Errorf("create shared memory: %w", err)
			}

			sendName := fmt.Sprintf("serial-tool-sendq-%s", id)
			sendQ, err := ringbuf.CreateSharedRing(sendName, 1*1024*1024)
			if err != nil {
				ring.Close()
				portA.Close()
				portB.Close()
				return nil, fmt.Errorf("create send queue shared memory: %w", err)
			}

			proc := &Process{
				id:             id,
				status:         "connected",
				mode:           "forward",
				config:         cfg,
				port:           portA,
				forwardPortB:   portB,
				forwardConfigB: *cfgB,
				historyRing:    ring,
				historyName:    sharedName,
				sendRing:       sendQ,
				sendRingName:   sendName,
				statsStop:      make(chan struct{}),
				startTime:      time.Now(),
			}

			pm.mu.Lock()
			pm.processes[id] = proc
			pm.portMap[port] = id
			pm.portMap[cfgB.Port] = id
			pm.mu.Unlock()

			proc.broadcastFn = pm.broadcast
			proc.pushEventFn = pm.pushEvent
			go proc.statsLoop(pm.broadcast, pm.pushEvent)
			proc.startForward()
			proc.recordSystemEvent("sys.forward_started", port, cfgB.Port)
			if pm.onProcessChanged != nil {
				pm.onProcessChanged()
			}
			return proc, nil
		}

		// mode == "single"
		pm.mu.RLock()
		existingPID, occupied := pm.portMap[port]
		pm.mu.RUnlock()
		if occupied {
			pm.mu.RLock()
			proc := pm.processes[existingPID]
			pm.mu.RUnlock()
			return proc, nil
		}

		serialPort, err := openSerialPort(&cfg)
		if err != nil {
			return nil, err
		}

		id := fmt.Sprintf("%d", pm.counter.Add(1))
		sharedName := fmt.Sprintf("serial-tool-history-%s", id)
		ring, err := ringbuf.CreateSharedRing(sharedName, 5*1024*1024) // 5 MB
		if err != nil {
			serialPort.Close()
			return nil, fmt.Errorf("create shared memory: %w", err)
		}

		sendName := fmt.Sprintf("serial-tool-sendq-%s", id)
		sendQ, err := ringbuf.CreateSharedRing(sendName, 1*1024*1024) // 1 MB
		if err != nil {
			ring.Close()
			serialPort.Close()
			return nil, fmt.Errorf("create send queue shared memory: %w", err)
		}

		proc := &Process{
			id:           id,
			status:       "connected",
			mode:         "single",
			config:       cfg,
			port:         serialPort,
			historyRing:  ring,
			historyName:  sharedName,
			stopCh:       make(chan struct{}),
			sendCh:       make(chan sendJob, 32),
			startTime:    time.Now(),
			statsStop:    make(chan struct{}),
			sendRing:     sendQ,
			sendRingName: sendName,
		}

		pm.mu.Lock()
		pm.processes[id] = proc
		pm.portMap[port] = id
		pm.mu.Unlock()

		proc.recordSystemEvent("sys.port_opened", port, cfg.Baud)
		proc.startIO(pm.broadcast, pm.pushEvent)
		if pm.onProcessChanged != nil {
			pm.onProcessChanged()
		}
		return proc, nil
	}

	// Idle process (no port specified – mode is stored but no ports opened)
	id := fmt.Sprintf("%d", pm.counter.Add(1))
	sharedName := fmt.Sprintf("serial-tool-history-%s", id)
	ring, err := ringbuf.CreateSharedRing(sharedName, 5*1024*1024) // 5 MB
	if err != nil {
		return nil, fmt.Errorf("create shared memory: %w", err)
	}

	sendName := fmt.Sprintf("serial-tool-sendq-%s", id)
	sendQ, err := ringbuf.CreateSharedRing(sendName, 1*1024*1024) // 1 MB
	if err != nil {
		ring.Close()
		return nil, fmt.Errorf("create send queue shared memory: %w", err)
	}

	proc := &Process{
		id:           id,
		status:       "idle",
		mode:         mode,
		historyRing:  ring,
		historyName:  sharedName,
		sendRing:     sendQ,
		sendRingName: sendName,
		startTime:    time.Now(),
	}

	pm.mu.Lock()
	pm.processes[id] = proc
	pm.mu.Unlock()

	proc.recordSystemEvent("sys.process_created_idle", "mode."+mode)
	if pm.onProcessChanged != nil {
		pm.onProcessChanged()
	}
	return proc, nil
}

// ForwardCreate is a backward-compatible wrapper that creates a forward process.
func (pm *ProcessManager) ForwardCreate(cfgA, cfgB SerialConfig) (*Process, error) {
	return pm.Create("forward", cfgA.Port, cfgA, &cfgB, true)
}

// Destroy destroys a process. If connected, disconnects first.
// Saves multistr entries before destroying.
func (pm *ProcessManager) Destroy(id string) error {
	pm.mu.Lock()
	proc, ok := pm.processes[id]
	if !ok {
		pm.mu.Unlock()
		return fmt.Errorf("process not found: %s", id)
	}

	// Save multistr entries before destroying (use cache if available)
	entries := proc.multistrCache
	if len(entries) == 0 {
		entries = ReadAllEntries(proc.sendRing)
	}
	if len(entries) > 0 {
		SaveEntriesToFile(id, entries)
	}

	if proc.status == "connected" && proc.mode == "forward" {
		portA := proc.config.Port
		portB := proc.forwardConfigB.Port
		delete(pm.portMap, portA)
		delete(pm.portMap, portB)
		pm.mu.Unlock()
		proc.recordSystemEvent("sys.forward_stopped", portA, portB)
		proc.stopForward()
		close(proc.statsStop)
		proc.port.Close()
		proc.forwardPortB.Close()
		proc.closeHistory()
		proc.closeSendRing()
		pm.mu.Lock()
		delete(pm.processes, id)
		pm.mu.Unlock()
		if pm.onProcessChanged != nil {
			pm.onProcessChanged()
		}
		return nil
	}

	if proc.status == "connected" {
		port := proc.config.Port
		delete(pm.portMap, port)
		pm.mu.Unlock()

		proc.recordSystemEvent("sys.port_closed", port)
		proc.stopIO()
		proc.closeHistory()
		proc.closeSendRing()

		pm.mu.Lock()
		delete(pm.processes, id)
		pm.mu.Unlock()
		if pm.onProcessChanged != nil {
			pm.onProcessChanged()
		}
		return nil
	}

	delete(pm.processes, id)
	pm.mu.Unlock()
	proc.recordSystemEvent("sys.process_destroyed_idle")
	proc.closeHistory()
	proc.closeSendRing()
	if pm.onProcessChanged != nil {
		pm.onProcessChanged()
	}
	return nil
}

// Connect binds an idle process to a serial port.
func (pm *ProcessManager) Connect(id string, port string, cfg SerialConfig) error {
	pm.mu.RLock()
	proc, ok := pm.processes[id]
	if !ok {
		pm.mu.RUnlock()
		return fmt.Errorf("process not found: %s", id)
	}
	if proc.status != "idle" {
		pm.mu.RUnlock()
		return fmt.Errorf("process %s is already connected", id)
	}
	// Fallback: if no port provided, use stored config from declaration
	if port == "" && cfg.Port == "" && proc.config.Port != "" {
		port = proc.config.Port
		cfg = proc.config
	}
	if port == "" {
		port = cfg.Port
	}
	_, occupied := pm.portMap[port]
	pm.mu.RUnlock()
	if occupied {
		return fmt.Errorf("port %s already connected", port)
	}

	serialPort, err := openSerialPort(&cfg)
	if err != nil {
		return err
	}

	pm.mu.Lock()
	if _, exists := pm.portMap[port]; exists {
		pm.mu.Unlock()
		serialPort.Close()
		return fmt.Errorf("port %s already connected", port)
	}
	proc.port = serialPort
	proc.config = cfg
	proc.status = "connected"
	proc.stopCh = make(chan struct{})
	proc.sendCh = make(chan sendJob, 32)
	proc.statsStop = make(chan struct{})
		proc.stopOnce = sync.Once{}
	proc.startTime = time.Now()
	pm.portMap[port] = id
	pm.mu.Unlock()

	// Reset send queue to clear any stale data
	if proc.sendRing != nil {
		proc.sendRing.Reset()
	}

	proc.recordSystemEvent("sys.port_connected", port, cfg.Baud)
	proc.startIO(pm.broadcast, pm.pushEvent)
	if pm.onProcessChanged != nil {
		pm.onProcessChanged()
	}
	return nil
}

// SwitchPort closes the current port (if connected) and opens a new port on the
// same process, preserving processId, history, and send queue.
func (pm *ProcessManager) SwitchPort(id string, port string, cfg SerialConfig) error {
	pm.mu.RLock()
	proc, ok := pm.processes[id]
	if !ok {
		pm.mu.RUnlock()
		return fmt.Errorf("process not found: %s", id)
	}
	if proc.status != "connected" {
		pm.mu.RUnlock()
		return fmt.Errorf("process %s is not connected", id)
	}
	oldPort := proc.config.Port
	pm.mu.RUnlock()

	// Check new port not already occupied by another process
	pm.mu.RLock()
	existingPID, occupied := pm.portMap[port]
	pm.mu.RUnlock()
	if occupied && existingPID != id {
		return fmt.Errorf("port %s already connected", port)
	}

	// Stop I/O on old port
	proc.recordSystemEvent("sys.port_switched", oldPort, port)
	proc.stopIO()

	// Open new port
	serialPort, err := openSerialPort(&cfg)
	if err != nil {
		// Try to reconnect to old port on failure
		oldCfg := cfg
		oldCfg.Port = oldPort
		if oldP, oldErr := openSerialPort(&oldCfg); oldErr == nil {
			proc.port = oldP
			proc.config = oldCfg
			proc.status = "connected"
			proc.stopCh = make(chan struct{})
			proc.sendCh = make(chan sendJob, 32)
			proc.statsStop = make(chan struct{})
			proc.stopOnce = sync.Once{}
			proc.startTime = time.Now()
			pm.mu.Lock()
			pm.portMap[oldPort] = id
			pm.mu.Unlock()
			if proc.sendRing != nil {
				proc.sendRing.Reset()
			}
			proc.startIO(pm.broadcast, pm.pushEvent)
			if pm.onProcessChanged != nil {
				pm.onProcessChanged()
			}
		}
		return fmt.Errorf("open new port: %w", err)
	}

	pm.mu.Lock()
	delete(pm.portMap, oldPort)
	if _, exists := pm.portMap[port]; exists {
		pm.mu.Unlock()
		serialPort.Close()
		return fmt.Errorf("port %s already connected", port)
	}
	proc.port = serialPort
	proc.config = cfg
	proc.status = "connected"
	proc.stopCh = make(chan struct{})
	proc.sendCh = make(chan sendJob, 32)
	proc.statsStop = make(chan struct{})
	proc.stopOnce = sync.Once{}
	proc.startTime = time.Now()
	pm.portMap[port] = id
	pm.mu.Unlock()

	if proc.sendRing != nil {
		proc.sendRing.Reset()
	}

	proc.startIO(pm.broadcast, pm.pushEvent)
	if pm.onProcessChanged != nil {
		pm.onProcessChanged()
	}
	return nil
}

// SetMode switches the process mode between "single" and "forward".
// Process must be idle (all ports disconnected).
func (pm *ProcessManager) SetMode(id, newMode string) error {
	if newMode != "single" && newMode != "forward" {
		return fmt.Errorf("invalid mode: %s (use single or forward)", newMode)
	}

	pm.mu.Lock()
	proc, ok := pm.processes[id]
	if !ok {
		pm.mu.Unlock()
		return fmt.Errorf("process not found: %s", id)
	}
	if proc.status != "idle" {
		pm.mu.Unlock()
		return fmt.Errorf("process %s is connected, must disconnect all ports before switching mode", id)
	}
	oldMode := proc.mode
	proc.mode = newMode
	pm.mu.Unlock()

	proc.recordSystemEvent("sys.mode_switched", "mode."+oldMode, "mode."+newMode)
	if pm.onProcessChanged != nil {
		pm.onProcessChanged()
	}
	return nil
}

// ConnectForward binds an idle forward-mode process to two serial ports and starts forwarding.
func (pm *ProcessManager) ConnectForward(id string, cfgA, cfgB SerialConfig) error {
	pm.mu.RLock()
	proc, ok := pm.processes[id]
	if !ok {
		pm.mu.RUnlock()
		return fmt.Errorf("process not found: %s", id)
	}
	if proc.status != "idle" {
		pm.mu.RUnlock()
		return fmt.Errorf("process %s is already connected", id)
	}
	if proc.mode != "forward" {
		pm.mu.RUnlock()
		return fmt.Errorf("process %s is not in forward mode (current: %s)", id, proc.mode)
	}
	// Fallback: if no ports provided, use stored config from declaration
	if cfgA.Port == "" && proc.config.Port != "" {
		cfgA = proc.config
	}
	if cfgB.Port == "" && proc.forwardConfigB.Port != "" {
		cfgB = proc.forwardConfigB
	}
	_, occupiedA := pm.portMap[cfgA.Port]
	_, occupiedB := pm.portMap[cfgB.Port]
	pm.mu.RUnlock()
	if occupiedA {
		return fmt.Errorf("port %s already connected", cfgA.Port)
	}
	if occupiedB {
		return fmt.Errorf("port %s already connected", cfgB.Port)
	}
	if cfgA.Port == cfgB.Port {
		return fmt.Errorf("cannot forward between the same port")
	}

	portA, err := openSerialPort(&cfgA)
	if err != nil {
		return fmt.Errorf("open port A (%s): %w", cfgA.Port, err)
	}
	portB, err := openSerialPort(&cfgB)
	if err != nil {
		portA.Close()
		return fmt.Errorf("open port B (%s): %w", cfgB.Port, err)
	}

	pm.mu.Lock()
	if _, exists := pm.portMap[cfgA.Port]; exists {
		pm.mu.Unlock()
		portA.Close()
		portB.Close()
		return fmt.Errorf("port %s already connected", cfgA.Port)
	}
	if _, exists := pm.portMap[cfgB.Port]; exists {
		pm.mu.Unlock()
		portA.Close()
		portB.Close()
		return fmt.Errorf("port %s already connected", cfgB.Port)
	}
	proc.port = portA
	proc.config = cfgA
	proc.forwardPortB = portB
	proc.forwardConfigB = cfgB
	proc.status = "connected"
	proc.stopCh = make(chan struct{})
	proc.sendCh = make(chan sendJob, 32)
	proc.statsStop = make(chan struct{})
	proc.stopOnce = sync.Once{}
	proc.forwardStopOnce = sync.Once{}
	proc.startTime = time.Now()
	pm.portMap[cfgA.Port] = id
	pm.portMap[cfgB.Port] = id
	pm.mu.Unlock()

	if proc.sendRing != nil {
		proc.sendRing.Reset()
	}

	proc.broadcastFn = pm.broadcast
	proc.pushEventFn = pm.pushEvent
	proc.recordSystemEvent("sys.forward_started", cfgA.Port, cfgB.Port)
	proc.startForward()
	go proc.statsLoop(pm.broadcast, pm.pushEvent)
	if pm.onProcessChanged != nil {
		pm.onProcessChanged()
	}
	return nil
}

// Disconnect closes the serial port but keeps the process alive.
// For forward mode, closes both ports and stops forwarding goroutines.
func (pm *ProcessManager) Disconnect(id string) error {
	pm.mu.Lock()
	proc, ok := pm.processes[id]
	if !ok {
		pm.mu.Unlock()
		return fmt.Errorf("process not found: %s", id)
	}
	if proc.status != "connected" {
		pm.mu.Unlock()
		return fmt.Errorf("process %s is not connected", id)
	}

	if proc.mode == "forward" {
		portA := proc.config.Port
		portB := proc.forwardConfigB.Port
		delete(pm.portMap, portA)
		delete(pm.portMap, portB)
		pm.mu.Unlock()

		proc.recordSystemEvent("sys.forward_stopped", portA, portB)
		proc.stopForward()
		close(proc.statsStop)
		close(proc.sendCh)
		proc.port.Close()
		proc.forwardPortB.Close()

		pm.mu.Lock()
		proc.status = "idle"
		pm.mu.Unlock()
		if pm.onProcessChanged != nil {
			pm.onProcessChanged()
		}
		return nil
	}

	port := proc.config.Port
	delete(pm.portMap, port)
	pm.mu.Unlock()

	proc.recordSystemEvent("sys.port_disconnected", port)
	proc.stopIO()

	pm.mu.Lock()
	proc.status = "idle"
	pm.mu.Unlock()
	if pm.onProcessChanged != nil {
		pm.onProcessChanged()
	}
	return nil
}

func (pm *ProcessManager) Get(id string) *Process {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.processes[id]
}

// AddViewer increments the viewer count for a source type on a process.
func (pm *ProcessManager) AddViewer(processId, source string) {
	pm.mu.Lock()
	proc, ok := pm.processes[processId]
	if !ok {
		pm.mu.Unlock()
		return
	}
	if proc.viewers == nil {
		proc.viewers = make(map[string]int)
	}
	proc.viewers[source]++
	pm.mu.Unlock()
	if pm.onProcessChanged != nil {
		pm.onProcessChanged()
	}
}

// RemoveViewer decrements the viewer count for a source type on a process.
func (pm *ProcessManager) RemoveViewer(processId, source string) {
	pm.mu.Lock()
	proc, ok := pm.processes[processId]
	if !ok || proc.viewers == nil {
		pm.mu.Unlock()
		return
	}
	if proc.viewers[source] > 0 {
		proc.viewers[source]--
	}
	if proc.viewers[source] == 0 {
		delete(proc.viewers, source)
	}
	pm.mu.Unlock()
	if pm.onProcessChanged != nil {
		pm.onProcessChanged()
	}
}

func (pm *ProcessManager) List() []map[string]any {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	result := make([]map[string]any, 0, len(pm.processes))
	for _, p := range pm.processes {
		entry := map[string]any{
			"processId":    p.id,
			"status":       p.status,
			"mode":         p.mode,
			"connected":    p.status == "connected",
			"viewers":      p.viewerSummary(),
			"autoSend":     p.getAutoSendStatus(),
			"sendRingName": p.sendRingName,
		}
		// Include config for connected processes and idle processes that have stored config
		hasConfig := p.status == "connected" || p.config.Port != ""
		if hasConfig && p.mode == "forward" {
			entry["portName"] = p.config.Port + " ↔ " + p.forwardConfigB.Port
			entry["baud"] = p.config.Baud
			entry["dataBits"] = p.config.DataBits
			entry["stopBits"] = p.config.StopBits
			entry["parity"] = p.config.Parity
			// Nested P1/P2 detail objects
			entry["p1"] = map[string]any{
				"port": p.config.Port, "baud": p.config.Baud,
				"dataBits": p.config.DataBits, "stopBits": p.config.StopBits, "parity": p.config.Parity,
			}
			if p.forwardConfigB.Port != "" {
				entry["forwardPortB"] = p.forwardConfigB.Port
				entry["forwardBaudB"] = p.forwardConfigB.Baud
				entry["forwardDataBitsB"] = p.forwardConfigB.DataBits
				entry["forwardStopBitsB"] = p.forwardConfigB.StopBits
				entry["forwardParityB"] = p.forwardConfigB.Parity
				entry["p2"] = map[string]any{
					"port": p.forwardConfigB.Port, "baud": p.forwardConfigB.Baud,
					"dataBits": p.forwardConfigB.DataBits, "stopBits": p.forwardConfigB.StopBits, "parity": p.forwardConfigB.Parity,
				}
			}
		} else if hasConfig {
			entry["portName"] = p.config.Port
			entry["baud"] = p.config.Baud
			entry["dataBits"] = p.config.DataBits
			entry["stopBits"] = p.config.StopBits
			entry["parity"] = p.config.Parity
			entry["config"] = map[string]any{
				"port": p.config.Port, "baud": p.config.Baud,
				"dataBits": p.config.DataBits, "stopBits": p.config.StopBits, "parity": p.config.Parity,
			}
		}
		result = append(result, entry)
	}
	return result
}

func (pm *ProcessManager) ListConnected() []map[string]any {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	result := make([]map[string]any, 0)
	for _, p := range pm.processes {
		if p.status == "connected" {
			result = append(result, map[string]any{
				"id":       p.id,
				"portName": p.config.Port,
				"baud":     p.config.Baud,
				"dataBits": p.config.DataBits,
				"stopBits": p.config.StopBits,
				"parity":   p.config.Parity,
			})
		}
	}
	return result
}

func (pm *ProcessManager) FirstConnectedID() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, p := range pm.processes {
		if p.status == "connected" {
			return p.id
		}
	}
	return ""
}

func (pm *ProcessManager) Send(id string, data string, format string) error {
	proc := pm.Get(id)
	if proc == nil {
		return fmt.Errorf("process not found: %s", id)
	}
	if proc.status != "connected" || proc.mode != "single" {
		return fmt.Errorf("process %s is not connected in single mode", id)
	}

	var raw []byte
	if format == "hex" {
		var err error
		raw, err = hex.DecodeString(data)
		if err != nil {
			return fmt.Errorf("invalid hex: %s", err.Error())
		}
	} else {
		raw = []byte(data)
	}

	select {
	case proc.sendCh <- sendJob{raw: raw, format: format}:
		return nil
	default:
		return fmt.Errorf("send buffer full (max 32 pending)")
	}
}

func (pm *ProcessManager) Count() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.processes)
}

func (pm *ProcessManager) ConnectedCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	count := 0
	for _, p := range pm.processes {
		if p.status == "connected" {
			count++
		}
	}
	return count
}

// GetHistory returns entries from the shared memory ring buffer, the oldest
// timestamp (for cold-data paging), the history file name, and the shared
// memory name. Uses Snapshot so the ring buffer is NOT drained — multiple
// clients can independently read the same data.
func (pm *ProcessManager) GetHistory(id string) map[string]any {
	proc := pm.Get(id)
	if proc == nil {
		return nil
	}
	if proc.historyRing == nil {
		return nil
	}
	proc.ringBufMu.Lock()

	// Snapshot the ring buffer — non-destructive, tail/count unchanged.
	packets := proc.historyRing.Snapshot()

	// Peek the oldest timestamp from the ring (without consuming it).
	oldestMs := proc.historyRing.OldestTimestampMs()

	proc.ringBufMu.Unlock()

	entries := parseHistoryPackets(packets)

	result := map[string]any{
		"history":    entries,
		"sharedName": proc.historyName,
	}
	if oldestMs > 0 {
		result["oldestTs"] = time.UnixMilli(oldestMs).Format("15:04:05.000")
	}
	if proc.historyFileName != "" {
		result["historyFile"] = proc.historyFileName
	}
	return result
}

func parseHistoryPackets(packets [][]byte) []HistoryEntry {
	entries := make([]HistoryEntry, 0, len(packets))
	for _, pkt := range packets {
		if len(pkt) < 11 {
			continue
		}
		ts := int64(binary.LittleEndian.Uint64(pkt[0:8]))
		dir := pkt[8]
		hexLen := binary.LittleEndian.Uint16(pkt[9:11])
		if int(hexLen)+11 > len(pkt) {
			continue
		}
		entries = append(entries, HistoryEntry{
			Timestamp: time.UnixMilli(ts).Format("15:04:05.000"),
			Hex:       string(pkt[11 : 11+int(hexLen)]),
			Direction: byteToDir(dir),
		})
	}
	return entries
}

// GetHistorySharedName returns the shared memory name for a process.
func (pm *ProcessManager) GetHistorySharedName(id string) string {
	proc := pm.Get(id)
	if proc == nil {
		return ""
	}
	return proc.historyName
}

func (pm *ProcessManager) GetStats(id string) (*ProcessStats, error) {
	proc := pm.Get(id)
	if proc == nil {
		return nil, fmt.Errorf("process not found: %s", id)
	}

	var rateBpsRead, rateBpsWrite float64
	var lastSample time.Time
	if raw := proc.statsSample.Load(); raw != nil {
		s := raw.(*statsSample)
		rateBpsRead = s.readRateBps
		rateBpsWrite = s.writeRateBps
		lastSample = s.lastSample
	}

	uptime := time.Since(proc.startTime)
	return &ProcessStats{
		ProcessID:    proc.id,
		StartTime:    proc.startTime.Format("15:04:05.000"),
		UptimeSec:    int64(uptime.Seconds()),
		BytesRead:    proc.bytesRead.Load(),
		BytesWritten: proc.bytesWritten.Load(),
		BytesPort1:   proc.bytesPort1.Load(),
		BytesPort2:   proc.bytesPort2.Load(),
		ReadRateBps:  rateBpsRead,
		WriteRateBps: rateBpsWrite,
		ReadErrors:   proc.readErrors.Load(),
		WriteErrors:  proc.writeErrors.Load(),
		LastSample:   lastSample.Format("15:04:05.000"),
	}, nil
}

func (pm *ProcessManager) DestroyAll() {
	pm.mu.Lock()
	var connected []*Process
	var forwardProcs []*Process
	for _, p := range pm.processes {
		if p.status == "connected" {
			connected = append(connected, p)
			if p.mode == "forward" {
				forwardProcs = append(forwardProcs, p)
			}
		}
	}
	for _, p := range connected {
		delete(pm.portMap, p.config.Port)
		if p.mode == "forward" {
			delete(pm.portMap, p.forwardConfigB.Port)
		}
	}
	allProcs := make([]*Process, 0, len(pm.processes))
	for _, p := range pm.processes {
		allProcs = append(allProcs, p)
	}
	pm.processes = make(map[string]*Process)
	pm.portMap = make(map[string]string)
	pm.mu.Unlock()

	for _, p := range forwardProcs {
		p.recordSystemEvent("sys.forward_stopped_shutdown", p.config.Port, p.forwardConfigB.Port)
		p.stopForward()
		close(p.statsStop)
		p.port.Close()
		p.forwardPortB.Close()
	}
	for _, p := range connected {
		if p.mode != "forward" {
			p.recordSystemEvent("sys.port_closed_shutdown", p.config.Port)
			p.stopIO()
		}
	}
	for _, p := range allProcs {
		p.closeHistory()
		p.closeSendRing()
	}
}

func (pm *ProcessManager) GoroutineCount() int {
	return runtime.NumGoroutine()
}

func (pm *ProcessManager) GoroutineStacks() string {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return string(buf[:n])
}

// SendTrigger reads one entry from the send queue ring buffer and pushes it
// through sendCh. When raw is true, the bytes are sent as-is (direct send);
// otherwise multistr binary header decoding is applied (quick-panel / queue mode).
func (pm *ProcessManager) SendTrigger(id string, raw bool) error {
	proc := pm.Get(id)
	if proc == nil {
		return fmt.Errorf("process not found: %s", id)
	}
	if proc.status != "connected" {
		return fmt.Errorf("process %s is not connected", id)
	}

	proc.sendRingMu.Lock()
	data, ok := proc.sendRing.Read(65535)
	proc.sendRingMu.Unlock()

	if !ok {
		return fmt.Errorf("send queue is empty")
	}

	var payload []byte
	if raw {
		payload = data
	} else {
		payload = DecodeEntryContentOnly(data)
		if entry, err := DecodeEntry(data); err == nil && entry.Hex {
			if decoded, err2 := EntryToRaw(entry); err2 == nil {
				payload = decoded
			}
		}
	}

	select {
	case proc.sendCh <- sendJob{raw: payload, format: "trigger"}:
		return nil
	default:
		proc.sendRingMu.Lock()
		proc.sendRing.Write(data)
		proc.sendRingMu.Unlock()
		return fmt.Errorf("send buffer full")
	}
}

// AutoSendStart starts auto-send on a process.
func (pm *ProcessManager) AutoSendStart(id string, intervalMs int, mode string, loop bool) error {
	proc := pm.Get(id)
	if proc == nil {
		return fmt.Errorf("process not found: %s", id)
	}
	if proc.status != "connected" || proc.mode != "single" {
		return fmt.Errorf("process %s is not connected in single mode", id)
	}
	return proc.startAutoSend(intervalMs, mode, loop)
}

// AutoSendStop stops auto-send on a process.
func (pm *ProcessManager) AutoSendStop(id string) error {
	proc := pm.Get(id)
	if proc == nil {
		return fmt.Errorf("process not found: %s", id)
	}
	proc.stopAutoSend()
	return nil
}

// AutoSendStatus returns the auto-send status for a process.
func (pm *ProcessManager) AutoSendStatus(id string) (*AutoSendStatus, error) {
	proc := pm.Get(id)
	if proc == nil {
		return nil, fmt.Errorf("process not found: %s", id)
	}
	s := proc.getAutoSendStatus()
	return &s, nil
}

// AutoSendSetInterval updates the interval of a running auto-send.
func (pm *ProcessManager) AutoSendSetInterval(id string, intervalMs int) error {
	proc := pm.Get(id)
	if proc == nil {
		return fmt.Errorf("process not found: %s", id)
	}
	return proc.setAutoSendInterval(intervalMs)
}

// MultistrSave persists the current entries to disk.
// Uses cache if available, otherwise reads from sendq.
func (pm *ProcessManager) MultistrSave(id string) error {
	proc := pm.Get(id)
	if proc == nil {
		return fmt.Errorf("process not found: %s", id)
	}
	proc.multistrMu.RLock()
	cached := proc.multistrCache
	proc.multistrMu.RUnlock()
	if len(cached) > 0 {
		return SaveEntriesToFile(id, cached)
	}
	entries := ReadAllEntries(proc.sendRing)
	return SaveEntriesToFile(id, entries)
}

// MultistrLoad loads entries from disk and writes them to sendq.
// Returns the loaded entries.
func (pm *ProcessManager) MultistrLoad(id string) ([]MultistrEntry, error) {
	proc := pm.Get(id)
	if proc == nil {
		return nil, fmt.Errorf("process not found: %s", id)
	}
	entries, err := LoadEntriesFromFile(id)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		return nil, nil
	}
	if err := WriteAllEntries(proc.sendRing, entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// MultistrReload re-reads entries from sendq into the multistr cache.
func (pm *ProcessManager) MultistrReload(id string) ([]MultistrEntry, error) {
	proc := pm.Get(id)
	if proc == nil {
		return nil, fmt.Errorf("process not found: %s", id)
	}
	entries := ReadAllEntries(proc.sendRing)
	proc.multistrMu.Lock()
	proc.multistrCache = entries
	proc.multistrDirty = false
	proc.multistrMu.Unlock()
	return entries, nil
}

// MultistrReadEntries reads entries from sendq (for GUI to restore).
func (pm *ProcessManager) MultistrReadEntries(id string) ([]MultistrEntry, error) {
	proc := pm.Get(id)
	if proc == nil {
		return nil, fmt.Errorf("process not found: %s", id)
	}
	return ReadAllEntries(proc.sendRing), nil
}

// MultistrWriteEntries writes entries to sendq and marks cache dirty.
func (pm *ProcessManager) MultistrWriteEntries(id string, entries []MultistrEntry) error {
	proc := pm.Get(id)
	if proc == nil {
		return fmt.Errorf("process not found: %s", id)
	}
	if err := WriteAllEntries(proc.sendRing, entries); err != nil {
		return err
	}
	proc.multistrMu.Lock()
	proc.multistrDirty = true
	proc.multistrMu.Unlock()
	return nil
}

// ClearHistory resets the history ring buffer for a process.
func (pm *ProcessManager) ClearHistory(id string) error {
	proc := pm.Get(id)
	if proc == nil {
		return fmt.Errorf("process not found: %s", id)
	}
	proc.ringBufMu.Lock()
	if proc.historyRing != nil {
		proc.historyRing.Reset()
	}
	proc.ringBufMu.Unlock()
	return nil
}

// GetSendRingName returns the send queue shared memory name for a process.
func (pm *ProcessManager) GetSendRingName(id string) string {
	proc := pm.Get(id)
	if proc == nil {
		return ""
	}
	return proc.sendRingName
}
