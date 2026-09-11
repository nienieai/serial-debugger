// Package contract 是守护进程与各客户端之间 IPC 方法面的唯一权威来源。
//
// 为什么需要它：同一个方法名此前在六处各自以裸字符串重复声明 ——
// daemon 的 dispatchForSession、daemon 日志摘要里的第二个 switch、
// client 的类型化包装、CLI 的命令分支、MCP 的工具表、GUI 的绑定方法。
// 方法名是裸 string，写错只在运行时变成 "unknown method"，因此已经产生了
// 实际漂移：multistr.status 这个 daemon 根本不存在的方法名被 CLI 与 MCP
// 同时使用；forward.create 与 process.create+mode:forward 两条通道并存且
// 参数名不同。新增一个端到端能力需要改 9~13 处。
//
// 这里把名字收敛成常量后：改名会编译期报错、写错不可能通过编译、
// 各层引用同一份定义。daemon 侧另有契约一致性测试，确保 contract.All
// 里的每个方法都真的被实现。
package contract

// Method 是 IPC 方法名。使用字符串底层类型是为了能直接与线路上收到的
// 方法名比较，同时保留类型安全。
type Method string

// ── 传输层：会话建立与探活 ──
const (
	Register  Method = "register"
	Subscribe Method = "subscribe"
	Ping      Method = "ping"
	Status    Method = "status"

	// DaemonInfo 返回守护进程的版本、IPC 协议号、PID 与运行时长。
	// 客户端建立会话后立即调用它核对协议版本 —— 守护进程是机器级单例，
	// 很可能是另一次构建留下的。
	DaemonInfo Method = "daemon.info"
)

// ── 端口 ──
const (
	Ports        Method = "ports"
	PortsRefresh Method = "ports.refresh"
	PortsProbe   Method = "ports.probe"
)

// ── 进程生命周期 ──
const (
	ProcessList       Method = "process.list"
	ProcessCreate     Method = "process.create"
	ProcessDestroy    Method = "process.destroy"
	ProcessConnect    Method = "process.connect"
	ProcessDisconnect Method = "process.disconnect"
	ProcessSwitch     Method = "process.switch"
	ProcessSetMode    Method = "process.setmode"
	ProcessWatch      Method = "process.watch"
	ProcessUnwatch    Method = "process.unwatch"
	ProcessWatched    Method = "process.watched"
	ForwardCreate     Method = "forward.create"
)

// ── 数据收发与历史 ──
const (
	SessionSend         Method = "session.send"
	SessionHistory      Method = "session.history"
	SessionStats        Method = "session.stats"
	SessionClearHistory Method = "session.clearhistory"
	SendTrigger         Method = "send.trigger"
	SendRingName        Method = "send.ringname"
)

// ── 自动发送与多字符串 ──
const (
	AutosendStart    Method = "autosend.start"
	AutosendStop     Method = "autosend.stop"
	AutosendStatus   Method = "autosend.status"
	AutosendInterval Method = "autosend.interval"

	MultistrSave   Method = "multistr.save"
	MultistrLoad   Method = "multistr.load"
	MultistrReload Method = "multistr.reload"
	MultistrRead   Method = "multistr.read"
	MultistrWrite  Method = "multistr.write"
)

// ── 历史文件 ──
const (
	HistoryFiles  Method = "history.files"
	HistorySearch Method = "history.search"
	HistoryEnable Method = "history.enable"
	HistoryStatus Method = "history.status"
	HistoryAttach Method = "history.attach"
	HistoryNew    Method = "history.new"
	HistoryDetach Method = "history.detach"
)

// ── 诊断与生命周期 ──
const (
	ClientList Method = "client.list"
	Threads    Method = "threads"
	Goroutines Method = "goroutines"
	Shutdown   Method = "shutdown"
)

// All 列出契约声明的全部方法。
//
// daemon 侧的一致性测试会逐个调用它们，确认没有任何一个是「声明了但没有
// 实现」；新增方法时必须同时加进这里，否则测试不会覆盖到它。
var All = []Method{
	// 传输层
	Register, Subscribe, Ping, Status, DaemonInfo,
	// 端口
	Ports, PortsRefresh, PortsProbe,
	// 进程生命周期
	ProcessList, ProcessCreate, ProcessDestroy, ProcessConnect, ProcessDisconnect,
	ProcessSwitch, ProcessSetMode, ProcessWatch, ProcessUnwatch, ProcessWatched,
	ForwardCreate,
	// 数据收发与历史
	SessionSend, SessionHistory, SessionStats, SessionClearHistory,
	SendTrigger, SendRingName,
	// 自动发送与多字符串
	AutosendStart, AutosendStop, AutosendStatus, AutosendInterval,
	MultistrSave, MultistrLoad, MultistrReload, MultistrRead, MultistrWrite,
	// 历史文件
	HistoryFiles, HistorySearch, HistoryEnable, HistoryStatus,
	HistoryAttach, HistoryNew, HistoryDetach,
	// 诊断与生命周期
	ClientList, Threads, Goroutines, Shutdown,
}

// String 实现 fmt.Stringer，便于日志与错误信息直接使用。
func (m Method) String() string { return string(m) }

// Valid 报告 m 是否是契约声明的方法。
func Valid(m Method) bool {
	for _, known := range All {
		if known == m {
			return true
		}
	}
	return false
}
