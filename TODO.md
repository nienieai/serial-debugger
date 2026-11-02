# 待办与已知问题

> 本文档维护：**待实现**（按优先级排序的表）、**已知问题**（表）、**进行中版本**（目前是 v0.9.1）与近期版本（v0.6.3 ～ v0.9.0）的已完成记录；
> 更早版本完成情况见 [CHANGELOG.md](CHANGELOG.md)。
> 会话文件（`.stlog`）的格式规范在 [docs/format/stlog-format.md](docs/format/stlog-format.md)（**已进版本控制**：改格式字段必须同 PR 改规范，版本演进见 [docs/format/CHANGES.md](docs/format/CHANGES.md)）。

## 待实现

优先级约定：P1 > P2 > P3（高 → 低）。

| 优先级 | 需求 | 依赖 | 说明 |
|--------|------|------|------|
| P1 | CLI/MCP/daemon 全量 i18n | 基础已有 | 命令输出、帮助、日志等跟随语言 |
| P1 | 守护进程跟随 GUI 启停 | 设置页面 | - |
| P1 | 会话文件：`history.files/open/close/export` IPC + replay 会话 + `process.list` 扩展 | `.stlog` 格式包已完成 | **现状**：只有两条路——守护进程录制、CLI/GUI 离线读文件。`.stlog` 不能挂成会话，GUI/CLI/MCP 都没法经守护进程列文件、回放、导出、做锁交接。详见方案 §5/§6/§7（阶段表 P2）。**接口形状**：`history.files` → `{files:[{file,kind,port,baud,mode,created,entryCount,size,locked,closedAt}],hasMore}`；`history.open` → `mode:"replay"\|"attach"`，返回 `{sessionId,kind,header,entryCount,firstPage}`；`process.list` 条目加 `kind`(`live`/`replay`)、`sessionFile`、`record`、`created`、`entryCount`。**注意**：`entryCount` 是扫描出的总条数（不含坏尾），与 `firstPage` 的首页数据分开；`offset ≤ entryCount` 另加 1e8 防误传，写错即空结果 |
| P1 | 会话文件：文件标签页搜索/过滤 + 真正虚拟滚动 | 现有分页加载 | **现状**：首屏 500 帧 + 「加载更多」，>2000 帧只能翻页；行是一次性建 DOM，没有搜索框，也没有按方向/来源/时间过滤。**待定**：搜索放读侧（`ReadStlogFrames` 目前只有 `offset/limit`，没有 keyword）还是前端过滤——读侧过滤要扫页，前端过滤要全量载入，两边都与「虚拟滚动」互相牵制。**建议**：先做读侧 `keyword` + 时间窗（复用统计块的 `firstTs/lastTs` 做页级预筛，避免逐记录扫），虚拟滚动另起一条（见「完全虚拟窗口」） |
| P2 | Web 客户端 | daemon 内嵌 HTTP + WebSocket | 浏览器操作串口，局域网可用，`--web` 开关控制 |
| P2 | 守护进程端点可配置（本机多实例先行） | 无 | **现状**：端点写死 `\\.\pipe\serial-tool-daemon`（`pipe/pipe_windows.go:42`，客户端 `client/client.go:181` 与 `:984` 都 Dial 同一个常量），单例锁 `Global\serial-tool-daemon`（`pipe/pipe_windows.go:94-102`），日志固定 `<exe>/logs/daemon.log`（`daemon/logfile.go:41-45`，`serial-cli logs` 按同一路径读，`cmd/serial-cli/main.go:219`），客户端拉起守护进程写死 `serial-daemon.exe --silent`（`client/client.go:1092`）——**同机无法跑第二个独立实例**，改端点也无处可配。**方案（第一步，本机多实例）**：`settings/gui.ini` 新增 `[daemon]` 段，`addr` 取值 `default`（默认实例，管道名/锁名/日志名/行为与现在**完全一致**）或 `pipe:<名字>`；`pipe` 包把 `Addr` 常量改为 `DefaultAddr` + `SetInstance(name)`/`Addr()`；锁名加 `-<名字>` 后缀（默认不加后缀），日志名默认 `daemon.log`、有实例时 `daemon-<名字>.log`；守护进程与 CLI 加全局 `--pipe <名字>`（优先级：命令行 > `settings.ini` > `default`），`client.startDaemonProcess` 把 `--pipe` 透传给子进程；GUI 设置页新增「守护进程」栏（端点输入框 + 自动启动开关 + 「重启生效」提示），9 个语言包各加 5 个键；`autostart=false` 时 GUI 不再拉起守护进程。字段语义定成**端点**而非实例名，第二步直接扩成 `tcp://host:port` 不用改字段。**实例名校验** `^[A-Za-z0-9_-]{1,32}$`（Windows 管道名不能含 `\`、`/`、`..`）。**不做**：运行时热切换（改端点需重启 GUI）、TCP、设置远程读取。**风险**：默认路径必须零行为变化，否则老部署与旧 CLI 直接失联；`pipe.Addr` 常量改函数会波及 `client/handshake_test.go`、`client/handshake_order_test.go` 里的 `pipe.Listen(pipe.Addr)` |
| P2 | 远程守护进程（TCP 传输 + 设置分层 + 工作区） | 上一条的端点字段 | **前置认知**：这是三管道回连模型——客户端先建 `st-<clientId>-resp/sub` 两条管道，守护进程再 Dial 回来（`daemon/ipc.go:479-493`），回连解析的是**守护进程自己主机**上的管道名。所以「GUI 与守护进程不在一个地址」不是加字段能解决的，跨机只能走 `\\主机\pipe\…`（SMB，要开 445、依赖 NTLM/空会话），不可取。**第二步（传输）**：加 TCP，并把回连改成**单连接多路复用**（响应/事件本来就带 `id`/`event` 字段、客户端已有 `pending` 表，见 `client/client.go:42-45`；`daemon/ipc.go` 的会话结构要跟着改）+ token 鉴权 + 默认只监听 `127.0.0.1`（显式 `--listen` 才对局域网开放）；非本地端点时 `autostart` 自动失效。**设置分层（与传输解耦，可独立做）**：① **引导层**（必须本地，鸡生蛋：先有地址才知道去问谁）——`[daemon] addr/autostart/label/token`，只由 GUI 读，落在 `settings/gui.ini`；② **共享层**（守护进程拥有，落 `<daemon>/daemon.ini`，**只由守护进程写**；GUI 经 `settings.get`/`settings.set` + 版本号读写，守护进程广播 `settings-changed` 让其它 GUI 刷新）——串口默认值、autosend 间隔、append 后缀、日志语言；③ **本地显示层**（GUI 写 `settings.ini`）——显示模式、hex 格式、字体字号、tab、主题、颜色/图标主题、窗口比例、自动建会话。**硬约束**：设置保存是整文件截断重写（`os.Create`），**一个文件不能有两个写者**——现已按组件拆成 `settings/{gui,cli,daemon}.ini`；共享层仍必须再单独一份文件（不能塞进 `gui.ini`，否则 GUI 保存时会抹掉共享字段）；GUI 本地留一份缓存做离线兜底，连上后以守护进程为准，首次连接用 `settings.seed` 把本地值推上去；远程时守护进程的日志语言必须显式 `settings.set`，否则它读的是自己 exe 旁的旧文件。**第三步（UX）**：工作区切换——最近端点列表 + 每个端点的显示名，切换即换守护进程。**注意**：现在两个 GUI 装在不同目录时会各自写各自的 `settings.ini` 却连同一个守护进程，设置本来就打架，共享层上移正好治这个 |
| P2 | 网络调试能力 | TCP/UDP 协议栈 | TCP Client/Server、UDP 收发，与串口统一的操作体验 |
| P2 | 短连接不计入客户端数量，另记调用次数 | 无 | **现象**：CLI/MCP 每条命令、每次工具调用都 `NewDaemonClient` 起一个短会话（`cmd/serial-cli/main.go:98`、`cmd/serial-mcp/tools.go:742`），attach 时 `AddViewer` 广播一次 `process-changed`、命令结束断开再 `RemoveViewer` 广播一次（`daemon/ipc.go:278` / `:429`）——GUI 收到这对 +1/−1，标签页上的客户端数字就「闪一下」。那个数字是 `viewers.gui + cli + mcp`（`frontend/app.js:3560`）。**需求**：非持续连接的调用**不累加客户端数量**，按来源单独记**调用次数**，是独立计数。**待定**：怎么判定「持续」——① 按来源（`gui` 恒持久、`cli`/`mcp` 默认短，但 CLI 也有长会话场景）；② register 加 `persistent` 字段由客户端声明（要改契约，MINOR）；③ 只认显式 `process.watch`（`daemon/ipc.go:1105`），但 GUI 是靠 register 时的 autoWatch（`daemon/ipc.go:519-525`）进 viewers 的，得一并挪过去。**接口形状**：`viewers` 收窄为「持续连接数」，另加 `calls:{cli,mcp}` + `lastCallAt`（进程级累计，随进程销毁归零；不跨守护进程重启持久化）；标签页数字只读 `viewers`，悬浮卡（`c-tabpop`）加一行「CLI 调用 N 次 · MCP N 次」。**注意**：`viewerSummary()`（`daemon/serial.go:235`）固定返回三键，加 `calls` 时别和 viewers 混进同一个 map |
| P2 | GUI CPU 占用优化 | `stats-count` 节流、批量 DOM 渲染 | - |
| P2 | 按视口字符预算限制历史缓存 | 实际字体量测（charW / lineH） | **现状**：缓存预算写死 `HISTORY_CACHE_MAX_CHARS = 6M` hex 字符（≈6000 屏，`history.js:33`），预算裁剪分支还有隐式下限 200 条——窗口大小、字号、缩放全没被考虑。**方案**：`C = cols × lines`（`cols = floor((clientWidth - padding)/charW)`，`charW` 用当前实际字体 canvas measureText 量，`lineH` 取 `.data-line` 的 computed lineHeight）；渲染窗口 = `C × (1 + 2~3 屏缓冲)`，缓存回看 = `C × N 屏`（N≈30~50）；裁剪以字符/字节为预算单位、按整条丢弃，丢弃量向上取整保证不超预算；**加最小帧数下限 `MIN_FRAMES = 5`**（大包时字符预算可能只够 1 条，不能把相邻帧裁光），仅绝对字节兜底（估算 `hex×2 + segments ≤ 32 MB`）可压过它。**注意**：① 包大小差 200 倍（21 B ~ 4 KB），包数只能当取整粒度、不能当预算单位；② 单条 4 KB 换行后可能超过一屏，窗口要按字符量而非「条数×固定行高」；③ resize / 字号变更 / 缩放后要重算并重裁。**参考数字**：1000×400 显示区、15 px 字体时 `C≈1950`，40 屏 ≈ 6.2 万 hex 字符（比现在小两个数量级） |
| P2 | 完全虚拟窗口（虚拟滚动） | 行高模型、上一条的视口字符预算 | **现状**：显示区是「窗口渲染 + 到边加载」——DOM 固定 150~400 行（`calcRenderCount()`，`history.js:62`），滚到顶再换一批并手动补偿 `scrollTop`（`_renderStart/_renderEnd`），滚动条只反映当前窗口而非整个缓存，`锁定滚动`/未读块都是为这套补偿服务的。**已做的轻量化 A**：`.data-row` 的 `content-visibility: auto` + `contain-intrinsic-size`（`style.css:961`），屏外行跳过布局/绘制。**目标**：顶部/底部占位 div + 只挂载可见的 20~40 行，滚动条代表整个缓存（回看 30~50 屏），删掉窗口补偿逻辑。**关键前提**：行高可算——`rowHeight = 时间戳行 + ceil(displayChars/cols) × lineH + 间距`（hex 模式 1 字节 ≈ 5 显示字符），不需要逐行测量。**必须一起定的取舍**：① 跨未挂载行的文本选择、Ctrl+A、浏览器查找会失效 → 用「复制全部缓存」替代；② 裁剪最旧行 / 追加新行 / resize 改变 cols 后按「首行索引 + 行内偏移」重锚 `scrollTop`，是最容易出 bug 的部分；③ 4 KB 大包单行可能超过一屏，滚动映射必须按字符而非条数；④ 高度估算偏差会让快速拖动微抖，可在每次渲染后实测可见行高度收敛。**工作量**：中（行高模型 + 滚动映射 + 锚定 + 选择策略），建议配「窗口尺寸 / 字号 / 4 KB 大包」三类夹具 |
| P2 | 高吞吐量下操作系统调度延迟优化 | 驱动接收缓冲区调大 | - |
| P2 | 辅助线程（超阈值自动创建） | - | - |
| P2 | Modbus RTU 协议支持 | Modbus 协议库 | Python 版已实现 |
| P2 | MCU 协议感知 | - | `\r\n` 分隔符 + `#` 回显过滤，Python 版已实现 |
| P2 | 智能重分包（还原粘包/断包） | 协议规则（分隔符/长度/CRC）、读循环字节时间戳 | **问题**：系统非实时调度 + 驱动缓冲会让一次 `Read` 返回**多帧粘在一起**或**半帧**；读循环还有 16 ms 合包窗口（`daemon/serial.go` 的 `readCoalesceWindow`）会进一步放大两种失真。现象：界面一行里出现两帧、一帧被拆成两行、Modbus CRC 看起来对不上。**目标**：守护进程侧加「帧重组器」，按会话可配规则把字节流还原成逻辑帧，GUI/CLI/MCP/导出统一受益；**不改原始存储**（仍存原始字节，帧信息只作为附加标记/索引），可一键切「原始 / 还原」视图。**规则（优先级从高到低）**：① 空闲间隔——按波特率折算 3.5 字符时间或显式配置 ms，需要读循环记录「每段首尾时间 + 字节数」；② 行分隔（CR / LF / CRLF）；③ 长度字段（偏移、宽度、字节序、是否含头/CRC）；④ 固定长度；⑤ 协议校验（Modbus RTU：地址+功能码+字节数推导总长，CRC-16 验证，失败则前后试探重同步）；⑥ 自定义正则/脚本（后期）。**硬约束**：① 时间戳只有 ms 精度，且 16 ms 合包后已无法逐字节还原真实到达时间，间隔规则只能近似；② 推断错误必须可回退——帧标记只用于展示/索引，原始字节永远保留；③ 3.5 字符时间在 921600 baud 下约 40 µs，Windows 定时器达不到，必须允许用固定长度/CRC 兜底；④ 不影响发送路径与既有 history 包格式。**验收**：构造随机切分（同一帧 1~4 段）与随机粘包（2~4 帧合并）夹具，统计还原率 / 误判率 / 额外延迟；Modbus 用真实设备帧验证。**工作量**：中（规则引擎 + 读循环时间戳 + 双视图），建议先做 Modbus RTU 与行分隔两种规则。**口径**：本项目里说的「帧解析」就是这一项（把字节流还原成协议帧），**字段解码**（地址/功能码/寄存器值）如要做也归在这里；`.stlog` 存储层只存原始字节，不猜帧边界 |
| P2 | 串口连接来源识别 | 端口列表 | 端口列表显示占用端口的进程/线程信息，自身连接标注来源标签 |
| P2 | 创建虚拟端口 | 虚拟串口驱动 | 创建成对虚拟串口，无硬件即可测试串口通信 |
| P2 | 文本/Hex 对照显示 | 分段解码已有 | 历史框分栏显示 Hex+偏移 与文本对照，选中联动 |
| P2 | 输入框镜像渲染迁移至 Go | Go 端文本分段解析 | 将 send mirror 的逐字符遍历移至 Go 端 |
| P2 | daemon 重启自动恢复磁盘历史 | `historyFile` 自动加载 | daemon 启动时**不主动**恢复；只有用户/客户端打开「保存历史」时，`openHistoryFile` 里的 `restoreLatestSession()` 才会在环形缓冲为空的前提下拉「最近一个会话」（v2 起读 `.stlog` 会话目录，`history/*.log` 已不再写）。要的是「daemon 一起来就把上次的会话挂回去」，但得先定「恢复哪个」—— 按端口、按最近修改时间、还是按上次 GUI 记下的标签列表 |
| P2 | CLI 与守护进程的多色输出（彩色日志/彩色 CLI） | 无（Windows 需 VT 支持） | **现状**：全仓库**没有任何 ANSI 转义**，控制台与日志都是纯文本。守护进程的每一行由 `formatLogLine(ts, category, msg)`（`daemon/logfile.go:93`）单点产出，`logOp()`（`daemon/main.go:137`）把**同一行**既打印到控制台又写进 `logs/daemon.log`；CLI 各命令是裸 `fmt.Fprintf(os.Stdout/os.Stderr, …)`。**目标**：① 终端里按类别着色（启动=青、连接=绿、串口=蓝、IPC=灰、关闭=黄、错误=红，时间戳弱化）；② `serial-cli logs` **读**文件时按 `[类别]` 着色（文件本身仍纯文本）；③ CLI 输出：错误红、警告黄、`check` 的三层结果与 `probe` 的命中/未命中分区着色，表格列对齐。**硬约束**：① **写进文件的行绝不能带转义** —— 只在「面向控制台的 writer」上着色，日志文件保持纯文本，否则 `serial-cli logs` 与外部比对会看到乱码；② **非 TTY 自动无色**（管道、重定向、`--silent` 后台模式），另给 `--color=auto\|always\|never` 与 `NO_COLOR` 环境变量；③ **MCP 绝不能着色**（stdio JSON-RPC，任何转义都会破坏协议）；④ Windows 控制台要先开 `ENABLE_VIRTUAL_TERMINAL_PROCESSING`（加在 `daemon/console_windows.go` 现有 `setConsoleOutputCP` 旁边，开失败就降级无色）——该文件的注释已经证明 `--silent` 时 `GetConsoleMode` 会失败，正好是「无 TTY」的天然判据；⑤ 现有断言 stdout 文本的测试要统一走 `--color=never` 或依赖非 TTY 自动无色。**顺带**：`serial-cli logs` 的类别列目前是 `[%-6s]` 定宽，着色时注意中文类别宽度。**工作量**：小（一个 `internal/color` 小包 + 三处调用点），风险低但触及所有命令输出，建议配一个输出快照夹具。可选扩展：深浅背景两套配色、`--theme` 选择 |
| P3 | GUI 性能面板 | `session.stats` IPC 已就绪 | - |
| P2 | Linux 版本 | 跨平台构建脚本 | 产出 `serial-{gui,daemon,cli,mcp}` 的 Linux 版本；GUI 走 Wails 的 Linux 后端（WebKitGTK），daemon/CLI/MCP 是纯 Go 可直接交叉编译。**已有 Linux 虚拟机，可在其上构建与实机验证**（需要时授权使用） |
| P2 | 标签高级信息浮窗 | `session.stats` / `GetClients` | 现在悬停标签只有原生 `title`（会话号 / 数据位 / 空闲进程）。改成**自绘浮窗**：连接状态（端口、波特率、是否转发、客户端数）+ 当前速率（B/s）+ 累计数据量（Tx/Rx 字节数与帧数），样式跟主题变量走，不依赖原生 tooltip。**0.8.3 已落地主体**：自绘浮窗 `c-tabpop`（`app.js:3123` 起）——连接状态/速率条/收发比/客户端/错误/运行时长，关串口后累计值仍显示（`live` 与 `hasData` 分开）；文件标签另有 `_tabPopRenderFile`，与会话卡共用骨架与两条进度条。**剩余**：累计 **Tx/Rx 帧数**未单独显示（当前只有字节数与速率） |
| P2 | 标签存储设置分离 | 历史/配置按 tag 存 | **后端已就绪（0.8.3）**：`history.enable` 带 `processId` 按进程开关、`history.status` 可查单进程；开启立即建文件、关闭保留已写内容；GUI 绑定 `SetHistoryEnabledFor` / `GetHistoryStatusFor` 已加。**剩余（下轮）**：标签级 UI 开关（计划放标签悬停卡片）、标签设置随会话持久化 |
| P2 | 新建标签默认保存历史 | 依赖上一条 | 全局开关已改为「默认自动保存」（`gui.ini` 的 `auto_save_history`），**默认关闭 = 默认不保存**；新标签继承该默认值，用户可在标签里单独开启（UI 待下轮） |
| P2 | 新建会话：从已打开线程进入 | `GetThreads` 已有；`sessClaim` / `MoveSessionToWindow` 已有 | **前提变了**：标签页不再永远等于串口进程数量 —— ①`new` / `settings` / `stlog` / `daemonlog` 这几类页本来就不是进程；②多窗口之后进程归某个窗口，别的窗口看不到它的标签；③CLI / MCP 建的进程只有「无主认领」那一次机会（`GetSessionsForWindow` 里的 `claimUnowned`）会被某个窗口捡走，之后就在那个窗口里了。**需求**：新建会话页（`frontend/tabpage.js:506` 的 `createModeChooserPage`，现在三张卡：单端口 / 端口转发 / 从文件打开）加第四张卡「从已打开线程进入」；点了**在原地换一屏**（不新开标签页），列出守护进程上所有串口进程：名称、端口、波特率、状态（idle / connected）、模式（single / forward）、来源（`viewers`）、是否已被窗口占用（`viewers.gui > 0`）。选中后在本窗口建一个绑定到它的标签。**要点**：①列表数据用现成的 `GetThreads()`（daemon 全量、含 viewers），但它没有「归属哪个窗口」—— `sessOwner` 只在 GUI 进程自己的内存里，得补一个窗口视角的字段，否则判不出「这个进程是不是已经在本窗口里」；②已被**别的**窗口占着时两种做法待定：(a) 提示「已在窗口 X 打开」不给进，(b) 用 `MoveSessionToWindow` 把它转过来（会从那个窗口消失）；③空列表 / 守护进程离线要和另外两张卡一样置灰并给提示，别让人点了没反应；④**前提问题**：「同一个进程能不能同时开在两个标签里」—— 允许的话，`emitSession` 的按会话定向、`state.tabs` 里 sessionId→页的一一映射都要改；不允许就得把「已占用」挡在入口。**注意**：卡片都由 `makeCard` 统一生成（图标 + 名称 + 三条特性 + 整宽 CTA），二级菜单是复用 `c-mode-card` / `o-chooser__grid` 还是新开一套列表样式，得先定 |
| P2 | 多字符串条目跟着工程走，没有工程就只存内存 | 工程 `settings.json` / 会话目录 | **现状**：条目存在守护进程的 `sendq` 环里，另有一份 `<daemon>/multistr/<processId>.json`（`daemon/multistr.go:208` 的 `multistrDir`），**按进程号命名** —— 进程一销毁（关标签、重建会话）文件就再也对不上，工程目录搬走也不跟着走；GUI 在条目变化时调 `MultistrSave(tab.sessionId)`（`frontend/app.js:399`）触发落盘。**需求**：①工程会话 → 条目写进工程目录，随工程一起走（换台机器也还在）；②没有工程的普通会话 → **只留内存**，不再往 `multistr/` 落盘。**待定**：存进工程的 `settings.json`（和 header / save 并列）还是单独一个 `multistr.json`（后者省得每改一条就重写整个工程设置）；工程里已有条目和界面当前条目谁优先（打开工程时覆盖，还是合并去重）。**注意**：CLI / MCP 也有 `multistr save/load`，改语义时两边要指向同一处，否则同一个进程两个入口看到的条目不一样 |
| P2 | 多字符串条目的输入框也要分段着色（和发送框一致） | 现成的 send mirror | **现状**：发送框有一层等宽 mirror 按 `display_colors` 着色 —— `_renderSendMirror` / `_renderSendMirrorHex`（`frontend/app.js:602` / `:653`）把文本模式的控制字符、Hex 模式的非法字节（`sendHexBad` 标红）、前缀与占位符分别上色，`_syncSendMirror`（`:725`）负责滚动同步。多字符串面板每条还是裸 `<textarea>`，写 Hex 打错一个字符完全没有提示。**需求**：条目输入区复用同一套 mirror，文本 / Hex 双模跟**条目自己的** `hex` 开关走（不跟全局），非法 Hex 当场标红。**要点**：①条目是一行一个 textarea，mirror 要按行懒创建（几十条时别一次性全建）；②行高与滚动必须和 textarea 严格对齐（现成实现靠同一 `font-family` + `scrollTop` 同步，`app.js:748` 那段字体串要在条目上也生效）；③条目在可滚动面板里，两层滚动同步别互相打架 |
| P2 | 单端会话的「脚本」页 | 执行环境选型（未定） | **需求**：单端会话里加一个脚本页 —— 写一段小脚本按脚本收发：发一串命令、等响应、按超时或正则判断、循环与延时，结果打在页内。**用途**：产线点检、协议冒烟，把「多字符串 + 自动发送」拼出来的固定流程变成可复用的一段。**待定（先定这个才谈实现）**：①在哪执行 —— 前端 JS（能直接摸收发绑定，但随标签页一起被关掉）还是 Go 侧起解释器（可无界面跑、易做超时与取消，但要定沙箱与 API 面）；②脚本能碰什么 —— 只给 `send` / `waitRx` / `onRx` / `log` 这几个原语，还是放开到全部绑定；③和守护进程的关系 —— 脚本跟着标签走（关标签就停），还是挂到守护进程上（关掉 GUI 还在跑，那就得配生命周期与权限）。**不做**：通用编程环境（不提供任意文件/网络访问）。形态上它是「工具」之一，可小窗化/贴边，见下一条 |
| P3 | 单端工具：可小窗化、可拼成页内带标签的贴边工具组 | 现成的面板拖拽 / 窗口机制 | **需求**：单端会话里的工具（多字符串面板、自动发送、脚本页……）统一成一种「工具」：①能**小窗化** —— 拖出去成独立小窗（可置顶，绑定当前会话，会话关掉小窗跟着关）；②能**拼回来** —— 贴在页面左右/下边缘，多个工具叠成一组**带标签的**贴边面板（标签切换、拖拽重排、宽高可拖）。**现状**：多字符串是右栏面板（`state.quickPanelRatio` + `initQuickPanelDrag`，只能拖宽窄），自动发送是页面里固定一行，速率与统计挤在状态栏 —— 三者形态各自写死。**依赖**：标签 tear-off 那套（`StartTearOffDrag` / `driveTabDrop`）已经把「页 → 窗口」和落点判定做通了，可以复用；小窗要单开一类窗口（不进 `tabs`、不能被拖成标签），得在 `createWindowNamed` 之外单起一条。**注意**：①贴边组要参与会话页布局（改宽会挤压历史/发送区，`send_ratio` 那套比例要跟着重算）；②小窗和贴边组操作的都是同一个会话，数据必须走 `emitSession` 的定向帧，不能广播；③关会话时要把它的所有小窗与贴边组一起收掉，否则留幽灵窗口 |
| P2 | 会话文件：续写 / 写协程与尾页缓冲 / origin 快照合并去重 | 方案 P3 阶段 | **现状（2026-11-01 更新）**：`daemon/stlog.go` 同步直写，写失败只记日志。**工程会话已经能续写**：`session.open` 见 `settings.json` 的 `save=true` 走 `resumeRecording`，工程会话上重新打开保存也走同一条（`openStlogFile` 见到 `sessionDir()` 非空），续写同一目录、同一个未封的包；**普通串口会话**重开仍另起一个会话目录。仍缺：写协程/有界队列、尾页撕裂重写、`origin` 字典跨会话复用（现在随文件重建）。**方案要点**：写协程改**有界 8 MB 字节队列**，生产者阻塞不丢，超 1 s → 停录 + 事件；尾页重写要求「前缀字节一致」才允许接续（§2.9）；新 origin 必须先落快照再允许被引用（§2.5） |
| P2 | 会话文件：导出 CSV / JSON + 时间范围导出 | 文本导出已完成 | **现状**：只有 `ExportStlogText`，整文件 `.txt`。**方案**：CSV 要防公式注入（`= + - @` 开头加前缀）与 RFC 4180 转义，`unknown(id=N)` 照实落格；`history.export` 的 `fromMs/toMs` 尚无可用的实现，GUI 上也没有范围选择入口 |
| P2 | 会话文件：「由文件开始」+ 推送与锁交接 | 上一条 IPC | 方案 §8：`history.open` 的 `mode:"attach"` 把磁盘历史灌进环形缓冲。**两条路径必须分开**：只有 live 录制会话移交才需要 `flush+fsync` 后交出写句柄；本地只读标签没有写句柄，不需要 fsync。混用会让只读标签平白多一次刷盘 |
| P3 | 每个标签一个图标 | 图标包 `resources\icons\` | **0.8.4 已落地**（`0.8.4-dev\tab-icon-preview-v2.html` 的 P4 方案）：类型靠左侧 14px 图标形状（`terminal` / `swap` / `file` / `folder` / `settings` / `plus`，中性灰、随图标主题切换），状态靠图标右下角 5px 点（绿=已连、紫=转发、半透明灰=空闲、空心环=只读）。同时收敛了原来「六套信号说三件事」的混乱：`.tab-dot`、`.tab-idle` 斜体、`.tab-forward` 绿加粗、未激活压暗都已删除，参数/保存/速率改成不拥挤才显示的信息槽（`tabsScroll[data-lvl]`，顺序 速率→保存→参数→名称省略）。补了内置图标 `file.svg` / `folder.svg`；保存标记复用已有的 `save`。**剩余**：外置图标包目前只认 `system` 一套，按标签类型自定义图标还没做 |
| P3 | all-in-one 融合可执行 | 现有四个入口 | 一个 exe 同时包含 GUI / CLI / MCP / daemon，按第一个参数或调用方式切换（双击 / `--cli` / `--mcp` / `--daemon`）。难点在 Windows 子系统：GUI 需要 `-H windowsgui`，而它会让 CLI 的 stdout 无处可去，需要 `AttachConsole`/`AllocConsole` 一类的处理，或分成两个构建标签 |
| P3 | 会话文件：设置页存储区块 + `keep_days` 清理 + `min_free_mb` 水位停录 | 方案 P4 阶段 | 现在 `flush_max_ms` / `max_size_mb` / `min_free_mb` / `keep_days` 都没有入口，值散在常量里。方案 §9/§10 已写死策略：清理只在守护进程启动时做一次；只删**未被写者占用**且 `created` 早于 `keep_days` 天的文件；**坏头文件不删**；每次删除记日志；`keep_days=0` 关闭清理。水位停录：写整页会越过 `max_size_mb` 或吃掉 `min_free_mb` 时停录并告警，不写出超限那页（文件保持可读） |
| P3 | 会话文件：边界与性能压测 | 方案 §12 清单 | 现有 3 组单元测试 + 6 个夹具（跨页切帧、坏链、影子恢复、超大帧），缺 512 MB 量级、崩溃注入（页尾撕裂/双影全坏）、断电后续写、CSV 大文件与导出耗时这几项。方案 §12 列了逐项验收标准，可直接当用例表 |
| P3 | 把 `.log` 也读进标签页 | 文件标签页已有 | 老格式（`history/*.log` 文本）拖入目前仍走原逻辑；与 `.stlog` 共用一个标签页壳能省一套行渲染与导出路径，但要处理「文本行 ≠ 帧」的映射（时间戳格式、方向前缀） |
| P2 | Linux 串口权限：聚合版走「装规则」，一次到位 | 分体版已完成（见 v0.9.0） | **策略（2026-11-02 定稿）**：按发布形态分两条 —— **分体版**（现在这四件套，解压即用）**绝不写系统文件**，只对当前设备节点做临时授权（已实现：`sysperm` 包 + GUI 横幅 + `serial-cli fix-permissions`，见 v0.9.0）；**聚合版**（all-in-one，安装式）才允许一次 pkexec 往 `/etc/udev/rules.d/60-serial-tool.rules` 写 uaccess 规则，之后永不打扰。规则形态已经在 `build/linux/install.sh:66-105`（`write_udev_rule` + `fix_permissions`，`install -m 0644` + `udevadm control --reload-rules` + `udevadm trigger --subsystem-match=tty`），缺的只是把它搬进二进制。**红线**：pkexec 只执行**编译进二进制的固定脚本**，绝不执行 `~/.local/lib/serial-tool/install.sh`——该目录在 `$HOME` 下、当前用户可写，pkexec 它等于送提权后门；也不做 polkit 免密规则（`result.yes` 会让任意本地用户无声提权，而"点一下不输密码"这档 Linux 上不存在）。**检测判据**：聚合版可以继续用「规则文件是否存在」判已修复（`install.sh:107-116` 的 `need_permission_fix`），分体版不能用 —— 规则在≠节点可读，已改成 `access(2)` 实探。**MCP 绝不参与**（stdio JSON-RPC，弹窗会卡死协议）。**工作量**：聚合版本体（见「all-in-one 融合可执行」）落地后半天 |
| P2 | 串口数据绘图（示波器式曲线） | 帧/字段解析（见「智能重分包」）、零第三方前端库 | **需求**：把收到的数据画成实时曲线（多通道、自动缩放、暂停、导出），用于看传感器 / ADC 数值趋势，而不是靠盯 Hex。**现状**：只有文本/Hex 显示（`frontend/history.js` 渲 `renderSegments` 出的 `[]Segment`），全仓库没有任何图表代码，`index.html` 也不引第三方 JS。**取数三层，从简到难**：①文本行按分隔符切数字（`-?\d+(\.\d+)?`）——先做这个；②定长/带长度字段的二进制帧（把 `probe.toml` 的规则思路扩展成「绘图规则」：偏移、类型、字节序、缩放系数）；③复用下一行「智能重分包」的帧规则 + 字段解码。**渲染**：Canvas 2D 自绘（网格/坐标轴/多折线/图例），画布要按 `devicePixelRatio` 放大。**性能是主要风险**：230400 baud ≈ 23 KB/s，一屏可能上万点，必须按像素列做 min/max 抽稀，且不能每帧全量重画（增量画、只重画尾部）；数据缓冲别放进 `appendLine` 的主线程路径——用 `connectFrameStream` 单独收、`requestAnimationFrame` 批量画。**交互**：暂停/继续、时间窗（5/10/30/60 s）、Y 轴自动/手动、通道开关、导出 CSV/PNG。**形态**：先做会话页里的可拖面板（复用 `.o-divider` 拖拽体系，与多字符串面板并列），独立标签页以后再说。**注意**：①抽稀只影响画，不能丢原始数据；②暂停时仍要继续收（否则切回来断档）；③历史/`.stlog` 回放能不能画，要先定「历史条目 → 数值」的解析时机，离线解析可复用同一套规则。**工作量**：解析 + 面板 1 天，Canvas 渲染 1~2 天，性能与交互 1~2 天 |
| P2 | 协议驱动的串口参数跟随（设备换速率，工具跟着换） | `serial.Port.SetMode`；需新增 `process.setparams` IPC 与协议规则层 | **场景**：设备按自己的协议在运行中改速率 —— 上电 9600、握手成功后切 115200；或收到「切换指令」先回 ACK 再切。现在工具一旦连上就固定参数，**设备换了速率之后这边还傻等在老速率上**（乱码、超时），只能人工断开重连。**现状**：`go.bug.st/serial` 的 `Port.SetMode()` 支持运行中改参数，但守护进程**没有暴露**任何「改运行中会话参数」的入口 —— `contract` 里只有 `process.setmode`，那是 single/forward 模式、不是串口参数；`Process` 也没有 `SetSerialConfig`，GUI 连接中同样改不了波特率。**要做三块**：① 新 IPC `process.setparams`（pid + baud/dataBits/stopBits/parity）：调 `port.SetMode` → 成功后更新 `p.config` 并广播 `process-changed`（GUI 标签与状态栏、CLI `sessions`、MCP `serial_sessions` 立刻显示新参数）；② **协议规则层**（TOML，风格对齐 `probe.toml`）：`[transitions]` 声明触发与动作，两种触发源都要 —— **本端发出**匹配指令（例 `AT\+BAUD=(\d+)`，可声明「发送后立即切」或「等 ACK 再切」）与**收到**匹配帧（例 `OK\+BAUD=(\d+)`）；动作至少支持 baud，顺带 parity/stopBits/dataBits；③ 观察点放在守护进程的读循环/发送路径（`daemon/serial.go`），切换成功写一条 `sys` 消息进历史（「参数已切换 115200→921600（规则 #3）」），失败也要留痕。**时序与坑**：①`SetMode` 会清收发缓冲，切换瞬间丢字节 —— 规则里必须能表达「先切后发」与「先收 ACK 后切」两种顺序，不能默认一种；②设备可能切完立刻发数据，切换要抢在下一帧之前完成；③切换失败（驱动拒绝/端口已关）要回滚旧参数并广播失败原因；④自动发送/多字符串队列正在跑时，切换前暂停、切完续；⑤同一会话可能被多客户端看着（GUI+CLI+MCP），切换是**会话级**的，所有端都得收到广播，不能各改各的；⑥规则误匹配的代价很高（会把好好的连接切坏），所以要能按会话启用/停用、并保留「手动恢复上一组参数」。**与下一行的分工**：本项是**协议确定触发**（判据来自规则，不猜）；P3 的评分自动识别只在没有规则可依时兜底。**工作量**：IPC + 执行/广播 1 天，规则层与两类触发 1~2 天，用真实设备或 com0com 测试 1 天 |
| P3 | 按数据流自动纠正串口参数（波特率 / 校验位 / 停止位） | `serial.Port.SetMode`（已可用）、`probe` 的波特率表与规则、`decode` 的容错统计 | **需求**：连上之后发现乱码 / 帧校验不过时，工具能判断出「参数不对」并给出（或自动切到）正确组合，省掉关端口反复试参数的来回。**可行性**：`go.bug.st/serial` 的 `Port.SetMode(*Mode)` 支持**运行中改参数**（Windows 底层就是 `SetCommState`），不必关端口 —— 这是本需求成立的前提。`probe` 已有波特率扫描与 `modbus_crc` 校验（`daemon/probe.go`），但它**要独占打开端口**，被会话占着就跳过（`probe.go:323`），所以不能直接复用，只能在已打开的 `serial.Port` 上做。**判据（核心难点）**：需要一个「这串数据像不像有效数据」的评分函数 —— 可打印字符比例、UTF-8/GB2312 解码成功率、行分隔符出现频率、按 `probe.toml` 规则或 Modbus CRC 的通过率；阈值必须用真实设备样本先标定，否则误判比乱码更烦人。**搜索策略**：组合爆炸（波特率 × 校验 × 停止位），分层来 —— 先固定 8N1 扫波特率，命中后再试 parity/stopbits；每档采样 200~500 ms，切换前暂停自动发送，切完给 1 s 观察窗。**交互**：只做「手动点『自动匹配参数』→ 给结果让用户确认」；**不要**默认后台自动改（正在调试的人会被打断）。**风险**：①`SetMode` 会清收发缓冲，切换瞬间丢数据；②个别驱动/设备 SetMode 后行为异常，要能降级成「关端口 → 改参数 → 重开」并保留历史；③切错参数可能让设备进入未知态，必须有「一键回到上一个可用组合」。**与其他两条的分工**：`probe` = 连之前识别设备；上一行「协议驱动参数跟随」= 协议明确宣告的切换；本项 = **没有规则可依时的猜测兜底**。**为什么排 P3**：判据要靠多台真实设备实测，做不准就是负价值；建议先把上面的「绘图」做出来，让数据看得见，再谈自动判断。**工作量**：判据与搜索 2~3 天（含实测），UI 半天 |

## 已知问题

状态取值：`已修复`（代码已改，含回归测试）、`已文档化`（只补了文档/说明，代码未改）、`部分修复`（有缓解方案，未完全解决）、`已缓解`（防护已生效，仍有极小概率触发）、`已知限制`（预期行为，不计划修复）、`待处理`（已记录，尚未处理）。

> 表内**不要再插空行**——Markdown 的空行会把表格截断成多张，第二张起没有表头，渲染出来是散行。（检查脚本 `todo_table_check.py` 在工作区的 `_audit/` 下，不在本仓库内。）

| # | 问题 | 状态 | 说明 |
|---|------|------|------|
| 1 | 清空后内容太少时上滚不触发 `expandHistory` | 部分修复 | display 内容不足以产生滚动条时 `onscroll` 事件无法触发；已保留可点击的「↑ 向上滚动加载更多」入口；改进方向：onscroll 替代方案，内容不足时也能召回 |
| 2 | 重复快速点击「加载磁盘历史」可能触发多次请求 | 已缓解 | 已加入 `_diskLoading` 互斥和 `pointerEvents` 禁用，极快连击下仍有极小概率穿透 |
| 3 | `loadTabHistory` 重新从 daemon 取数据后 `_clearedAt` 标记丢失 | 已知限制 | 切换标签导致缓存被重新覆盖时清除标记线消失，所有历史均可见；此为预期行为 |
| 4 | `go vet` 报告 `ringbuf_windows.go` 多处 `unsafe.Pointer` 使用警告 | 已知限制 | 2 处（2026-11-01 清掉零引用的 `OpenSharedRing` 后从 3 处减为 2 处，剩下的在 `ringbuf_windows.go` 的 `mapAddr` 转换）。转换对象是 `MapViewOfFile` 返回的文件映射地址，不是 Go 堆内存，GC 不会移动它，生命周期由 `UnmapViewOfFile` 约束，属 vet 保守误报。**注意 `go vet` 结果会被构建缓存**：换用干净的 `GOCACHE` 才会重新分析，否则可能读到过期的「干净」结果 |
| 5 | `.settings-page--overlay` 遗留死代码 | 已修复 | v0.6.5 删除（`frontend/style.css` 定义与后代选择器两处，JS 无引用） |
| 6 | `frontend/style.css` 头部注释版本号过时 | 已修复 | v0.6.5 更新为 `v0.6.5 — CSS` |
| 7 | 控制字符 CRLF 渲染双路径并存 | 已知限制 | `renderSegments()`（Go 解码新路径）合并为单节点 `data-ws="crlf"`（标记 `←↵`），旧 decoder 路径拆为 cr + lf 两个节点；后续统一路径后可简化 |
| 8 | send 环无跨进程互斥，多客户端并发写同一会话可能损坏 | 已知限制 | `ringbuf` 实现是 SPSC：`Write` 对 head 做无 CAS 读-改-写，head 发布与 count 自增分两步；`client.SendWrite` 由各客户端进程各自打开环直接写入，跨进程无互斥。**实测未能触发**：8 客户端 × 256 B × 5 轮、16 客户端 × 1024 B × 4 轮（共 104 次并发写）全部数据完整——写窗口只有几百字节的拷贝时间。彻底修复需选定「发送全部走 IPC」或「加跨进程命名互斥」 |
| 9 | 进程数量无上限 | 已知限制 | 每个进程持有 5 MB 历史环 + 1 MB 发送队列。任何本机客户端都可通过 `process.create` 无限创建。正常使用（几个标签页）无影响；若出现失控客户端会耗尽内存 |
| 10 | `history.attach` 不校验进程是否已连接 | 已缓解 | v0.7.4 把灌入环的那段（`loadHistoryIntoRing`）纳入 `ringBufMu`，不再与 `readLoop` 并发写同一个环，并先判空环。仍不校验进程是否已连接——该入口 GUI/CLI/MCP 均未暴露（`AttachHistoryFile` 绑定无人调用），仅 IPC 层可达 |
| 11 | 共享环生命周期竞态：进程摘除后指针仍被并发持有 | 已修复 | v0.7.4 修复。`pm.Close()` 在释放 `pm.mu` 之后才调 `closeHistory()`，而 `Process` 指针是裸共享的（`Get` 取到指针即放锁），于是读取方可能在环被 `UnmapViewOfFile` 之后才碰它——`GetHistory`/`ClearHistory` 把快照/清零打在已撤销映射上，`recordHistory` 写已关闭的 fd。实测 `go test -race` 复现：`WARNING: DATA RACE` 紧接 `unexpected fault address ... [signal 0xc0000005]`。严重性在于 dispatch 路径无 `recover`，守护进程一崩即所有会话与串口一起断。修法：环的置空移入保护它的锁内（`ringBufMu` / `sendRingMu`），使用方持锁后重新判空；`historyFile` 指针改用 `histFileMu`；文件指针改动收敛到 `detachHistoryFile()`。`sendRing` 有同样窗口（表现为对 nil 调方法 panic），一并修掉。回归测试 `daemon/ringlife_test.go`，已做变异验证 |
| 12 | 历史环写满 5 MB 后静默冻结 | 已修复 | v0.7.5.5 修复。`Write` 在环满时返回 `false`，而 `recordHistory` **忽略了这个返回值**——写满那一刻起，新数据既不进环也不被任何人发现（磁盘文件仍在追加）。按 115200 饱和（11.5 KB/s）算，5 MB 约 **7.6 分钟**写满，此后「往回翻」永远翻到同一段、`oldestTs` 永远停在那一刻，而界面上完全看不出来。现改用 `WriteEvict`：空间不足时挤掉最旧的包（真正的环形语义 = 保留**最新**的一段），单包超过整环容量时才记一次 `ringDrops`（随 `session.history` 返回）。回归 `TestWriteEvictKeepsNewest`、`TestWriteEvictWrapAroundIntegrity`、`TestHistoryRingDoesNotFreezeWhenFull` |
| 13 | IPC 管道对本机任意进程开放，无鉴权 | 已知限制 | `source` 字段（`gui`/`cli`/`mcp`）由客户端自称且影响行为（自动观察进程、查看计数）。单用户桌面场景下可接受，但这是**设计决定**而非疏漏，多用户/多会话环境需重新评估 |
| 14 | `ports.probe` 不传端口时探测全部端口 | 待处理 | 5 端口 × 7 波特率 × 3 规则，同步阻塞在 dispatch 中，单次可达数分钟。建议要么强制指定端口，要么移入独立 goroutine 并加上限 |
| 15 | 前端三处功能失效 | 已修复 | v0.7.4 修复，三处均在浏览器中实测确认（`_audit/gui_test/verify_todo15.py`，并做变异验证）：①速率告警色永不出现——`statusbar.js` 产出 `.rate-high`/`.rate-warn`，而 CSS（`style.css:647-648`）只定义 `.rate-orange`/`.rate-red`，前者计算色为落回默认色；改为与 CSS 及 `app.js:3201-3202` 一致的类名 ②下拉框选中态高亮与 `scrollIntoView` 永不生效——`app.js:1161` 加的是 `' selected'`，而 CSS（`.cs-option.is-selected`）与 `app.js:1215` 查的是 `is-selected` ③多标签共用相同 DOM id（`tabpage.js:85` 每页都发 `id="displayContent"`），`i18n.js` 用 `document.getElementById` 只拿到第一个，切换语言时其余标签页的系统消息仍是旧语言；改为按 `.display-content .sys-msg[data-sys-raw]` 遍历全部页面 |
| 16 | 解码逻辑 Go 与 JS 各一份 | 已知限制 | `decode/decode.go` 注释自称 "Mirrors JS _decodeUTF8Tolerant byte-for-byte"；加 `history.js` 的 legacy 回退共三条路径。发送侧 GBK 用「非 ASCII 算 2 字节」估算 |
| 17 | 各包测试覆盖不均 | 部分修复 | v0.7.4 给 `client` 补上首个测试（`handshake_test.go`，覆盖三管道握手失败路径；`handshakeTimeout` 因此从常量改为变量以便测试）。`protocol`、`contract`、`config` 仍为零 |
| 18 | `applySendRatio()` 把类名当 `overflow` 值使用，两个隐藏分支从未生效 | 已修复 | v0.7.2 改为 `classList.toggle('is-hidden', cond)`，转发模式分支一并清理该类。原 `app.js:4030/4034` 写 `style.overflow = 'is-hidden'`，而 `is-hidden` 是类名（`style.css:71` → `display:none !important`），不是 `overflow` 的合法值，CSSOM 静默忽略该赋值。实测 `inlineOverflow` 恒为空、`is-hidden` 类从未被加上；`sendRatio=0.005` 时发送区只剩 4px 残条（40px 工具栏被 `overflow:hidden` 裁切），`0.97` 时显示区被 `min-height:40px` 夹住而非隐藏。应为 `classList.toggle('is-hidden', cond)` |
| 19 | 浅色主题缺失 4 个语义色变量 | 已修复 | v0.7.2 给两个浅色块补上 `--accent:#2563eb; --green:#15803d; --red:#dc2626; --yellow:#b45309`（均为浅底上可读的加深版本，`--accent` 取与既有 `--fwd-p1` 同色）。原两个浅色块各定义 17 个变量，深色块 21 个，缺 `--accent`/`--green`/`--red`/`--yellow`。`var()` 无 fallback 时属性在 computed-value 阶段失效并回落到初始值，所以不是「颜色不对」而是**属性整体消失**：设置页 4 个开关处于「开」时轨道 `rgba(0,0,0,0)` + 白圆点 = 完全不可见；另波及 `.status-dot.is-online`、`.tab-dot.live`、`.rate-orange`/`.rate-red`、`.port-occupied`，以及 `.cs-option.is-selected`/`.settings-mode-btn.is-active`/`.settings-theme-card.is-selected`/`.settings-lang-item.is-selected` 等一批 `background:var(--accent); color:#fff` 的选中态（白字透明底） |
| 20 | 悬浮条预留空间无单一事实来源 | 部分修复 | v0.7.2 引入 `--reserve-top: 76px`，`.display-content` 的 `padding-top` 与 `.c-clear-btn` 的 `top` 均由它推导，2px 重叠已消除。其余悬浮元素（send-controls / settings-header / 转发按钮）仍各自写死数值，未一并收敛。原 6 个悬浮元素（config-bar / send-controls / settings-header / 转发按钮 / 清空 / 发送）的空间靠在滚动子容器的 `padding` 里预留，取值 74/44/60/40 四种，间距取法不一（+30 / +4 / +8 / +0）。`.c-clear-btn` 底边 76px（`top:48` + `h:28`）而 `.display-content` 预留 74px，差 2px，实测压住第一条数据行 48×2px。建议引入 `--reserve-*` 变量并让预留值由悬浮条高度推导 |
| 21 | `.display-area` 的 `min-height` 被 JS 内联覆盖，文档失准 | 已修复 | v0.7.2 在 `LAYOUT-RULES.md` §6 明确「以 JS 为准」并说明原因。原 `style.css:574` 与 §6 都写 `min-height: 0`，但 `app.js:4029` 每次 `applySendRatio()` 都写 `displayArea.style.minHeight = '40px'`，实测 computed 为 40px。该覆盖是有意的（拖动时不显示区消失），但使「查 CSS 即可得布局真相」的假设不成立 |
| 22 | `.tabs-scroll` 的 `overflow-y: visible` 无法实现 | 已知限制 | CSS 规定一个轴为 `visible`、另一轴非 `visible` 时，`visible` 会被计算成 `auto`。实测 computed 为 `auto`，且 `scrollH=37 / clientH=36`——**任何标签数下都溢出 1px（含只有 1 个标签时）**，溢出的正是激活标签 `margin-bottom:-1px` 那一截，被裁掉。因此 `LAYOUT-RULES.md` §4 记的「激活态向下覆盖分隔线」效果从未真正生效 |
| 23 | 类名前缀约定 76% 未落实 | 已知限制 | `CLAUDE.md` 规定 `o-`/`c-`/`t-`/`is-` 四类前缀，实测 272 个不同类名中 206 个（76%）不符合，合规仅 24%（`c-` 34、`is-` 12、`o-` 10、`t-` 10）。`.data-line`、`.config-bar`、`.cs-*`、`.qp-*`、`.settings-*`、`.ctrl-*` 均为旧命名。改造量大且易破图，建议只对新代码强制 |
| 24 | 响应式只有 1 个宽度断点 | 已知限制 | `style.css` 共 5 个 `@media`，其中 4 个是配色（dark/light），宽度断点仅 `min-width: 800px` 一个，且只作用于设置页。主界面在 760px 下靠 flex 自然收缩，无专门窄屏规则 |
| 25 | `z-index: 1` 被 4 个元素共用，层序依赖 DOM 顺序 | 已知限制 | `#sendMirror`(0)、`#sendInput`、`#btnSend`、`#btnClearFloat`、`.cs-dropdown` 同为 1。目前靠 DOM 顺序得到正确层序、未出问题，但改动顺序即可能破图。整体尺度自洽：内容 0–5 < 拖拽指示 10 < 遮罩 50 < 菜单/弹窗 100 < 子菜单 110 |
| 26 | 标签栏横向滚动无视觉提示 | 已修复 | **2026-10-26 修复**：原来 `.tabs-scroll` 只有滚轮映射（`deltaY → scrollLeft`），标签多到溢出时没有任何可滚提示。现给标签栏加左右两个浮动箭头（`‹` / `›`）：`index.html` 里用 `.tabs-viewport` 把 `#tabsScroll` 包一层（箭头必须绝对定位在滚动容器**之外**，放里面会随内容滚走；视口 `flex-grow: 0`，宽度跟着标签走，所以「+」仍然紧跟最后一个标签，只有溢出时才被 `flex-shrink` 收到可用宽度、把「+」留在右边缘）；箭头直接复用发送区的 `.send-scroll-arrow`（绝对定位 + `pointer-events:none`，不吃布局、不挡点击），只覆盖宽度与渐变底色为 `--bg2`。`bindTabsScroll()` 在滚动/`ResizeObserver`/`renderTabs()` 后重算「还能不能往两边滚」，两个方向各自独立显隐。夹具 `_audit/tabs_scroll_hint.py` 15 项（不溢出时全隐藏、溢出时右侧出现、滚到最右只剩左、滚回最左只剩右、滚到中间两个都在、关到不溢出重新隐藏、箭头不压住「+」、点击穿透、不溢出时「+」紧跟最后一个标签、溢出时「+」仍贴右边缘）；变异验证：不装更新函数 / 视口改回 `flex-grow: 1` 各自变红 |
| 27 | `daemon not running: ` 前缀仍由底层产生 | 已修复 | v0.7.5 修复。v0.7.0 只清掉了 CLI 那一层，`client.CallOnce` 与 `pipe.dialPipe` 仍在用 `fmt.Errorf("daemon not running: %v", err)` 包装——失败原因可能是端点残留、权限、或对端刚退出，硬套「守护进程未运行」会把排查方向带偏。实测 `check` 输出由 `守护进程未运行 (daemon not running: pipe not available: ...)` 变为 `守护进程未运行 (pipe not available: ...)`。**副产物**：去掉包装后暴露出重试逻辑的误用（连不上也重试 3 次并谎称握手失败），已一并修掉，见 v0.7.5 说明 |
| 28 | 双击控制台程序表现为「闪退」，且原因不可读 | 部分修复 | v0.7.4 起「守护进程已在运行中」这句会同时落盘（`daemon/logfile.go`），控制台关闭后仍可在 `<exe>/logs/daemon.log` 里查到，不再是永久丢失。**仍未解决**：控制台本身仍是一闪即关，用户当场看不到；③ MCP 双击后静默 EOF 退出仍无任何提示。建议：用 `GetConsoleProcessList` 判断「控制台只有自己一个进程」即视为双击启动，退出前暂停等按键。原始记录：① 守护进程是机器级单例，已有实例时把 `daemon.already_running` 写到 stderr 后 `os.Exit(1)`；② CLI 无参数运行，打印用法后退出；③ MCP 是 stdio 服务器，双击时没有输入流、立即 EOF 退出（静默）。用户无从判断是崩溃还是正常退出（实测事件日志与 WER 均无崩溃记录，可确认非崩溃） |
| 29 | 守护进程回连失败时原因被丢弃，只剩无信息的超时 | 已修复 | v0.7.4 修复。握手期间起 goroutine 读 `daemonConn`，并且**为它单独设一个 select 分支**——回连失败时客户端要等的 resp 管道永远不会来，只在超时分支里顺手取一下的话，即使原因已到达也要干等满 5 秒。`client/handshake_test.go` 用假守护进程验证，并做变异验证：修复前 `2.0s` + 「守护进程未在 5 秒内回连 resp 管道」，修复后 `0.3–1.5ms` + 守护进程给出的真实原因。原记录：守护进程回连失败时会 `WriteMessage(daemonConn, ...Error)` 说明原因（`daemon/ipc.go:483/491`），但客户端此时阻塞在等 `respCh`（`client/client.go:139-150`），**从不读 `daemonConn`** |
| 30 | 客户端拉起的守护进程日志被整体丢弃 | 已修复 | v0.7.4 修复：日志双路输出（控制台 + `<exe>/logs/daemon.log`，超 2 MB 启动时轮转为 `.1`），新增 `serial-cli logs [n]` 查看（不连守护进程，起不来时才有用）。两个 Windows 细节都是实测踩出来的：① 必须共享打开——`os.OpenFile` 是独占共享模式，守护进程一运行日志就没人读得到，而「它跑着的时候去看日志」正是唯一用途；② 必须用 `FILE_APPEND_DATA` 而不是 `GENERIC_WRITE`——`CreateFile(OPEN_ALWAYS)` 会停在偏移 0，第二个实例写「已在运行中」时覆盖日志开头，实测留下一截被截断的旧行 |
| 31 | `sendqueue` JSON 未知字段被静默丢弃，装载成空内容 | 已修复 | v0.7.4 修复（外部测试报告发现，本机复测确认）。CLI 把文件解析成 `[]map[string]any` 再走 IPC，未知字段连丢两次（CLI 的 map 忽略一次、守护进程的 struct 再忽略一次），于是 `{"data":"ONE"}` / `{"text":"ONE"}` 返回 `success:true, entries:1` 而实际内容是**空**——使用者直到发现发不出东西才知道字段名写错。现于 CLI 侧用 `DisallowUnknownFields` 严格校验并给出可读报错（含接受字段名与示例），另接受 `data` 作为 `content` 的别名以兼容最常见手误。回归测试 `cmd/serial-cli/sendqueue_test.go`，已做变异验证 |
| 32 | `sendqueue` 不接受带 UTF-8 BOM 的 JSON | 已修复 | v0.7.4 修复。PowerShell 5.1 的 `Set-Content -Encoding UTF8` 会写 BOM，而 Go 的 json 解析器报 `invalid character 'ï' looking for beginning of value`——用户完全看不出是自己文件的编码问题。现解析前剔除 BOM。**本机核对时我自己也踩了一次同类坑**：用 PowerShell `Set-Content` 改写 Go 源文件把中文搞成了乱码，只能删掉重写 |
| 33 | autosend 空队列时静默空转 | 已修复 | v0.7.4 修复。`autosend start <ms> queue` 在队列为空时返回 `success:true` 然后一个字节不发、零告警；现象与「发送路径坏掉」无法区分，外部报告观察到的「不发送数据」里就混着这一种。现 queue 模式在装载后校验条目数，为空则明确报错并复位启用位 |
| 34 | autosend 计数器跨轮累加，`sendCount` 不是本轮数值 | 已修复 | v0.7.4 修复。`startAutoSend` 不复位 `autoSendSendCount`/`autoSendErrorCount`/`autoSendLastSend`，实测上一轮 queue 留下的 `sendCount=26` 会原样出现在下一轮 single 的 `status` 里，看起来像「刚启动就发了 26 次」。现于 start 时归零（在 `autoSendMu` 下，与读取侧一致） |
| 35 | `writePortSilent` 对空闲进程解引用 nil 串口 | 已修复 | v0.7.4 修复（写 autosend 测试时暴露）。`writePortSilent` 直接 `p.port.Write`，而空闲进程的 `port` 是 nil 接口。生产路径上 `AutoSendStart` 有 `status=="connected"` 守卫，但**自动发送跑到一半进程被断开**时循环仍会 tick，那次解引用就是一次 nil panic；dispatch 路径无 `recover` 且守护进程是机器级单例，一崩即全断。现加 nil 守卫并返回「串口未连接」错误，由上层按普通发送失败计数 |
| 36 | 注册握手在**并发/负载**下失败（三轮报告追查） | 已修复 | 根因是 **v0.7.4 引入的回归**：守护进程在两条回连管道都成功之后才写注册确认（`daemon/ipc.go:532`，位于 `addSession` 之后），确认因此可能抢在客户端 `Accept` 交出 respConn/subConn 之前到达；而 v0.7.4 为透传失败原因引入的监听用 select「谁先到按谁处理」，把先到的**成功确认判成了握手失败**。插桩实测：客户端收到 `{"result":{"registered":true}}` 却返回「注册未完成: 」——原因为空，因为 `reasonFromMsg` 对成功确认返回 `("", false)` 而调用方仍按失败处理。这一条解释了全部既有观测：失败总在 ~2ms 内且三次重试**一起**失败（同一竞态被连续触发，故重试无效——报告测到 3 次重试只把 7.8% 降到 6.3%，若独立应为 0.08%）；守护进程侧显示已完成注册、无回连失败、未驱逐、无写确认失败（因为它真的成功了）；失败率随机器变慢而升高（负载越重，Accept 调度越晚，确认越易抢先）。**修法**：用统一截止时间等待两条管道，只在 `msg.Error` 非空时中断握手，成功确认只表示已登记、继续等在途管道。**验证**：新增 `client/handshake_order_test.go`（假守护进程刻意先送确认、延迟 300ms 再连管道），变异验证还原旧逻辑即报出与测试机逐字一致的签名；实机满负载 + 三种并发度共 1180 次调用 **0 失败**，「open COM99 掩盖」0/60。原记录见下方 v0.7.4.2 段 |
| 37 | `connectOnce` 的 sub 管道失败/超时分支漏关 `respLn` | 已修复 | v0.7.4.2 修复。该分支只关了 `daemonConn`、`respConn`、`subLn`，漏了 `respLn`；加入握手重试后，每次失败都会泄漏一个 listener 及其预建管道实例。同类问题也在 client.go 其余失败分支一并核对补齐 |
| 38 | 文档承诺「新旧混用会被拦」不成立 | 已修复 | v0.7.4.2 修正文档（外部报告 GAP-3）。`protocolVersion` 只在**破坏性变更**时升号，因此同为协议 1 的不同版本（0.7.4 与 0.7.4.2）是设计上兼容、可以混用的，不会也不应该报错；原 `操作说明.md` 那句「新旧程序混用（换过版本）→ 提示 IPC 协议版本不匹配」暗示任何版本混用都会被拦，属过度承诺。已改为准确表述（比对的是协议号，`check` 会打印双方版本与协议号） |
| 39 | `probe` 每条规则只读一次串口，多字节响应被截断 | 已修复 | v0.7.5 修复。外部报告连续四版记录「`probe COM5` 检测不到地址 4 的设备」。`ProbePorts` 对每条规则只调一次 `p.Read`，而 Windows 下 `go.bug.st/serial` 的 `Read` **一有字节就返回**（其实现为 `if readed > 0 { return }`），于是响应被截断。插桩实测 9 字节 Modbus 响应只读到第 1 个字节（`发=040300080001059D 收 n=1 bytes="04"`），`min_response_len` 与匹配双双落空；这也使 `modbus_crc` 类规则对多字节响应一律失效。**修法**：抽出 `probeRead()` 循环累积，终止条件显式化（未收到数据则继续等 / 已收到数据后本轮读空即帧结束），总预算 `max(3×timeout, 1s)`——不依赖 `SetReadTimeout` 在库内的行为，因为该库为 CH340 打过补丁而本工具目标设备大量使用 CH340/CH343。实测 **0/5 → 5/5 命中**，无应答端口仍 0.05s。**注意**：此前两版循环修都会挂住，原因是用 `time.Now()` 自判截止会与库内部计时错位；本版改为只用「本轮是否读到字节」判定 |
| 40 | `probe` 规则改从站地址须手工重算 CRC | 已文档化 | v0.7.4.3 起文档化。`probe_hex` 是**含 CRC 的整帧**，改地址不重算 CRC 会因校验失败而设备完全不应答，而报错只是「未检测到已知设备」——用户看不出是自己 CRC 写错。已在 `操作说明.md` §九 详述并随包交付 `modbus_crc.py`（含已知帧自检、支持 `--sweep` 列出地址 1–16 的帧）。**仍建议根治**：把从站地址做成规则字段（如 `slave_addr`），由引擎生成帧并算 CRC，替掉「整帧 + 手算 CRC」这个脆弱设计 |
| 41 | `match_type` 的 `regex` 与 `substring` 匹配对象不同，易误用 | 已文档化 | v0.7.5 文档化（外部报告把它报为「行为不一致」）。`substring` 的 `match_value` 是**十六进制串**，在响应的十六进制文本里找；`regex` 的 `match_value` 是**正则**，匹配**响应原始文本**。所以 `substring "04"` 命中 hex 串 `"040302…"`，而 `regex "^04"` 是在原始字节里找 ASCII 的 `0`/`4`，匹配不到——语义自洽，非缺陷。已在 `操作说明.md` §九 写明差异与各自适用场景。**外部报告已在 0.7.5.1 轮撤回该判断**（§5.1：属设计差异，作者解释正确），并同步修正了 0.7.4.3 报告。**可改进**：给 `match_type` 加白名单校验，写错时明确报错而不是静默不匹配 |
| 42 | 语言文件中 Tx/Rx 大小写混用 | 已修复 | v0.7.5 修复。`en.json` 的 `stats.*` / `tooltip.*` / `settings.color_*` 用大写 `RX`/`TX`，而 `stat.rx` / `stat.tx` / `display.*` 用 `Rx`/`Tx`，同一界面混用两种写法。统一到 `Rx`/`Tx`——与前端 `history.js` 硬编码的 `'Rx'`/`'Tx'` 及 9 种语言里 8 种的原状一致。改动 `en.json` 14 处、`es.json` 8 处、`fr.json` 8 处；i18n 一致性检查 9 语言 × 374 键全绿 |
| 43 | 发送框文本模式：镜像层与 textarea 排版逐格错开 | 已修复 | v0.7.5.1 修复（外部测试反馈）。镜像层把空格换成 `·`、并把真实空格设为 `font-size: 0`，这一格的推进宽度于是变成 `·` 的宽度；字体里两者不等宽就逐格错开且随空格数累加。实测设置里 10 种可选字体 **8 种**中招（Cascadia Code/Fira Code/JetBrains Mono/Source Code Pro 每空格 +8px 即整一格、宋体 +7px、楷体 +7.5px、微软雅黑 −0.83px、Segoe UI −0.84px；Consolas 与 Courier New 恰好为 0）。表现为光标不在可见字形上、点选偏移、选中时叠出两层字。改为真实字符保留原宽度＋标记绝对定位叠加（静态位置，保持基线对齐），修复后漂移 0～0.64px。仅作用于 `.send-input-mirror` |
| 44 | Hex 输入的预处理在四条路径上分叉 | 已修复 | v0.7.5.1 修复。手动发送（`sendData`）与回写环形缓冲都做了 `replace(/0x/gi,'').replace(/[\s,]+/g,'')`，而自动发送（`startAutoSend`）与字节统计（`updateSendInfo`）只有 `replace(/\s/g,'')`。高亮层把 `0x` 当合法前缀、把 `,` 当分隔符，于是「全绿 + 手动能发 + 自动发送报 invalid hex」；字节数也偏大（`0xAA 0xBB` 显示 4B 实际 2B）。实测 `0xAA 0xBB` / `AA,BB` / `0xAA` 自动发送全部失败，修复后正常发出。现抽出 `_normalizeHexInput` 单一实现，四条路径共用，夹具断言其外无残留 |
| 45 | 1 位十六进制数字高亮判绿但发送必失败，且失败无提示 | 已修复 | v0.7.5.1 修复。同一 1 位数字在落单 token 时判合法（`len>=1 && len<=2`）、在长串按 2 字符切分后落单时判非法，而后端 `hex.DecodeString` 对奇数长度一律拒绝；`sendData` 的 `catch {}` 又把错误整个吞掉，于是 `A`、`AA B` 全绿却「点了发送没反应也没解释」。保留了宽容高亮（便于边打字边看），改为新增发送框悬浮提示：悬停或发送被拦下时在发送按钮上方浮出原因，5 秒自动消失、`pointer-events:none`、9 语言本地化（`send.hint_empty`/`hint_odd`/`hint_badchar`） |
| 46 | 发送框占位符写死中文，9 种语言的 `send.placeholder` 闲置 | 已修复 | v0.7.5.1 修复。`#sendInput::placeholder` 被设为 `transparent`，用户看到的是镜像层里硬编码的 `'输入要发送的数据...'`。改镜像层走 `t('send.placeholder')`，并在 `applyI18n` 里重绘镜像层 |
| 47 | 发送框里 `0x` 前缀与合法字节同色，`.send-hex-prefix` 无视觉作用 | 已修复 | **2026-10-26 修复**：`.send-hex-prefix` 改用 `var(--placeholder)`（弱化一档），与字节色 `--dc-hexTx` 区分开。实测计算色：前缀 `rgb(153,153,153)` vs 字节 `rgb(217,119,6)`。夹具 `_audit/send_hex_prefix.py`（与 #48 合在同一个文件）断言两者计算色不相交；变异验证：改回 `--dc-hexTx` 立即变红。原记录：v0.7.5.1 记录，两个类都用 `var(--dc-hexTx)`，实测同为 `rgb(217,119,6)`，这个类等于没有区分作用 |
| 48 | 发送框 hex 分词：`0x` 紧贴字节串末尾时整段被判红 | 已修复 | **2026-10-26 修复**：前缀识别条件 `i + 2 < text.length` → `i + 1 < text.length`。`AA0x` 现在渲染成「`AA`（合法）+ `0x`（前缀）」，不再整段标红；`AA0x` 的文字也没丢字符（断言拼回原文）。夹具 `_audit/send_hex_prefix.py`（9 项，含 `0xAA`、`0xAA 0xBB` 两个对照与「非法 `GG` 仍标红」的护栏）；变异验证：改回 `i + 2` 后 2 项变红。原记录：v0.7.5.1 记录，仅显示问题，不影响发送（`_normalizeHexInput` 会剥掉 `0x`，实际能发出去，属假红灯） |
| 49 | `fakeDaemonThatRejectsRegister` 的「端点被占用则跳过」守卫在 Windows 上失效 | 待处理 | v0.7.5.1 记录（写发送框回归时撞到）。该守卫靠 `pipe.Listen(pipe.Addr)` 返回错误判断「已有真守护进程」，但 `pipe.createPipeInstance` 用的是 `pipeUnlimitedInstances`，**同名管道允许第二个实例**，于是真守护进程在跑时守卫不触发，测试不是跳过而是**误报失败**（客户端可能连到真守护进程上并握手成功，于是 `err == nil` 触发 `t.Fatal`）。实测有守护进程时 `TestHandshakeSurfacesDaemonReason` / `TestHandshakeReasonDoesNotClaimRejection` 失败，杀掉后三个用例 0.5s 全过。建议改用独占探测（先试着 `Dial` 成功即视为占用）或直接探测 `serial-daemon` 进程 |
| 50 | **`probe` 的「空结果」有两条静默路径，与「没有设备」不可区分** | 已修复 | v0.7.5.2 修复。**（a）耗时与超时**：`probeRead` 的总预算是 `max(3×timeout, 1s)`，默认 `timeout_ms=200` → 取 1s 下限；端口静默时每次尝试都跑满这 1s。实测同一静默端口：1 次尝试 **1.11s**、7 次 **7.48s**、默认 21 次 **10.18s**，而 `client.Call` 的 IPC 超时是硬编码 10s → 必然 `request timeout: ports.probe`；守护进程并不停，继续扫完（实测后台探测总耗时 **33.2s**）。**（b）假阴性**：这段窗口内再做任何探测都在几十毫秒内返回空结果——报告实测「正确配置 + 编码器在线」也得到 `未检测到已知设备`（§3.3：58ms，已用 MCP 证实与客户端无关）；本地复现：被会话占用 46ms、超时后紧接着 61~65ms，而那次探测**结束后**恢复 1093~1245ms。**（c）根因两条，报告只点到第一条**：① `probe.go` 的 `if occupiedPorts[portName] { continue }`；② `if err != nil { continue }`——`ProbePorts` 内部 open/read 失败被裸 `continue` 吞掉、无日志。本地因果实验证明②才是超时路径的真正机制（`sessions` 显示无任何进程持有端口，跟随探测仍在 65ms 内返回空）。**修法**：`ProbePorts` 改返回 `ProbeOutcome`（results/skipped/attempts/elapsedMs/budgetExhausted/busy），两条路径都显式化并补日志；加默认 12s 总预算（`--budget`/`budgetMs` 可覆盖）+ `ports.probe` 请求超时放宽到 25s；并发探测用 `TryLock` 直接返回 busy；`probeRead` 带出读取错误；`baud_rates` 改为按可能性排序（115200 提到最前——设备不应答时每档约 3s，12s 只够前 4 档，原升序会把最常用的 115200 排在第 4 位，实测预算用尽时它根本没试过）。回归 `daemon/probe_skip_test.go`（6 条，含一条 elapsedMs 必须落到命名返回值的用例）。原文如下（保留溯源）：v0.7.5 起存在，0.7.5.1 轮由外部测试报告与本地复现共同确认 |
| 51 | 历史显示窗口：性能机制放错了位置 | 已修复 | v0.7.5.3 修复。窗口本身是对的（实测 DOM 无上限时单帧成本 139.7µs@2000 节点 → 4005.3µs@20000 节点，O(n²)；有窗口时恒定 ~1000µs），问题在于它只界住了「DOM 有多少」没界住「每帧做多少事」。① **逐条渲染**：每帧两次读 `scrollHeight`（读是 O(节点数)：150 节点 44.3µs、20000 节点 4313µs；写 `scrollTop` 只要 2µs）→ 改为入站帧进队列、rAF 合并成一批，一批一次 fragment/一次裁剪/一次贴底，贴底改写极大值不读；实测吞吐 893 → 242131 帧/秒，持续流入单条 1000µs → 240µs。② **锁定滚动就不裁剪**（贴底与裁剪绑在一个 `if` 里）→ 实测锁定后喂 5000 条 DOM 涨到 5000 行；现裁剪恒做，冻结语义交给 `_frozen`/`_atBottom`。③ **`renderHistoryLines` 的 50ms 节流是「丢弃」不是「合并」** → 实测渲染后立刻清空导致显示区空白、连续切换显示模式后画面与 state 不一致；现改为不丢弃（合并由批量队列承担）。④ **`expandHistory` 在 `_renderStart==0` 时第一步就 return**，而取更早的两条路都在 return 之后 → 「加载更早」整条不可达（实测 0 次 RPC、无提示）；现拆开「扩窗」与「取更早」。⑤ 冻结期间未读区用占位块撑高度：视图不动（实测 scrollTop 变化 0px）、滚动条继续变化（+123002px）、DOM 仍有界。⑥ 窗口行数改为按视口推导（`calcRenderCount()` 此前从未被调用）+ `设置 → 高级 → 历史窗口行数` 可覆盖。⑦ 补 `loadTabHistory`/`clearDisplay` 的 null 守卫，消掉控制台那条 `Cannot set properties of null (setting 'innerHTML')`。夹具 `_audit/sendhl_test/hist_fixture.py`（8 项）+ 两处变异验证 |
| 52 | `probeRead` 的 1s 隐藏下限让默认波特率永远轮不到 | 已修复 | v0.7.5.3 修复。该预算实际只决定「等第一个字节最多等多久」（循环里 `len(resp) > 0` 排在 `After(deadline)` 之前，收到字节后下一个空读就结束），所以沉默端口独自承担成本。原 `max(3×timeout_ms, 1s)` 在默认 `timeout_ms=200` 下把每次尝试从 600ms 抬到 1000ms → 每档 3s、7 档 21s > 12s 预算 → 默认列表里 230400/460800/921600 永远轮不到。现 `max(3×timeout_ms, 300ms)`（300ms 仅防呆）+ 总预算 15s；实测单次尝试 1106 → 753ms，默认配置 13.4s 覆盖全部 7 档、skipped 为空。新增 `TestDefaultBudgetCoversShippedBaudList` 把预算与随包 `probe.toml` 绑定 |
| 53 | CLI 的探测输出漏掉 `busy` 字段（文档承诺了它） | 已修复 | v0.7.5.3 修复（0.7.5.2 轮报告 P1）。守护进程返回了 `busy`，CLI 序列化时丢掉，导致 `操作说明.md` 写的 `("busy": true)` 与 `grep busy` 自检永远匹配不到。抽出 `probeOutcomeJSON` 并加 `TestProbeOutcomeJSONIncludesBusy` / `TestProbeOutcomeJSONBusyAlwaysPresent` 锁住字段集合 |
| 54 | `config/probe.toml` 顶部注释与实际预算公式脱节 | 已修复 | v0.7.5.4 修复（0.7.5.3 轮报告 §4.1）。v0.7.5.3 把每次尝试成本从 1s 降到 600ms、总预算 12s → 15s，但随包配置的注释仍写 `max(3×timeout_ms, 1s) ≈ 3s` 与「默认 12s」，照注释估算会得出「每档 3s、7 档 21s」的旧结论。现按实际公式重写，并在 `daemon/probe.go` 的预算常量旁加提醒：改预算数字必须同步这份注释（功能由 `TestDefaultBudgetCoversShippedBaudList` 兜底，注释漂移只能靠提醒） |
| 55 | CLI 把未知长选项当成端口名静默接受 | 已修复 | v0.7.5.4 修复（0.7.5.3 轮报告 §4.2）。`serial-cli probe --json` 不报错，而是把 `--json` 当作端口，结果里多一条「端口 `--json` 打不开」的 skipped，读者会以为真有个端口有问题。现抽出 `parseProbeArgs`：未知 `-` 前缀参数直接报错，`--config`/`--budget` 缺值或非法值分别报清楚；校验提前到建立连接之前（参数写错不该等连上守护进程才报）。附 3 条单元测试。**未动其他命令**：它们的多余参数会走到「进程不存在」这类明确错误，不会静默产生误导性条目，改动风险大于收益 |
| 56 | MCP 工具描述里 `budgetMs` 的默认值滞后 | 已修复 | v0.7.5.4 修复（0.7.5.3 轮报告 §6.2.4）。`serial_probe_ports` 的 inputSchema 写 `default 12000`，实际已是 15000 |
| 57 | `session.history` 一次返回整环，界面只要一屏 | 已修复 | v0.7.5.5 修复。整环 5MB ≈ 18 万条（实测 180788 条），一次读取要「逐包复制 + 逐条建 `HistoryEntry` + JSON 序列化 15.86 MB + 过桥 + 前端逐条 decode」。新增 `limit`/`beforeTsMs`/`sameTsSkip` 三个参数与 `ringbuf.SnapshotPage`（只复制选中的一页）：**实测 180788 条整取 79.4 ms / JSON 15.86 MB → 一页 5000 条 5.0 ms / 0.44 MB（时间 16×、载荷 36×），首次加载 10000 条 9.8 ms / 0.88 MB。** `limit<=0` 仍是整取（CLI/MCP 老行为不变）。基准 `BenchmarkHistoryRead` |
| 58 | 前端历史缓存无上限，开一天的标签页会吃满内存 | 已修复 | v0.7.5.5 修复。`state.historyCache[tab]` 只 push 从不裁，等于把整环（以及环里已被挤掉的更早数据）永远留在 JS 里。现改为有界：跟随时保留 10000 条，往回翻时放宽到 40000 条（否则刚回补的一页会被自己立刻裁掉，游标原地不动，「往上翻」就再也翻不动），永远从**最旧**的一端裁，于是「最新的一屏」始终在手上（回到底部不需要任何 RPC），裁到窗口里时同步移除对应的 DOM 行。**实测持续灌 30000 条后缓存停在 10032 条、尾条仍是最新那条**，DOM 稳定在窗口内。夹具 `hist_paging_fixture.py` C 节 |
| 59 | 「往上翻加载更早」在缓存头仍然不可达 | 已修复 | v0.7.5.5 修复（v0.7.5.3 只修了一半）。上一版把 `expandHistory` 内部的 `return` 挪走了，但**滚动触发器**还是 `if (scrollTop < 80 && _renderStart > 0)`：当窗口正好停在缓存头时 `_renderStart == 0`，滚到顶因此不调用 `expandHistory`，一次 RPC 也不会发。实测把窗口滑到缓存头后滚到顶：0 次调用。现改为只要滚到顶就调用（`expandHistory` 自己判断要不要取更早），并用 `_ringExhausted` 与「游标没动过」双重保险避免滚到顶变成 RPC 风暴 |
| 60 | 契约测试建出的进程与 5MB 共享内存不回收 | 已修复 | v0.7.5.5 修复（写分页测试时撞到）。`TestEveryContractMethodIsDispatched` 用裸参数调 `process.create`，真的建出一个进程（并映射 5MB 共享内存，名字带本进程实例令牌），而它的 `ProcessManager` 既没 `DestroyAll` 也没人引用。同一个测试二进制里**后面任何再建进程的测试**都会撞上 `共享内存 "serial-tool-history-xxxx-1" 已存在`——谁后跑谁中招。现 `defer pm.DestroyAll()`。这也是「测试之间的隐式顺序依赖」，值得记住 |
| 61 | 磁盘历史的深分页仍是「从头扫」 | 待处理 | v0.7.5.5 记录。回补到环头之后，磁盘这一侧走的是 `history.search(file, "", 200, offset)`——空关键字顺序扫描 + 递增 `offset`，每翻一页都要从文件头扫到 offset，页数越多越慢，而且方向是从文件**开头**往后，而用户要的是从游标往**更早**。要做对需要给历史文件建一次「条目偏移索引」（4 字节/条，18 万条约 720KB）再按游标二分。当前只在「环已翻完」时才提示，实测触发不到，故未动 |
| 62 | 冻结回看时的缓存上限是 40000 条 | 已知限制 | v0.7.5.5 记录。往回翻时缓存放宽到 `HISTORY_CACHE_HARD`，超过就从最旧的一端裁（同时移除对应 DOM 行，屏幕上不会留下缓存里不存在的内容）。即**单次回看的最大深度约 40000 条**（约 40 屏），再往回翻会重新取到刚才被裁掉的那一页。放宽或做成设置项需要前向分页（`afterTs`）才能安全地裁掉新的一侧，属下一批 |
| 63 | 取「最新一页」仍需遍历整个环 | 待处理 | v0.7.5.5 记录。包长可变且只在前缀里，从最新一端往回走无法知道上一个包的起点，所以取最新 N 条只能从最旧一端走一遍（环里 18 万包约 1.5–3 ms，已用快路径把取模/逐字节去掉）。彻底解决要在写侧维护一个「包尾索引」（head 附近 N 个偏移），属可选优化——当前 5 ms/页 已足够 |
| 64 | 设置标签页被当成会话标签页，串口会挂到设置页上 | 已修复 | v0.7.5.6 修复（手工测试发现）。`openPort`/`openForwardPorts` 用 `getActiveTab()` 取目标，而它把设置页原样返回；设置页的 `_page` 只有 `show`/`hide`，没有 `portSelect`/`btnOpen`，于是 `pageEl()` 的全局回退交出**别的标签页**的端口选择 —— 串口真的打开了却挂在设置页对象上（没有界面能管它），设置页还长出 `sessionId`、标签变成「COM3 @ 115200」。实测：`0:SET sid=3 open=True label=COM3`。现引入 `isSessionTab()`/`activeSessionTab()`/`requireSessionTab()`，会话类入口先过这一关，停在设置页时给可读提示（`serial.need_session_tab`，9 语言）。夹具 `tab_shuffle_fixture.py` A 节 |
| 65 | 建空闲进程时不挡 `syncDaemonSessions`，同一进程多出一个标签页 | 已修复 | v0.7.5.6 修复。`addTab()` 一直有 `_creatingTab` 保护，`openPort`/`openForwardPorts` 没有：守护进程的 `process-changed` 若抢在 `tab.sessionId = sid` 之前到达，同步会替同一个进程再建一个标签页（追加在末尾，也就是用户看到的「原来的标签页跑到后面」）。现补上同样的保护；夹具 B 节用「把同步插到 `CreateIdleProcess` 返回之前」确定性地复现该窗口，变异验证能报红 |
| 66 | 僵尸清理把设置页算进「还有别的标签页」 | 已修复 | v0.7.5.6 修复。清理条件 `state.tabs.length > 1` 把设置页也数进去，于是**只要开着设置页**，单独一个空闲会话标签页就会被销毁（用户手上只剩设置页）。现只数会话标签页。夹具 C2 节 |
| 67 | 会话回归时新建标签页而不是找回原来那个 | 已修复 | v0.7.5.6 修复。同步第 3 步在列表一时不含该会话时清掉 `sessionId`，等它回到列表里第 1 步按「未登记」新建 —— 同一个会话有两个标签页，原标签页的滚动位置与历史缓存全丢。现记 `_orphanSid`，回来时挂回原标签页。夹具 C 节用**标签页 id 不变**来钉住（只断言数量会被「销毁+重建」蒙过去） |
| 68 | `updateOpenBtn` 经全局回退改到别的标签页的按钮 | 已修复 | v0.7.5.6 修复。切到设置页时 `pageEl('btnOpen')` 回退到 `document.getElementById` = **第一个标签页**的按钮，把它 `display:none`。现只动当前页自己的元素。夹具 D 节 |
| 69 | 队列 `delay` 显式写 0 被当作「未设置」回落到 1000 ms | 已修复 | v0.7.5.7 修复（外部报告 0.7.5.5 轮 §八，P3）。`delay` 用 `int` 承载，JSON 零值语义把「省略」与「显式 0」混成一件，再被 `delay < 1 → 1000` 兜底 —— 写 0 等于等 1 秒（报告实测 20 条 19.2 s，与 `delay:1000` 几乎一致，想连发只能写 1）。现语义定为：省略/null = 1000（保持文档承诺）、0 = 不额外延时、1–60000 原样、>60000 夹住、负数报错；「省略」与「显式 0」的区分做在 API 边界（CLI 用 `*int`、MCP 写前归一化）。实测 10 条一轮：省略 9102 ms / 0 111 ms / 1 106 ms / 10 213 ms / 1000 9060 ms |
| 70 | `DecodeEntryContentOnly` 的 delay 下限（5）严于编码器下限（1），delay=1~4 的条目会把二进制头当数据发出去 | 已修复 | v0.7.5.7 修复（查 §八 时顺带发现，报告未覆盖）。`EncodeEntry` 允许 1，而剥头的启发式校验写 `delay < 5 → 这不是条目`，于是 delay=1~4 的条目走 `send.trigger(raw=false)` 时原样发出 `版本+标志+delay+note_len+内容`。该路径由 `App.TriggerSend` 与 IPC `send.trigger{raw:false}` 暴露（GUI 当前未调用、MCP 用 `raw:true`），所以一直没被踩到。两处下限现统一到 `DelayMax` 常量，回归 `TestDecodeEntryContentOnlyStripsHeaderForEveryEncodableDelay` |
| 71 | 凭一份**陈旧事件载荷**就清掉活着的会话，导致「打开串口时多建一个进程 + 多出一个标签页」 | 已修复 | v0.7.5.8 修复（用户人工复测 + 其机器上的 `daemon.log` 定案）。`syncDaemonSessions` 第 3 步只要载荷里没有某个 sessionId 就清掉它；而事件载荷可能是**该进程创建之前**发出的（实测 `processes: []` 落在标签页刚拿到 sessionId 之后）。会话被清 → 标签页看起来空闲 → 点「打开串口」再建一个进程（旧的成孤儿，`daemon.log`: `create idle #1` → 3.4s → `create idle #2` → `connect #2`）→ 孤儿被第 1 步补成多余标签页。现改为：**只有当载荷会移除在线会话时才先 `GetSessions()` 核实**（新增不核实），另保留 0.7.5.6 的孤儿找回作第二道网。夹具 F 节确定性复现（注入陈旧空载荷），变异验证还原后能重建出「1→2 进程 + 多一个标签页」 |
| 72 | 会话清理的 3 秒宽限期是死代码（`connectedAt` 从来没人写） | 已修复 | v0.7.5.8 修复。第 3 步写着 `if (tab.connectedAt && Date.now() - tab.connectedAt < 3000) return;`，但全仓库**只有这一处读**、没有任何赋值 —— 于是「刚连上就收到不含该会话的事件」时保护完全失效。现于 `openPort` / `openForwardPorts` / 孤儿找回三处写入 `connectedAt` |
| 73 | `autoCreateSession` 的自动建会话把自己的 sessionId 清掉（「开串口又多一个标签页」的真因） | 已修复 | v0.7.5.9 修复（用户 0.7.5.8 复测 + 带时间戳的 `daemon.log` 定案）。自动建分支在 `addTab` 之后写了 `daemonSessions = []`，本意是「别为刚建的进程再建一个标签页」，但**第 3 步正是用这份列表判断会话是否存在** —— 列表一空就把刚挂上的 sessionId 清掉。日志实证：`r4 create idle #1` → 5.2s → `r8 create idle #2` → `r9 history #1`（孤儿补出的标签页）。现改为**重新取一份权威列表**。**注意**：0.7.5.8 修的是「陈旧事件载荷」那条路径（另一种成因），本条是自动建会话自己清空列表，两者症状相同成因不同 |
| 74 | `addTab` 不写 `connectedAt`，宽限期在这条路径上不生效 | 已修复 | v0.7.5.9 修复。0.7.5.8 只给 `openPort`/`openForwardPorts`/孤儿找回三处写了 `connectedAt`，而**自动建会话走的是 `addTab`** —— 于是「列表还没包含这个会话」的窗口期在这条路径上毫无保护。现于 `addTab` 建标签页时一并写入。与 #73 互为备份：变异验证显示只还原任一处夹具仍全绿，还原两处才重建出现象 |
| 75 | CLI 的 `history` / `sendqueue` 先连守护进程、后校验参数 | 待处理 | v0.7.5.9 记录（本轮跑冒烟时撞到）。这两个命令的参数校验发生在 `runCommandWithClient` 内部，而客户端连接在进入它之前就建立了 —— 于是**守护进程不可达时，参数写错的报错会被连接错误盖掉**（实测 `history --json` 在守护进程不可达时只报「注册握手失败…」，看不到「未知参数 --json」）。`probe` 在 0.7.5.4 已把校验提前到建立连接之前（报告 §4.2），这两个命令是同样的改进点。属体验问题，不影响正确性 |
| 76 | `send.trigger{raw:false}` 没有黑盒入口，导致「条目剥头」那条修复无法验收 | 已修复 | v0.7.5.10 修复（0.7.5.7 轮报告 §8 点名）。该路径此前只有 Wails 绑定 `App.TriggerSend`（前端未调用）与裸 IPC 可达，CLI 无命令、MCP 只有 `raw:true` —— 测试方直接写「若需覆盖，需要一个可达入口」。而 `ARCHITECTURE.md` 的「发送模式」表里**本来就写着** `sendone（单条）→ send.trigger`，属文档/实现不一致。现补 `serial-cli sendone [processId] [--raw]`（默认剥头、`--raw` 对照），未知参数报错，附 `sendone_args_test.go` |
| 77 | `autosend status` 的 `queueCount` 是**字节数**而非条目数，`entriesCount` 在 CLI 路径恒为 0 | 待处理 | 0.7.5.10 轮报告 §六 发现（**命名歧义陷阱，非功能缺陷**，也非该版引入——用 0.7.5.8 副本同口径对照，两版一致）。`queueCount = sendRing.UsedSpace()`，即队列里的**字节总数**（实测每条 = 内容长度 + 7：2 字节长度前缀 + 5 字节条目头 + 内容）；`entriesCount = len(p.multistrCache)`，而该缓存只由 `multistr.load`/`multistr.reload`/队列模式自动发送填充，**CLI 的 `sendqueue` 只写环、不填缓存**。于是并列的两个字段一个口径是字节、一个恒为 0，使用者（含测试方，其 0.7.5.7 附录 B 曾把 `queueCount` 当条目数当观测点）会把 `queueCount` 读成「队列里有几条」。建议三选一并同步文档：改名为 `queueBytes`、或新增 `queueEntries` 给出真实条目数、或让 `entriesCount` 反映真实条目数；无论选哪个，都要在 `操作说明.md` 写明 `autosend status` 各字段口径 |
| 78 | 多客户端同时操作同一进程时，历史里的 tx 条目**分不清是谁发的** | 待处理 | 0.8.0 轮实测撞到（我在 MCP 侧往用户在 GUI 里的 COM12 会话发 Modbus 帧，用户看到的就是一串没有任何出处的 `tx`，问「不知道是我发的还是其他客户端发的」）。**记录层**：`RxTxMessage`（`daemon/serial.go:31`）与 `HistoryEntry`（`:43`）都没有来源字段，`recordHistory(hexData, direction)` / `recordHistoryAt(ts, hexData, direction)`（`:201` / `:210`）的签名里也没有来源，三个写 tx 的地方（`writeLoop` `:1003`、`sendOneRound` `:706`、`autoSendLoop` `:540`）都只能传方向。**更前一层就已经丢了**：客户端的发送是**直接写共享发送环**（`client/client.go:616` `SendWrite(ringName, raw)`）再 `SendTrigger`（`:620`，只带 `processId` 与 `raw`，**不带长度也不带来源**），守护进程从环里成批捞（`writeLoop`），于是「哪几字节是哪个客户端写的」这个对应关系在入环那一刻就没了。IPC 层的 `source`（`daemon/ipc.go:596` / `:752`，经 `sourceLabel` 得到 gui/cli/mcp）只说明**这次调用是谁发起的**，不等于**环里这段字节是谁写的** —— 任何客户端都能触发发送别人刚写进环的字节。目前唯一的线索是 `viewers`（`daemon/serial.go:190` `viewerSummary`），它说明「谁在看」而不是「谁在发」。**修法三个方向**：① **帧级来源标记** —— 发送环里每条记录加 1 字节来源 id（gui/cli/mcp/auto），`writeLoop` 解出后写进 history，前端在 tx 行加小标签；要改环格式，**协议号需升**；② **发送统一走 IPC**（由守护进程拷贝），与 #8「发送环无跨进程互斥」同一个方向，一次同时解决归属与并发写；③ 只做「尽力而为」（`send` 事件带 `source`、前端标在最近一条 tx 上）—— 并发写会错标，不建议。**为什么要记**：排查「谁在乱发」「这帧是我点的还是脚本发的」时现在完全无从下手，而这个场景在有 MCP/CLI 自动化的环境里是常态 |
| 79 | 下拉框「同步」明显滞后、刷新时卡顿（端口下拉尤其） | 部分修复 | **2026-10-26 更新**：② 已解决（见下）；① **已解决**——`_csRefresh` 增加选项签名短路（值 + 显示文字 + `option` 类 + 选中项，见 `_csOptionsSig`），签名没变只更新触发器文字、不碰 `dd.innerHTML`；`.cs-option` 的点击改事件委托挂在 `dd` 上（重建后无需逐项重绑，portal 到 body 也照样生效）；`updatePortList` 不再对三个工具栏下拉做 `_applyPortOptions`（那是 `document.getElementById`，只命中第一页，等于白填一遍再被按页填充覆盖），只保留设置弹窗里那两个唯一实例。实测：内容没变时连续 `_csRefresh` 5 次 / `updatePortOccupied` 连调两次 / 同一份端口列表再刷一次，**都是 0 次重建**；端口列表真的变了、占用状态真的变了仍照常更新（占用只要切类，0 次重建）。夹具 `_audit/dropdown_no_rebuild.py` 10 项；变异验证：去掉签名短路后 3 项变红。**仅剩 ③**：打开下拉时同步 `scrollIntoView` 会强制布局（`:1231-1232`）。原记录：0.8.0 实测反馈（2026-10-25，用户原话「选择下拉框同步缓慢」）。① **观察器风暴** —— `_markOccupied` 对每个 `<option>` **无条件**写 `opt.className`，而每个 `<select>` 都挂着 `MutationObserver`（`childList+subtree+attributes`），回调做的是**整表重建**；`updatePortOccupied` 一次动 3 个下拉、`updatePortList` 末尾必调、`ports-changed` 又触发 `refreshPorts` → 单次刷新最坏 3×N 次全量重建；② **程序化设值不刷新触发文字** —— `_setSelectVal` 与 `_applyPortOptions` 只写 `el.value`（不是 attribute、也不派发 `change`），而 `.cs-text` 只在 `_csRefresh` 里更新 → 切标签后下拉框显示的文字仍是旧的。② 的修法（已做）：新增 `_applySelectValue()` 写值后无条件 `_csRefresh`、缺选项时先补一个，并新增 `_populateTabPortSelects(root)` 按每个标签页的 root 填充 |
| 80 | 切到 Hex 发送后工具栏仍显示「字」，并残留文本字符 | 已修复 | **2026-10-26 修复**：根因是 `_renderSendMirrorHex` 里的 `for (var t = 0; ...)`——`var` 提升到整个函数作用域，把 i18n 的 `t()` 顶掉；而「内容为空 → 写占位符」那条分支正好要调 `t('send.placeholder')`，于是抛 `TypeError: t is not a function`，`toggleSendFormat` 在 `_syncSendMirror` 处中断，后面的 `updateSendQuickBtn()` 根本没执行——表现就是输入框已清空、按钮仍是「字」、镜像层留着上一段文本（含 `↵` 标记）。循环变量改名为 `ti` 即修复。夹具 `_audit/sendformat_switch.py` 9 项断言，变异验证：还原 `var t` 立即复现 2 条 TypeError 与 4 条失败。原记录：0.8.0 实测反馈（2026-10-25，「已经切换到 Hex 发送但是还是显示文本发送，并且文本的图象字符残留」）。**根因待复现**，已核对四处：① 同一工具栏有**两个近名按钮**、含义不同 —— `btnSendQuick`（`tabpage.js:147-149`，切 `sendFormat`，处理函数 `toggleSendFormat` 在 `app.js:3207`）与 `btnToggleSend`（`tabpage.js:96`，切的是**发送区显示** `state.txDisplayMode`，`toggleTxDisplay` 在 `app.js:3218`）；只点前者时后者照旧显示「↾ 字」，而两者文案只差一个图标前缀，很容易被当成同一个「发送格式」；② 发送状态有两份 —— `getSendState()` 用 `tab._s`（`app.js:56-61`），另有 `tab._page.ui`（`tabpage.js:274`）**只在切标签时被写一次**（`app.js:2538-2539`）且全仓库**无任何读取**，是死状态但字段同名；③ 按钮文案有三条互不相干的写入口 —— 建页初值硬编码 `' 字'`（`tabpage.js:149`，不走 i18n）、`updateSendQuickBtn`（`app.js:3239`，`innerHTML` 重建含 SVG 图标）、`updateToggleSizers`（`:959-983`，只写 `data-wide` 幽灵宽度、不写文案）；④「残留」不像镜像层没清 —— 两条渲染路径都在末尾 `container.innerHTML=''`（`app.js:267` / `:352`），更可疑的是文本模式的 `ctrl-mark`（`·` / `←` / `↵`，`app.js:271-288`）与 `.send-mirror-placeholder`（`:242-245` / `:293-295`）的旧节点，以及 `.toggle-btn` 的幽灵宽度 `::before{content:attr(data-wide);visibility:hidden}`（`style.css:309-312`；`btnSendQuick` 已被 `:316` 关掉，`btnToggleSend` 仍启用）。**需要复现信息**：单标签还是多标签、切换瞬间输入框里有没有内容、点的是工具栏按钮还是菜单项。**修法方向**：删掉或合并 `_page.ui`，按钮文案只保留 `updateSendQuickBtn` 一处，切换格式时强制重建镜像并断言旧节点数为 0 |
| 81 | 端口下拉里「已占用」的黄色高亮没了 | 已修复 | **2026-10-26 修复**：`_markOccupied(sel, owner)` 改为收**元素**，`updatePortOccupied()` 按 `state.tabs` 逐页 `root.querySelector('#portSelect' / '#portSelectA' / '#portSelectB')` 标注——不再用 `document.getElementById`（多标签时只命中文档里第一份，除第一页外其余页面的 `<option>` 永远拿不到 `port-occupied`，样式在 `style.css:338`，自定义下拉的 `.cs-option.port-occupied` 在 `:521`）。`owner` 传入该页所属标签页，「前向模式下自己另一端算占用」按本页判定（旧实现用 `getActiveTab()` 并跳过 `state.activeTab`，只有活动页恰好是第一页时才碰巧对）。新增 `_syncOccupiedDropdown(sel)` 把占用类按 option 值对齐同步到已渲染的 `.cs-option` 副本（含 portal 到 body 的打开态），不必重开下拉才看得见；且只在 `className` 真变化时才写，顺带解决 #79 ① 的「无条件写」那一半。夹具 `_audit/port_occupied_pages.py` 19 项断言（多页标注 / 自排除 / 跨页占用 / 前向 A·B 各自另一端 / 副本同步 / 空闲端口不变色 / 二次调用 0 次写）；变异验证：`app.js` 退回旧实现后 9 项立即变红。**保留语义**：判定仍依赖 `t.portOpen && t.syncedParams`，未打开的「已声明」端口不标黄 |
| 82 | 没开串口的页面上，右下角仍挂着上一次会话的统计与速率 | 已修复 | **2026-10-26 修复**（用户截图：转发页「端口转发空闲」却显示 `P1:0 P2:0 P1:0/s (0%) P2:389/s (3%)`）。两条独立原因：① `_updateStatsVisible()` 只判「守护进程在线 + 是会话页」，不看 `portOpen`，空闲会话也照显示；② 速率只写进 DOM、**从不按会话缓存也从不清理**，`closePort()` 里还先 `refreshStatsDOM()` 再置 `portOpen=false`，于是数字停在断开那一刻、速率停在最后一次 `stats-rate` 事件上（切标签更是直接沿用上一个会话的速率）。修法：**统计区不隐藏**（用户确认：没开串口也要显示），改为 `refreshStatsDOM()` 在 `!tab.portOpen` 时直接 `resetStats()` 显示 0（不读缓存、也不去拉 daemon）；`reset()` 改走同一个 `fmtSpeed(0,0)`（原来硬编码 `'0 B/s(0%)'`，与正常路径的 `'0/s (0%)'` 两种写法）；速率按会话存进 `state.statsBase[sid].txRate/rxRate`，`refreshStatsDOM()` 连字节带速率一起按会话回放（没收到过就是 0）；`closePort()` 删掉该会话缓存、改完状态后再 `refreshStatsDOM()`；**先 `setStatsMode()` 再写值**（原来反着来，切到转发页时 `P1/P2` 会先显示单端口的数字，要等下一次事件才纠正）；开串口后主动刷新。夹具 `_audit/statusbar_stats_reset.py` 13 项（空闲仍显示但为 0、写进去的残留值会被刷掉、连上可见、字节/速率按会话、切标签不串台、切回恢复、closePort 后仍显示且归零、缓存已删）；变异验证：不回放速率 / 先写值再切模式 各自变红。**另**：`refreshStatsDOM` 在 `!portOpen` 的提前返回里也要先 `setStatsMode()`，否则空闲转发页的统计箭头会停在上一个标签页的 TX/RX 上（同一处修复，夹具里加了一条断言） |
| 83 | 空闲的转发标签显示为绿色（看起来像正在转发） | 已修复 | **2026-10-26 修复**（用户截图：标签栏里「端口转发空闲」是绿色，而同样空闲的「单端口空闲」是灰色斜体）。`.tab-forward { color: var(--green) !important }` 只要 `mode === 'forward'` 就加，`!important` 连 `.tab-idle` 的灰色一起盖掉，于是「空闲」和「正在转发」看起来一样。改为只在 `mode === 'forward' && portOpen` 时加 `.tab-forward`，空闲时退回 `.tab-idle`（灰色斜体），真正转发时才绿 + 加粗。夹具 `_audit/tab_forward_color.py` 9 项（空闲活动/非活动、非活动与单端口页同色、斜体与字重、连上变绿加粗、断开退回）；变异验证：去掉 `portOpen` 判定后 4 项变红 |
| 84 | 右上角主题按钮没有图标 | 已修复 | **2026-10-26 修复**（用户反馈「右上角主题按键没图标」）。两条叠加的原因：① `index.html` 的 `data-icon="theme_palette"` 这个键**两头都没有** —— Go 内嵌的 `config/icons/system/*.svg` 里没有，`icons.js` 的静态兜底表里也没有，`icon()` 直接返回空串；② 就算键存在也看不见：`.c-theme-btn` 只有 `width: 28px` 而没有清 `padding`，全局 button 的 `5px 14px` 内边距把内容区压成 **0px**，里面的 `<svg>` 作为 flex 子项被 `flex-shrink` 挤成 0 宽（实测 `getBoundingClientRect().width === 0`）。修法：兜底表补 `theme_palette`（同时补一份 SVG 到 `config/icons/system/`）；`_initIcons()` 由「整体替换」改为**与兜底表合并**（原来 `Object.assign({}, data)`，Go 少哪个键那个图标就会被清空，切标签时的 `_populateIcons` 还会再刷一遍）；`.c-theme-btn` 补 `padding: 0`，并给 `.c-theme-btn > svg` 钉住 18×18 + `flex-shrink: 0`（`_populateIcons` 塞的是裸 svg，原来那条规则只认 `.theme-icon` 类）。夹具 `_audit/theme_icon.py` 10 项（有 svg、属性 18×18、**渲染尺寸** 18×18、按钮内容区容得下图标、Go 只回少量图标时不丢、全文档 `[data-icon]` 都有图标）；变异验证：键名写错 / 改回整体替换 / padding 不清零，3 组各自变红 |
| 85 | 新建会话页「很小气」，且多语言键不完整 | 已修复 | **2026-10-26 重做**（用户反馈「新建会话窗口重新设计，当前感觉很小气，然后还有多语言 key 不完整」）。**版式**：原来是 208×约 120 的小方块（标题 18px、正文 12px、只有一行说明 + 一条示意线）。现在卡片 380×315：44px 图标块、18px 名称、一句话说明、**三条特性**、连接示意（两端点 + 断开式中线的 ↔）、底部「开始 →」；外层 26px 标题 + 14px 副标题 + 30px 间距，网格 `repeat(auto-fit, minmax(280px, 1fr))`（窄窗自动单列，不新增断点）。新增 `swap` 图标（`icons.js` 兜底 + `config/icons/system/swap.svg`）。**多语言**：① `_rebuildTabLabel(tab)` 首行是 `if (!tab.syncedParams) return;` —— 选择页/设置页没有 `syncedParams`，标签文字因此停在建页时的语言（切 English 后标签仍是「新建会话」），现在按 `type` 处理 `new` / `settings`；② 前端 `t()` 引用了但 **9 个语言文件里都不存在**的 11 个键，一直靠中文默认值兜底：`settings.tab_size`/`eol_sequence`/`bubble_font`/`font_family`/`font_size`/`color_head_italic`/`color_bold_tip`/`color_italic_tip`、`status.daemon_offline`/`daemon_offline_hint`/`cursor_fmt`；连同新增的 7 个 `newtab.*` 键一起补齐，9 语言 **392 → 410** 键，parity 全等。RTL 下「开始 →」箭头镜像（`scaleX(-1)`）。夹具 `_audit/chooser_redesign_i18n.py` 35 项（版式、图标、三条特性、尺寸、9 语言逐项无回落、标签页标题跟随、`dir=rtl`、箭头镜像、11 个旧缺键有英文译文）；`mode_picker_smoke.py` 的旧版式断言同步更新；变异验证：卡片名称不挂 i18n / 标签不跟随语言 / 卡片退回旧尺寸，3 组各自变红 |
| 86 | 调色板弹窗内部文案写死中文，8 种非中文语言下仍是中文 | 已修复 | **2026-10-26 修复**（起因：用户截图指出「调色板标题没有背景俩字」）。独立项目 `调色板` 先出 0.2.5（41 个 key 的 i18n 注入：`i18n` 属性 / `el.i18n` / `setI18n()` / 全局表，`title` 属性仍最优先），再出 0.2.6（长语言字号自适应 `_fitTexts()`，`--cp-fit-min-size` / `--cp-fit-ratio` 可调，标题缩到下限后走省略号兜底）。本项目：① 组件连升 0.2.3 → 0.2.4 → 0.2.6（均为原样副本、哈希一致）；② `_colorPickerI18n()` 把 41 个 key 映射到 `colorpicker.*` 并写进 `i18n` 属性，9 语言各补 41 条，键数 410 → 453、parity 全等；③ 标题改三段式「设置项名 · 背景/字体 · 深色/浅色」，为此补 2 个列名短词键 `settings.color_bg_short` / `color_fg_short`；④ 弹窗宽度用内联样式覆盖成 360px（组件 `:host` 写死 320，三段式在 en/fr/ja/ru 下会被压到 15~17px 或用省略号截断；360px 下 9 语言都保持 19px 单行，最紧 5.9px 日语）。夹具 `_audit/colorpicker_title.py` 75 项（9 语言标题 + 组件内 8 处文案 + 字号/截断/宽度）、`_audit/colorpicker_component.py` 22 项（组件行为 + 0.2.3 实时对照） |
| 87 | 设置页标题「设置」越用越黑，像叠了重影 | 已修复 | **2026-10-26 修复**（用户截图：设置页左上角「设置」二字带明显黑色重影，「改一次字体阴影就黑一次」）。根因不在样式（`computed textShadow === 'none'`，全仓库也没有 `text-shadow`）：`refreshSettingsPage()` 找的是 `.settings-layout`，却拿 `buildSettingsPage()`（返回整个 `.settings-wrapper`）去 `replaceChild`，于是每刷新一次就**多套一层 wrapper、多复制一个 `position:absolute` 的 `.settings-header`**；header 叠在同一坐标 (16,62)，标题文字一层层压上去就成了"黑重影"。触发点两处：切语言（`i18n.js:28`）、颜色区「重置」按钮（`settingspage.js:1023`）—— 改字体/字号本身不重建，只是用户往往在改完设置后切语言看效果。实测：切 3 次语言后 wrapper/header/title 各 4 个，标题区域平均灰度 **211.64 → 174.40**（暗像素占比 18.68% → 30.77%）；人为克隆 3 份 header 得到同样的 174.40，确认是叠层而非阴影。修法：替换目标改成 `layout.closest('.settings-wrapper')`，并向上找到**最外层** wrapper（已叠出多层的窗口刷新一次即自愈回 1 层），nav/panel 的查找改在新 wrapper 内做。夹具 `_audit/settings_refresh_no_dup.py` 12 项（改字体/改字号/切 3 次语言/颜色重置/激活面板保留/自愈）；变异：替换目标退回 layout → 5 项红、wrapper 一路涨到 7 |
| 88 | 设置页「数据高亮」的预览不像显示框 | 已修复 | **2026-10-27 改**（用户请求：「能让设置中高亮的预览窗口用类似显示框的渲染方式吗」）。旧预览是 `innerHTML` 拼串：字号写死 10px/11px、颜色逐 span 内联、控制字符自己拼 `.ctrl-mark/.ctrl-real`、转义字节用带内联色的裸 span，跟主显示区两套实现 —— 所以预览里的行距、字重、`←↵` 标记、`/FF`、Hex 行都跟真实显示框对不上。改法：① `makeDataRow()` 增加可选第 6 参数 `{ displayMode }`（默认行为不变），让预览能在同一栏里同时画「文本」和「Hex」两种渲染而不去动全局 `state.displayMode`；② `_renderColorPreview()` 改为每栏调 4 次 `makeDataRow`（Rx 文本 / Rx Hex / Tx 文本 / Tx Hex，转发用 P1/P2），segments 手工构造，转义/控制字符全部走 `renderSegments()` 的真实路径；③ 深浅两栏各用 `_applyPreviewVars(box, mode)` 把该主题的 `--dc-*` 内联到容器（`_applyDisplayColors` 只写 `:root[data-theme=…]`，两套配色要同屏并排只能这样）；④ `_applyDisplayStyle()` 的规则选择器加上 `.c-color-preview`，预览字号/字体/tab 宽度跟 `state.displayFontSize` / `displayFontFamily`。**顺带发现**：旧预览硬编码的「~10B」是错的，转发示例实际是 9 字节，真实渲染算出来是 `~9B`。夹具 `_audit/settings_color_preview.py` 21 项；变异：把 `.c-color-preview` 从字号规则里去掉 → 3 项红，`makeDataRow` 忽略 `opts` → 2 项红 |
| 89 | Linux 下打开串口报 `Permission denied`，界面没有任何可操作指引 | 已修复（0.9.0） | **2026-11-01 实测**（Ubuntu 26.04.1 + CH340 `/dev/ttyUSB0`，`crw-rw---- root:dialout`，当前用户不在 `dialout` 组）：点「打开串口」弹出 `打开失败RuntimeError: Permission denied`，来自 `frontend/app.js` 的 `openPort` catch（只拼了 `t('serial.open_fail') + e`）。根因是 Linux 设备节点的 DAC 权限，WebView 把它包成 `RuntimeError` 后界面原样显示，用户看不出该做什么。**已修（0.9.0）**：新增 `sysperm` 包（`access(2)` 探测 + pkexec 固定脚本做临时授权）、GUI 非模态横幅、`serial-cli fix-permissions`；`permission denied` 不再直接弹原错误，而是由横幅接管并给授权入口。详见 v0.9.0 那段与 CHANGELOG。**红线不变**：不能 pkexec 执行安装目录里的脚本（那目录在 `$HOME` 下可写，等于送提权） |
| 90 | 用户态安装版的守护进程被 GUI 菜单关掉后，systemd 用户服务不会自动恢复 | 待处理 | **2026-11-01 实测**：安装包装的服务是 `Restart=on-failure`，而 GUI 菜单「关闭守护进程」走的是正常退出（`daemon` 收到 IPC shutdown 后 exit 0），于是服务变 `inactive` 且不会被拉回（journal 可见 `00:32:28 GUI 请求关闭守护进程 / 守护进程已停止`，之后 `is-active` 一直 false）。再开 GUI 时 `autoStartDaemon` 默认 false（`settings.go:46`），界面就停在离线，用户只能手点菜单、或像我那次直接在终端里跑 `serial-daemon`（00:35 手起，和 systemd 那份不会打架——`flock` 单例挡住了重复实例）。**待定**：① GUI 的关闭菜单改为先 `systemctl --user stop serial-tool`；② 或服务改 `Restart=always` + 关闭时走 stop；③ 或干脆让 GUI 独占守护进程生命周期、安装包不装 systemd 服务。三条互斥，先定策略再改 |
| 91 | 同一个工程推送两次会得到两个守护进程，界面上多出一个同名标签 | 待处理 | **2026-11-01 实测**，修完「推送工程看不到标签」（见下方 v0.9.0 记录）之后暴露的相邻问题：`session.open` 每次都新建进程，`OpenStlogSession` 也不查这个工程是不是已经开着；前端 `_waitProjectTab` 一看到已有同名工程标签就立刻返回，于是第二个进程没人接手 —— 补上归属认领之后，它会以**重复标签**的形式出现。**两条路**：① 推送前先查已有会话里有没有同 `sessionPath` 的，有就切过去、不再 `session.open`（改前端 + 一个只读查询）；② 守护进程 `OpenSession` 里先按 `sessionPath` 查 `pm`，命中就复用那个进程。② 更彻底，但要先定「工程已经开着、而它的端口正被别的会话占用」怎么报 |
| 92 | 疯狂刷屏时把文件拖进窗口，会弹出一个「文件 C:/…」的空白窗口 | 待处理 | **0.8.4 遗留**（2026-10-31 18:18 用户报告，合并 commit 明写「拖入文件的偶发失败仍未定位」）。当时截图：刷屏中的主窗口旁边多出一个「无标题」窗口，地址栏是 `文件 C:/…\0.8.4-dev\history\姿态传感器`、内容空白 —— 拖放没被 `preventDefault` 拦住，WebView 按默认动作导航到了 `file://`。**v3 复现不出**（用户 2026-11-01 确认），但机制没修：Windows 上 v3 的拖放**仍然全靠 JS**（`$GOMODCACHE/github.com/wailsapp/wails/v3@v3.0.0/pkg/application/webview_window_windows.go` 的 `setupChromium` 里那段注释写明 `JS calls preventDefault()`），主线程被刷屏占住时那个窗口期还在，`frontend/app.js:8065` 的捕获阶段 `preventDefault` 与它跑在同一个线程上，一起掉队。**要复现就照原样试**：COM16 @230400 刷满屏时拖工程目录进去；真复现了先挂 `PerformanceObserver('longtask')` 量主线程长任务，再决定是压渲染成本还是加兜底 |
| 93 | 「这个漏历史到另一个窗口……」——工程的历史显示到了别的标签页 | 待处理 | **0.8.4 遗留**（2026-10-31 18:32 用户报告，附截图），当时没定性、用户随后转去合并 main，至今没人解释过这条。截图状态：两个标签「#0」「姿态传感器 1」，当前页显示「进程已创建（空闲，模式=单端口）」+ 实时数据。**已排除**：不是目录重名 —— `history\` 下只有 `姿态传感器` 一个工程目录，而 `UniqueDirName` 生成的是 `base (2)` 不是 `base 1`（`sessionfile/dirname.go:99`）。**怀疑方向**：推送后另起了会话，历史留在原工程里，界面上看着像「历史跑到别的标签去了」。**需要用户确认现象**才能定性（是数据串台，还是「推送后没接着原历史录」的另一面 —— 后者已随 save 语义一起修，本条是否同一件事尚未确认） |
| 94 | 先开 GUI、后开守护进程，有概率连不上 | 部分修复 | **0.8.4 遗留**。根因在 v3 迁移时定位到并改掉了：前端曾有一条「离线时每秒试连」的轮询，它和用户点「启动守护进程」抢同一把锁，把点击静默吞掉（结论留在 `frontend/app.js:2883-2884` 的注释里）。现在删掉了轮询，改成 Go 侧 `watchDaemonReady` 经 `pipe.WaitReady` 每 1s 重发 `daemon-ready`（`app.go:1020` 的 `daemonReadyRepeat`），丢一次也能自愈。**剩下的只是复验**：GUI 在跑 → 控制台起 `serial-daemon` → 1.5s 内应出现 `[daemon] 事件 daemon-ready` 与 `CheckDaemonStatus ok=true`。顺带：前端注释仍写「每 3s 重发」（`app.js:2877`、`:2958`），实际是 1s，改注释时一并改 |
| 95 | 守护进程换实例后 GUI 不会自己重连，界面停在「在线」 | 待处理 | **2026-11-01 实测**（验证「日志路径由守护进程自报」时撞到）：`serial-cli shutdown` 停掉旧实例、1 秒后从另一个目录起新实例，之后 GUI 再没打过一条 `GetSessionsForWindow`，标签页停在旧状态；重启 GUI 才连上。**怀疑**：`daemon-ready` 的处理是 `if (!state.daemonOnline) tryConnect()`（`frontend/app.js:2708`），若干净关闭那一刻客户端没把 `daemonOnline` 置回 false（`daemon-shutdown` 没送到或没处理），新实例就绪后 `tryConnect` 直接返回。**待查**：① 守护进程退出前是否真广播了 shutdown 事件；② 心跳（2s×3 + 一次重连确认）为何没把这种情况判成掉线；③ 稳妥的修法大概是「`daemon-ready` 到达时比对 `daemon.info` 的 PID，换了实例就无条件重连一次」 |
| 96 | 多字符串面板收起时，右边缘的拖拽条与窗口边框缩放抢同一个位置 | 已修复（0.9.1） | **2026-11-02 用户报告**：面板收起（`quickPanelRatio <= 0.01`）后最右侧只剩下那条 5px 分隔条，而它同时是「从右边缘把面板拖出来」的手柄。把鼠标放到窗口右边缘想改窗口大小，命中的正是这条手柄 —— 要么缩放不生效、反而把面板拉了出来（`initQuickPanelDrag` 的 `mousedown` 抢到事件后 `preventDefault()`，原生 resize 不会再触发；`ratio` 一旦 > 0.01 面板立刻显示），要么光标落在非客户区、面板完全拖不出来。两个状态都是 `cursor: ew-resize`，用户无从判断当前抓的是哪一个。**成因**：`.o-divider--quick` 是 `width: 5px` + `margin-right: -3px`（`frontend/style.css:1538`），`::before` 再向左扩 3px，热区约 10px 正好压在客户区右边缘上。[LAYOUT-RULES.md](LAYOUT-RULES.md) §7 把「折叠时保留分隔条作拖出手柄」写成刻意设计，这条就是它的副作用（`.o-divider--quick` 折叠时只 `display:none` 面板、分隔条保留）。**方向**：① 收起态给分隔条加状态类，把 `margin-right` 收回 0、热区改到面板内侧，最后 4~6px 让给窗口 resize；② 或收起态不做贴边手柄，改从菜单/标签入口打开面板；③ 先按实际 DPI 量 Windows 原生 resize 边框的像素宽度（125%/150% 缩放会更宽）再定让开多少，Linux 下窗口管理器差异更大，需一并量。**注意**：`initDividerDrag` 已用 `closest('.o-divider')` + `classList.contains('o-divider--quick')` 排除过 quick 分隔条，改这里别再引入同类互相抢事件。**已修（0.9.1）**：`applyQuickPanelRatio()` 在 `r <= 0.01` 时给分隔条加 `.is-collapsed`。**第一版走的是「把线内缩 10px」**（`margin-right: 10px`），用户实测反馈「右边漏出一条大竖条，超级难看」——线不在边缘本身就是新的视觉缺陷，所以改成**线不动、只挪命中带**：`.is-collapsed` 给元素 `pointer-events: none`（最右侧归还给窗口边框缩放），`::before` 改为 `right: 6px; width: 16px; pointer-events: auto`，即距边缘 6~22px 响应拖拽，比原来的 10px 更好抓；面板展开时两条规则都不生效。LAYOUT-RULES §7 同步改了口径并写明「不要用 margin 内缩」 |
| 97 | 拖标签出新窗口后，源窗口停在设置页/欢迎页时右下角仍显示上一条会话的串口统计，Tab/编码/行尾三项也不隐藏 | 已修复（0.9.1） | **2026-11-02 用户报告**（截图：源窗口留在「设置」页，右下角仍是 `↑Tx:0 ↓Rx:0 ↑Tx:0/s (0%) ↓Rx:0/s (0%)`）。**根因**：拖出走的 `_detachTab()`（`frontend/app.js:2580`）只做了「销毁页面 → 删缓存 → splice → `_clampActiveTab()` → 显示新页 → `renderTabs()`」，**唯独漏了切换页之后的状态栏收尾**。对照组 `_closeTabNow()`（`frontend/app.js:4441-4478`，普通关标签）会调 `updateModeUI / updateEmptyState / renderTabs / updateOpenBtn / updateSyncIndicator / _updateStatsVisible() / updateDaemonUI / loadTabHistory|clearDisplay`，而 `_detachTab` 一个都没调；`switchTab()` 那条路走的是 `_syncTabDisplayChrome()`（`app.js:4282` 的 setTimeout）+ `_updateStatsVisible()`（`app.js:4307`），拖出走的却是 `tearoff-start` / `tearoff-done` 事件（`app.js:2922-2960`）→ `_detachTab`，两条收尾全绕过。于是 `#statusbar-stats` 保持拖走前的可见状态与数字（`_updateStatsVisible()` 在 `app.js:3831-3838`，是唯一会 `display:none` + `resetStats()` 的地方），`.dsp-item`（`Tab:4` / `UTF-8` / `LF`，`frontend/statusbar.js:101-131`）也不隐藏（`_syncStatusDispItems()` 在 `app.js:4263`，同样没跑）。**影响**：0.9.0 引入 tear-off 时即有；设置页 / 欢迎页 / 新建页上会看到上一条会话的统计与显示参数，容易被当成当前值（与 REQUIREMENTS #107「无会话/设置页时隐藏」不符）。**修法**：在 `_detachTab` 末尾（`_clampActiveTab()` 之后）补 `_syncTabDisplayChrome()` + `_updateStatsVisible()` + `updateModeUI()` + `updateEmptyState()` + `updateOpenBtn()` + `updateSyncIndicator()` + `updateDaemonUI()` + `refreshStatsDOM()`；更稳的是把 `_closeTabNow` 的收尾抽成 `_afterActivePageChanged()`，关闭 / 拖出 / 切页三处共用，免得以后再加路径又漏。**验证**：拖走最后一个会话标签 → 源窗口停在设置页时三项与统计应整块消失；把标签拖回来（`sessions-changed` 重新同步）后应恢复。**已修（0.9.1）**：按「更稳的」那条做 —— 新增 `_afterActivePageChanged()`（显示新页 + `updateModeUI` / `updateEmptyState` / `renderTabs` / `updateOpenBtn` / `updateSyncIndicator` / `_updateStatsVisible` / `updateDaemonUI` / `_syncTabDisplayChrome`，会话页再补 `loadTabHistory` + `refreshStatsDOM`），`_detachTab`、`_closeTabNow`、`openSettingsPage` 三处统一调用；`openSettingsPage` 顺手去掉了重复的网格重绘（`settingsTab._page.show()` 里已经排了一次） |

## v0.9.1 进行中

| 需求 / 问题 | 说明 |
|---|---|
| （暂无） | 0.9.1 开发线刚开，这一轮的条目还没进来 |

## v0.9.0 已完成

| 需求 / 问题 | 说明 |
|---|---|
| 工程的 save 标记改记「用户意图」 | `closeStlogLocked` 不再把 `settings.json` 的 `save` 清成 false —— 正常封存（关标签 / 关 GUI / 退守护进程）只关文件；只有用户在标签里手动关保存才把「别录了」写回工程（`markSessionNotSaving`），全局默认变化也不算用户表态。原行为等于「正常关过一次的工程永远不再自动录」，就是用户报的「推送到守护进程就没有启动保存」。`3ea6d05` |
| 工程会话上开保存续写这个工程 | `openStlogFile` 见到 `sessionDir()` 非空就改走 `resumeRecording`，不再在 `history/` 下另建目录（只有工程目录读不出来才退回新建）；`resumeRecording` 接上录制后把 `save=true` 写回工程，否则「手动开保存 → 录完关掉 → 下次推送」又不续录；`openHistoryFile` 不再给工程会话跑 `restoreLatestSession`（会把无关的「最近一个会话」读进环）。`53db096` |
| 推送工程把新会话认领给发起窗口 | `OpenStlogSession` 漏了 `sessClaim`，而 `GetSessionsForWindow` 只在「本窗口名下一条都没有」时才认领无主会话 —— 窗口里但凡开着串口或挂着空闲标签，推送回来的工程就一直是「无主」，本窗口永远建不出标签，5 秒后弹「守护进程没有把这个工程挂上来（进程可能创建失败）」。实机验证：`GetSessionsForWindow "main" -> 2 个（守护进程上 2 个）`。`2cc4c70` |
| 清掉零引用死代码 + 修 `history_save_test.go` 的陈旧断言 | `deadcode -test ./...` 报出的 35 个不可达函数（v1 `.log` 写入器残骸、`EncodePage`/`PageCRC`/`StoredCRC` 这第二份页实现、内置图标包 API 等）；`openStlogFile`/`resumeRecording` 的收尾提成 `attachStlogLocked`。`history_save_test.go` 三条用例还在数 `history/*.log`（v2 起只写 `.stlog` 目录），断言恒为 0 —— 两条一直红、一条假绿，改成按会话目录 + 封存后的记录数断言。`369181d` |
| 「守护进程 → 查看守护进程日志」标签页 | 菜单开一个 `type: 'daemonlog'` 的单例标签页（`frontend/daemonlog.js`），按偏移量增量读 `<exe>/logs/daemon.log`（新绑定 `ReadDaemonLog`）：首次取末尾 128 KB 并丢掉半行，之后每 800 ms 续读，切走停轮询、切回补读。走文件而不是事件流，是为了能看见 GUI 启动**之前**的行 —— 排查「守护进程起不来」时那些行才关键。等宽着色（`[错误]` 红 / `[警告]` 黄）、跟随/清空/复制/打开目录、DOM 最多留 5000 行。9 个语言包各加 11 键。**日志路径由守护进程自己报**：`daemon.info` 增加 `logPath`（`protocol.DaemonInfo`，`omitempty`，老守护进程不回时客户端退回「与 GUI 同目录」的推导），GUI 只在「从头读」时问一次并缓存 —— 否则用户态安装 / systemd 服务 / 手工从别处起的守护进程，GUI 就永远显示「日志文件还没生成」 |
| 再启动一次 GUI = 在新窗口打开（单实例） | 以前双击第二次会起**第二个进程**：两个 GUI 各连守护进程、各认领会话，同一份配置两边各写各的。改用 v3 的 `application.SingleInstance`（`main.go`）：命名互斥体判重，第二份进程发一条 `WM_COPYDATA` 就自己 `os.Exit`，第一份收到 `OnSecondInstanceLaunch` 后走 `NewWindow()` 开一个和菜单「新建窗口」完全同等的窗口。**唯一 ID 按 exe 路径分组**（`instance.go`：路径小写归一化后 FNV32），所以开发版 `build/bin/serial-gui.exe` 与部署版 `serial-gui-dev.exe` 各算一个实例、可以同时开着；同一路径重复启动才落到同一实例。回调里**只入队**、由 worker 建窗口 —— 回调跑在 v3 的 COPYDATA 消费 goroutine 上，那条 channel 容量只有 1（`single_instance.go:77-101`），消费侧一阻塞，主线程的 `wndProc` 卡在塞数据上、建窗口的 `InvokeSync` 又在等主线程，连点两下必死锁。顺带让新窗口按已有窗口数错开 28px（`NewWindow`，8 档循环），否则连开三个全落在同一点，看着像「没反应」。**实测**（2026-11-01，开发版）：启动三次后 `serial-gui` 进程数恒为 1，日志 `[second-instance] … → 在新窗口打开` + `[window] 新窗口 "win-1"/"win-2"`，窗口坐标 410,160 → 438,188 → 466,216。**已知限制**：① 第一份进程主线程卡住时，第二次启动会阻塞在 `SendMessage` 上（v3 没给超时）；② 两个进程在启动瞬间同时抢锁，落败的那个可能还没找到消息窗口就退出，那一次点击等于丢失（实测连发两次都成功，未复现）；③ 关掉最后一个窗口后进程还有 300 ms 才退，这期间到达的通知会开出一个「随即跟着退」的窗口；④ 启动参数目前只记日志，双击 `.stlog` 打开还没接 |
| 跨窗口并入标签时「+」不跟着让位 | 把单标签窗口拖进另一个窗口的标签栏（Go 侧 `mergeWindowIntoTab` → 目标窗口用 `_ghostDrag` 接着拖）时，「+」不移动 —— 被拖进来的标签从「+」右边飘过去，看着像整块跑出了标签栏；同一窗口里拖排序没有这个现象。差别在 `_tabDragStart` 记了「+」的位置（`addLeft/addW`）并每帧推它，而 `_ghostBegin` 没记、`_ghostMove` 也没推。修法：把「夹在标签栏里 + 推『+』」抽成 `_tabDragApplyX`，`_tabDragMove` 与 `_ghostMove` 共用同一份；`_ghostBegin` 也记下「+」的位置，`_ghostEnd` 复位 `transform`。纯前端改动，`deploy-dev.ps1` 同步 `frontend\` 即可，不用重编 exe。**实测**（2026-11-01，部署版）：按住拖入时「+」跟着浮起的标签挪到 660，松手后停在「会话 #1 \| 会话 #2 \| +」，没有残留内联样式；同窗口排序行为不变 |
| 拖标签进出可以反悔（浏览器行为） | 以前「单标签窗口拖进别人标签栏」只要在标签条上停 300ms（`tabMergeDwell`）就落地了：归属转过去、源窗口藏起来，松手前没有任何退路 —— 源窗口还会在 `tearoff-done` 里当场 `CloseCurrentWindow`，连 `Show()` 回来都不可能。现在 `BeginWindowTabDrag` 的轮询 goroutine 变成能来回切的两态循环：未并入 → 光标在别人标签条带里待够 300ms 就并入；已并入 → 光标**离开标签条带超过 48px**（`tabBandLeaveMargin`，只是贴着边缘抖不弹）就当场退回（`unmergeWindowFromTab`：归属退回 + 源窗口 `Show()` + 新帧 `tab-drop-cancel`）；再回去停一下又能并进去。松手才落地：已并入 → `tab-drop-end` + 关源窗口；没并入 → 窗口留在原地。**源窗口不再自我关闭**：`tearoff-start` / `tearoff-done` 改成「停车」（`state._parked`：不自动补空闲会话、不关窗），结局交给 Go 在松手时按最终归属判定 —— `finishTabDrag` 的并入/就地成窗两个分支都调 `closeWindowIfEmpty`，`BeginWindowTabDrag` 松手那段直接关。日志四行：`开始盯落点` / `随窗口并入` / `从 … 退回` / `松手：没有并进别人标签栏`。**实测**（2026-11-02，部署版）：并入 → 拖出（窗口当场重现并继续跟手，落在 370,269）→ 拖回再并入 → 松手落地；另一轮「并入后拖出再松手」窗口留在原地；拖出成窗再拖回源窗口松手走 cancel、两页不变（`_parked` 清掉、标签补回来）。**限制**：拖出成窗那条路仍然是**松手才判**（过程里没有实时的落点预览） |
| 拖出成窗再拖回去也不盲猜（两条拖动合成一条驱动） | 上一条留下的缺口：`StartTearOffDrag` 建好跟随窗后，goroutine 只管 `SetPosition` 跟光标，**松手才调 `finishTabDrag` 判一次**（回源标签条=取消 / 落别的标签条=并入 / 落别处=就地成窗），过程中没有任何预览。现在把两态循环抽成 `driveTabDrop(self, owner, w, sid, follow)`，两条入口共用：`BeginWindowTabDrag`（单标签窗口拖标签，`follow=false`，窗口由系统拖窗循环带着走）、`StartTearOffDrag`（标签拖出成窗，`follow=true`，循环里 `SetPosition` 跟光标）。于是拖出来的窗口压到任何标签条上停 300ms，那一页就当场以幽灵标签插进去（能左右挑位置、也能拖出来退回），松手才落地。`mergeWindowIntoTab` 增加 `owner` 参数：`tearoff-done` 的 `from` 必须是原始源窗口，且**目标等于源时不发** —— 把标签拖回自己窗口时那条广播会和新标签的同步打架，源窗口会把自己刚补回来的标签又摘掉。删掉 `finishTabDrag` / `inTabBand` / `notifyAfterClose`（都只服务「松手才判」那套，`app.go` 净减 56 行）。**实测**（2026-11-02，开发版）：拖出成窗（win-2 跟手）→ 压到源窗口标签条上停住 → `会话 6 随窗口并入 "main" 的标签栏（源 "main"）`＋截图里幽灵标签已插进那排 → 拖出条带 → `会话 6 从 "main" 的标签栏退回（源 "win-2"）`、窗口当场重现 (396,264) → 再压回去 → 松手 → `[close] window="win-2"`、两页都在 main。**限制**：预览只认「标签条带」，拖到内容区不预览（和并入的判定一致） |
| Linux 串口权限：分体版只做临时授权，不碰系统文件 | **策略（2026-11-02 定稿）**：按发布形态分两条 —— 分体版（解压即用）**绝不写系统文件**，一次 pkexec 给当前 `/dev/ttyUSB*`、`/dev/ttyACM*` 加 ACL，拔插或重启后节点重建、权限失效，需要再点一次；聚合版（安装式）才允许落 uaccess 规则，见「待实现」。**实现**：新增 `sysperm` 包 —— `sysperm_linux.go` 用 `access(2)` 判断（不能看 mode 位：uaccess 与 setfacl 给的是 ACL，节点仍然是 `crw-rw----`），`sysperm_other.go` 恒 `supported=false`；`FixTemp()` 里是**编译进二进制的固定脚本**，优先 `setfacl -m u:<uid>:rw`、系统里没有 setfacl 时退回 `chmod 0666`（设备会短暂所有人可读写，拔插/重启即恢复），成功后重新探测确认，只有真的可读写了才算成功。GUI 新增绑定 `GetSerialPermission` / `FixSerialPermission`；横幅 `#permBanner` 是**非模态**的（插在标签栏与内容区之间参与 flex 布局，出现时把内容压下去，不挡任何控件）：连上守护进程后探一次、`startDaemon` 成功后探一次、打开串口/端口转发失败且错误含 `permission denied` 时再探 —— 探到被拒节点就由横幅接管提示，探不到才退回原来的弹窗（否则设备中途被拔会「什么都不显示」）。设置项 `serialPermissionHint`（`gui.ini`，默认开，设置 → 存储与行为）：关掉后不再自动弹，错误弹窗里仍写明 `serial-cli fix-permissions` / `install.sh --fix-permissions` 两条路。CLI 加 `serial-cli fix-permissions [--yes] [--json]`，不走守护进程，非 tty 且没 `--yes` 时只提示不阻塞。**9 个语言包各加 11 键**（581 → 592），顺手把上一轮插错位置的 `confirm.remember_choice` 挪回排序位置。**验证**：`gofmt -l .` 空、`go build ./...`（Windows）、`GOOS=linux GOARCH={amd64,arm64} go build ./sysperm/ ./cmd/serial-cli/`、`go test ./sysperm/`、两个 JS 的 `node --check` 全过。**2026-11-02 在 Ubuntu 26.04.1 + CH340 虚拟机实测**：横幅出现 `/dev/ttyUSB0` → `fix-permissions --yes` 输密码 → `getfacl` 出现 `user:ns:rw-` → 复探 `denied: []`、`ok: true` → GUI 重开不再报、标签 `/dev/ttyUSB0` 已连接 RX 188.40k → `create /dev/ttyUSB0 115200` 返 `success: true`。ssh/后台启动的 GUI 实测返回 `no_agent`（`Error creating textual authentication agent`），已单独映射成一句人话 + 手动修法 |
| 关窗口确认框加「记住选择」+ 关于页过期文案 | **记住选择**：三选一对话框（`_onWindowCloseConfirm` → `showChoice`）以前只能每次问，可对话框窗口本来就支持勾选框（`dialog.js:68` 按 `spec.dontAsk` 渲染，`ResolveDialog` 会带回 `dontAsk`），只是前端没传。现在 `showChoice` 多一个 `dontAskLabel`，勾上就把这次的选择写回 `window_close_action`（**关窗前落盘**），下次 destroy 直接销毁、transfer 直接转交，都不再问。配套把 Go 的判定抽成 `windowCloseNeedsConfirm(configured, sessions, degraded)`：只有 `ask` 才弹；`destroy` 不再追问（设置页文案本来就写着「直接销毁（会断串口）」，行为和文案对不上）；`transfer` 但没别的窗口可转（退化成销毁）仍然要问。**顺带修掉一个真 bug**：destroy 不再弹框之后，`allowWindowClose` 的「不需要确认」分支以前只处理 transfer，会话根本没人销毁 —— 实测关掉 win-1 后 `[close] … 仍有 1 个会话未销毁`，守护进程上留了个孤儿（串口一直开着）；现在这条分支按 `closeMode` 分派，destroy 走 `destroyWindowSessions`。**关于页过期文案**：`后端 Go / Wails v2` → `v3`；「程序目录」原来传的是空串（`settingspage.js:711`），新增绑定 `GetAppDir()` 后显示真实路径；两处写死的版本兜底（`'0.6.1'` / `'0.8.2'`）拿掉，取不到版本就不显示徽标。**实测**（2026-11-02，部署版）：关于页显示 `C:\Embedded\test_py\串口调试工具\0.9.0-dev` 与 `Go / Wails v3`；关窗对话框出现「记住选择」，勾上选「转到其他窗口」→ `gui.ini` 写入 `window_close_action = transfer`、会话转到 main，再关一个新窗口直接转交不再弹；把设置改成 destroy 后关窗不再弹框且会话被销毁（日志 `销毁会话 1/1`）。9 个语言包各加 `confirm.remember_choice`（580 → 581 键） |
| 关于页的「渲染」与「平台」文案对齐 Linux | 关于页「渲染」一行以前写死 `WebView2`，Linux 版（Wails v3 + WebKitGTK）上就是假话；「说明」第三段也写着「运行于 Windows 平台」。现在新增绑定 `GetRenderEngine()`（windows → `WebView2`、linux → `WebKitGTK`、其它 → `GOOS`），这一行改为从后端取；`settings.about_p3` 在 9 个语言包里统一改成「Windows 与 Linux」，键数不变（仍 593）。**实测**（2026-11-02，Ubuntu 虚拟机）：关于页显示「渲染 WebKitGTK」，程序目录为 Linux 路径 |

## v0.8.3 已完成

| 需求 / 问题 | 说明 |
|---|---|
| 会话文件格式 v1.4 定稿 | 从连续追加改为 **4 KB 定长页**：页头 64 B + 记录区 + 页尾 CRC（`[4092,4096)`）；记录 12 B 头、允许跨页分片（`dir` bit7 续片链 + `firstRecOff`，`0xFFFF` 表示本页无帧首）；双影子页**只前滚**恢复；统计块 44 B 让列表页不必解析记录。方案与两张图在本机 `docs/archive/`（已 gitignore，不进仓库） |
| `sessionfile` 包 | 槽读写/选槽/校验、页读写与整页 CRC、统计块、分片与拼接（含断链）、双影恢复、扫描索引、文本导出；`e7d7959` |
| 离线 CLI | `serial-cli stlog-read / stlog-verify / stlog-dump / stlog-export`；`stlog-read` 带 12 字符 origin 列，`stlog-export` 带 `--limit` 截断提示；`a440de3` |
| 守护进程录制 | 与 `history/*.log` **同源双写**：跟随 `history.enable`，`flush` 定时器批量落页；不做文件流转、不参与 `history.attach/new/detach`；`d2c4e0a` |
| GUI 拖放 + 只读标签页 | 拖入或从选择页选 `.stlog` → 开只读标签页：首屏 500 帧 + 「加载更多」、`↑ 年` 切换（持久化 `stlog_show_year`）、来源列与顶栏完整来源表、大小/页/记录/rx-tx；`0b67c64`、本次 |
| 文本导出 | `sessionfile.WriteText` + `ExportStlogText`：CRLF、32 B 换行、来源列宽动态、系统帧按文本输出；截断时提示 |
| 标签浮窗统一 | 会话卡与文件卡同一套骨架，都有「已载入」与「收发比」进度条；长值右对齐可换行（原来「2000 帧」被裁掉）；`tabpop.loaded` 等键补进 9 语言 |
| 自动滚动误冻结 | 根因是 `content-visibility:auto` 让 `scrollHeight` 在行进入视口时虚增，而判定只看 `scrollHeight` → 误判「离开底部」并冻结跟滚。改为只有 `scrollTop` 变小才算用户上翻；已冻结时仍以「真回到底部」解冻 |
| 部署脚本 | `deploy-dev.ps1`：`-Build` 先构建 4 个 exe + 前端，再镜像部署到 `0.8.3-dev`（含 `resources-system` 子树守卫），逐文件校验字节数；目标目录有同名 exe 在跑就拒绝部署。`BUILD.md` 记「资源查找顺序」与「热重载版从磁盘读前端」两个坑 |
| 示例夹具 | `stlog-samples/` 6 个（基础 / 多页 / 跨页切帧 / 坏链恢复 / 单页多片 / 整页无帧头），另 `07-real-com16/` 两段真实录制 |
| **.stlog v2：一次录制 = 一个目录**（`c3d280a`） | 目录名 `<yyyyMMdd_HHmmss>_<id8>`（24 字符，允许改名）；`manifest.json`（只记连接）+ `settings.json`（业务设置 + 预留 `plot`）+ `index.bin`（可重建缓存）+ `pkg-%04d.stlog`（每包都是合法 v1.4）。切包 `entryCost = 12 + payload + 来源串`，默认 64 MiB（可配 8 MiB–1 GiB），一帧不跨包、超大帧独占一包。`ReadSession` 目录走 v2、文件走 v1.4 |
| 停止写 `history/*.log`（`c36b428`） | 录制只剩 `.stlog`；老 `.log` 只读可读；历史恢复（`history.new` / `history.attach`）改从 `.stlog` 读回 |
| 会话文件交给守护进程（`6073969`…`f0ff709`） | 新增 IPC `session.open`：只接受工程目录，按 `settings.json` 建单端口 / 转发进程并灌入历史；单个 `.stlog` 直接拒绝。GUI 打开工程先是只读页（**离线可用**），顶栏「推送到守护进程」把它恢复成会话页后自动关掉只读页；失败保留只读页并报错。`process.list` 增加 `sessionPath`，同步时不再给文件会话多建标签页 |
| 拖入解析 + 毛玻璃遮罩（`688538d`、`18f4ff3`） | 工程目录与 `.stlog` 都能开，无法识别的路径汇总成一个警告窗；遮罩覆盖「菜单栏下沿 → 状态栏上沿」，z-index 压过「守护进程未启用」；守护进程未启动时不再显示引导页 |
| 浮窗定稿（`69b1721`、`d5d3961`、`6f97647`） | 文件页浮窗换成 v2 结构（端口 → 参数 → 两条车道 → 分流比 → 帧/时长/来源）；尖角整段删除 —— 1× 下 45° 抗锯齿永远对不齐边色 |
| 系统帧不再显示成 Tx（`73f6e02`） | `Sys` 标签、无箭头、强制文本模式 |
| 许可证 MIT → Apache 2.0 | `LICENSE` 换全文 + 新增 `NOTICE`；README / THIRD_PARTY_NOTICES / docs / `wails.json` / 三个 `winres.json` 同步；用 `go-winres make` 重新生成 6 个 `rsrc_windows_*.syso`，exe 的 `LegalCopyright` 与 `FileVersion`（0.8.0 → 0.8.3）一并对齐 |

## v0.8.2 已完成

| 需求 / 问题 | 说明 |
|---|---|
| 新建会话页重设计（#85） | 卡片从 208px 小方块改成 380×315：图标 + 名称 + 说明 + 三条特性 + 连接示意 + 「开始 →」；标题/副标题分层，窄窗自动单列 |
| 多语言补键（#85） | 新增 7 个 `newtab.*` 键并补齐 11 个「前端引用了但语言文件里没有」的键；9 语言 392 → 410，parity 全等；选择页与设置页的标签文字跟随语言 |
| 图标库 | 新增 `swap` 图标（Go 内嵌 + JS 兜底），供端口转发卡片使用 |
| 调色板升级 0.2.3 → 0.2.6 | 同步独立项目（原样副本，哈希一致），两版连着上：**0.2.4** 修「取色按钮是个 emoji `💧`」（0.2.3 有 Font Awesome 就用 FA 图标，没有就退化成彩色 emoji，本项目没有 FA）、「未设 `theme` 时弹窗背景全透明 `rgba(0,0,0,0)`」、滑块/输入不派发 `color-change`、`save` 的 `index` 错位、触摸屏拖不动；**0.2.6** 带来 41 个 key 的 i18n 与长语言字号自适应。调用面仍是 `theme`/`transparency`/`color` + `open()` + `apply` |
| 调色板标题 + 内部文案 i18n（#86） | 标题「设置项名 · 背景/字体 · 深色/浅色」，列名用短词，弹窗内联加宽 360px；组件 41 个 key 全部接上 9 语言（键数 410 → 453）。夹具 `colorpicker_title.py` 75 项 + `colorpicker_component.py` 22 项 |
| 设置页标题叠影（#87） | `refreshSettingsPage()` 改成替换整个 `.settings-wrapper`（原来只换内层 `.settings-layout`，导致每刷新一次多套一层 wrapper + 一个绝对定位 header 叠在同一坐标）；另加"向上找最外层 wrapper"的自愈，已叠出多层的窗口刷新一次即恢复。夹具 `settings_refresh_no_dup.py` 12 项 |
| 数据高亮预览改用显示框渲染（#88） | 预览不再 `innerHTML` 拼串，改调 `makeDataRow(..., {displayMode})` + `renderSegments()`，结构/样式/字号字体与主显示区同源；深浅两栏各自内联 `--dc-*`。顺带修正旧预览硬编码的 `~10B`（实际 9 字节）。夹具 `settings_color_preview.py` 21 项 |
| 资源统一到 `resources/` | 主题改成「一个主题一个文件夹」（`theme.json` + 自带素材），图标包与语言包同根；旧 `builtin/`、`themes/colors/`、`themes/icons/` 不再读取 |
| 资源分内置 / 外置两层 | `resources-system\`（内置，动态版）与 `resources\`（外置，成品版也认），查找顺序 内置 → 外置 → 内嵌，逐个文件回退；成品版顺序自动退化成 外置 → 内嵌。列表里内置资源强制在前 |
| 主题支持图片 | 全页背景 `--app-bg-image`、四条栏背景与遮罩、主题卡缩略图 `--thumb-image`；外置素材由 Go 读盘内联成 data URI（asset server 出不了 frontend 目录）。条栏用 `background-attachment: fixed` 与全页背景拼成同一张照片 |
| 设置页外观卡 | 「整体外观」改名「深浅模式」，三张迷你窗口预览卡；颜色主题卡改色样样式（底色 + `Aa` + 强调色胶囊），支持深浅的左右各画一半 |
| 数据高亮调整 | 控件右对齐、去掉行选中高亮、行距与组间距加大、行间淡分割线；双列门槛 1100→1300，表头改 `B`/`I` 与月亮/太阳图标，长语言不再压字 |
| 新建会话页（第二版） | #85 的 380×315 大卡改成紧凑网格卡（绿点特性 + 整宽「开始」），卡片 315→208px，`auto-fit` 加模式自动换行；深色下 CTA 文字色取 `--input-bg` 保证对比 |
| 标签栏拖动重做 | 竖线指示器换成实时补位：原标签压暗在栏内滑动、占位块不可见只占位、FLIP 让位动画；pointer 事件替代 HTML5 拖放（后者隐藏源元素会被取消拖动），`setPointerCapture` 保证拖出窗口也能松手；「+」可被推到窗口边界；拖到左右边缘自动滚动 |
| 多字符串拖动重做 | 与标签栏同一套：压暗行、不可见占位块、FLIP、上下边缘自动滚动、推挤「+ 添加」 |
| 菜单下拉层级 | 下拉被标签栏盖掉上半截 —— `.header` 自成层叠上下文（z-index 6） |
| 文言文语言包 | 补齐到 461 键（旧包 344），放外置层 `resources\i18n\lzh.json` |
| 旧路径文案 | 「创建示例 / 打开文件夹」与 9 语言提示从 `themes\colors\`、`\i18n\` 改到 `resources\{themes,icons,i18n}` |

## v0.8.1 已完成

| 需求 / 问题 | 说明 |
|---|---|
| GUI 前端热重载 | 新增构建标签 `hotreload`：`wails build -devtools -tags hotreload` 后前端从磁盘读、保存即整页刷新；发布构建不带该标签时这套代码**完全不参与编译**（发布 exe 里搜不到 `hotreload`）。落地在 `devui_hotreload.go` / `devui_release.go` / `main.go` 三处接线 |
| 新建页先选模式（原 TODO P2） | 删掉工具栏的「⇄ 单端口」切换按钮；点 `+` 先开一个选择页标签（两张调色盘卡片：单端口 / 端口转发），选定后才建空闲进程并**原地**换成会话页。直接关掉选择页不会留下孤儿进程。`isSessionTab()` 统一标识非会话页，`updateModeUI`/`switchTab`/`renderTabs`/`updatePortOpenBtn` 都按它区分 |
| 串口详细设置 → 工具栏同步 | 端口/波特率一改就同步（含下拉框显示文字），缺选项时自动补；`_syncSettingsToBar`/`_syncBarToSettings` 收敛到 `_settingsSyncPairs()` |
| 端口列表按标签页填充 | 新增 `_populateTabPortSelects(root)`，`updatePortList` 对每个页面各填一次（此前 `getElementById` 只命中第一页，其余标签页端口下拉永远为空） |
| 开关串口不再把文本/Hex 冲回文本 | 显示模式增加 `_persistDisplayModes()`：切换时写回标签页副本，避免 `syncDaemonSessions` 用旧副本回填全局（实测夹具 + 变异验证） |
| 冻结视图整屏空白 / 往上翻加载不到更早 | 占位块整体换成「右下角未读胶囊」；`expandHistory` 的裁剪方向改为从底部裁；`_syncSpacer` 自愈改成 `_updateUnreadChrome` |
| 自绘滚动条 | 显示区与发送框共用 `_initOverlayScrollbar()`：8px、同一套颜色，支持拖动/点轨道翻页；显示区轨道从配置栏底边起，发送框轨道从工具栏底边起；原生滚动条全部隐藏 |
| 拖动后「滚不动」 | 滚动条松手回到底部即解冻；`applySendRatio` 改完高度后若在跟随则重新贴底 |
| 分隔条 | 上限按配置栏实测下沿（贴住浮动栏下方）、下边界贴底且只剩一条线（收起时隐掉状态栏上边框）；拖动比例改由几何反算，1:1 跟手 |
| 显示区收起时隐藏内容 | `is-collapsed` 时收起浮动按钮/数据流/滚动条，避免从配置栏模糊层后面透出来 |
| 发送框 | 焦点框去掉（光标即提示）；滚动条统一为自绘 8px；清空键/发送键各左移让出滚动条位置 |
| 状态栏 | 「运行中」与任务栏同色同高并贴左；客户端计数加 5 秒权威兜底刷新（`GetClients()`），徽章悬停显示具体 clientId |
| 端口「已占用」高亮（#81） | 按每个标签页的 root 标注（不再只命中第一份下拉），自排除按本页判定；已渲染的 `.cs-option` 副本同步更新；状态没变时不写 `className` |
| 下拉不再整表重建（#79 ①） | `_csRefresh` 加选项签名短路（值/文字/类/选中项），签名没变就直接返回；`.cs-option` 点击改事件委托；`updatePortList` 不再对工具栏下拉做 `getElementById` 空填 |
| 发送框 Hex 着色（#47 / #48） | `0x` 前缀改用弱化色（与字节色区分）；`AA0x` 紧贴末尾的前缀不再整段标红 |
| 标签栏滚动提示（#26） | 标签栏左右浮动箭头（复用发送区 `.send-scroll-arrow`，点击穿透、不占布局），溢出与滚动位置变化时各自显隐 |
| 状态栏统计（#82） | 始终显示；端口没连上时显示 0（不显示上一次会话的数字）；速率按会话缓存并随标签切换回放；先定模式再写值，避免 P1/P2 先闪一次单端口数字 |
| 转发标签着色（#83） | `.tab-forward` 只在真的在转发时加：空闲转发页和单端口空闲页一样是灰色斜体，连上才变绿加粗 |
| 主题按钮图标（#84） | 补 `theme_palette` 图标；图标表改为「兜底 + Go 合并」；按钮清 `padding`、`> svg` 钉死 18px 且禁止收缩 |

## v0.8.0 已完成

| 需求 | 说明 |
|------|------|
| 0.8.0 里程碑（稳定基线） | 把 0.7.5.1 ～ 0.7.5.10 的修复整体转入 `main` 并作为稳定基线交付。**本版不含功能改动**：源码与 0.7.5.10 的区别只有版本号，四个 exe 的行为与 0.7.5.10 完全一致（0.7.5.10 轮外部测试报告 P1 全过、全量回归全绿） |
| 已知未修项交接 | 0.7.5.10 报告唯一新发现 `queueCount` 口径问题记入 TODO #77，留给 0.8.1；测试方建议把「设置页守卫」移出必验清单（无黑盒入口，属代码级保障），已按此结论执行 |

## v0.7.5.10 已完成

| 需求 | 说明 |
|------|------|
| `sendone` 黑盒入口（TODO #76） | 新增 CLI 命令，使「多字符串条目剥头」可按 `delay=1` 的条目在接收端直接验收；ARCHITECTURE 的发送模式表补上「必须有黑盒入口」的说明 |

## v0.7.5.9 已完成

| 需求 | 说明 |
|------|------|
| 自动建会话不再清掉自己的会话（TODO #73） | 自动建之后重新取权威列表，不再 `daemonSessions = []` |
| `addTab` 补 `connectedAt`（TODO #74） | 宽限期在自动建会话路径上生效 |
| 验证方式升级 | `autocreate_unit_fixture.py`：**桩替换 Go 绑定**，不碰真实守护进程/串口，可在任何人正在用的情况下确定性复现；`autocreate_unit_mutation.py` 三组合变异验证 |

## v0.7.5.8 已完成

| 需求 | 说明 |
|------|------|
| 陈旧载荷不再清掉活着的会话（TODO #71） | 会移除在线会话时才先 `GetSessions()` 核实；新增不核实 |
| 宽限期真正生效（TODO #72） | 三处写入 `connectedAt` |
| 验证 | 夹具 F 节确定性复现 + 变异 D6 还原后重建用户现象；六个变异 6/6 捕获，夹具 28/28 |

## v0.7.5.7 已完成

| 需求 | 说明 |
|------|------|
| 队列 delay 语义（TODO #69） | 省略=1000 / 0=不延时 / 上限 60000 / 负数报错；CLI 与 MCP 边界各自区分「省略 vs 显式 0」 |
| delay 剥头下限对齐（TODO #70） | `DecodeEntryContentOnly` 不再拒绝 1–4；`send.trigger(raw=false)` 不再把条目头当数据发 |
| 验证 | CLI 侧 5 档耗时实测 + MCP 侧 3 项实测 + 新增 3 个测试文件（含 serial-mcp 的首个测试） |

## v0.7.5.6 已完成

| 需求 | 说明 |
|------|------|
| 设置页不再被当成会话页（TODO #64） | 会话类入口统一走 `isSessionTab`/`requireSessionTab`；停在设置页时给提示而不是静默挂错 |
| 建进程不再多出标签页（TODO #65） | `openPort`/`openForwardPorts` 补 `_creatingTab` 保护 |
| 僵尸清理只数会话标签页（TODO #66） | 设置页不占会话位 |
| 会话回来时找回原标签页（TODO #67） | `_orphanSid` + 重新绑定，不再销毁重建 |
| 按钮不再被别的页改掉（TODO #68） | `updateOpenBtn` 只动当前页元素 |
| 夹具稳定性 | 历史窗口夹具的单条渲染成本改为 3 次取最快（并打印样本），阈值不再被调度噪声误报 |

## v0.7.5.5 已完成

| 需求 | 说明 |
|------|------|
| 历史读取改分页（TODO #57） | `session.history` 新增 `limit`/`beforeTsMs`/`sameTsSkip`，守护进程侧新增 `ringbuf.SnapshotPage`（只复制选中一页）；实测整环 18 万条 79.4ms/15.86MB → 一页 5000 条 5.0ms/0.44MB，基准 `BenchmarkHistoryRead` |
| 前端历史缓存有界（TODO #58） | 跟随保留 10000 条、回看放宽到 40000 条，从最旧一端裁；实测灌 30000 条后停在 10032 条且尾条为最新 |
| 「加载更早」滚动触发（TODO #59） | 滚动触发器去掉 `_renderStart > 0` 前置；`_ringExhausted` + 游标推进双重防抖 |
| 自动回补开关 | `设置 → 高级 → 自动回补更早历史`（开启/关闭，9 语言）：关闭后滚到顶只摆可点提示 |
| 历史环写满即冻结（TODO #12） | `recordHistory` 不再忽略 `Write` 的返回值；新增 `WriteEvict`，环满时挤掉最旧包，单包超容量才计 `ringDrops` |
| 事件时间戳与环内时间戳统一 | `RxTxMessage.TsMs` = 写进环的同一个值；分页游标靠它精确对齐（两边各取一次 `time.Now()` 会在毫秒边界错开） |
| CLI 分页 | `history [pid] [--limit n] [--before ms] [--skip n]`，未知参数报错，附 `history_args_test.go` |
| MCP 分页 | `serial_history` 新增 `limit`/`beforeTsMs`/`sameTsSkip` 与描述、schema 更新 |
| 契约测试泄漏共享内存（TODO #60） | `TestEveryContractMethodIsDispatched` 补 `defer pm.DestroyAll()` |

## v0.7.5.4 已完成

| 需求 | 说明 |
|------|------|
| 同步 `probe.toml` 注释（TODO #54，报告 §4.1） | 按实际公式重写；`probe.go` 加漂移提醒 |
| 未知参数应报错（TODO #55，报告 §4.2） | 抽出 `parseProbeArgs` + 提前校验 + 3 条单元测试 |
| 同步 MCP schema 默认值（TODO #56，报告 §6.2.4） | `default 12000` → `15000` |

## v0.7.5.3 已完成

| 需求 | 说明 |
|------|------|
| 历史窗口按批渲染（TODO #51） | 入站帧队列 + rAF 合并成批；一批一次 fragment/裁剪/贴底；贴底改写不读。吞吐 893 → 242131 帧/秒，持续流入单条 1000 → 240µs |
| 裁剪恒定执行（TODO #51） | 此前锁定时完全不裁剪，实测喂 5000 条 DOM 5000 行；现恒做，冻结由 `_frozen`/`_atBottom` 表达 |
| 往回翻：视图冻结但滚动条继续变化（TODO #51） | 未读区用占位块撑高度；实测 scrollTop 变化 0px、内容高度 +123002px、DOM 有界 |
| 去掉丢弃式重绘节流（TODO #51） | 实测清空后空白、切换显示模式画面不跟随；改为不丢弃 |
| 「加载更早」可达（TODO #51） | `expandHistory` 拆开「扩窗」与「取更早」，`_renderStart==0` 时仍会去取 |
| 窗口行数按视口推导 + 高级设置（TODO #51） | 接上从未被调用的 `calcRenderCount()`；`设置 → 高级 → 历史窗口行数`（自动/150/300/400/600），9 语言 |
| 控制台 null 异常（TODO #51） | `loadTabHistory` / `clearDisplay` 补 null 守卫 |
| 默认波特率全覆盖（TODO #52，按 C1 方案） | 去掉 1s 隐藏下限 + 预算 15s；13.4s 覆盖全部 7 档、skipped 为空 |
| CLI 补 `busy`（TODO #53，报告 P1） | 抽出 `probeOutcomeJSON` 并加字段集合测试 |

## v0.7.5.2 已完成

| 需求 | 说明 |
|------|------|
| `probe` 空结果与「没有设备」不可区分（TODO #50，报告 P1） | `ProbePorts` 改返回 `ProbeOutcome`：被占用 / 打不开 / 读不了 / 超预算 四条「压根没探成」的路径全部进 `skipped` 并带原因，且补上日志（此前连日志都没有）。实机：并发探测 54ms 返回 `busy`、被会话占用 73ms 返回明确原因，默认配置 12.7s 返回结构化结果并列出未试波特率 |
| 客户端 10s 超时先于守护进程放弃 | 总预算默认 12s（`--budget` / MCP `budgetMs` 可覆盖），到点返回已有结果并列出未试项；`ports.probe` 请求超时放宽到 25s |
| 预算内够不到最常用的波特率 | `baud_rates` 改为按可能性排序，115200 提到最前（原升序下 12s 预算用尽时 115200 根本没试过） |
| 并发探测返回空结果 | `probeMu.TryLock`，第二个探测返回 `busy: true` +「已有另一次设备探测正在进行，本次未执行」 |
| `probeRead` 丢弃读取错误 | 改为返回错误；串口打开成功但读不了的端口不再被报成「未检测到」 |
| CLI / MCP / 文档没说清这三种「没检测到」 | CLI 输出带 skipped/attempts/elapsedMs/budgetExhausted 并在 stderr 提示；MCP 描述与 `budgetMs` 参数；`操作说明.md` §九 新增「怎么看探测结果」 |

## v0.7.5.1 已完成

| 需求 | 说明 |
|------|------|
| 文本模式高亮与光标逐格错位（TODO #43） | 镜像层用 `·` 顶替了空格的宽度，比例字体下每空格错半格到一整格（宋体累计 +77px）。改为真实字符保留原宽度、标记绝对定位叠加。10 种字体实测漂移 0～0.64px |
| Hex 预处理四条路径分叉（TODO #44） | 抽出 `_normalizeHexInput` 单一实现，发送／自动发送／回写环形缓冲／字节统计共用。`0xAA 0xBB`／`AA,BB`／`0xAA` 自动发送由失败变为正常发出，字节数由 4B 修正为 2B |
| 1 位数字判绿却发不出去且无提示（TODO #45） | 新增发送框悬浮提示（悬停 + 发送被拦下两条触发），9 语言本地化，5 秒自动消失、不拦鼠标 |
| 占位符写死中文（TODO #46） | 镜像层改走 `t('send.placeholder')` 并在切语言时重绘 |
| 自动发送失败弹窗缺分隔符 | 9 种语言的 `serial.start_fail` 都没有尾随冒号，实测显示为 `Start failedinvalid hex: ...`；在代码里补 `': '` |

## v0.7.5 已完成

| 需求 | 说明 |
|------|------|
| `probe` 多字节响应被截断（报告四轮未愈） | `Read` 一有字节就返回而探测只读一次，9 字节响应只读到 1 字节，`modbus_crc` 规则对多字节响应一律失效。改为循环累积并在帧收完后结束。实测 0/5 → 5/5 命中，无应答端口耗时不变 |
| 端口描述偶发退化为端口名 | 描述靠起 PowerShell 子进程查 WMI（超时 10s），繁忙时返回空 map，而空描述的回退是端口名本身。改为直接读注册表，WMI 保留为兜底。refresh 1–2s → 0.08s |
| `daemon not running` 断言（#27） | 0.7.0 只清了 CLI 那层，`client.CallOnce` 与 `pipe.dialPipe` 的包装仍在。改为让底层错误自己说话 |
| 连接阶段失败被误当成握手失败并重试 | 去掉上条包装后暴露：守护进程未运行也报「注册握手失败（已尝试 3 次）」。现按阶段区分，确定性失败立即返回 |
| Tx/Rx 大小写统一（#42） | `en`/`es`/`fr` 共 30 处大写改为 `Tx`/`Rx`，与前端及其余 8 种语言一致 |
| 许可证评估 | 评估 Apache-2.0 后决定**保持 MIT**：依赖全为宽松许可证（技术上可行），但其相对 MIT 的核心增量（明示专利授权）对本工具无实际意义，而「必须标注修改过的文件」等义务对嵌入式受众是摩擦。单一版权人，将来可随时再改 |

## v0.7.4.2 已完成

| 需求 | 说明 |
|------|------|
| 注册握手并发失败（约 8%） | 先复现到可处置程度（8 路并行 8.8%，串行 0.3%），推翻外部报告的根因推断，加握手重试（全新 clientId）把 8.8% 压到 0.21%。**随后定位到真正根因并修复**——见下条与「已知问题」#36 |
| 根因：注册确认先到被判成握手失败 | v0.7.4 引入的回归。确认消息在两条回连管道都成功后由守护进程写出，可能抢在客户端 `Accept` 之前到达，而旧的 select 逻辑「谁先到按谁处理」，把成功确认判成失败。改为统一截止时间等待、只在 `msg.Error` 非空时中断。实测满负载 + 三种并发度共 1180 次 0 失败 |
| 握手重试暴露的 listener 泄漏 | `connectOnce` 的 sub 管道失败/超时分支漏关 `respLn`，重试会让每次失败泄漏一个 listener 及预建实例。见 #37 |
| 文档过度承诺「新旧混用被拦」 | 改为准确表述：比对的是协议号，同为协议 1 的版本设计上兼容可混用。见 #38 |

## v0.7.4 已完成
| 需求 | 说明 |
|------|------|
| 共享环生命周期竞态致守护进程崩溃 | `pm.Close()` 在释放 `pm.mu` 之后才拆共享内存，而 `Process` 指针裸共享，读取方可能碰到已 `UnmapViewOfFile` 的环。实测 `-race` 复现 `DATA RACE` + `0xC0000005`；因 dispatch 无 `recover` 且守护进程是机器级单例，后果是所有会话与串口一起断。环的置空移入保护它的锁内、使用方持锁后重新判空；`historyFile` 改用 `histFileMu`；`sendRing` 同源窗口一并修掉 |
| 回连失败原因被丢弃（只剩超时） | 客户端在握手期间读 `daemonConn`，并为它单独设 select 分支，从而**快速失败**而非干等 5 秒。实测 2.0s → 0.3–1.5ms |
| 客户端拉起的守护进程零日志 | 日志双路输出到 `<exe>/logs/daemon.log`（2 MB 轮转），新增 `serial-cli logs [n]`（无需守护进程在运行）。Windows 下须共享打开（否则运行中读不到）且须 `FILE_APPEND_DATA`（否则覆盖日志开头） |
| 前端三处逻辑从未生效 | 速率告警类名对齐 CSS；`selected` → `is-selected`；`refreshSysMsgI18n` 改为遍历全部标签页而非 `getElementById` 只取第一个 |
| `sendqueue` 静默容错与 BOM | CLI 侧改为严格字段校验（`DisallowUnknownFields`）并给出含字段名与示例的报错，接受 `data` 作 `content` 别名；解析前剔除 UTF-8 BOM |
| autosend 三处状态问题 | queue 模式空队列改为明确报错（此前静默空转）；start 时归零 `sendCount`/`errorCount`/`lastSend`（此前跨轮累加）；`writePortSilent` 补 nil 串口守卫（此前对空闲进程解引用即 panic） |
| 测试补充 | 新增 `daemon/ringlife_test.go`（并发建销 + 读写环的生命周期竞态）、`daemon/logfile_test.go`（共享读、追加语义、轮转）、`daemon/autosend_regress_test.go`（空队列/计数归零/间隔生效/nil 串口）、`client/handshake_test.go`（首个 client 包测试，覆盖三管道握手失败路径与措辞）、`cmd/serial-cli/sendqueue_test.go`（严格校验/BOM/别名/空内容）。关键测试均做变异验证 |

> 本节后半部分（`sendqueue` 严格校验、autosend 三处）来自外部 AI Agent 的《串口调试工具 v0.7.4 全面测试报告》，已于本机用 COM3/COM4 交叉互连 + COM12（Canaan K230 Buildroot 控制台）独立复测确认。**报告中另有两条 P0 经实测证伪**（single 模式不发送、`intervalMs` 不生效），详见该报告复核记录。

## v0.7.3 已完成

| 需求 | 说明 |
|------|------|
| 多字符串发送「+ 添加」改为表格末尾行 | 原为独立页脚条 `.qp-footer`（带 `border-top`、独占一行高度），与表格分离。现改为表格最后一行 `.qp-add-row`：数据少时紧跟末行之后（实测间距 0px）。原按钮带的 `.toggle-btn` 有 `width: max-content` 会阻止撑满整行，故新行不再使用该类 |
| 快速面板滚动条跨过表头与添加行 | `.qp-table` 既是定义列宽的网格、又是滚动容器，滚动条必然跨过表头与添加行（实测跨度 `184..926`，应为 `217..889`）。改为三段式：`.qp-scroll` 成为唯一滚动容器，跨度与「表头下沿..添加行上沿」完全重合。`subgrid` 随之不可用（其轨道来自母网格、不随滚动区滚动条收窄，会溢出到滚动条底下），表头与滚动区改为两个独立网格共享确定值列模板 `--qp-cols`——用 `max-content` 时表头文字与行内控件固有尺寸不同，实测宽面板下偏差可达 14px，改定值后 enable/hex/delay/send 四列为 0 |
| 横向滚动条过粗 | 全局规则只有 `::-webkit-scrollbar { width: 6px }`，`width` 约束的是竖向滚动条；横向的粗细由 `height` 决定，未设置即回落平台默认值（Windows 上十几像素）。补为 `width: 6px; height: 6px` |
| 滚动条出现/消失导致整列跳动 | `.qp-scroll` 加 `scrollbar-gutter: stable` 让滚动条槽恒占 6px，`.qp-hdr` 右内边距相应取 `10px`（4+6）。实测有/无滚动条两态下列偏差完全一致 |
| 表头「延时(ms)」被截成「延时(…)」 | 拆成独立网格后表头单元格宽度等于列轨道本身（44px），而「延时(ms)」需 60px（55 内容 + 4 内边距 + 1 边框）。原来 `subgrid` + `gap: 0` 时单元格宽度是「轨道 + 间隙」（54+6=60）正好放得下，故属拆分网格引入的回归。延时列改为 62px，从内容列最小值（48→40）补回 |
| 快速面板横向滚动条恢复（限高 6px） | 表头在滚动区之外，只让数据行横滚会导致表头不跟随、列错位；故 `.qp-hdr` 自身也横向可滚（滚动条隐藏），由 `tabpage.js` 的 `scroll` 监听同步 `scrollLeft`，横滚条只由 `.qp-scroll` 显示一条。实测横滚前后列偏差一致（最大 3.5px）。各列最小值合计 284px，触发横滚条的面板宽度约 298px |
| 「先开守护进程再开 GUI 有时连不上」 | 两层根因：① 守护进程在 Windows 控制台里运行时，**点击控制台窗口会让它进入选择（QuickEdit）模式，此后进程往控制台写输出会阻塞**；日志是同步 `fmt.Printf`，一卡住 `handleRegister` 就停在回连途中，客户端只能等 5 秒超时（控制台标题会带「选择」前缀）。现于启动时调 `SetConsoleMode` 关掉 `ENABLE_QUICK_EDIT_MODE`（须同时设 `ENABLE_EXTENDED_FLAGS` 才生效），改动成功时记一条日志——已用 `cmd /c .bat` 在真实控制台里验证该日志出现。② `CheckDaemonStatus` 原先用 `tasklist` 当硬闸门，误判（安全软件拦截 / CreateProcess 失败 / 输出格式变化）即拒绝连接，且无超时——卡住会让 `a.checking` 永远停在 true 导致永久离线。现改为直接拨 IPC（无守护进程时 `WaitNamedPipe` 立刻返回 `ERROR_FILE_NOT_FOUND`），`tasklist` 加 3 秒超时，`checkOffline` 不再以它的结果决定是否尝试连接 |
## v0.7.2 已完成

| 需求 | 说明 |
|------|------|
| 隐藏分支从未生效 | `applySendRatio()` 把类名 `'is-hidden'` 当成 `overflow` 的值赋值，CSSOM 静默忽略非法值，`r >= 0.97`（收起显示区）与 `r <= 0.01`（收起发送区）两个分支从未执行；面板只被 flex 压到 0 高度，`sendRatio = 0.005` 时留下 4px 高的工具栏残条。改为 `classList.toggle('is-hidden', cond)`，转发模式分支一并清理 |
| 浅色主题缺 4 个语义色变量 | 两个浅色块各 17 个变量 vs 深色块 21 个，缺 `--accent`/`--green`/`--red`/`--yellow`。`var()` 无 fallback 时属性在 computed-value 阶段失效、整体回落初始值，4 个设置开关「开」态不可见（透明轨道 + 白圆点），并波及状态栏在线点、标签活动点、速率告警色、端口占用标记及一批 `background:var(--accent); color:#fff` 的选中态 |
| 清空按钮压住第一条数据行 | 按钮底边 76px（`top:48` + `h:28`）vs `.display-content` 预留 74px，重叠 48×2px。引入 `--reserve-top: 76px`，`padding-top` 与按钮 `top` 由同一值推导 |
| `LAYOUT-RULES.md` 与实现不符 | 补三处说明：`.display-area` 的 `min-height` 以 JS（40px）为准；`.tabs-scroll` 的 `overflow-y: visible` 受 CSS 规范限制实际为 `auto`（故「激活标签覆盖分隔线」不生效）；`.o-divider--quick` 折叠时保留是刻意设计（拖出手柄） |
| 文档版本号滞后 | `LAYOUT-RULES.md`、`frontend/style.css` 头部、`ARCHITECTURE.md` 标题与构建章节同步到 v0.7.2 |

本轮另记录 6 条未修项于「已知问题」#22–#27。

## v0.7.1 已完成

| 需求 | 说明 |
|------|------|
| Linux 支持（daemon / CLI / MCP） | 按平台拆分成对文件：管道改 `$XDG_RUNTIME_DIR/serial-tool/` 下的 Unix 域套接字（0700），共享内存改 `/dev/shm` + `mmap(MAP_SHARED)`，控制台代码页与串口流控设置下沉到平台文件。`pipe.Addr` 由常量改为平台变量，新增 `pipe.Endpoint(name)` |
| 共享内存打开路径误用声明长度 | 打开既有对象时调用方传的大小为 0，触发「声明的数据区超出映射长度」；改用 `Fstat` 取真实文件大小，且只在创建者进程退出时 `unlink` |
| 平台代码约定无文档 | `ARCHITECTURE.md` 新增 §13：条件编译的文件级粒度、三条硬规则（标签与后缀是 AND、后缀必须是合法 GOOS/GOARCH）、7 对平台文件清单、`go list` 排查法 |
| 换行符依赖本机 `core.autocrlf` | 新增 `.gitattributes` 固定 `eol=lf`；此前 Windows 上 `gofmt -l .` 把全部 Go 文件报成未格式化，Linux 上却干净 |
| 平台文件标签不一致 | `client/process_windows.go` 补 `//go:build windows`，14 个平台文件统一风格 |

## v0.7.0 已完成

| 需求 | 说明 |
|------|------|
| 广播阻塞导致守护进程整体卡死 | 事件推送改为每会话一个写入协程 + 有界队列（256），广播只做非阻塞投递；慢客户端被单独摘除而非拖住所有人。实测：一个不读事件的客户端此前能让新客户端注册阻塞 63～143 s，修复后稳定 66～125 ms |
| 管道层两个挂死缺陷 | `winPipeConn.Close` 先 `CancelIoEx` 再 `CloseHandle`（同步句柄上的 `CloseHandle` 会等待在途 I/O，从别的协程关闭会永久挂住）；`Close` 改为幂等。摘除到会话清理完成由 8.7 s 降为 1 ms。新增 `pipe.CancelPending` |
| IPC 协议版本协商 | 新增 `daemon.info`（版本 + 协议号 + PID + 运行时长），`status` 一并返回版本；客户端建会话后立即核对，不匹配明确报错；`serial-cli start` 自动重启版本不一致的守护进程（先等旧实例真正退出，否则新实例会因 Global 互斥体被占用而静默失败） |
| 命名管道握手竞态 | 管道实例改为在 `Listen` 返回前创建，并在每次 `Accept` 取走后立即补建，使管道名全程有实例可用。此前实例在 `Accept()` 内部才创建，对端可能拿到 `ERROR_FILE_NOT_FOUND`（实测连续调用 CLI 约 80% 失败） |
| 共享内存同名复用 | `CreateSharedRing` 检查 `ERROR_ALREADY_EXISTS`（此前只在句柄为 0 时才看错误码，而同名对象会「成功」返回既有句柄，导致两个无关进程共享内存） |
| 环名跨实例冲突 | 环名加入守护进程实例令牌；顺带把四处逐字重复的环名构造收敛为 `newRingNames()` |
| 共享内存头部校验 | 打开已有映射时校验魔数与**布局版本**（版本字段此前只写不读），并给数据区大小加上界；打开路径改为「先按头长度建视图校验，再按声明大小重建」 |
| probe 配置查找缺口 | 补上 `<exe>/config/probe.toml`：发布包是 exe 与 config 子目录同级，此前从其它工作目录启动时随包配置被静默忽略 |
| 方法面收敛为单一事实来源 | 新增 `contract` 包（45 个方法名常量 + `All` + `Valid`）；`client.Call`/`CallOnce` 形参改为 `contract.Method`，写错方法名无法通过编译 |
| 误导性的错误文案 | 去掉 CLI 连接失败时硬套的 `daemon not running: ` 前缀——失败原因可能是未运行、版本不匹配或握手失败，断言错误结论会把排查方向带偏；`serial-cli check` 改为报告双方版本并区分三种情况 |
| 测试补充 | `ringbuf`（15 例）与 `contract` 从零覆盖；`pipe` 增加握手竞态、`CancelPending` 唤醒读写、`Close` 不阻塞与幂等；`daemon` 增加契约一致性（每个声明的方法都真的实现 / dispatch 不得出现字面量方法名）与畸形参数边界测试。关键测试均做变异验证 |

## v0.6.5 已完成

| 需求 | 说明 |
|------|------|
| MCP stdio 分帧合规 | `serial-mcp.exe` 由 Content-Length 分帧改为换行分隔 JSON-RPC，符合 MCP stdio 传输规范；此前规范客户端完全无法通信 |
| 无 daemon 时 CLI 挂死 | `pipeListener.Close()` 改用 `CancelIoEx` 中止挂起的 `ConnectNamedPipe`：`DisconnectNamedPipe` 与 `CloseHandle` 都会等待该 pending I/O，导致 listener 永久阻塞 |
| CLI 一次性命令 4.1 s 开销 | 同源问题（40 × 50 ms × 2 个 listener 的自连接重试），已移除，降至约 60–80 ms |
| `probe` 开箱可用 | `probe.toml` 增加 `//go:embed` 内置回退；搜索路径修正为 `<exe>/../../config/` 并去除重复候选 |
| CLI 用法错误静默退出 | 14 处 `if interactive { 打印; return }` 改为先打印提示再判断模式 |
| `autosend` 可选 pid | 参数校验 `< 3` → `< 2`，`stop` / `status` 可不带 pid；补 `interval` 帮助行 |
| `go test ./...` 可运行 | `config.T` 的 args 改为切片参数，规避 `go vet` 的 printf wrapper 误判，config 包不再构建失败 |
| 文档同步 | ARCHITECTURE.md 的 MCP 分帧、BUILD.md 的 probe 搜索路径说明 |

## v0.6.4 已完成

| 需求 | 说明 |
|------|------|
| `DecodeEntryContentOnly` 误判修复 | 新增 `flags`（≤0x03）和 `delay`（5–60000 ms）校验，防止 Modbus 首字节 0x01 被误判为 multistr 版本头 |
| `send.trigger` raw 参数 | 新增 `raw` 参数区分原始发送（`raw:true` 跳过解码）和 multistr 快捷面板发送（`raw:false`），修复 Hex 数据首字节 0x01 时字节丢失 |
| 自动发送逐条目延迟 | `sendOneRound` 默认延迟从硬编码 1000 ms 改为跟随 `autoSendIntervalMs`，兜底 100 ms |
| Hex 显示跟随设置颜色 | 移除 `.hex-data` 中冗余的 `color: var(--fg)`，方向专属颜色变量（`--dc-hexRx` 等）不再被覆盖 |
| 启动显示模式同步 | `syncDaemonSessions` 新标签页继承全局 `state.displayMode`/`state.txDisplayMode`，激活时显式刷新按钮文字和样式 |
| `findProbeConfig` 路径增强 | 新增 `<exe>/../config/probe.toml` 搜索路径，覆盖 build/bin → config 开发场景 |
| MCP ServerInfo 版本统一 | `serial-mcp.exe` ServerInfo.Version 从硬编码 `0.5.9` 改为 `version.Version` |

## v0.6.3 已完成

| 需求 | 说明 |
|------|------|
| 环形缓冲区快照读取 | `ringbuf.Snapshot()` + `OldestTimestampMs()` 替代 `DrainAll()`，客户端读取历史不清空缓冲区，多客户端独立读取 |
| 历史清空本地化 | GUI `clearDisplay()` 仅为本地视图操作，不通知 daemon，不清环形缓冲区和磁盘文件 |
| 上滚召回历史 | 清空后上滚从 daemon 快照召回环形缓冲区数据；缓存边界外 `SearchHistory` 分页回溯磁盘文件 |
| 磁盘历史翻页 | `_searchDiskHistory` 含去重、`_diskOffset` 分页追踪、`_diskLoading` 互斥防连点 |
| 浮动按键栏重构 | 发送工具栏 RX/TX/发送格式切换按键合并到 `c-fwd-float-btns`，`updateModeUI` 根据 single/forward 模式自动显隐 |
| 浮动栏样式统一 | RX/TX 按键新增 `disp-btn-rx`/`disp-btn-tx`，使用时间戳颜色变量 `--dc-tsRx`/`--dc-tsTx`，Hex 模式实心背景白字 |
| 转发模式浮动栏精简 | 移除 `⇃字` `↾字`，仅显示 `[P1 文] [P2 文] \| [锁定滚动]` |
| 多标签页元素查询修复 | `_setSelectVal`/`getSerialConfig`/`openSerialPort`/`openForwardPorts`/`swapForwardPorts`/`_syncSettingsToBar` 改用 `pageEl` 活动页查询 |
| `pageEl` 属性名匹配 | `updateModeUI` 中 `pageEl('forwardFloatBtns')` → `pageEl('fwdFloatBtns')`，修复 TabPage 属性名不匹配导致的全局查询回退 |
| `_syncPortSelectsFromTab` 闲置声明进程 | 判断条件从 `tab.portOpen` 改为 `tab.portOpen \|\| sp.portName`，闲置声明进程也写入端口/波特率 |
| 统计初始化拉取 | `refreshStatsDOM` 无缓存时主动调 `GetSessionStats` 从 daemon 拉取统计 |
