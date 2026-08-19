# serial-debugger — 跨平台串口调试工具 v0.6.4

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

基于 **Go + Wails v2** 的跨平台串口调试工具：串口扫描与连接、文本/Hex 双模收发、定时自动发送、端口转发、设备探测、多标签页与历史持久化，一个工具覆盖串口调试全流程。

**架构亮点**

- **3 管道 IPC + 共享内存**：daemon/resp/sub 命名管道承载控制与事件订阅；共享内存环形缓冲区承载数据（历史 5 MB/进程、发送队列 1 MB/进程），发送走共享内存、IPC 仅触发。
- **守护进程 + 多客户端**：GUI / CLI / MCP 统一接入同一守护进程，ProcessManager 管理 idle/connected 状态与 single/forward 模式；心跳 5 s 一次，连续 3 次失败判定断连。
- **解码引擎 Go 化**：Go 端 per-tab goroutine 逐字节容错解码，产出 `[]Segment` 分段结构；前端纯 DOM 渲染（零 `innerHTML`），解码与渲染彻底解耦。

## 组成

| 可执行文件 | 说明 |
| --- | --- |
| `serial-daemon.exe` | 守护进程，单实例（互斥体），ProcessManager 管理 idle/connected 双状态 + single/forward 双模式 |
| `serial-gui.exe` | Wails v2 桌面客户端，多标签页，3 管道持久连接，纯事件驱动 |
| `serial-cli.exe` | 命令行工具，交互 REPL + 一次性命令，34 条命令（含 help） |
| `serial-mcp.exe` | MCP 协议工具，JSON-RPC 2.0 over stdio，28 个工具 |

## 功能特性

### 串口核心

- 进程生命周期管理：idle ↔ connected 状态机，端口双向映射去重，显式销毁，断连保留
- 双模显示/发送（文本/Hex），独立缓冲区，单键切换
- 串口切换：连接中直接切换到其他端口，保留进程和历史
- 端口转发：绑定双串口双向透传，方向以 P1/P2 独立显示和统计
- 设备端口探测：TOML 规则配置，3 种匹配模式（substring / regex / modbus_crc），自动跳过已连接端口
- 流控支持：None / RTS/CTS / XON/XOFF

### 数据路径

- 发送走共享内存（1 MB/进程），IPC 仅触发
- 守护进程内置自动发送引擎：单条模式 + 多条目队列模式，运行中可修改
- 共享内存环形缓冲区（5 MB/进程），客户端可直接映射读取
- 历史持久化双写：环形缓冲区 + 磁盘文件同步追加

### 事件系统

- 订阅式推送：`rx` / `tx` / `1` / `2` / `send-error` / `ports-changed` / `process-changed` / `daemon-shutdown` / `stats-count` / `stats-rate` / `clients-changed` / `multistr-changed`
- 统计拆分：count（变化推送）+ rate（每秒推送），速率 ≥80% 橙色、≥90% 红色
- 订阅后立即推送当前状态

### 主题与外观

- **颜色主题**：文件驱动 + `_modes` 声明（dark/light 双模式），三段式色值（common/dark/light），CSS 全变量化，外部主题自动发现
- **图标主题**：内置 `config/icons/system/`（20 个 SVG），外部 `themes/icons/<名称>/` 文件夹格式，`_fallback` 回退
- **数据高亮自定义**：单端口 10 项 + 端口转发 9 项独立颜色设置，深浅色独立/统一，集成调色板（RGBA 滑块 + EyeDropper + 预设色板）
- **响应式设置页**：800 px 断点，宽屏左右分栏，窄屏导航悬浮覆盖层；搜索框内嵌图标
- **模糊渐变条**：config-bar / send-controls / settings-header / 转发按键栏统一透明 + 渐变模糊

### 前端架构

- 纯组件化标签页，`index.html` 仅保留 4 个结构元素，其余 JS 动态构建
- TabPage 独立 DOM 子树 + show/hide/destroy 生命周期，per-tab 完整状态
- 设置页独立标签页，侧边栏导航 + 气泡卡片布局
- Go 端 per-tab goroutine 解码，产 `[]Segment` 分段结构；JS `renderSegments()` 纯 DOM 渲染，零 `innerHTML`
- 空格和 CR/LF 可视化为标记（`·` / `←` / `↵`），`user-select: none` 保证剪贴板原样拷贝
- CSS 架构分层：`o-` 布局 / `c-` 组件 / `t-` 皮肤 / `is-` 状态

### 国际化

- 9 种语言（zh / en / ja / zh-Hant / ko / ar / ru / fr / es），含阿拉伯语 RTL
- 外部 i18n 文件覆盖内置翻译，`_font` 字段绑定语言首选字体

> **翻译说明**：多语言翻译由 AI 辅助生成，可能存在不准确、遗漏或语境偏差，欢迎提交修正（内置文件见 `config/i18n/`，外部覆盖见上方「配置」表）。

### 连接管理

- 双层断连检测：管道断裂即时检测 + 心跳兜底（5 s ping，连续 3 次失败判定断连）
- 状态栏三色气泡实时显示 GUI/CLI/MCP 连接数
- JS 侧 `_connecting` 互斥防重连风暴，Go 侧锁区合并 + 身份校验

## 快速使用

> ⚠️ **使用须知**
> - **安全**：本工具当前无安全相关设计（无认证、加密、访问控制等），仅限在受信任的本地环境使用，请勿用于安全敏感场景——详见[安全说明](#安全说明)。
> - **翻译**：界面多语言由 AI 辅助翻译，可能存在不准确之处，欢迎提交修正——详见[国际化](#国际化)。

### GUI

1. 启动 `serial-gui.exe`（自动拉起守护进程）。
2. 点击「扫描」选择目标串口（如 `COM3`），设置波特率等参数，点击「打开」。
3. 在发送区输入数据，切换文本 / Hex 模式后点击「发送」；接收区实时回显。

### CLI

```bash
serial-cli start                # 启动守护进程
serial-cli ports                # 列出可用串口
serial-cli open COM3 115200     # 打开串口
serial-cli send "AT\r\n"        # 发送文本
serial-cli status               # 查看守护进程状态
serial-cli shutdown             # 关闭守护进程
```

> 不带参数进入交互 REPL，输入 `help` 查看全部命令。

### MCP

`serial-mcp.exe` 作为 MCP 服务端通过 stdio 提供 JSON-RPC 2.0 接口，可接入支持 MCP 协议的客户端。

## 安装与构建

### 下载

获取对应平台的构建产物压缩包，解压即用。Windows 需系统已安装 WebView2 Runtime。

### 从源码构建

```bash
# 在仓库根目录执行
go build -ldflags="-s -w" -o build/bin/serial-daemon.exe ./daemon/
go build -ldflags="-s -w" -o build/bin/serial-cli.exe    ./cmd/serial-cli/
go build -ldflags="-s -w" -o build/bin/serial-mcp.exe    ./cmd/serial-mcp/
wails build -devtools  # GUI，产物在 build/bin/
```

版本号统一维护在 `version/version.go`，改一处全部可执行文件同步。详细构建矩阵与调试方法见 [构建文档](BUILD.md)。

## CLI 命令（34 条，含 help）

| 类别 | 命令 |
| --- | --- |
| 守护进程 | `start` `check` `status` `shutdown` |
| 端口 | `ports` `refresh` `probe [ports...] [--config]` |
| 进程 | `create` `declare` `open` `connect` `disconnect` `close` `switch` `setmode` `forward` |
| 数据 | `send [--hex]` `stats` `history` |
| 自动发送 | `autosend start/stop/status/interval` `sendqueue` |
| 多字符串 | `multistr save/load/reload/status` |
| 历史 | `history-files` `history-search` `history-enable` `history-status` `history-attach` `history-new` `history-detach` |
| 诊断 | `sessions` `monitor` `threads` `goroutines` |
| 其他 | `help` |

## MCP 工具（28 个）

`serial_start_daemon` `serial_list_ports` `serial_refresh_ports` `serial_create` `serial_open` `serial_connect` `serial_disconnect` `serial_switch` `serial_forward_create` `serial_declare` `serial_set_mode` `serial_close` `serial_send` `serial_sessions` `serial_history` `serial_status` `serial_monitor` `serial_shutdown` `serial_stats` `serial_port_watch` `serial_autosend_start` `serial_autosend_stop` `serial_autosend_status` `serial_sendqueue` `serial_multistr_save` `serial_multistr_load` `serial_multistr_status` `serial_probe_ports`

## 配置

| 位置 | 说明 |
| --- | --- |
| `config/probe.toml` | 设备探测规则（substring / regex / modbus_crc） |
| `config/themes/` | 颜色主题（`system.json` 内置，`_modes` 声明模式） |
| `config/icons/system/` | 内置图标主题（20 个 SVG + `icons.json`） |
| `config/i18n/` | 内置翻译（9 语言） |
| `themes/icons/<名称>/` | 外部图标主题（文件夹 = 主题，`_fallback` 回退） |
| 外部 i18n 文件 | 覆盖内置翻译，`_font` 字段绑定语言首选字体 |

## 文档

| 文档 | 说明 |
| --- | --- |
| [框架文档](ARCHITECTURE.md) | 架构设计、IPC 协议、进程生命周期、时序图 |
| [需求文档](REQUIREMENTS.md) | 需求列表与实现状态 |
| [构建文档](BUILD.md) | 环境要求、构建矩阵、产物说明 |
| [待办与已知问题](TODO.md) | 待实现功能、已知问题、版本完成记录 |
| [布局规则](LAYOUT-RULES.md) | 前端布局与 CSS 约束 |
| [贡献指南](CONTRIBUTING.md) | 提交规范、翻译贡献、行为准则 |
| [第三方许可](THIRD_PARTY_NOTICES.md) | 依赖许可证清单（MIT/BSD/Apache/ISC） |

## 版本历史

### 0.6.4

- 修复 Hex 发送字节丢失：`DecodeEntryContentOnly` 误将 Modbus 首字节 0x01 判为 multistr 条目头，新增 flags/delay 校验防误判
- `send.trigger` IPC 新增 `raw` 参数：直接发送传 `raw:true` 跳过 multistr 解码，快捷面板传 `raw:false` 保持兼容
- 修复自动发送/多字符串逐条目延迟默认 1000 ms：改为跟随 `autoSendIntervalMs`，兜底 100 ms
- 修复 Hex 显示不跟随设置颜色：`.hex-data` 的 `color: var(--fg)` 覆盖方向颜色变量（`--dc-hexRx`/`--dc-hexTx` 等），移除冗余属性
- 修复启动后 TX/RX 显示模式初始状态不一致：`syncDaemonSessions` 新标签页改用全局 `state.displayMode` 初始值，激活时显式刷新按钮
- `findProbeConfig` 新增搜索路径 `<exe>/../config/probe.toml`（build/bin → config 开发场景）
- MCP ServerInfo 版本从硬编码 `0.5.9` 改为 `version.Version` 统一同步

### 0.6.3

- 数据路径重构：环形缓冲区从 Drain 消费模型改为 Snapshot 观察模型，客户端读取历史不清空缓冲区，多客户端独立读取
- 历史清空本地化：GUI 清空历史仅为本地视图操作，不通知 daemon，不清环形缓冲区和磁盘文件
- 上滚召回历史：清空后上滚从 daemon 环形缓冲区快照召回数据，超出范围翻磁盘文件（SearchHistory 分页+去重）
- 历史框左下角浮动按键栏重构：发送工具栏 RX/TX/发送格式切换按键合并到浮动栏，模式切换自动显隐
- 浮动栏按键样式统一：RX/TX 按键使用时间戳颜色（tsRx 绿/tsTx 蓝），P1/P2 保持端口颜色，Hex 模式实心背景
- 转发模式浮动栏精简为 [P1 文] [P2 文] | [锁定滚动]
- 非首个标签页多项修复：`_setSelectVal`/`getSerialConfig`/`openSerialPort`/`openForwardPorts` 改用 `pageEl` 活动页查询
- `_syncPortSelectsFromTab` 支持闲置声明进程写入端口/波特率
- `refreshStatsDOM` 无缓存时主动从 daemon 拉取统计

### 0.6.2

- 高亮颜色系统重构：所有项增加背景色+加粗+斜体设置，双层列标题
- 控制字符渲染改为子元素方案（`.ctrl-mark` 标记 + `.ctrl-real` 真实字符），Tab 用 `::after` 覆盖层不占流空间，换行保留正常高度
- 控制字符符号：空格 `·`、Tab `→`、CR `←`、LF `↵`、CRLF `←↵`，空格纳入可见控制字符开关
- 复制链路重写：`cloneContents()` + 显式 `\n` → `textContent`，标记不入剪贴板
- 发送框 IDE 化：透明 textarea 叠加镜像 div，控制字符实时可视化；Hex 模式下合法/非法字节分色
- 转义格式五选一：`/FF` `\xFF` `0xFF` `<FF>` `[FF]`
- Hex 解析增强：支持 `0x` 前缀、逗号分隔、无分隔连续字符串
- 状态栏重构：守护进程合并为按键；新增光标行列、制表符长度、行尾序列显示；统计颜色同步时间戳设置
- 设置页：编码格式/制表符长度/行尾序列合并为气泡卡片；字体大小统一上调；新增显示字体、行尾序列设置
- 发送按键和格式切换按键新增纸飞机图标；设置齿轮图标更新；新增 `reset`、`send` 内置图标
- 修复 `_refreshThemeNames` 变量遮蔽导致运行时错误；修复多标签输入框镜像不渲染

### 0.6.1

- 连接管理加固：重连互斥、锁区合并、身份校验、同 PID 会话清理
- 离线遮罩范围精确化，断连标签页完整清理
- 数据高亮颜色自定义：单端口 10 项、端口转发 9 项独立设置，深浅独立/统一，集成调色板
- 转发模式颜色键独立（ts1/ts2、text1/text2、hex1/hex2、fstat 等），默认值与单端口解耦
- 颜色设置 UI 重构：功能分组、列表式布局、列标题、色块 tooltip、预览 flex 并排
- 空格可视化：`·` 气泡标记，CR/LF 风格统一，剪贴板原样拷贝
- 模糊效果统一：所有浮动条改为透明 + 渐变模糊（blur 5 px + mask-image，3/4 全模糊）
- 设置页响应式：800 px 断点，宽屏分栏窄屏悬浮覆盖，搜索内嵌放大镜和清空叉
- 图标扩展：新增 `menu.svg`
- 日语/文言文完整重译，外置语言 pt/lzh 恢复
- 菜单栏、颜色主题卡片、关于内容、状态栏、下拉框等多项 UI 修复和优化

### 0.6.0

- 解码引擎迁移至 Go：逐字节容错解码，产分段结构交前端纯 DOM 渲染
- 前端架构重构：精简 HTML，TabPage 独立 DOM 子树 + per-tab 状态，弹窗全部 JS 化
- 设置页重构：独立标签页，左右分栏，新增关于面板
- 标签栏水平滚动，内置图标文件化（17 SVG），版本号统一

### 0.5.9

- 主题系统重构：文件驱动 + 三段式色值 + CSS 全变量化
- 发送框工具栏：三按键分立 + `data-icon` 声明式图标 + 水平滚动
- 多字符串面板图标化 + 循环发送

### 0.5.8

- 多字符串发送引擎：daemon queue 升级为多条目引擎（逐条延迟 + 循环 + 启用控制）
- CLI/MCP 扩展 sendqueue 和 multistr 命令族

### 0.5.7

- 前端 CSS 架构分层（o-/c-/is-/t- 命名前缀）
- 多字符串面板 CSS Subgrid 重构，JS 拆分为 4 模块

### 0.5.6

- i18n 键值补全，9 语言全翻译；波特率下拉框支持自定义数值输入

### 0.5.5

- 历史持久化双写：环形缓冲区 + 磁盘文件；历史搜索与召回；自动保存开关
- 完整 9 语言专业化重译，外部 i18n `_fallback` 回退

### 0.5.4

- 多 GUI 同步广播，流控支持（RTS/CTS、XON/XOFF）
- 设置文件改为 INI 格式，阿拉伯语 RTL 布局，DevTools 集成

### 0.5.2

- 菜单栏 SVG 图标 + 悬浮窗，主题按键 SVG mask-icon，端口转发交换按键

### 0.5.1

- 编码格式全局设置（ASCII/GB2312/UTF-8），容错解码引擎，Hex 显示样式独立配置，消息去重

### 0.5.0

- 客户端列表推送 `clients-changed`，状态栏三色气泡，设置持久化

### 0.4.9

- 统一进程模型：Process 新增 mode 字段（single/forward），进程模式切换

### 0.4.8

- 设备端口探测：TOML 规则配置，3 种匹配模式（substring/regex/modbus_crc）

### 0.4.7

- 端口转发 + 串口切换，GUI 标签页拖拽排序

### 0.4.6

- 自动发送成为守护进程功能，stats 拆分 count/rate，文本/Hex 独立发送缓冲区

### 0.4.5

- stats 事件速率推送，状态栏实时字节统计，循环发送

### 0.4.4

- Hex 发送支持空格分隔，历史缓冲区统一 5 MB，管道断裂即时检测

### 0.4.3

- 3 管道 IPC 架构（请求/响应/事件分离），心跳替代轮询

### 0.4.2

- 单持久管道，订阅式事件推送，空闲进程管理

### 0.4.1

- 进程生命周期重构，双状态机 + 端口去重，共享内存环形缓冲区

### 0.4.0

- 异步发送通道 + 硬件自动探针，会话 I/O 统计

### 0.3.x

- CLI 独立可执行文件，MCP 协议支持，Wails v2 框架，Windows 命名管道 IPC

### 0.2.x

- Go 重写替代 Node.js，WebView 桌面窗口，前后端分离

### 0.1.0

- Node.js 后端，Express + Socket.IO，Go WebView 桌面启动器

## 安全说明

> **本工具当前无安全相关设计**：不包含认证、授权、加密、访问控制或审计日志等安全机制；串口收发内容与历史记录以明文形式处理与存储。仅限在受信任的本地环境使用，请勿在不可信网络或安全敏感场景中部署。

## 贡献与许可

- 本项目基于 [MIT License](LICENSE) 开源，欢迎社区贡献——见 [贡献指南](CONTRIBUTING.md)。
- **AI 辅助声明**：本项目在开发与文档维护过程中使用了 AI Agent 工具辅助（代码生成与审计、文档润色、多语言翻译等）；文档与翻译内容由 AI 辅助产出，可能存在错误或不准确之处，欢迎指正与贡献修正。
