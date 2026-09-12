# 待办与已知问题

> 本文档维护当前待实现事项、已知问题，以及近期版本（v0.6.3 ～ v0.7.1）的已完成记录；
> 更早版本完成情况见 [README.md](README.md)「版本历史」。

## 待实现

优先级约定：P1 > P2 > P3（高 → 低）。

| 优先级 | 需求 | 依赖 | 说明 |
|--------|------|------|------|
| P1 | CLI/MCP/daemon 全量 i18n | 基础已有 | 命令输出、帮助、日志等跟随语言 |
| P1 | 守护进程跟随 GUI 启停 | 设置页面 | - |
| P2 | Web 客户端 | daemon 内嵌 HTTP + WebSocket | 浏览器操作串口，局域网可用，`--web` 开关控制 |
| P2 | 网络调试能力 | TCP/UDP 协议栈 | TCP Client/Server、UDP 收发，与串口统一的操作体验 |
| P2 | GUI CPU 占用优化 | `stats-count` 节流、批量 DOM 渲染 | - |
| P2 | 高吞吐量下操作系统调度延迟优化 | 驱动接收缓冲区调大 | - |
| P2 | 辅助线程（超阈值自动创建） | - | - |
| P2 | Modbus RTU 协议支持 | Modbus 协议库 | Python 版已实现 |
| P2 | MCU 协议感知 | - | `\r\n` 分隔符 + `#` 回显过滤，Python 版已实现 |
| P2 | 串口连接来源识别 | 端口列表 | 端口列表显示占用端口的进程/线程信息，自身连接标注来源标签 |
| P2 | 创建虚拟端口 | 虚拟串口驱动 | 创建成对虚拟串口，无硬件即可测试串口通信 |
| P2 | 文本/Hex 对照显示 | 分段解码已有 | 历史框分栏显示 Hex+偏移 与文本对照，选中联动 |
| P2 | 输入框镜像渲染迁移至 Go | Go 端文本分段解析 | 将 send mirror 的逐字符遍历移至 Go 端 |
| P2 | daemon 重启自动恢复磁盘历史 | `historyFile` 自动加载 | daemon 启动时扫描 `history/*.log`，匹配进程 ID 的文件自动加载到环形缓冲区 |
| P3 | GUI 性能面板 | `session.stats` IPC 已就绪 | - |

## 已知问题

状态取值：`部分修复`（有缓解方案，未完全解决）、`已缓解`（防护已生效，仍有极小概率触发）、`已知限制`（预期行为，不计划修复）、`待处理`（已记录，尚未处理）。

| # | 问题 | 状态 | 说明 |
|---|------|------|------|
| 1 | 清空后内容太少时上滚不触发 `expandHistory` | 部分修复 | display 内容不足以产生滚动条时 `onscroll` 事件无法触发；已保留可点击的「↑ 向上滚动加载更多」入口；改进方向：onscroll 替代方案，内容不足时也能召回 |
| 2 | 重复快速点击「加载磁盘历史」可能触发多次请求 | 已缓解 | 已加入 `_diskLoading` 互斥和 `pointerEvents` 禁用，极快连击下仍有极小概率穿透 |
| 3 | `loadTabHistory` 重新从 daemon 取数据后 `_clearedAt` 标记丢失 | 已知限制 | 切换标签导致缓存被重新覆盖时清除标记线消失，所有历史均可见；此为预期行为 |
| 4 | `go vet` 报告 `ringbuf_windows.go` 多处 `unsafe.Pointer` 使用警告 | 已知限制 | 3 处（`ringbuf_windows.go` 的 `mapAddr` 转换）。转换对象是 `MapViewOfFile` 返回的文件映射地址，不是 Go 堆内存，GC 不会移动它，生命周期由 `UnmapViewOfFile` 约束，属 vet 保守误报。**注意 `go vet` 结果会被构建缓存**：换用干净的 `GOCACHE` 才会重新分析，否则可能读到过期的「干净」结果 |
| 5 | `.settings-page--overlay` 遗留死代码 | 已修复 | v0.6.5 删除（`frontend/style.css` 定义与后代选择器两处，JS 无引用） |
| 6 | `frontend/style.css` 头部注释版本号过时 | 已修复 | v0.6.5 更新为 `v0.6.5 — CSS` |
| 7 | 控制字符 CRLF 渲染双路径并存 | 已知限制 | `renderSegments()`（Go 解码新路径）合并为单节点 `data-ws="crlf"`（标记 `←↵`），旧 decoder 路径拆为 cr + lf 两个节点；后续统一路径后可简化 |
| 8 | send 环无跨进程互斥，多客户端并发写同一会话可能损坏 | 已知限制 | `ringbuf` 实现是 SPSC：`Write` 对 head 做无 CAS 读-改-写，head 发布与 count 自增分两步；`client.SendWrite` 由各客户端进程各自打开环直接写入，跨进程无互斥。**实测未能触发**：8 客户端 × 256 B × 5 轮、16 客户端 × 1024 B × 4 轮（共 104 次并发写）全部数据完整——写窗口只有几百字节的拷贝时间。彻底修复需选定「发送全部走 IPC」或「加跨进程命名互斥」 |
| 9 | 进程数量无上限 | 已知限制 | 每个进程持有 5 MB 历史环 + 1 MB 发送队列。任何本机客户端都可通过 `process.create` 无限创建。正常使用（几个标签页）无影响；若出现失控客户端会耗尽内存 |
| 10 | `history.attach` 不校验进程是否已连接 | 已缓解 | v0.7.4 把灌入环的那段（`loadHistoryIntoRing`）纳入 `ringBufMu`，不再与 `readLoop` 并发写同一个环，并先判空环。仍不校验进程是否已连接——该入口 GUI/CLI/MCP 均未暴露（`AttachHistoryFile` 绑定无人调用），仅 IPC 层可达 |
| 11 | 共享环生命周期竞态：进程摘除后指针仍被并发持有 | 已修复 | v0.7.4 修复。`pm.Close()` 在释放 `pm.mu` 之后才调 `closeHistory()`，而 `Process` 指针是裸共享的（`Get` 取到指针即放锁），于是读取方可能在环被 `UnmapViewOfFile` 之后才碰它——`GetHistory`/`ClearHistory` 把快照/清零打在已撤销映射上，`recordHistory` 写已关闭的 fd。实测 `go test -race` 复现：`WARNING: DATA RACE` 紧接 `unexpected fault address ... [signal 0xc0000005]`。严重性在于 dispatch 路径无 `recover`，守护进程一崩即所有会话与串口一起断。修法：环的置空移入保护它的锁内（`ringBufMu` / `sendRingMu`），使用方持锁后重新判空；`historyFile` 指针改用 `histFileMu`；文件指针改动收敛到 `detachHistoryFile()`。`sendRing` 有同样窗口（表现为对 nil 调方法 panic），一并修掉。回归测试 `daemon/ringlife_test.go`，已做变异验证 |
| 12 | 历史环写满 5 MB 后静默冻结 | 已知限制 | `Write` 返回 false 时该条不再入环（磁盘文件仍在追加）。表现为长时间高速采集后，重新打开标签页只能看到较早的历史。需点「清空历史」或依赖磁盘分页 |
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
| 26 | 标签栏横向滚动无视觉提示 | 待处理 | `.send-scroll-wrap` 有浮动箭头 + 拖拽滚动，`.tabs-scroll` 只有滚轮映射（`app.js:1567` 的 `deltaY → scrollLeft`），无箭头也无其它提示。13 个标签时最后一个被裁在右边缘，用户未必知道可以滚 |
| 27 | `daemon not running: ` 前缀仍由底层产生，与 v0.7.0 记录矛盾 | 待处理 | v0.7.0「已完成」表记「去掉 CLI 连接失败时硬套的 `daemon not running: ` 前缀」，CLI 自身那层确实去掉了（`cmd/serial-cli/main.go:220` 有注释说明为何不该断言），但 `client/client.go:742` 与 `pipe/pipe_other.go:92` 仍在用 `fmt.Errorf("daemon not running: %v", err)` 包装，文案仍会透出（实测 `serial-cli check` 输出 `守护进程未运行 (daemon not running: pipe not available: ...)`） |
| 28 | 双击控制台程序表现为「闪退」，且原因不可读 | 部分修复 | v0.7.4 起「守护进程已在运行中」这句会同时落盘（`daemon/logfile.go`），控制台关闭后仍可在 `<exe>/logs/daemon.log` 里查到，不再是永久丢失。**仍未解决**：控制台本身仍是一闪即关，用户当场看不到；③ MCP 双击后静默 EOF 退出仍无任何提示。建议：用 `GetConsoleProcessList` 判断「控制台只有自己一个进程」即视为双击启动，退出前暂停等按键。原始记录：① 守护进程是机器级单例，已有实例时把 `daemon.already_running` 写到 stderr 后 `os.Exit(1)`；② CLI 无参数运行，打印用法后退出；③ MCP 是 stdio 服务器，双击时没有输入流、立即 EOF 退出（静默）。用户无从判断是崩溃还是正常退出（实测事件日志与 WER 均无崩溃记录，可确认非崩溃） |
| 29 | 守护进程回连失败时原因被丢弃，只剩无信息的超时 | 已修复 | v0.7.4 修复。握手期间起 goroutine 读 `daemonConn`，并且**为它单独设一个 select 分支**——回连失败时客户端要等的 resp 管道永远不会来，只在超时分支里顺手取一下的话，即使原因已到达也要干等满 5 秒。`client/handshake_test.go` 用假守护进程验证，并做变异验证：修复前 `2.0s` + 「守护进程未在 5 秒内回连 resp 管道」，修复后 `0.3–1.5ms` + 守护进程给出的真实原因。原记录：守护进程回连失败时会 `WriteMessage(daemonConn, ...Error)` 说明原因（`daemon/ipc.go:483/491`），但客户端此时阻塞在等 `respCh`（`client/client.go:139-150`），**从不读 `daemonConn`** |
| 30 | 客户端拉起的守护进程日志被整体丢弃 | 已修复 | v0.7.4 修复：日志双路输出（控制台 + `<exe>/logs/daemon.log`，超 2 MB 启动时轮转为 `.1`），新增 `serial-cli logs [n]` 查看（不连守护进程，起不来时才有用）。两个 Windows 细节都是实测踩出来的：① 必须共享打开——`os.OpenFile` 是独占共享模式，守护进程一运行日志就没人读得到，而「它跑着的时候去看日志」正是唯一用途；② 必须用 `FILE_APPEND_DATA` 而不是 `GENERIC_WRITE`——`CreateFile(OPEN_ALWAYS)` 会停在偏移 0，第二个实例写「已在运行中」时覆盖日志开头，实测留下一截被截断的旧行 |

## v0.7.4 已完成

| 需求 | 说明 |
|------|------|
| 共享环生命周期竞态致守护进程崩溃 | `pm.Close()` 在释放 `pm.mu` 之后才拆共享内存，而 `Process` 指针裸共享，读取方可能碰到已 `UnmapViewOfFile` 的环。实测 `-race` 复现 `DATA RACE` + `0xC0000005`；因 dispatch 无 `recover` 且守护进程是机器级单例，后果是所有会话与串口一起断。环的置空移入保护它的锁内、使用方持锁后重新判空；`historyFile` 改用 `histFileMu`；`sendRing` 同源窗口一并修掉 |
| 回连失败原因被丢弃（只剩超时） | 客户端在握手期间读 `daemonConn`，并为它单独设 select 分支，从而**快速失败**而非干等 5 秒。实测 2.0s → 0.3–1.5ms |
| 客户端拉起的守护进程零日志 | 日志双路输出到 `<exe>/logs/daemon.log`（2 MB 轮转），新增 `serial-cli logs [n]`（无需守护进程在运行）。Windows 下须共享打开（否则运行中读不到）且须 `FILE_APPEND_DATA`（否则覆盖日志开头） |
| 前端三处逻辑从未生效 | 速率告警类名对齐 CSS；`selected` → `is-selected`；`refreshSysMsgI18n` 改为遍历全部标签页而非 `getElementById` 只取第一个 |
| 测试补充 | 新增 `daemon/ringlife_test.go`（并发建销 + 读写环的生命周期竞态）、`daemon/logfile_test.go`（共享读、追加语义、轮转）、`client/handshake_test.go`（首个 client 包测试，覆盖三管道握手失败路径）。关键测试均做变异验证 |

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
