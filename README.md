# serial-debugger — 跨平台串口调试工具 v0.7.5.10

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

👋 欢迎使用 serial-debugger！如果它帮到了您，欢迎 **Star**；遇到问题请提交 **Issue**，有改进想法欢迎 **Pull Request**，任何反馈都是对我们的支持。

基于 **Go + Wails v2** 的跨平台串口调试工具：串口扫描与连接、文本/Hex 双模收发、定时自动发送、端口转发、设备探测、多标签页与历史持久化，一个工具覆盖串口调试全流程。

**架构亮点**

- **3 管道 IPC + 共享内存**：daemon/resp/sub 命名管道承载控制与事件订阅；共享内存环形缓冲区承载数据（历史 5 MB/进程、发送队列 1 MB/进程），发送走共享内存、IPC 仅触发。
- **守护进程 + 多客户端**：GUI / CLI / MCP 统一接入同一守护进程，ProcessManager 管理 idle/connected 状态与 single/forward 模式；心跳 5 s 一次，连续 3 次失败判定断连。
- **解码引擎 Go 化**：Go 端 per-tab goroutine 逐字节容错解码，产出 `[]Segment` 分段结构；前端纯 DOM 渲染（零 `innerHTML`），解码与渲染彻底解耦。

## 快速使用

> 📦 **下载**：最新构建产物见 [Releases](https://github.com/nienieai/serial-debugger/releases)（GitHub Actions 自动构建，解压即用；Windows 需已安装 WebView2 Runtime）。

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

> 不带参数进入交互 REPL，输入 `help` 查看全部命令；完整命令速查见 [CLI 速查表](CLI-CHEATSHEET.md)。

### MCP

`serial-mcp.exe` 作为 MCP 服务端通过 stdio 提供 JSON-RPC 2.0 接口，可接入支持 MCP 协议的客户端。

## 组成

| 可执行文件 | 说明 |
| --- | --- |
| `serial-daemon.exe` | 守护进程，单实例（互斥体），ProcessManager 管理 idle/connected 双状态 + single/forward 双模式 |
| `serial-gui.exe` | Wails v2 桌面客户端，多标签页，3 管道持久连接，纯事件驱动 |
| `serial-cli.exe` | 命令行工具，交互 REPL + 一次性命令，34 条命令（含 help） |
| `serial-mcp.exe` | MCP 协议工具，JSON-RPC 2.0 over stdio，28 个工具 |

### 架构一览

```mermaid
graph LR
    subgraph 客户端
        GUI[serial-gui 桌面端]
        CLI[serial-cli 命令行]
        MCP[serial-mcp MCP 服务]
    end
    subgraph 守护进程 serial-daemon
        IPC[3 管道 IPC<br/>daemon / resp / sub]
        PM[ProcessManager<br/>进程状态机]
        RB[(共享内存<br/>历史 5MB / 发送队列 1MB)]
    end
    GUI <-->|请求 / 响应 / 事件订阅| IPC
    CLI <-->|3 管道或 CallOnce| IPC
    MCP <-->|3 管道或 CallOnce| IPC
    IPC --> PM
    PM --> RB
```

> 数据走共享内存（发送队列），IPC 承载控制与事件。历史上曾规划「客户端直接映射共享内存读取历史」，但该路径**从未接通**（`ringbuf.OpenSharedRing` 无调用点，且它只映射只读视图、返回对象上的读操作会写 tail，按原设计调用会崩），历史读取实际全部走 IPC 的 `session.history`。详见「已知限制」。

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
- 共享内存环形缓冲区（5 MB/进程），历史读取经 IPC 的 `session.history`（`Snapshot` 语义，读不消费；带 `limit`/`beforeTsMs`/`sameTsSkip` 可分页取，界面默认只取最新一页、往上翻时按 5000 条回补）
- 历史持久化双写：环形缓冲区 + 磁盘文件同步追加
- 环形缓冲区写满时挤掉最旧的包（保留最新的一段），不会停止记录

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

> 💡 **仅需命令行工具时**（无需 GUI），可直接用 `go install` 安装：
>
> ```bash
> go install github.com/nienieai/serial-debugger/cmd/serial-cli@latest
> go install github.com/nienieai/serial-debugger/cmd/serial-mcp@latest
> go install github.com/nienieai/serial-debugger/daemon@latest
> ```
>
> GUI（`serial-gui`）需 Wails 环境（`wails build`），完整构建矩阵见 [BUILD.md](BUILD.md)。

版本号统一维护在 `version/version.go`，改一处全部可执行文件同步。仓库已配置 [GitHub Actions 自动构建](.github/workflows/release.yml)：推送 `v*` 标签或手动触发即可产出各平台二进制并发布 Release。

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
| [CLI 速查表](CLI-CHEATSHEET.md) | 常用命令速查与示例 |

## 版本历史

### 0.7.5.10（2026-09-13）

补上 0.7.5.7 报告 **§8 点名的那条可验证性缺口**：`send.trigger{raw:false}`（多字符串条目剥头）此前**只有 Wails 绑定 `App.TriggerSend` 与裸 IPC 能走**，CLI 没有命令、MCP 只有 `raw:true`，于是「剥头是否覆盖全部 `delay` 取值」这条修复**在验收上不可证**——测试方明确写了「若需覆盖，需要一个可达入口」。

- **新增 `serial-cli sendone [processId] [--raw]`**：从发送队列取一条发出去，与 GUI 快捷面板同语义（`raw=false` 走 `DecodeEntryContentOnly` 剥掉条目头）；`--raw` 用于对照——原样把队列里的字节发出去。未知参数报错（与 `probe`/`history` 同一套规矩），附单元测试 `sendone_args_test.go`。
- **`ARCHITECTURE.md` 本来就写着这个映射**（「发送模式」表里的 `sendone（单条）` → `send.trigger`），但实现里没有这个命令——属文档/实现不一致。本版补齐，并把「这条路径必须有黑盒入口」这句话写进了那张表。
- **验收方法**（也写进了给测试的说明）：队列里放一条 `delay: 1` 的条目（`[{"content":"ABCD","hex":false,"enabled":true,"delay":1}]`），`serial-cli sendone` 发一次，接收端应当只收到 **4 字节 `41 42 43 44`**；修复前会多出 5 字节二进制头（`01 01 01 00 00` + 内容）。`--raw` 可用来对照这条头确实存在。

> 说明：剥头逻辑本身在 0.7.5.7 已修并有单元测试（`TestDecodeEntryContentOnlyStripsHeaderForEveryEncodableDelay` 覆盖 0/1/2/4/5/10/999/1000/60000），本版补的是**黑盒可达的入口**——只有入口可达，那条修复才算验收通过。

### 0.7.5.9（2026-09-13）

0.7.5.8 之后用户人工复测**仍然**出现「新开守护进程 → 一个标签页 → 打开串口 → 又多一个标签页」。这次拿到了**完整时间戳的日志**，定位到真正的成因——**`autoCreateSession` 的自动建会话把自己的 `sessionId` 清掉了**。

**证据**（用户机器 `0.7.5.8\logs\daemon.log`）：

```
00:03:07.463 r3 process.list      ← 同步取会话列表（守护进程里是空的）
00:03:07.491 r4 create idle → #1  ← autoCreateSession 自动建 #1
00:03:12.758 r8 create idle → #2  ← 5.2 秒后点「打开串口」，又建了一个
00:03:12.763 r9 history #1        ← 紧接着读 #1 的历史 = 孤儿进程补出来的标签页
```

**根因**（两处，都在 `syncDaemonSessions` / `addTab`）：

```js
await addTab(false);
if (state.tabs.length > 0) daemonSessions = [];   // ← 元凶
```

这行的本意是「别为刚建出来的进程再建一个标签页」，但**第 3 步正是用这份列表判断「会话还在不在」**——列表一空，刚挂上去的 `sessionId` 立刻被判定为「已消失」而清掉；而 `addTab` 又**从来没写过 `tab.connectedAt`**（0.7.5.8 只在 `openPort`/`openForwardPorts`/孤儿找回三处写了），所以那道 3 秒宽限期在这条路径上不生效。标签页于是变成「看起来空闲」→ 点「打开串口」时 `openPort` **又建一个进程** → 旧进程随即被同步第 1 步补成一个多余标签页。

**修法**：
- 自动建会话之后**重新取一份权威列表**（而不是清空），让第 1/3 步都看到真实状态；
- `addTab` 也写 `connectedAt`，让宽限期在这条路径上生效（与上一条互为备份）。

> **为什么 0.7.5.8 没修到**：0.7.5.8 修的是**陈旧事件载荷**清掉会话（另一条路径，确实存在）。本条是自动建会话**自己**把列表清空导致的。两者症状相同、成因不同——这也是为什么我上一轮在开发机复现时看着「自愈」了：那里守护进程恰好紧跟着广播了一次 `process-changed`，孤儿找回把会话救回来了；用户那次没等到。

**验证**：新增 `_audit/sendhl_test/autocreate_unit_fixture.py`——**用桩替换 Go 绑定**（不碰真实守护进程与串口，因此不会打断任何人正在用的会话），确定性地走「空列表 → 自动建会话 → 打开串口」：8 项全绿，轨迹为 `CreateIdleProcess → GetSessions(核实) → ConnectSession:1:COM3`，只有 1 个进程 / 1 个标签页。变异验证 `autocreate_unit_mutation.py` 跑三种组合：**还原两处（= 0.7.5.8 的行为）** 时夹具完整重建出用户的现象（`sid=None`、`CreateIdleProcess` 两次、守护进程剩下 `["1/idle","2/connected"]`）；只还原其中一处仍全绿——如实标注两处互为备份。

### 0.7.5.8（2026-09-13）

修掉 0.7.5.6/0.7.5.7 之后仍能复现的**同一族症状的第三个成因**：新开守护进程 → GUI 有一个标签页 → 点「打开串口」→ **又多出一个标签页**。

**证据来自测试者/用户机器上留下的守护进程日志**（`0.7.5.7\logs\daemon.log`）：

```
23:14:28.486 GUI 创建空闲进程 #1     ← 新开守护进程后 GUI 自动建的空闲进程（autoCreateSession）
23:14:31.861 GUI 创建空闲进程 #2     ← 点「打开串口」时又建了一个
23:14:31.866 GUI 连接进程 #2 → COM3
```

**根因链**（每一环都有实测）：

1. `syncDaemonSessions` 会收到**陈旧的事件载荷**——它可能发在某个进程创建**之前**（实测出现过 `processes: []` 落在某标签页刚拿到 `sessionId` 之后）。
2. 第 3 步凭**这一份载荷**就断定「会话没了」，把活着的 `sessionId` 清成 `null`（并记 `_orphanSid`）。本该拦住它的 3 秒宽限期是**死代码**：`tab.connectedAt` 只被读、**从来没人写过**。
3. 标签页于是看起来是空闲的 → 点「打开串口」→ `openPort` **再建一个进程**（旧进程成孤儿）。日志里就是 `create idle #2`。
4. 孤儿进程随后被同步第 1 步按「未登记」补成**单独一个标签页** → 用户看到「自动又新建了一个标签页」。

**修法**：

- **下结论之前先核实**：只有当一份载荷会**移除**某个在线标签页的会话时，才先向守护进程 `GetSessions()` 核实一次，用核实结果继续；载荷说「新增」不核实（那是常态）。这样陈旧事件再也不能清掉活着的会话。
- **让宽限期真正生效**：在 `openPort` / `openForwardPorts` / 孤儿找回三处写入 `tab.connectedAt`（此前该字段无人赋值）。
- 0.7.5.6 的孤儿找回逻辑保留为第二道网。

**验证**：夹具 `_audit/sendhl_test/tab_shuffle_fixture.py` 新增 F 节，**注入那份陈旧空载荷**（确定性复现，不靠运气），断言会话不被清、不会多建进程、不会多出标签页——28 项全绿。变异验证把「先核实」这一步还原后，夹具立刻重建出用户的现象：`1 → 2 个进程 ['8/idle','9/connected']`、标签页 `[COM3 @ 115200, 单端口空闲]`——六个变异 **6/6** 全部被捕获（`tab_shuffle_mutation.py`）。

> 这是「多一个标签页」的**第三个**独立成因：0.7.5.6 修的是「设置页被当成会话页」与「建进程不挡同步」，0.7.5.7 之后剩下的这一条是「凭陈旧载荷清掉活着的会话」。

### 0.7.5.7（2026-09-13）

**先记 0.7.5.5 的验收结论**（外部测试报告）：上轮 3 项全部关闭；本版两个 P1 实测通过——历史分页 **8 页 / 799 条、条目级重复 0、1200 帧零缺失**；环写满后仍在记录（灌 7.37 MB 后新数据可读出、`oldestTsMs` 在滑动、`ringDrops` 未出现）；CLI / GUI / MCP 三客户端回归全绿。报告只留了 1 条 P3，就是下面第一条。

- **队列条目的 `delay` 显式写 `0` 被当成「没写」**（报告 §八，P3）。`delay` 字段此前用 `int` 承载，JSON 的零值语义让「省略」与「显式 0」都变成 0，再被下游 `delay < 1 → 1000` 兜底——于是**写 0 等于等 1 秒**：报告实测 20 条花 19.2 s，与 `delay: 1000` 几乎一致，想连发只能写 `1`，与直觉相反。现明确语义并让三处判断一致：

  | 写法 | 含义 |
  |------|------|
  | 省略 / `null` | 默认 **1000 ms**（保持文档承诺） |
  | `0` | **不额外延时**，立即发下一条 |
  | `1`–`60000` | 按该毫秒数 |
  | `>60000` | 夹到 `60000` |
  | 负数 / 非整数 | **报错**（不再静默按默认值走） |

  「省略」与「显式 0」的区分做在 **API 边界**（`serial-cli sendqueue` 用 `*int`、MCP `serial_sendqueue` 在写之前归一化），守护进程侧只看确定值。**实测（10 条一轮）：省略 9102 ms、`delay=0` 111 ms、`delay=1` 106 ms、`delay=10` 213 ms、`delay=1000` 9060 ms**；MCP 侧同样验证（省略 9134 ms / 显式 0 108 ms / 负数报错）。

- **同一字段上还藏着一个更隐蔽的 bug**（本轮新发现，报告未覆盖）：`DecodeEntryContentOnly` 校验 delay 时写的是 **`delay < 5` 就判定「这不是一个条目」**，而编码器允许写下限 **1**。于是 `delay = 1`～`4` 的条目走 `send.trigger(raw=false)` 这条路径时，**整段二进制（版本+标志+delay+note_len+内容）会被当作数据发给串口**，而不是只发内容。该路径由 `App.TriggerSend` 与 IPC `send.trigger{raw:false}` 暴露（当前 GUI 未调用它，MCP 走的是 `raw:true`），所以一直没被踩到。现两处下限统一到同一个常量，并补 `TestDecodeEntryContentOnlyStripsHeaderForEveryEncodableDelay`（覆盖 0/1/2/4/5/10/999/1000/60000）。

- 新增测试：`daemon/multistr_delay_test.go`（编码/剥头/规格化）、`cmd/serial-cli/queue_delay_test.go`（省略 vs 显式 0 vs 负数 vs 上限）、`cmd/serial-mcp/delay_test.go`（MCP 边界归一化，该包的首个测试文件）。

> 版本关系：**0.7.5.6** 修的是 GUI 标签页错位（设置页被当成会话页），**0.7.5.7 = 0.7.5.6 + 本报告这条 P3 + 上面那个隐蔽 bug**，可以直接测 0.7.5.7。

### 0.7.5.6（2026-09-13）

本版修的是**手工测试发现的一组标签页错位**：先开设置页 → 新建会话标签页 → 回到会话标签页开串口，会出现「串口标签页跑到后面、多出一个新建标签页、界面变成设置页」。根因是一类问题：**设置标签页被当成了会话标签页**。

- **设置页会被挂上一个真的串口会话**（最严重的一条）。`openPort` / `openForwardPorts` 用 `getActiveTab()` 取目标标签页，而它会把设置页原样返回；设置页的 `_page` 只是一个 `show`/`hide` 的壳，没有 `portSelect` / `btnOpen`，于是 `pageEl()` 的**全局回退**交出了**别的标签页**的端口选择——串口真的开了，却挂在设置页对象上：没有任何界面能管它，设置页标签还长出了 `sessionId`，标签栏上显示成「COM3 @ 115200」而页面仍是设置页。实测复现：`0:SET sid=3 open=True label=COM3`。现引入 `isSessionTab()` / `activeSessionTab()` / `requireSessionTab()`，所有会碰串口、发送队列、历史缓存的入口先过这一关；停在设置页时给一句可读提示（新键 `serial.need_session_tab`，9 语言）。

- **建进程时不挡同步，于是多出一个标签页**。`addTab()` 一直用 `_creatingTab` 挡住 `syncDaemonSessions`，而 `openPort` / `openForwardPorts` 建空闲进程时**没有**：守护进程的 `process-changed` 事件若抢在 `tab.sessionId = sid` 之前到达，同步就会替**同一个进程**再建一个标签页（新标签页追加在末尾 = 「原来的标签页跑到后面」）。现补上同样的保护。

- **僵尸清理把设置页算成「还有别的标签页」**。清理条件写的是 `state.tabs.length > 1`，于是**只要开着设置页**，单独一个空闲会话标签页就会被销毁；下次同步再把它补回来时位置已经变了。现改为只数会话标签页。

- **会话回来时不「找回」原标签页**。同步第 3 步在守护进程列表一时不含该会话时会清掉 `sessionId`；等它回到列表里，第 1 步按「未登记」给它新建一个标签页——同一个会话于是有两个标签页，原标签页的滚动位置与历史缓存一并丢失。现记下 `_orphanSid`，会话回来时**重新挂回原来那个标签页**。

- **切到设置页会改到别的标签页的按钮**。`updateOpenBtn()` 经 `pageEl` 的全局回退拿到了**第一个标签页**的「打开串口」按钮并把它隐藏。现只动当前页自己的元素。

回归夹具 `_audit/sendhl_test/tab_shuffle_fixture.py`（24 项，含用户路径的真实点击复刻）＋ `tab_shuffle_mutation.py` 把五处修复逐个还原，验证夹具确实会报红（5/5 全部捕获）。

顺带修了夹具自身的一处不稳定：历史窗口夹具的单条渲染成本阈值在负载下会抖（同一档实测 230~440 µs），改为每档取 3 次里**最快**的一次——真正的 O(n) 退化会把最快的一次也抬起来——并把三次样本全部打印，便于自行判断。

### 0.7.5.5（2026-09-13）

本版把历史的**读取路径**从「一次取整环」改成「按页取」，并顺手修掉一个藏了很久的数据丢失：**历史环写满 5 MB 之后就不再记录任何新数据**。

**先说环为什么必须能挤出旧的**（TODO #12，此前记为「已知限制」）。`ringbuf.Write` 在空间不足时返回 `false`，而 `recordHistory` 一直**忽略这个返回值**——于是写满那一刻起，新数据既不进环也不被任何人发现（磁盘文件仍在追加，界面上完全看不出来）。按 115200 饱和（11.5 KB/s）算，5 MB 约 **7.6 分钟**写满；此后 `oldestTs` 永远停在那一刻，「往回翻」永远翻到同一段。现改用 `WriteEvict`：空间不足时挤掉最旧的包（真正的环形语义 = 保留**最新**的一段），只有单包超过整环容量时才计一次 `ringDrops`（随 `session.history` 返回）。

- **历史读取改分页**。整环 5 MB ≈ **180788 条**（实测），一次读取要「逐包复制 + 逐条建 `HistoryEntry` + JSON 序列化 15.86 MB + 过桥 + 前端逐条 decode」，而界面一次只看得见一屏。`session.history` 新增 `limit`（页大小）、`beforeTsMs` + `sameTsSkip`（游标）；守护进程侧新增 `ringbuf.SnapshotPage`，全程只记位置（4 字节/包），最后才复制选中的那一页。**实测：整环 79.4 ms / JSON 15.86 MB → 一页 5000 条 5.0 ms / 0.44 MB（时间 16×、载荷 36×）；首次加载 10000 条 9.8 ms / 0.88 MB。**`limit<=0` 仍是整取，CLI/MCP 的老行为不变。基准 `BenchmarkHistoryRead` 随代码留存。

- **游标为什么要带 `sameTsSkip`**。毫秒时间戳会撞车（同一毫秒到达多帧是常态）。只用 `ts < beforeTs` 当游标，边界那一毫秒剩下的帧会被**永久丢掉**；只要 `<` 之外的任何近似判断又会让同一批帧被反复返回。所以调用方额外报出「我手上有几条属于这一毫秒」，守护进程据此正好补齐缺的那几条。为了让两边对得上，**广播事件与写进环的必须是同一个毫秒值**——`RxTxMessage.TsMs` 现在由写环的那一处产生（此前两边各取一次 `time.Now()`，会在毫秒边界错开）。属性测试 `TestHistoryPagingReconstructsFullRing`（500 条、每 7 条同一毫秒、页大小 33，与 7 互质 → 每几页必然从某一毫秒组中间切开）逐条比对「一页页翻回来 == 整环」。

- **前端缓存有界**（TODO #58）。`state.historyCache[tab]` 此前只 push 从不裁，等于把整环（以及环里已被挤掉的更早数据）永远留在 JS 里。现改为：跟随时保留 10000 条，往回翻时放宽到 40000 条（否则**刚回补进来的一页会被立刻裁掉、游标原地不动**，往上翻就再也翻不动了），永远从**最旧**的一端裁，于是「最新的一屏」始终在手上（回到底部不需要任何 RPC）；裁到窗口里时同步移除对应的 DOM 行。**实测持续灌 30000 条后缓存停在 10032 条、尾条仍是最新那条。**

- **「往上翻加载更早」此前仍然不可达**（TODO #59，v0.7.5.3 只修了一半）。上一版把 `expandHistory` 内部的 `return` 挪走了，但**滚动触发器**还是 `if (scrollTop < 80 && _renderStart > 0)`——窗口正好停在缓存头时 `_renderStart == 0`，滚到顶因此一次 RPC 也不发。现改为只要滚到顶就调用，另加两道防抖（`_ringExhausted`、游标必须推进），避免滚到顶变成 RPC 风暴。

- **`设置 → 高级 → 自动回补更早历史`**（开启 / 关闭，9 语言）。关闭后滚到顶只摆一个可点的「↑ 向上滚动加载更多」，由用户决定何时取；大历史或远程场景下把请求时机交回给人。**实测关闭时滚到顶 0 次 RPC，点提示才取。**

- **CLI / MCP 也能分页**。`serial-cli history [pid] [--limit n] [--before ms] [--skip n]`（未知参数报错，与 `probe` 同一套规矩，附单元测试）；MCP `serial_history` 增加 `limit` / `beforeTsMs` / `sameTsSkip` 三个参数并更新描述与 schema。

- **契约测试漏收自己建的共享内存**（TODO #60）。`TestEveryContractMethodIsDispatched` 用裸参数调 `process.create`，真的建出一个进程并映射 5 MB 共享内存，而它的 `ProcessManager` 没人 `DestroyAll`——同一个测试二进制里**后面任何再建进程的测试**都会撞上 `共享内存 "serial-tool-history-xxxx-1" 已存在`。写这批分页测试时正是被它绊住，现补上回收。

- **已知取舍**：单次回看的最大深度约 40000 条（超出会从最旧一端裁掉，见 TODO #62）；磁盘历史的深分页仍是「从头顺序扫 + 递增 offset」（TODO #61），要做得先给历史文件建条目偏移索引。

### 0.7.5.4（2026-09-13）

小版本，修掉 0.7.5.3 轮外部报告的三处发现——**都属文档/提示级，不影响功能**，但都是我自己的疏漏。

- **`config/probe.toml` 的注释没跟着改**（报告 §4.1）。0.7.5.3 把每次尝试的成本从 1 s 降到 600 ms、总预算 12 s 提到 15 s，但随包配置顶部的注释还写着旧的 `max(3×timeout_ms, 1s) ≈ 3s` 与「默认 12s」——读者照注释估算会得出「每档 3 秒、7 档 21 秒」的旧结论，与实测（1.8 s / 12.6 s）不符。现按实际公式重写，并把 `timeout_ms` 说明成「唯一且诚实的旋钮」。另在 `daemon/probe.go` 的预算常量旁加了提醒：改预算数字必须同步这份注释（功能有 `TestDefaultBudgetCoversShippedBaudList` 兜底，注释漂移只能靠提醒）。

- **未知长选项被当成端口名**（报告 §4.2）。`serial-cli probe --json` 不报错，而是把 `--json` 解释成一个端口，结果里多出一条「端口 `--json` 打不开」的 `skipped`——读者会真的以为有个端口有问题。现抽出 `parseProbeArgs`：未知的 `-` 前缀参数直接报 `未知参数 "--json"（probe 支持：端口名…、--config <路径>、--budget <毫秒>）`；`--config` / `--budget` 缺值或值非法也分别报清楚。校验提前到**建立连接之前**——参数写错不该等到连上守护进程才报出来，没有守护进程时更是根本报不出来。附 3 条单元测试（拒绝未知 flag、拒绝缺值、正常用法照旧）。

- **MCP 的 `budgetMs` schema 默认值滞后**（报告 §6.2.4）。工具描述里写 `default 12000`，实际已是 15000。已同步。

### 0.7.5.3（2026-09-13）

本版把**历史显示窗口**的性能机制放回正确的位置，并顺手修掉三处与它纠缠在一起的正确性问题；另外按外部报告建议让 `probe` 的默认波特率不再被静默跳过。

**先说窗口为什么不能拆**（实测，8 字节帧）。DOM 无上限时单帧成本随节点数线性增长——2000 节点 139.7 µs、20000 节点 4005.3 µs，整体 O(n²)；有窗口时恒定在 ~1000 µs。成本主项是「改完 DOM 立刻读 `scrollHeight`」：读一次 150 节点 44.3 µs、20000 节点 4313 µs，而写 `scrollTop` 只要 2 µs。**所以窗口只界住了「DOM 有多少」，没界住「每帧做多少事」**，本版补的就是后半句。

- **入站帧改按批渲染**。此前每条数据都独立建 DOM、独立读一次 `scrollHeight`（而且是两次）、独立滚一次；现改为先进队列、由 rAF 合并成一批：一批一次 fragment 插入、一次裁剪、一次贴底，另有 200 ms 定时兜底（窗口最小化时 rAF 不触发）。贴底改为写 `scrollTop = 极大值`（浏览器夹到底部），不再读 `scrollHeight`。**实测吞吐 893 → 242131 帧/秒（同一 DOM 上限）；持续流入的单条成本 ~1000 µs → 240 µs。**

- **裁剪恒定执行，锁定的语义收窄为「不贴底」**。此前 `appendLine` 里 `if (!state.scrollLocked)` 把贴底和裁剪绑在一起，于是**锁定滚动就完全不裁剪**——实测锁定后喂 5000 条，DOM 涨到 5000 行。「锁定滚动、往回翻历史」正是最需要省资源的用法，却是唯一没有上界的那条路。现在裁剪恒做，冻结与否由 `_frozen` / `_atBottom` 决定。

- **往回翻时视图冻结，但滚动条继续变化**。视口下方用一个「↓ N 条未显示」的占位块撑开高度（按已显示行的实测平均高估算），于是三者同时成立：视图不动、滚动条继续变、DOM 仍有界。实测往回翻时 `scrollTop` 变化 **0 px**（改前被新数据拽走 200 px），内容高度 **+123002 px**，DOM 稳定在窗口上限内。

- **去掉「丢弃式」重绘节流**。`renderHistoryLines` 开头原本是 `if (now - _lastRender < 50) return;`——把 50 ms 内的重绘请求**直接丢掉**。实测两个后果：渲染之后立刻点「清空」→ **显示区空白**（`innerHTML` 已被清掉，重绘却被丢弃，也没有别的东西再触发）；连续切换显示模式 → 第 2 次起**画面与 `state` 不一致**。合并语义已由批量队列承担，这里改为每次都同步生效。

- **「加载更早」整条路此前不可达**。`expandHistory` 第一步算 `newStart` 后 `if (newStart >= _renderStart) return;`，于是 `_renderStart == 0` 时立刻返回——而取更早数据的两条路（守护进程分页、磁盘历史提示）**都在这个 return 之后**。实测：缓存只有 30 条且有磁盘历史文件时，调用 `expandHistory` 触发 **0 次** RPC、滚到顶 **0 次**、提示为空。现把「扩窗」与「取更早」拆开，到缓存头就直接去取。

- **窗口行数按视口推导**，`设置 → 高级 → 历史窗口行数` 可覆盖（自动 / 150 / 300 / 400 / 600）。`calcRenderCount()` 早就写好却**从未被调用**（写死 `HISTORY_WINDOW = 100`，且按固定字号估算）。

- **补掉控制台里的 `Cannot set properties of null (setting 'innerHTML')`**。`loadTabHistory` / `clearDisplay` 在 `getActivePage()` 为空时直接对 null 写 `innerHTML`，正是测试中反复出现的那条未捕获异常。

- **`probe`：默认波特率不再被静默跳过**。`probeRead` 的预算实际只决定「等第一个字节最多等多久」——循环里 `len(resp) > 0` 排在 `After(deadline)` 之前，一旦收到字节，下一个空读就结束，与预算大小无关；**沉默端口独自承担这个成本**。此前预算写 `max(3×timeout_ms, 1s)`，那个 1 s 下限在默认 `timeout_ms=200` 下把每次尝试从 600 ms 抬到 1000 ms：每档 3 s、7 档 21 s，超过 12 s 总预算，于是默认列表里 `230400/460800/921600` **永远轮不到**（0.7.5.2 轮报告把它记为「噪音」）。现改为 `max(3×timeout_ms, 300ms)`（300 ms 只防呆），总预算 12 s → 15 s。**实测沉默端口单次尝试 1106 → 753 ms；默认配置探测 13.4 s 覆盖全部 7 档、`skipped` 为空。** 新增 `TestDefaultBudgetCoversShippedBaudList` 把预算与随包 `probe.toml` 绑定，谁加了波特率或规则而没调预算就会报红。

- **CLI 补上 `busy` 字段**（报告 P1）。`操作说明.md` 承诺了 `busy` 并建议 `grep busy` 自检，而 CLI 序列化时把它丢掉了（守护进程返回了）。抽出 `probeOutcomeJSON` 并加测试锁住字段集合。

### 0.7.5.2（2026-09-13）

本版修掉外部测试报告（0.7.5.1 轮）点名的那条 P1：**`probe` 的「空结果」与「没有设备」不可区分**，会把在线设备报成「未检测到已知设备」。报告已把根因定位到「被占用端口被静默跳过」；本地复现后确认**根因有两条，报告只点到了第一条**。

- **两条静默路径，返回的空结果完全同形**。① `ProbePorts` 里 `if occupiedPorts[portName] { continue }`——端口被**会话**占用；② `if err != nil { continue }`——`ProbePorts` 内部的 **open/read 失败被裸 `continue` 吞掉，连一行日志都没有**。② 才是「客户端超时后假阴性」的真正机制：实测同一时刻 `sessions` 返回 `{"processes": []}`（**没有任何进程持有端口**，走不到①），而跟随探测仍在 **65 ms** 内返回空结果；那次探测结束后立刻恢复为 1093～1245 ms。后台那次探测实际跑了 **33.2 s**（CLI 10 s 就放弃，守护进程一直扫完）。现两条路径都变成显式的 `skipped: [{port, reason}]` 并补上日志。

- **总预算，消灭「客户端先放弃」**。设备不应答时每次尝试都要跑满 `probeRead` 的预算下限（默认 `timeout_ms=200` → 1 s），默认配置 3 规则 × 7 波特率 = 21 次尝试，必然超过 20 s；而 IPC 请求超时是硬编码 10 s，于是客户端先走、守护进程还在扫，调用方拿到 `request timeout: ports.probe` 而不是结论。现整次调用有**总预算（默认 12 s，可用 `--budget <ms>` / MCP 的 `budgetMs` 覆盖）**，到点立即返回已有结果并列出**哪些端口/波特率没试**；同时 `ports.probe` 的请求超时放宽到 25 s，确保客户端不再先于守护进程放弃。

- **波特率改按可能性排序**。设备不应答时每档约 3 s，12 s 预算只够前 4 档——而原来的升序把**最常用的 115200 排在第 4 位**，实测预算用尽时 `115200` 根本没试过（报告里的编码器正好是 115200）。现改为 `[115200, 9600, 19200, 38400, 230400, 460800, 921600]`。

- **并发探测直接说明，不再伪装成「没有设备」**。同一时刻只允许一次探测，进行中再发起会立刻返回 `busy: true` 与「已有另一次设备探测正在进行，本次未执行」。旧版本正是靠空结果表达这件事，因而被读成「设备不在」。

- **读取错误不再丢弃**。`probeRead` 现在把读取错误带出来：串口打开成功但根本读不了（USB 拔出、句柄被抢）此前同样报「未检测到已知设备」。

- **CLI 与 MCP 都说明白**。CLI 输出带 `skipped / attempts / elapsedMs / budgetExhausted`，未探完时在 stderr 追加「不能据此判断『没有设备』」；MCP 工具描述明确「空 `results` 不等于没有设备」，并新增 `budgetMs` 参数。`操作说明.md` §九 增加「怎么看探测结果」一节（三种「没检测到」的区别）。

### 0.7.5.1（2026-09-13）

本版修掉发送框（数据输入区）的几处问题：**文本模式的高亮与光标逐格错位**、**Hex 输入的预处理在四条路径上分叉**，以及最要命的一条——**高亮全绿却发不出去，而且毫无提示**。

- **文本模式高亮层与文本框逐格错开**。发送框的文字是透明的，用户看到的是叠在下面的「镜像层」，而光标与选区由 textarea 按**真实文本**排版后绘制。镜像层此前把空格换成 `·` 标记、并把真实空格设为 `font-size: 0`，于是这一格的推进宽度变成了 `·` 的宽度——只要字体里 `·` 与空格的宽度不同，两层就会逐格错开，且误差随空格数累加。实测软件设置里 10 种可选字体有 **8 种**中招：Cascadia Code / Fira Code / JetBrains Mono / Source Code Pro **每个空格错 8px（整整一格）**，宋体 +7px、楷体 +7.5px、微软雅黑 −0.83px；表现为光标不在你看到的字上、点选位置偏移，选中文本时框里还会叠出两层字。现改为**真实字符一律保留原宽度**，`·`／`←`／`↵` 标记改为绝对定位叠加、一点宽度都不占。修复后同样字体下漂移降到 **0～0.64px**（亚像素取整量级）。改动只作用于发送框镜像层，接收区显示不受影响。

- **「1 位十六进制数字」判定自相矛盾，且发送失败无任何反馈**。同一个 1 位数字，落单时被判为合法、而在长串按 2 字符切分后落单时被判为非法；而后端 `hex.DecodeString` 对奇数长度一律拒绝。于是 `A`、`AA B` 显示**全绿**却发不出去，同时 `sendData` 的 `catch {}` 把错误整个吞掉——**点了发送什么也没发生，也没有任何解释**。本版保留原有的宽容高亮（便于边打字边看），新增**发送框悬浮提示**：悬停发送框、或点发送／自动发送被拦下时，在发送按钮上方浮出具体原因（如「十六进制位数必须是偶数（当前 3 位）」「含非十六进制字符「G」，只能是 0-9 与 A-F」），5 秒后自动消失，不拦截鼠标操作，文案按 9 种语言本地化。

- **自动发送与手动发送走了两条不同的预处理**。手动发送与回写环形缓冲都会剥掉 `0x` 前缀与逗号，自动发送却只剥空白；而高亮层明确把 `0x` 当合法前缀、把 `,` 当分隔符——界面在鼓励这种写法。于是出现「高亮全绿 + 手动能发 + 自动发送报 `invalid hex`」。实测 `0xAA 0xBB`、`AA,BB`、`0xAA` 自动发送全部失败，现已与手动发送一致。

- **字节数与预计耗时算错**。字节统计同样只剥空白：`0xAA 0xBB` 显示 4B（实际 2B）、`0xAA` 显示 2B（实际 1B）、`AA,BB,CC` 显示 4B（实际 3B），预计耗时随之翻倍。

上面两条的根因是同一套规则散落在四处。现抽出 `_normalizeHexInput` 作为唯一实现，发送／自动发送／回写环形缓冲／字节统计共四条路径共用，并在回归夹具里加了硬断言：该函数之外不得再残留任何 `0x`／空白／逗号的剥离写法。

- **发送框占位符写死中文**。`send.placeholder` 在 9 个语言文件里都有、代码也把它写进了 textarea，但 `#sendInput::placeholder` 被设为透明，用户看到的其实是镜像层里硬编码的中文。现镜像层改走 i18n，并在切换语言时重绘。

- **顺带**：自动发送失败弹窗少了分隔符——9 种语言的 `serial.start_fail` 都没有尾随冒号，实测显示成 `Start failedinvalid hex: ...`。

### 0.7.5（2026-09-13）

本版修掉外部测试报告连续四轮记录的**设备探测失败**，以及同样被连续几轮看到的**端口描述退化**；另处理两处挂起的小问题。

- **`probe` 读不完多字节响应，导致设备识别失败（报告四轮未愈）**。报告连续四轮记录「`probe` 检测不到地址 4 的设备」，并实测出唯一可行的规避方案：把 `match_value` 缩到 1 字节量级配 `substring`。插桩定位到根因——Windows 下 `go.bug.st/serial` 的 `Read` **一有字节就返回**（其实现为 `if readed > 0 { return }`），而 `ProbePorts` 对每条规则**只 `Read` 一次**。实测 9 字节 Modbus 响应只读到第 1 个字节（`发=040300080001059D 收 n=1 bytes="04"`），于是 `min_response_len` 与匹配双双落空。这也解释了报告的另一条观察：`modbus_crc` 类规则对多字节响应**一律失效**（校验 CRC 需要完整帧）。现抽出 `probeRead()` 在超时预算内循环累积；终止条件是显式的（不依赖 `SetReadTimeout` 在库内的行为——该库为 CH340 打过补丁，而本工具的目标设备大量使用 CH340/CH343）：未收到数据则继续等，已收到数据后本轮读空即认为帧结束，总预算 `max(3×timeout, 1s)`。实测同一场景 **0/5 → 5/5 命中**，无应答端口耗时仍为 0.05 s。

- **端口描述偶发退化成端口名**。报告几轮里观察到 `refresh` 的 `description` 偶尔变成端口名本身。根因有两条：① `loadPortDescriptions()` 靠**每次起一个 PowerShell 子进程查 WMI**（超时 10 s），机器繁忙时子进程起不来或查询超时，返回空 map；② `buildPortList` 对空描述的回退是 `desc = p`（端口名），于是所有端口一起退化。现改为**直接读注册表**（无子进程、无 WMI）：从 `Enum\<类>\<实例容器>\<实例>\FriendlyName` 取值（如 `"USB-SERIAL CH343 (COM3)"`），只遍历可能含 COM 口的类，不扫整棵树。WMI 路径保留为兜底。实测描述全部正确，`refresh` 耗时 **1–2 s → 0.08 s**。

- **`daemon not running` 断言彻底去掉（TODO #27）**。0.7.0 只清掉了 CLI 那一层，`client.CallOnce` 与 `pipe.dialPipe` 仍在用 `fmt.Errorf("daemon not running: %v", err)` 包装。失败原因可能是端点残留、权限、或对端刚退出，硬套「守护进程未运行」会把排查方向带偏。

- **连接阶段的失败不再重试、也不再谎称握手失败**。去掉上面那层包装后暴露出：守护进程未运行时报的是「注册握手失败（已尝试 3 次）: connect to daemon: pipe not available」——连都连不上，既不该重试也不该说成握手失败。现按阶段区分：连不上守护进程 / 回调管道建不出来 / register 发不出去属确定性失败，立即返回；只有真正走到握手阶段的失败才重试。

- **`Tx`/`Rx` 大小写统一（挂起项）**。统一到 `Tx`/`Rx`，与前端 `history.js` 硬编码的 `'Rx'`/`'Tx'` 及 9 种语言里 8 种的原状一致。此前 `stats.*` / `tooltip.*` / `settings.color_*` 用大写 `RX`/`TX`，而 `stat.rx` / `display.*` 用 `Rx`/`Tx`，同一界面混用两种写法。改动 `en.json` 14 处、`es.json` 8 处、`fr.json` 8 处。

- **许可证保持 MIT**。评估过改为 Apache-2.0：依赖全部是宽松许可证（MIT 20 / BSD-3 6 / BSD-2 4 / Apache 1 / ISC 1，无 GPL 系），技术上完全可行。但 Apache-2.0 相对 MIT 的核心增量是**明示专利授权**，对串口调试工具没有实际意义；而它引入的「必须标注修改过的文件」等义务对嵌入式受众反而是摩擦。结论是保持 MIT，将来若有企业用户提出要求再改（本项目为单一版权人，随时可改）。

### 0.7.4.3（2026-09-13）

本版修掉三轮外部测试报告持续追查的那条**严重**问题——**注册握手间歇失败**，并且查明它是 **0.7.4 引入的回归**。

- **根因：注册确认抢在回连管道之前到达，却被判成握手失败。** 守护进程在**两条回连管道都成功之后**才写注册确认（`daemon/ipc.go`，位于 `addSession` 之后），因此这条确认可能先于客户端 `Accept` 协程交出 `respConn`/`subConn` 到达——机器越慢、负载越重，窗口越大。而 0.7.4 为透传「回连失败原因」引入的监听逻辑用 `select` 同时等 `respCh` 与 `earlyCh`，**谁先到就按谁处理**，于是先到的**成功确认被判成了握手失败**。插桩实测：客户端收到 `{"result":{"registered":true}}` 却返回「注册未完成: 」——原因为空，因为 `reasonFromMsg` 对成功确认返回 `("", false)`，调用方却仍按失败处理。

  这一个缺陷解释了此前所有反常观测：① 失败总在 2 ms 内、且三次重试**一起**失败——不是随机故障，而是同一个竞态被连续触发，所以**重试完全无效**（外部报告测到 3 次重试只把 7.8% 降到 6.3%，若三次独立应为 0.08% 量级）；② 守护进程日志显示会话已完成注册、无回连失败、未被驱逐、无写确认失败——**因为它真的成功了**，是客户端自己判错；③ 失败率随机器性能下降而升高——负载越重，`Accept` 调度越晚，确认越容易抢先；④ 所谓「错误掩盖业务错误」8–10% 是同一缺陷的另一面；⑤ 所谓「日志可观测性失效（三计数相等）」其实是**正确**的——失败会话确实完成了注册。

- **修法**：用**统一的截止时间**等待两条管道，并且只在 `msg.Error` 非空时中断握手——成功确认只表示守护进程已登记，`resp`/`sub` 还在路上，必须继续等它们。改为循环而非两段 `select` 还有个附带好处：两条管道共享同一个 `handshakeTimeout`，不会在 `respCh` 分支里另开新的超时窗口（否则最坏情况要等 2×，重试代价也翻倍）。

- **验证**：新增 `client/handshake_order_test.go`——假守护进程刻意先送确认、延迟 300 ms 才连两条管道，断言客户端必须成功。**变异验证**：还原旧的早退逻辑后，该测试报出的错误与外部测试机观测到的签名**逐字一致**。实机（满 CPU 负载 + 三种并发度，共 **1180 次调用**）**0 失败**；`open COM99` × 60 次**0 次**被握手错误掩盖。

- 顺带修掉 `connectOnce` 的 sub 管道失败/超时分支**漏关 `respLn`**（加了重试后每次失败都会泄漏一个 listener 及其预建管道实例）。握手重试保留为安全网，但实测修复后失败尝试为 **0 次**——连重试都没触发。

### 0.7.4.2（2026-09-13）

补丁版，处理 v0.7.4.1 测试报告里仍未解决的那条**严重**问题。

- **注册握手失败现在会自动重试**。这份报告最重的一条是「并发下注册握手间歇失败」，本版先把它复现到可处置的程度：

  | 条件 | 次数 | 失败 |
  |------|------|------|
  | 串行 | 300 | 1（0.3%） |
  | **8 路并行 × 60 轮** | 480 | **42（8.8%）** |

  即它**只在并发建立会话时出现**，与报告测到的 9.3% 吻合。但报告推断的根因（客户端监听器未就绪）不成立——实测反驳有 5 条：① 守护进程日志显示该会话**已完成注册**（`已注册` 那行位于 `addSession` 内，而 `addSession` 在两条回连管道都成功之后），回连压根没失败；② 没有一条「连接客户端 resp/sub 管道失败」；③ 失败会话**未被驱逐**；④ 守护进程**没有写确认失败**（本版新增该日志后实测 0 次）；⑤ 报告说「失败指纹失效（三者计数相等）」实为统计方法问题——失败会话同样会打「已注册」，真正的指纹是「已注册但从未开始通信」，现已做成常驻错误日志。

  现场是矛盾的：守护进程侧写成功，客户端侧读 `daemonConn` 失败。指向 `pipe` 层并发下的句柄/缓冲竞态，**根因未定位**。因此本版采取重试：每次重试换**全新 clientId**（因而是全新管道名，不会踩上次残留），全部失败时错误里带尝试次数与原因，失败尝试仍在守护进程侧留日志。实测 **8.8% → 0.21%**（1/480），守护进程侧 506 次注册对 480 次成功会话，27 次失败尝试可见。这是对已知缺陷的缓解，不是修复——问题没有被藏起来。

- **顺带修掉两处由此暴露的缺陷**。① `connectOnce` 的 sub 管道失败/超时分支**漏关 `respLn`**：加了重试后，每次失败都会泄漏一个 listener 及其预建管道实例。② 握手失败的表达从「关闭 `earlyCh` 通道」改为「发送 `earlyResult{msg, err}`」——关闭通道会让 `select` 立刻返回，与错误信息的记录相互竞争，这正是定位过程中一度看不到真实错误的原因。

- **修正文档中一处过度承诺**。`操作说明.md` 原写「提示 IPC 协议版本不匹配：新旧程序混用（换过版本）」——这暗示**任何**版本混用都会被拦。实际机制是比对 `protocolVersion` 这一个整数：它只在**破坏性变更**时才改号，所以同为协议 1 的不同版本（如 0.7.4 与 0.7.4.2）是**设计上兼容、可以混用**的，不会也不应该报错。文档已改为准确表述，并提示混用极旧版本可能缺少新事件/新字段。

### 0.7.4.1（2026-09-12）

**0.7.4 的补丁版。** 内容与 0.7.4 完全一致（下一条），单独发版的原因是 0.7.4 的归档已在外部测试中流转，而这批修复包含对 `sendqueue` 输入校验与自动发送状态的改动——值得用一个可区分的版本号重新交付，避免「同名不同物」的混淆。

本版同时在 `操作说明.md` 中补上了 **`intervalMs` 与条目延迟 `delay` 的分工说明**（见下条最后一段），这正是外部测试报告把 queue 模式的 1000 ms 条目延迟误判成「`intervalMs` 不生效」的根源。

### 0.7.4（2026-09-12）

本版是**稳定性与可诊断性**版本：修掉一个会让整个守护进程静默退出的内存生命周期竞态、一个让「连不上」永远查不出原因的诊断黑洞，以及三处写了代码却从未生效的前端逻辑。

- **守护进程崩溃竞态（严重）**。`pm.Close()` 在释放 `pm.mu` 之后才拆共享内存，而 `Process` 指针是裸共享的（`Get` 拿到指针后就放锁）。于是读取方可能在环已被 `UnmapViewOfFile` 之后才去碰它——`GetHistory`/`ClearHistory` 会把快照/清零打在已撤销的映射上，`recordHistory` 会写已关闭的 fd。实测（`go test -race`）复现为 `WARNING: DATA RACE` 紧接 `unexpected fault address ... [signal 0xc0000005]`。危害之所以是大级别：dispatch 路径上**没有任何 `recover`**，而守护进程是机器级单例——它一崩，所有 GUI / CLI / MCP 会话与串口一起断。修法是让环的置空发生在保护它的锁内，使用方持锁后**重新判空**；发送环（`sendRing`）有同样的窗口，一并修掉（那边表现为对 nil 调方法而 panic）。`daemon/ringlife_test.go` 是并发回归测试，已做变异验证：还原修复即复现上述崩溃。
- **连不上时不再只有一句无信息量的超时**。守护进程回连客户端管道失败时，会把原因写在注册管道上（`daemon/ipc.go` 的三处错误分支），但客户端在等 `respCh` 的 5 秒里**从不读那条管道**，原因被丢弃。现在客户端在握手期间同时监听它，并且**单独为它设一个 select 分支**——因为回连失败时那条 resp 管道永远不会来，只在超时分支里顺手取一下的话，即使原因 0.5 ms 就到了，用户仍要干等满 5 秒。实测：修复前 2.0 s（测试用超时）+「守护进程未在 5 秒内回连 resp 管道」，修复后 0.3–1.5 ms + 守护进程给出的真实原因。
- **守护进程日志落盘（此前无人值守启动等于零日志）**。由客户端拉起的守护进程是 `--silent` 且 `Stdout`/`Stderr` 未重定向（`os/exec` 约定接到空设备），而 `logOp` 只做 `fmt.Printf`——于是守护进程侧的任何错误都没有任何地方能看到，这正是上一个问题难以定位的原因。现在日志双路输出到 `<exe>/logs/daemon.log`，新增 `serial-cli logs [n]` 可随时查看（**不需要守护进程在运行**，因为守护进程起不来时才是它最有用的时刻）。两个 Windows 细节：文件必须以共享模式打开（`os.OpenFile` 是独占的，否则守护进程一运行日志就没人读得到，而「它跑着的时候去看日志」正是唯一用途）；且必须用 `FILE_APPEND_DATA` 而不是 `GENERIC_WRITE`——`CreateFile(OPEN_ALWAYS)` 会停在偏移 0，第二个实例写「已在运行中」时会覆盖掉日志开头（实测留下一截被截断的旧行）。另外该消息现在同时落盘，不再只写 stderr。
- **三处前端逻辑从未生效**（均已在浏览器中实测确认）：① 状态栏速率告警产出 `rate-high`/`rate-warn`，而 CSS 只定义 `rate-orange`/`rate-red`，告警色永不出现；② 端口下拉的选中项加的是 `selected`，而 CSS 与 `scrollIntoView` 查的是 `is-selected`，选中高亮与自动滚动双双失效；③ 切换语言时只有第一个标签页的系统消息会刷新——`refreshSysMsgI18n` 用 `document.getElementById('displayContent')`，而每个标签页都发了同一个 id，该函数只拿到第一个。现按类名遍历全部页面。
- **`sendqueue` 不再静默吞掉写错的字段名**。此前 CLI 把文件解析成 `[]map[string]any` 再走 IPC，未知字段会被连丢两次（CLI 的 map 忽略一次、守护进程的 struct 再忽略一次），于是 `{"data":"ONE"}` 这类输入返回 `success:true, entries:1` 而实际装进去的是**空内容**——使用者直到发现什么都发不出去才知道字段名写错了。现于 CLI 侧用 `DisallowUnknownFields` 严格校验，报错里直接给出接受的字段名与示例；`data` 作为 `content` 的别名仍被接受，以兼容最常见的手误。同时解析前剔除 UTF-8 BOM——PowerShell 5.1 的 `Set-Content -Encoding UTF8` 会写 BOM，此前会报 `invalid character 'ï'`，用户完全看不出是自己文件的编码问题。
- **自动发送的三处状态问题**。① queue 模式在空队列时返回 `success:true` 然后一个字节不发、零告警，现象与「发送路径坏掉」无法区分，现改为明确报错；② `startAutoSend` 不复位计数器，上一轮留下的 `sendCount` 会原样出现在这一轮，现于启动时归零；③ `writePortSilent` 直接解引用 `p.port`，而空闲进程的 port 是 nil 接口——生产路径虽有 `status=="connected"` 守卫，但「自动发送跑到一半进程被断开」时循环仍会 tick，那一次解引用就是打崩整个守护进程的 nil panic（dispatch 无 `recover`、守护进程是机器级单例），现补 nil 守卫按普通发送失败计数。
- **关于第三方测试报告**：本版参考了一份外部 AI Agent 的全面测试报告。其中 `sendqueue` 静默容错、空队列空转、BOM、协议版本校验等结论已独立复测确认并修复；但报告列为 P0 的两条——「single 模式不发送数据、`sendCount` 虚报」与「`intervalMs` 不生效、恒定 1 条/秒」——**经本机实测证伪**：COM3→COM4 交叉互连、接收侧独立计数，`sendCount:21` 对应对端实收 21 块（3 字节/块），块间隔中位数 **0.201 s**（`intervalMs=200`）。报告测到的「约 1 秒节拍」实际是 queue 模式每条目自带的 1000 ms 延迟（`MultistrEntry.Delay`），与 `intervalMs` 是两个不同的量。报告列为「本轮最严重」的「注册握手 11% 失败」，本机反复复测为**偶发且远低于该比例**（120 次慢节奏 1 次；紧贴连跑 500 与 800 次均 0 次），且守护进程日志证明它**已经完成注册**（`addSession` 在两条回连管道都成功之后），故报告推断的「监听器未就绪」不成立；已改为不再把该情况断言成「守护进程拒绝了注册」。

### 0.7.3（2026-09-12）

本版以**快速面板布局**为主，另修复一处守护进程连接故障（详见下文最后一条，涉及 Go 侧改动）。

- **快速面板改为三段式布局，滚动条只覆盖数据行**。上一版把「+ 添加」做成表格最后一行后暴露出一个问题：`.qp-table` 既是定义列宽的网格、又是滚动容器，滚动条因此跨过了表头与添加行。现拆成 **表头 / 滚动区 / 添加行** 三段：`.qp-scroll` 成为唯一的滚动容器，滚动条被严格夹在表头下沿与添加行上沿之间（实测跨度 `217..889`，与两者完全重合）。代价是 `subgrid` 不能再用了——`subgrid` 的轨道来自母网格，不随滚动区自身的滚动条收窄，轨道会溢出到滚动条底下——所以表头与滚动区改为两个独立网格共享同一份**确定值**列模板 `--qp-cols`。列宽必须确定：用 `max-content` 时表头（文字）与行（控件）的固有尺寸不同，列会各自漂移，连 `1fr` 列宽也跟着变，实测宽面板下偏差可达 14px；改为定值后 enable / hex / delay / send 四列偏差为 0。
- **修复「先打开守护进程再打开 GUI 有时连不上」**。这个故障有两层根因：

  ① **守护进程的日志会阻塞它自己**。Windows 控制台开启「快速编辑模式」时，只要用户点击一下控制台窗口，控制台就进入选择状态，此时**进程往该控制台写输出会阻塞**（控制台标题会带上「选择」前缀）。而守护进程的日志是同步 `fmt.Printf`，日志一被卡住，正在处理连接的 `handleRegister` 就停在中途，客户端只能等到 5 秒超时。现于启动时调用 `SetConsoleMode` 关掉 `ENABLE_QUICK_EDIT_MODE`（须同时设置 `ENABLE_EXTENDED_FLAGS` 才生效），并在改动成功时记一条日志。

  ② **GUI 把一次启发式检查当成了连接的前提**。`CheckDaemonStatus` 在尝试 IPC 之前先用 `tasklist` 判断「守护进程是否在跑」，而那是启发式检查——被安全软件拦截、系统繁忙导致 `CreateProcess` 失败、或输出格式变化时都会返回 false，于是守护进程明明在等待，GUI 却连试都不试；而且该调用没有超时，一旦卡住会让 `a.checking` 永远停在 true，之后所有检查都直接返回 false（永久离线）。现改为直接拨 IPC：没有守护进程时 `WaitNamedPipe` 会立刻返回 `ERROR_FILE_NOT_FOUND`（`pipe.dialPipe` 的注释已说明），并不慢，而且是权威判据。同时给 `tasklist` 加 3 秒超时，`checkOffline` 也不再以它的结果决定是否尝试连接。
- **修复表头「延时(ms)」被截成「延时(…)」**。拆成独立网格后表头单元格宽度等于列轨道本身，而「延时(ms)」实测需 55px 内容宽（加内边距 4px 与右边框 1px 共 60px），44px 的轨道放不下，被 `.qp-col` 的 `overflow: hidden; text-overflow: ellipsis; white-space: nowrap` 截断。原来用 `subgrid` + `gap: 0` 时单元格实际宽度是「轨道 + 间隙」（54 + 6 = 60），正好放得下——所以这是拆分网格引入的回归。现把延时列定为 62px，并从内容列的最小值里补回（48 → 40），裁剪阈值仍约为 298px。
- **修复横向滚动条过粗**。全局规则只写了 `::-webkit-scrollbar { width: 6px }`，而 `width` 约束的是竖向滚动条；横向滚动条的粗细由 `height` 决定，未设置时回落到平台默认值（Windows 上十几像素）。现补为 `width: 6px; height: 6px`。
- **快速面板的横向滚动条恢复，但限定为 6px**。表头在滚动区之外，若只让数据行横向滚动，表头不会跟随、列会错位；因此 `.qp-hdr` 自身也横向可滚（滚动条隐藏），由 `tabpage.js` 的 `scroll` 监听同步 `scrollLeft`，横滚条只由 `.qp-scroll` 显示一条。实测横滚后列偏差与横滚前完全一致（最大 3.5px）。同时把各列最小宽度压到合计 `284px`，使触发横滚条的面板宽度约为 298px（内容列是 `1fr`，正常宽度下会自动撑开，观感不变）。实测：674 / 449 / 375 / 322 / 300px 下无横滚条，255px 时出现 6px 横滚条。
- **滚动条槽固定占位**。`.qp-scroll` 用 `scrollbar-gutter: stable` 让滚动条槽恒占 6px，`.qp-hdr` 右内边距相应取 `10px`（4 + 6）。这样滚动条出现/消失时不会有整列跳动——实测有/无滚动条两种状态下列偏差完全一致。
- **状态栏右侧改用「箭头 + Tx/Rx」**。原来各语言的 `stat.tx`/`stat.rx` 并不统一——中文是「发送/接收」、繁中「傳送/接收」、韩文「송신/수신」、阿拉伯文用词，而 en/ja/fr/es/ru 已经是 `Tx`/`Rx`；而且**根本没有箭头**，`.stat-arrow` 只是带颜色的文字标签（类名叫 arrow、内容是文字）。现统一为 `↾ Tx: 0` / `⇃ Rx: 0`，9 种语言的 `stat.tx`/`stat.rx` 都取 `Tx`/`Rx`。

  大小写取 **`Tx`/`Rx` 而非 `TX`/`RX`**：数据区每条记录的时间戳前缀是**硬编码**的 `history.js: 'Rx' / 'Tx'`（不走 i18n，各语言一致），而状态栏与数据区同屏显示，两处挨着的缩写必须同形；设置页的颜色预览同样用 `Rx ⇃` / `Tx ↾`。箭头则沿用项目既有约定：**TX = `↾`、RX = `⇃`**（同上两处），避免再造一套方向符号。
- **多字符串发送的「+ 添加」改为表格末尾的一行**。原来它是一个独立的页脚条（`.qp-footer`，带 `border-top`、自己占一行高度），与表格是分离的。现改为表格的最后一行（`.qp-add-row`），数据少时紧跟最后一条数据行之后（实测间距 0px）。原按钮带的 `.toggle-btn`（`display: inline-grid; width: max-content`）会阻止撑满整行，故不再使用。

`LAYOUT-RULES.md` §7 与速查索引已同步说明三段式结构、定值列模板与滚动条槽的对应关系。

验证：滚动条跨度在 3 / 14 / 60 行与窄/宽面板下均为「表头下沿..添加行上沿」；有/无滚动条时列偏差一致（最大 4px，四列为 0）；点击仍能新增行；无未捕获页面异常。

### 0.7.2（2026-09-12）

本版为**前端缺陷修复**，来自一次对 GUI 布局的系统性核对（几何测量 + 浅色/深色对照 + 逐条核对 `LAYOUT-RULES.md` 与实现）。前两项都是「代码写错了但不报错」的静默失效。

- **修复隐藏分支从未生效**（`applySendRatio()`）。原代码写 `style.overflow = 'is-hidden'`——把**类名**当成了 `overflow` 的值。CSSOM 对非法值静默忽略，所以 `sendRatio >= 0.97`（收起显示区）与 `<= 0.01`（收起发送区）两个分支从未执行过；面板只是被 flex 压到 0 高度，`sendRatio = 0.005` 时留下 4px 高的发送工具栏残条。改为 `classList.toggle('is-hidden', cond)`，转发模式分支一并清理该类。
- **修复浅色主题缺失 4 个语义色变量**。两个浅色变量块各定义 17 个变量，而深色块有 21 个，缺 `--accent` / `--green` / `--red` / `--yellow`。由于 `var()` 无 fallback 时属性会在 computed-value 阶段失效并回落到初始值，表现不是「颜色不对」而是**属性整体消失**：设置页 4 个开关处于「开」时轨道全透明 + 白圆点 = 完全不可见；此外状态栏在线点、标签活动点、速率告警色、端口占用标记，以及 `.cs-option.is-selected` / `.settings-*.is-selected` 等一批 `background: var(--accent); color: #fff` 的选中态都会变成白字透明底。已补齐为浅底可读的加深版本（`--accent` 取与既有 `--fwd-p1` 同色）。
- **修复清空按钮压住第一条数据行**。`.c-clear-btn` 底边在 76px（`top: 48px` + `height: 28px`），而 `.display-content` 只预留 74px，差 2px，实测重叠 48×2px。根因是悬浮条的预留空间由各处分别写死、没有单一来源，现引入 `--reserve-top`，`.display-content` 的 `padding-top` 与按钮的 `top` 都由它推导。
- **同步 `LAYOUT-RULES.md`**：补充三处与实现不符或未记录的说明——`.display-area` 的 `min-height` 实际由 JS 写为 40px；`.tabs-scroll` 的 `overflow-y: visible` 因 CSS 规范（一轴 `visible`、另一轴非 `visible` 时 `visible` 会被计算为 `auto`）无法生效，故「激活标签向下覆盖分隔线」实际不生效；`.o-divider--quick` 在面板折叠时保留是刻意设计（从右边缘把面板拖出来的手柄），不是残留。

另将 6 条未修项记入 `TODO.md`：类名前缀约定落实率仅 24%、响应式只有一个宽度断点（800px）、`z-index: 1` 被 4 个元素共用、标签栏横向滚动无视觉提示、`client`/`pipe` 层仍在产生误导性的 `daemon not running: ` 前缀等。

验证：`LAYOUT-RULES.md` 声明 vs 实测 45 项核对 **0 项不符**；GUI 端到端 **20/20** 通过且控制台无 error；浅色/深色下 4 个开关轨道分别为 `rgb(37,99,235)` / `rgb(76,194,255)`；清空按钮与首行重叠由 2px 降为 0。

### 0.7.1（2026-09-12）

本版为**平台支持**更新：守护进程、CLI、MCP 三个可执行文件现在可在 Linux 上构建并运行。Windows 行为不变。

- **新增 Linux 支持**。此前 `daemon/main.go` 直接 import `golang.org/x/sys/windows`，导致整个守护进程包只能在 Windows 上编译；共享内存也只有 Windows 命名映射一种实现。现按平台拆分为成对文件（`//go:build windows` / `//go:build !windows`）：管道改用 `$XDG_RUNTIME_DIR/serial-tool/` 下的 Unix 域套接字（目录 0700），共享内存改用 `/dev/shm` + `mmap(MAP_SHARED)`，控制台代码页与串口流控设置下沉到各自平台文件。`pipe.Addr` 由常量改为平台变量，并新增 `pipe.Endpoint(name)` 统一三处管道名构造。
- **修复共享内存打开路径误用声明长度**。非 Windows 实现最初按调用方传入的大小建立映射，而打开既有对象时调用方传的是 0，触发「声明的数据区超出映射长度」。现改用 `Fstat` 取真实文件大小，并且只在创建者进程退出时 `unlink`，避免误删其他进程仍在使用的映射。
- **文档新增平台相关代码约定**（`ARCHITECTURE.md` §13）。说明 Go 条件编译的**文件级**粒度、三条硬规则（`//go:build` 与文件名后缀是 **AND** 关系而非覆盖；后缀必须是合法 GOOS/GOARCH，`unix`/`other`/`posix` 都不是），7 对平台文件清单，以及用 `go list` 在各 GOOS 下核对实际参与编译的文件集这一排查手段。
- **新增 `.gitattributes` 固定 `eol=lf`**。此前换行符取决于各人机器上的 `core.autocrlf`（本机为 `true`），表现为 Windows 上 `gofmt -l .` 把全部 Go 文件报成未格式化、而 Linux 上干净。仓库内本就以 LF 存储，此文件只是把现状显式固定。
- `client/process_windows.go` 补上 `//go:build windows`：后缀本就隐式生效，此改动仅为与其余 13 个平台文件统一风格。

Linux 端到端验证（Ubuntu 22.04.5，socat 建立 PTY 回环）：11 个包全部构建通过；守护进程创建 `/run/user/1000/serial-tool/daemon.sock`；CLI 报 `{"version":"0.7.1","protocolVersion":1}`；两个会话分别打开 PTY 两端，A 发送的 `4C494E55582D302E372E30` B 字节级完整收到；共享内存创建→2 个对象（24+5 MB、24+1 MB），销毁→0。四个 GOOS（windows / linux / darwin / freebsd）下 `go list` 列出的文件集互斥且完整，无缺失、无重复符号。

### 0.7.0（2026-09-12）

本版以**稳定性与接口一致性**为主，未新增功能。五项修复各自都来自实测复现，而非代码审阅的推测。

- **修复广播阻塞：一个卡住的客户端会拖死整个守护进程**（严重度最高）。事件推送此前在持有会话锁的情况下逐会话同步写管道，而同步写管道在缓冲区满时会阻塞。实测：一个注册后停止读取事件的客户端，能让任何新客户端注册阻塞 **63～143 秒**（多次运行），阻塞时长等于该客户端的存活时长；期间该串口的读循环也停在原地丢数据。现改为每个会话一个写入协程 + 有界队列，广播只做非阻塞投递，慢客户端被单独摘除而不是让所有人承担代价。修复后同一场景 `status` 稳定在 66～125 ms。
- **引入 IPC 协议版本协商**。守护进程是机器级单例，而 GUI / CLI / MCP 是各自独立升级的可执行文件，「新客户端 + 旧守护进程」是常态。此前协议层没有任何版本概念，守护进程甚至无法报出自身版本；差异会静默落到数据层（例如多字符串条目格式不符时被降级成乱码并原样发给串口）。现在新增 `daemon.info` 方法，客户端建立会话后即刻核对协议版本，不匹配则明确报错；`serial-cli start` 会自动重启版本不一致的守护进程。
- **修复命名管道握手竞态**。管道实例原先在 `Accept()` 内部才创建，而调用方普遍是「Listen → `go Accept()` → 立刻向对端发消息」。若 Accept 的协程尚未被调度，对端会拿到 `ERROR_FILE_NOT_FOUND`，表现为高频率的「管道不可用」（实测连续调用 CLI 时约 80% 失败）。现在 `Listen` 返回前即创建实例，并使管道名在全生命周期内始终有实例可用。
- **加固共享内存**。`CreateFileMapping` 遇到同名对象会「成功」并返回既有 section 的句柄，此前会被静默复用，使两个无关进程共享同一块环形缓冲区；现已明确报错，并给环名加上守护进程实例令牌以杜绝撞名。头部布局版本字段此前只写不读，数据区大小则直接信任被映射内存自己声明的值，现均纳入校验。
- **引入 `contract` 包，把 IPC 方法面收敛为单一事实来源**。同一个方法名此前在六处各自以裸字符串重复声明，已产生实际漂移（`multistr.status` 这个不存在的方法名被 CLI 与 MCP 同时使用；`forward.create` 与 `process.create` 两条通道并存且参数名不同）。现在方法名是常量，改名会编译期报错，各层不可能再引用不存在的方法。

测试方面新增：`ringbuf` 与 `contract` 从零覆盖；`pipe` 增加握手竞态、取消在途 I/O、Close 不阻塞等回归测试；`daemon` 增加契约一致性与畸形参数边界测试。关键测试均做过变异验证（去掉被测逻辑后测试确实失败）。

### 0.6.5（2026-09-11）

- **修复 MCP stdio 分帧不符合规范**：`serial-mcp.exe` 改用换行分隔 JSON-RPC。原 Content-Length 分帧并非 MCP 传输格式，任何规范兼容的 MCP 客户端都完全无法与之通信（而服务器声明的是 `protocolVersion: 2025-06-18`）
- **修复无守护进程时 CLI 永久挂死**：`pipeListener.Close()` 原对挂起的 `ConnectNamedPipe` 依次调用 `DisconnectNamedPipe` 与 `CloseHandle`，二者都会等待该 pending I/O 完成，导致 `status` / `ports` / `sessions` / `shutdown` 等命令无限阻塞（>30 s 不返回）；改用 `CancelIoEx` 中止挂起 I/O
- **修复一次性 CLI 命令固定 4.1 s 开销**：源于同一 listener 关闭路径的自连接重试（40 × 50 ms × 2 个 listener），现降至约 60–80 ms
- **修复 `probe` 开箱不可用**：`probe.toml` 增加 `//go:embed` 内置回退；修正 `<exe>/../../config/probe.toml` 搜索路径（原 `../config` 在 `build/bin` 场景下指向 `build/config`，即 v0.6.4 声称的修复实际未生效）并去除重复候选
- **修复 CLI 用法错误静默退出**：非交互模式下参数错误此前零输出直接 exit 1，现打印用法提示后再退出
- **修复 `autosend stop` / `autosend status` 不带 pid 不可用**：参数校验由 `len(args) < 3` 改为 `< 2`，与帮助文本中 `[pid]` 可选一致
- **修复 `go test ./...` 整体无法运行**：`config.T` 的 args 改为切片参数，避免 `go vet` 将其判定为 printf wrapper、进而拒绝所有非恒定 key 的调用
- `ARCHITECTURE.md` / `BUILD.md` 中 MCP 分帧与 probe 搜索路径描述同步修正

### 0.6.4（2026-06-05）

- 修复 Hex 发送字节丢失：`DecodeEntryContentOnly` 误将 Modbus 首字节 0x01 判为 multistr 条目头，新增 flags/delay 校验防误判
- `send.trigger` IPC 新增 `raw` 参数：直接发送传 `raw:true` 跳过 multistr 解码，快捷面板传 `raw:false` 保持兼容
- 修复自动发送/多字符串逐条目延迟默认 1000 ms：改为跟随 `autoSendIntervalMs`，兜底 100 ms
- 修复 Hex 显示不跟随设置颜色：`.hex-data` 的 `color: var(--fg)` 覆盖方向颜色变量（`--dc-hexRx`/`--dc-hexTx` 等），移除冗余属性
- 修复启动后 TX/RX 显示模式初始状态不一致：`syncDaemonSessions` 新标签页改用全局 `state.displayMode` 初始值，激活时显式刷新按钮
- `findProbeConfig` 新增搜索路径 `<exe>/../config/probe.toml`（build/bin → config 开发场景）
- MCP ServerInfo 版本从硬编码 `0.5.9` 改为 `version.Version` 统一同步

### 0.6.3（2026-05-26）

- 数据路径重构：环形缓冲区从 Drain 消费模型改为 Snapshot 观察模型，客户端读取历史不清空缓冲区，多客户端独立读取
- 历史清空本地化：GUI 清空历史仅为本地视图操作，不通知 daemon，不清环形缓冲区和磁盘文件
- 上滚召回历史：清空后上滚从 daemon 环形缓冲区快照召回数据，超出范围翻磁盘文件（SearchHistory 分页+去重）
- 历史框左下角浮动按键栏重构：发送工具栏 RX/TX/发送格式切换按键合并到浮动栏，模式切换自动显隐
- 浮动栏按键样式统一：RX/TX 按键使用时间戳颜色（tsRx 绿/tsTx 蓝），P1/P2 保持端口颜色，Hex 模式实心背景
- 转发模式浮动栏精简为 [P1 文] [P2 文] | [锁定滚动]
- 非首个标签页多项修复：`_setSelectVal`/`getSerialConfig`/`openSerialPort`/`openForwardPorts` 改用 `pageEl` 活动页查询
- `_syncPortSelectsFromTab` 支持闲置声明进程写入端口/波特率
- `refreshStatsDOM` 无缓存时主动从 daemon 拉取统计

### 0.6.2（2026-05-26）

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

### 0.6.1（2026-05-25）

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

### 0.6.0（2026-05-24）

- 解码引擎迁移至 Go：逐字节容错解码，产分段结构交前端纯 DOM 渲染
- 前端架构重构：精简 HTML，TabPage 独立 DOM 子树 + per-tab 状态，弹窗全部 JS 化
- 设置页重构：独立标签页，左右分栏，新增关于面板
- 标签栏水平滚动，内置图标文件化（17 SVG），版本号统一

### 0.5.9（2026-05-20）

- 主题系统重构：文件驱动 + 三段式色值 + CSS 全变量化
- 发送框工具栏：三按键分立 + `data-icon` 声明式图标 + 水平滚动
- 多字符串面板图标化 + 循环发送

### 0.5.8（2026-05-19）

- 多字符串发送引擎：daemon queue 升级为多条目引擎（逐条延迟 + 循环 + 启用控制）
- CLI/MCP 扩展 sendqueue 和 multistr 命令族

### 0.5.7（2026-05-18）

- 前端 CSS 架构分层（o-/c-/is-/t- 命名前缀）
- 多字符串面板 CSS Subgrid 重构，JS 拆分为 4 模块

### 0.5.6（2026-05-18）

- i18n 键值补全，9 语言全翻译；波特率下拉框支持自定义数值输入

### 0.5.5（2026-05-18）

- 历史持久化双写：环形缓冲区 + 磁盘文件；历史搜索与召回；自动保存开关
- 完整 9 语言专业化重译，外部 i18n `_fallback` 回退

### 0.5.4（2026-05-17）

- 多 GUI 同步广播，流控支持（RTS/CTS、XON/XOFF）
- 设置文件改为 INI 格式，阿拉伯语 RTL 布局，DevTools 集成

### 0.5.2（2026-05-14）

- 菜单栏 SVG 图标 + 悬浮窗，主题按键 SVG mask-icon，端口转发交换按键

### 0.5.1（2026-05-14）

- 编码格式全局设置（ASCII/GB2312/UTF-8），容错解码引擎，Hex 显示样式独立配置，消息去重

### 0.5.0（2026-05-14）

- 客户端列表推送 `clients-changed`，状态栏三色气泡，设置持久化

### 0.4.9（2026-05-08）

- 统一进程模型：Process 新增 mode 字段（single/forward），进程模式切换

### 0.4.8（2026-05-08）

- 设备端口探测：TOML 规则配置，3 种匹配模式（substring/regex/modbus_crc）

### 0.4.7（2026-05-07）

- 端口转发 + 串口切换，GUI 标签页拖拽排序

### 0.4.6（2026-05-06）

- 自动发送成为守护进程功能，stats 拆分 count/rate，文本/Hex 独立发送缓冲区

### 0.4.5（2026-05-06）

- stats 事件速率推送，状态栏实时字节统计，循环发送

### 0.4.4（2026-05-05）

- Hex 发送支持空格分隔，历史缓冲区统一 5 MB，管道断裂即时检测

### 0.4.3（2026-05-05）

- 3 管道 IPC 架构（请求/响应/事件分离），心跳替代轮询

### 0.4.2（2026-05-05）

- 单持久管道，订阅式事件推送，空闲进程管理

### 0.4.1（2026-05-04）

- 进程生命周期重构，双状态机 + 端口去重，共享内存环形缓冲区

### 0.4.0（2026-05-04）

- 异步发送通道 + 硬件自动探针，会话 I/O 统计

### 0.3.x

- CLI 独立可执行文件，MCP 协议支持，Wails v2 框架，Windows 命名管道 IPC

### 0.2.x

- Go 重写替代 Node.js，WebView 桌面窗口，前后端分离

### 0.1.0（2026-05-01）

- Node.js 后端，Express + Socket.IO，Go WebView 桌面启动器

## ⚠️ 安全说明

> **本工具当前无安全相关设计**：不包含认证、授权、加密、访问控制或审计日志等安全机制；串口收发内容与历史记录以明文形式处理与存储。仅限在受信任的本地环境使用，请勿在不可信网络或安全敏感场景中部署。

## 贡献与许可

- 本项目基于 [MIT License](LICENSE) 开源，欢迎社区贡献——见 [贡献指南](CONTRIBUTING.md)。
- **AI 辅助声明**：本项目在开发与文档维护过程中使用了 AI Agent 工具辅助（代码生成与审计、文档润色、多语言翻译等）；文档与翻译内容由 AI 辅助产出，可能存在错误或不准确之处，欢迎指正与贡献修正。
