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
	has := p.stlog != nil
	p.stlogMu.Unlock()
	if has {
		return
	}

	// 工程会话（session.open 打开的项目）：录进这个工程，不另建目录。
	// 「打开了工程又另起一个文件录」只会让历史散成两处，也是用户明确否掉的形态。
	//
	// 这里必须先放锁：resumeRecording 自己要拿 stlogMu。中间那段窗口里别人把
	// 录制建起来了也无所谓，下面拿到锁后会重新判一次。
	if dir := p.sessionDir(); dir != "" {
		p.resumeRecording(dir)
		p.stlogMu.Lock()
		rec := p.stlog != nil
		p.stlogMu.Unlock()
		if rec {
			return
		}
		// 工程目录读不出来（被删/损坏）才退回新建，免得用户点了保存却什么都不录。
		logOp("错误", "进程 #%s 续写工程 %s 没成功，改为新建会话", p.id, dir)
	}

	p.stlogMu.Lock()
	defer p.stlogMu.Unlock()
	if p.stlog != nil { // 拿到锁后重新判：这段时间里可能已经被建好了
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
	// Display 让单个 pkg 拎出来也能按当初的样子显示（工程目录仍以
	// settings.json 为准，见 settingsdoc.go）。
	//
	// 客户端只在用户改设置时才推 display，新建会话那一刻这里是空的；空着就
	// 落 daemon.ini 的默认值 —— 那正是 GUI 此刻在用的那一份，不落等于白加字段。
	display := sessionfile.DisplayStrings(p.Display())
	if len(display) == 0 {
		display = processDaemonSettings().Display
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
		// 帧格式与流控也是「录制这一刻的配置」：工程推回守护进程时按它重开串口，
		// 少了这四项，19200·7E2 的采集会被按 115200·8N1 重开。
		DataBits:    p.config.DataBits,
		StopBits:    p.config.StopBits,
		Parity:      p.config.Parity,
		FlowControl: p.config.FlowControl,
		Display:     display,
	}
	sess, err := sessionfile.CreateSessionIn(dir, set)
	if err != nil {
		logOp("错误", "建会话目录失败 (进程 #%s): %v", p.id, err)
		return
	}
	p.attachStlogLocked(sess)
	// 记下「这个工程在录」：推送回守护进程时据此决定要不要接着录（见 OpenSession）。
	// 只改 save 一个字段，别整份写回 —— 那会冲掉之后改的 label / display。
	if err := sess.SetSaveFlag(true); err != nil {
		logOp("错误", "进程 #%s 写 save 标记失败: %v", p.id, err)
	}
	logOp("历史", "进程 #%s 开始录制 %s", p.id, sess.Dir())
}

// attachStlogLocked 把一个已打开的会话挂到进程上，并起 flush 定时器。
//
// 调用方必须持有 stlogMu，且已经确认 p.stlog == nil —— 「新建会话」和「续写
// 工程」这两条的收尾完全一样，只有选目录和写 save 标记不同。
func (p *Process) attachStlogLocked(sess *sessionfile.Session) {
	p.stlog = sess
	p.stlogName = filepath.Base(sess.Dir())
	p.stlogPending = false
	p.stlogStop = make(chan struct{})
	p.startStlogTicker(p.stlogStop)
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

// resumeRecording 把一个已存在工程的录制接回来：同一个目录、接着往最后一个包里写。
//
// 两个调用点：OpenSession 见到 settings.json 的 save=true（上次是开着保存关掉的），
// 以及工程会话上用户重新打开保存（openStlogFile）。和 SetSaveHistory(true) 的区别
// 就在这点：那个会新建一个会话目录，这个续写旧的。
func (p *Process) resumeRecording(dir string) {
	sess, err := sessionfile.OpenSessionDir(dir)
	if err != nil {
		logOp("错误", "进程 #%s 接着录 %s 失败: %v", p.id, dir, err)
		return
	}
	p.saveHistory.Store(true)
	p.saveHistorySet.Store(true)

	p.stlogMu.Lock()
	if p.stlog != nil { // 已经有录制在跑（理论上不会）
		p.stlogMu.Unlock()
		_ = sess.Close()
		return
	}
	p.attachStlogLocked(sess)
	p.stlogMu.Unlock()
	// 把「这个工程在录」写回工程文件 —— 和 openStlogFile 一样。少了这一句，
	// 「推送后手动开保存」录着录着关掉，下次推送又不会续录了（意图没落盘）。
	// 必须在解锁之后写：SetSaveFlag 是整份读改写，不该压着 stlogMu。
	if err := sess.SetSaveFlag(true); err != nil {
		logOp("错误", "进程 #%s 写 save 标记失败: %v", p.id, err)
	}
	logOp("历史", "进程 #%s 接着录制 %s", p.id, dir)
}

// markSessionNotSaving 把「这个工程别录了」写回它的 settings.json。
//
// 只在用户显式关掉保存时调用（SetSaveHistory(false)），必须赶在 Close 之前写：
// Close 会把目录封存、文件关掉。
//
// 锁内只取指针、锁外写盘 —— SetSaveFlag 是「读文件 → 改一个字段 → 写回」，
// 毫秒级 IO，不该压着 stlogMu（appendStlog 每收一帧都要拿它）。
func (p *Process) markSessionNotSaving() {
	p.stlogMu.Lock()
	w, name := p.stlog, p.stlogName
	p.stlogMu.Unlock()
	if w == nil {
		return
	}
	if err := w.SetSaveFlag(false); err != nil {
		logOp("错误", "清除工程的 save 标记失败 (进程 #%s, %s): %v", p.id, name, err)
	}
}

// closeStlogLocked 关掉当前录制文件并清空状态；调用方必须持有 stlogMu。
//
// ⚠️ 这里**不清** settings.json 的 save 标记。那个字段记的是「这个工程要不要
// 录」（用户意图），不是「现在正在录」（运行时状态）：封存发生在关标签、关 GUI、
// 守护进程退出这条链上，清掉它等于「正常关过一次的工程永远不再自动录」，也就是
// 用户报的「推送后没启动保存」。真要表达「别录了」只有一条路：用户在标签里手动
// 关掉保存（SetSaveHistory(false) → markSessionNotSaving）。
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
