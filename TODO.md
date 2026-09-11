# 待办与已知问题

> 本文档维护当前待实现事项、已知问题，以及近期版本（v0.6.3 ～ v0.7.0）的已完成记录；
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
| 10 | `history.attach` 不校验进程是否已连接 | 待处理 | 它会把整个文件灌入历史环（`loadHistoryIntoRing`），而该路径绕过 `ringBufMu`，与正在收数据的 `readLoop` 并发写同一个环。GUI 未暴露此入口（`AttachHistoryFile` 绑定无人调用），CLI/MCP 亦无，仅 IPC 层可达 |
| 11 | 关闭进程同时刷新历史可能因 unmap 竞态崩溃 | 待处理 | `closeHistory` 不持 `ringBufMu` 就 `UnmapViewOfFile`，而 `GetHistory`/`ClearHistory` 只持该锁；dispatch 路径无 `recover`，一次 panic 即整个守护进程退出 |
| 12 | 历史环写满 5 MB 后静默冻结 | 已知限制 | `Write` 返回 false 时该条不再入环（磁盘文件仍在追加）。表现为长时间高速采集后，重新打开标签页只能看到较早的历史。需点「清空历史」或依赖磁盘分页 |
| 13 | IPC 管道对本机任意进程开放，无鉴权 | 已知限制 | `source` 字段（`gui`/`cli`/`mcp`）由客户端自称且影响行为（自动观察进程、查看计数）。单用户桌面场景下可接受，但这是**设计决定**而非疏漏，多用户/多会话环境需重新评估 |
| 14 | `ports.probe` 不传端口时探测全部端口 | 待处理 | 5 端口 × 7 波特率 × 3 规则，同步阻塞在 dispatch 中，单次可达数分钟。建议要么强制指定端口，要么移入独立 goroutine 并加上限 |
| 15 | 前端三处功能失效 | 待处理 | ①速率告警色永不出现（JS 产出 `.rate-high`/`.rate-warn`，CSS 只定义 `.rate-orange`/`.rate-red`）②下拉框选中态高亮与 `scrollIntoView` 永不生效（`selected` vs `is-selected`）③多标签共用相同 DOM id，`i18n.js` 只刷新第一个标签的系统消息 |
| 16 | 解码逻辑 Go 与 JS 各一份 | 已知限制 | `decode/decode.go` 注释自称 "Mirrors JS _decodeUTF8Tolerant byte-for-byte"；加 `history.js` 的 legacy 回退共三条路径。发送侧 GBK 用「非 ASCII 算 2 字节」估算 |
| 17 | 各包测试覆盖不均 | 待处理 | `daemon`/`pipe`/`ringbuf`/`decode` 有测试；`client`（四端共用的三管道协议）、`protocol`、`contract`、`config` 仍为零 |

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
