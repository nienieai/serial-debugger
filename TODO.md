# 待办与已知问题

> 本文档维护当前待实现事项、已知问题，以及近期版本（v0.6.3 ～ v0.7.5.1）的已完成记录；
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
| 26 | 标签栏横向滚动无视觉提示 | 待处理 | `.send-scroll-wrap` 有浮动箭头 + 拖拽滚动，`.tabs-scroll` 只有滚轮映射（`app.js:1567` 的 `deltaY → scrollLeft`），无箭头也无其它提示。13 个标签时最后一个被裁在右边缘，用户未必知道可以滚 |
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
| 47 | 发送框里 `0x` 前缀与合法字节同色，`.send-hex-prefix` 无视觉作用 | 待处理 | v0.7.5.1 记录。`style.css` 里 `.send-hex-prefix` 与 `.send-hex-ok` 都用 `var(--dc-hexTx)`，实测同为 `rgb(217,119,6)`，这个类等于没有区分作用。接收区也是同一套处理（`formatHex` 产出整串由外层统一着色），所以算一致而非缺陷——但若本意是要区分前缀，就是漏配了颜色 |
| 48 | 发送框 hex 分词：`0x` 紧贴字节串末尾时整段被判红 | 待处理 | v0.7.5.1 记录。`_renderSendMirrorHex` 的前缀识别条件写成 `i + 2 < text.length`（应为 `i + 1 < text.length`），字符串结尾的两字符序列因此不会被当作前缀；渲染阶段有个「token 恰好等于 `0x` 就当前缀」的兜底把多数情况盖住了，但 `AA0x` 这种紧贴写法仍整段标红——而 `_normalizeHexInput` 会把 `0x` 剥掉、实际能发出去（假红灯）。仅显示问题，不影响发送 |
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
