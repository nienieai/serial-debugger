package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// 会话录制（v2：一次录制 = 一个目录）。与 history/*.log 写的是同一份数据：
// 同一个 ts、同一个方向、同一个来源，只是编码换成会话文件格式。
//
// 目录里 manifest.json 仅做连接，settings.json 放端口/模式等设置，index.bin 是
// 可删的缓存，数据落在 pkg-NNNN.stlog —— 每个包本身仍是完整合法的 v1.4 文件，
// 写满 pkgMaxBytes 自动切下一个包（帧绝不跨包）。
//
// 这一层只做「直接落盘」：不参与 history.attach/new/detach 那套文件流转，
// 读取侧也不改——GUI 仍走 .log + 环形缓冲，会话目录靠拖进界面或
// `serial-cli stlog-read` 查看。格式规范见 docs/format/stlog-v2.md。
//
// 三个入口都挂在既有路径上，不新增钩子点：
//   - serial.go 的 newHistoryFile()      → openStlogFile()：建 .log 时顺带建 .stlog
//   - serial.go 的 detachHistoryFile()   → detachStlogFile()：关 .log 时封存 .stlog
//   - serial.go 的 recordHistoryAt()     → appendStlog()：每条记录镜像一份
//
// 于是「保存历史」开关、进程关闭、SetSaveHistory(false) 全都自动覆盖。

var (
	// stlogFlushMS 是尾页刷盘上界，对应方案 §2.11 的 flush_max_ms（默认 1000）。
	// 0 表示关掉定时器，只在页满或关闭时写页（"省盘"，写放大 1.60×）。
	// 先在代码里写死；将来接 settings/daemon.ini 的 [storage] 段时改成读设置。
	stlogFlushMS = 1000

	// stlogTickMS 是定时器的检查粒度。不要求精确——过冲一个 tick 无所谓，
	// 真正写盘的是 Writer.Flush，这个循环只负责"到点叫一次"。
	stlogTickMS = 200
)

// openStlogFile 建一个会话目录（v2：一次录制 = 一个文件夹）。
//
// 目录里是 manifest.json（仅做连接）、settings.json（端口/模式等设置）、
// index.bin（缓存）和 pkg-0001.stlog（数据，本身仍是完整合法的 v1.4 文件）。
//
// v2 起不再写 history/*.log：同一份数据逐条对照过（ts / 方向 / payload / 来源
// 全一致），而 .log 存的是 hex 字符串，体积是 .stlog 的两倍。写入只剩这一条路。
//
// 幂等：已经在录就直接返回。全局默认被反复切换、或用户重复点"开始保存"时，
// 不会堆出一串空目录。
func (p *Process) openStlogFile() {
	p.stlogMu.Lock()
	defer p.stlogMu.Unlock()
	if p.stlog != nil {
		return
	}

	dir := historyDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logOp("错误", "建会话目录失败 (进程 #%s): %v", p.id, err)
		return
	}

	// Port/Baud 只取建会话这一刻的快照：设置记的是"开始录制时的配置"，
	// 之后 SwitchPort 不回写（.log 也不记录配置，两边语义一致）。
	// 转发录制另外记 B 口：光靠记录里的方向字节只能知道"这是转发"，
	// 不知道哪个口是 A。
	mode, portB := sessionfile.ModeSingle, ""
	if p.forwardConfigB.Port != "" {
		mode, portB = sessionfile.ModeForward, p.forwardConfigB.Port
	}
	set := sessionfile.Settings{
		V: sessionfile.SlotPayloadVer,
		// Label 记的是「开始录制这一刻的线程名」，不是端口名：改名之后新包会带上
		// 新名字，已封包的字节不动（包头槽是自描述快照，见 v2 §3）。
		Label: p.Name(),
		Port:  p.config.Port,
		Baud:  p.config.Baud,
		Mode:  mode,
		PortB: portB,
	}
	sess, err := sessionfile.CreateSessionIn(dir, set)
	if err != nil {
		logOp("错误", "建会话目录失败 (进程 #%s): %v", p.id, err)
		return
	}
	p.stlog = sess
	p.stlogName = filepath.Base(sess.Dir())
	p.stlogPending = false
	p.stlogStop = make(chan struct{})
	p.startStlogTicker(p.stlogStop)
	logOp("历史", "进程 #%s 开始录制 %s", p.id, sess.Dir())
}

// startStlogTicker 起定时 flush 协程；stlogFlushMS <= 0 时不启动（省盘模式）。
func (p *Process) startStlogTicker(stop chan struct{}) {
	if stlogFlushMS <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(time.Duration(stlogTickMS) * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p.flushStlogIfDue(stop)
			}
		}
	}()
}

// flushStlogIfDue 在「有未刷数据」且「距第一条未刷记录 ≥ stlogFlushMS」时写一次页。
//
// 锚点是"第一个未刷字节到达"，不是"上次 flush"：空闲时不能反复重写同一页，
// 那正是方案 §2.11 里 39× 写放大的来源。
//
// stop 用来识别"这条协程对应的文件已经被换掉/关掉了"——重新开录会建新 channel，
// 老协程可能在 Close 之后才跑到这里。
func (p *Process) flushStlogIfDue(stop chan struct{}) {
	p.stlogMu.Lock()
	defer p.stlogMu.Unlock()
	if p.stlog == nil || !p.stlogPending || p.stlogStop != stop {
		return
	}
	if time.Since(p.stlogSince) < time.Duration(stlogFlushMS)*time.Millisecond {
		return
	}
	if err := p.stlog.Flush(); err != nil {
		logOp("错误", "刷 .stlog 失败 (进程 #%s, %s): %v", p.id, p.stlogName, err)
		p.closeStlogLocked()
		return
	}
	p.stlogPending = false
}

// appendStlog 把一条记录镜像进 .stlog；没有在录时立即返回。
//
// 字段映射：
//   - rx/tx/1/2：hexData 是十六进制串，解码成原始字节；方向用 dirToByte
//   - system：hexData 其实是文案（"sys.port_connected|COM13|115200" 这种
//     i18n key），直接按 UTF-8 字节存，方向 0
//   - origin 非空时用 OriginInline（记录尾带 1 B 长度 + 字符串），空则 OriginNone
//
// 空 payload 直接跳过：格式层本来也不允许零长记录，写进去只是浪费一次锁。
func (p *Process) appendStlog(ts int64, hexData, direction, origin string) {
	p.stlogMu.Lock()
	recording := p.stlog != nil
	p.stlogMu.Unlock()
	if !recording {
		return
	}

	var payload []byte
	dir := sessionfile.DirSystem
	if direction == "system" {
		payload = []byte(hexData)
	} else {
		b, err := hex.DecodeString(hexData)
		if err != nil {
			logOp("错误", "进程 #%s 的数据不是合法 hex，这条不进 .stlog: %v", p.id, err)
			return
		}
		payload = b
		dir = dirToByte(direction)
	}
	if len(payload) == 0 {
		return
	}

	originByte := sessionfile.OriginNone
	originStr := ""
	if origin != "" {
		originByte = sessionfile.OriginInline
		originStr = origin
		if len(originStr) > sessionfile.MaxOriginStr {
			originStr = originStr[:sessionfile.MaxOriginStr]
		}
	}

	p.stlogMu.Lock()
	defer p.stlogMu.Unlock()
	if p.stlog == nil { // 期间被关掉了
		return
	}
	// 格式层要求 ts 严格递增（同一毫秒内的记录会被 +1），所以 .stlog 里的毫秒
	// 可能比环/`.log` 里的晚 1 ms 量级；后两者不受影响。
	if err := p.stlog.Append(uint64(ts), dir, originByte, originStr, payload); err != nil {
		logOp("错误", "写 .stlog 失败 (进程 #%s, %s): %v", p.id, p.stlogName, err)
		p.closeStlogLocked()
		return
	}
	if !p.stlogPending {
		p.stlogPending = true
		p.stlogSince = time.Now()
	}
}

// detachStlogFile 停定时器并封存当前 .stlog；Close 内部会先 Flush 再 Sync，
// 所以这里返回之后文件一定是完整可读的。
func (p *Process) detachStlogFile() {
	p.stlogMu.Lock()
	defer p.stlogMu.Unlock()
	if p.stlogStop != nil {
		close(p.stlogStop)
		p.stlogStop = nil
	}
	p.closeStlogLocked()
}

// closeStlogLocked 关掉当前录制文件并清空状态；调用方必须持有 stlogMu。
func (p *Process) closeStlogLocked() {
	w := p.stlog
	name := p.stlogName
	p.stlog = nil
	p.stlogName = ""
	p.stlogPending = false
	if w == nil {
		return
	}
	if err := w.Close(); err != nil {
		logOp("错误", "关闭 .stlog 失败 (进程 #%s, %s): %v", p.id, name, err)
		return
	}
	logOp("历史", "进程 #%s 已封存 %s", p.id, name)
}
