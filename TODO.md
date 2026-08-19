# 待办与已知问题

> 本文档维护当前待实现事项、已知问题，以及近期版本（v0.6.3 / v0.6.4）的已完成记录；
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
| 4 | `go vet` 报告 `ringbuf_windows.go` 多处 `unsafe.Pointer` 使用警告 | 待处理 | 共 4 处（L57 / L89 / L92 / L127 / L130 附近），共享内存映射的既有代码；`go build` 正常，需专项审查指针运算安全性 |
| 5 | `.settings-page--overlay` 遗留死代码 | 待处理 | `frontend/style.css` L1079–1090 仍保留居中模态样式，但 JS 已无引用（设置页 v0.6.0 起为独立标签页）；确认后删除 |
| 6 | `frontend/style.css` 头部注释版本号过时 | 待处理 | 文件头注释仍写 `v0.5.7 — CSS`，与当前 v0.6.4 不符，需更新 |
| 7 | 控制字符 CRLF 渲染双路径并存 | 已知限制 | `renderSegments()`（Go 解码新路径）合并为单节点 `data-ws="crlf"`（标记 `←↵`），旧 decoder 路径拆为 cr + lf 两个节点；后续统一路径后可简化 |

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
