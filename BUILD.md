# 构建说明

## 环境要求

| 工具 | 最低版本 | 用途 | 备注 |
| --- | --- | --- | --- |
| Go | 1.26+ | 后端编译 | 检查：`go version` |
| Wails CLI | v3.0 | GUI 构建 | `go install github.com/wailsapp/wails/v3/cmd/wails3@latest` |
| WebView2 运行时 | Evergreen | GUI 运行 | 仅 Windows；目标机需安装 |
| GTK4 + WebKitGTK 6.0 | 6.0 | GUI 构建/运行 | 仅 Linux：构建装 `libgtk-4-dev libwebkitgtk-6.0-dev libsoup-3.0-dev`，运行装 `libgtk-4-1 libwebkitgtk-6.0-4 libsoup-3.0-0` |

守护进程 / CLI / MCP 三个可执行文件在 Windows 与 Linux 上均可构建，不需要 Wails、WebView2 或 GTK，仅需 Go 工具链。

> 首次构建 GUI 前可运行 `wails3 doctor` 自检（`wails doctor` 是 v2 的旧命令）。

## 快速构建

在仓库根目录执行：

```bash
go mod tidy

# 守护进程 / CLI / MCP
go build -ldflags="-s -w" -o build/bin/serial-daemon.exe ./daemon/
go build -ldflags="-s -w" -o build/bin/serial-cli.exe ./cmd/serial-cli/
go build -ldflags="-s -w" -o build/bin/serial-mcp.exe ./cmd/serial-mcp/

# GUI：统一走 build.ps1（内部 wails3 build，版本号从 wails.json 读）
.\build.ps1            # dev 构建：前端从磁盘读，保存即刷新
.\build.ps1 -Release   # release 构建：前端嵌进 exe，可单文件分发
```

产物：`build/bin/serial-daemon.exe`、`serial-cli.exe`、`serial-mcp.exe`、`serial-gui.exe`

Linux 下去掉 `.exe` 后缀；GUI 必须是 **cgo 构建**（不能从 Windows 交叉编译，要在 Linux 上编）：

```bash
go build -ldflags="-s -w" -o build/bin/serial-daemon ./daemon/
go build -ldflags="-s -w" -o build/bin/serial-cli    ./cmd/serial-cli/
go build -ldflags="-s -w" -o build/bin/serial-mcp    ./cmd/serial-mcp/
go build -tags production -trimpath \
  -ldflags "-s -w -X github.com/nienieai/serial-debugger/version.Version=0.10.0" \
  -o build/bin/serial-gui .
```

> Linux 上**不要**用 `wails3 build` 也没关系 —— 它最终也是这条 `go build -tags production`。装了 `wails3` 时直接 `wails3 build` 更方便（会顺带生成 bindings）。

平台差异的实现约定见 [ARCHITECTURE.md](ARCHITECTURE.md) §13。

## 按需构建

并非每次修改都需要全量构建，按变更范围选择对应目标：

### 仅修改前端 (JS/CSS/HTML)

```bash
.\build.ps1
```

前端文件（`frontend/*`）由 Wails embed 嵌入 `serial-gui.exe`，Go 代码无变化时无需重编 daemon/CLI/MCP。

### 仅修改守护进程

```bash
go build -ldflags="-s -w" -o build/bin/serial-daemon.exe ./daemon/
```

适用场景：`daemon/*.go`、`protocol/`、`ringbuf/`、`pipe/` 变更。

### 仅修改 CLI

```bash
go build -ldflags="-s -w" -o build/bin/serial-cli.exe ./cmd/serial-cli/
```

### 仅修改 MCP

```bash
go build -ldflags="-s -w" -o build/bin/serial-mcp.exe ./cmd/serial-mcp/
```

### 修改共享包 (client / pipe / protocol / ringbuf / config / decode / version)

共享包被多个可执行文件引用，需按依赖速查重编所有引用方：

```bash
go build -ldflags="-s -w" -o build/bin/serial-daemon.exe ./daemon/
go build -ldflags="-s -w" -o build/bin/serial-cli.exe ./cmd/serial-cli/
go build -ldflags="-s -w" -o build/bin/serial-mcp.exe ./cmd/serial-mcp/
.\build.ps1
```

### 仅修改 GUI Go 后端 (app.go / settings.go / main.go / platform_*.go)

```bash
.\build.ps1
```

### 依赖关系速查

直接依赖（`client` 内部依赖 `pipe` / `protocol` / `ringbuf`，CLI / MCP / GUI 经 `client` 间接依赖三者）：

```text
daemon ────── config, pipe, protocol, ringbuf
CLI    ────── client, config, version
MCP    ────── client, config, version
GUI    ────── client, config, decode, version
client ────── pipe, protocol, ringbuf        （传递依赖）
```

影响面速查（修改某包需重编谁）：

| 修改的包 | 需要重编 |
| --- | --- |
| `pipe` / `protocol` / `ringbuf` | 全部 4 个（daemon 直接 + CLI/MCP/GUI 经 client） |
| `client` | CLI、MCP、GUI（daemon 不依赖 client） |
| `config` | 全部 4 个 |
| `decode` | 仅 GUI |
| `version` | CLI、MCP、GUI |

## 产物说明

| 产物 | 说明 | 是否随 Release 分发 |
| --- | --- | --- |
| `build/bin/serial-daemon.exe` | 守护进程，单实例 | 是 |
| `build/bin/serial-gui.exe` | Wails v3 GUI，多标签页 | 是（目标机需 WebView2 运行时） |
| `build/bin/serial-cli.exe` | CLI 工具（49 条命令） | 是 |
| `build/bin/serial-mcp.exe` | MCP 协议工具（29 个工具） | 是 |
| `<exe>/resources/i18n/` | 外置语言包（`{lang}.json`，覆盖内置翻译或新增语言） | 可选 |
| `<exe>/resources/themes/` `<exe>/resources/icons/` | 外置主题（`themes/<id>/theme.json` 与 `icons/<包名>/`） | 可选 |

Linux 产物同名去掉 `.exe`；运行 GUI 需要 `libgtk-4-1 libwebkitgtk-6.0-4 libsoup-3.0-0`（装包脚本在 `build/linux/`，`make-package.sh` 出单文件 `.run`，`install.sh` 装到 `~/.local`，可选 `--fix-permissions` 写 uaccess 规则）。

> 4 个可执行文件必须放在同一目录下，GUI 通过 `os.Executable()` 自动查找同目录的 `serial-daemon`。

### 部署到开发测试目录

```powershell
.\deploy-dev.ps1            # 只同步产物（改了前端 / 语言包）
.\deploy-dev.ps1 -Build     # 先重编译再部署（改了 Go 代码）
```

目标目录默认 `<仓库>\..\串口调试工具\0.9.1-dev`，可用 `-Target` 指定。

之所以要脚本而不是只拷 exe，是因为**只换 exe 会有两处永远读到旧内容**：

| 内容 | 原因 |
| --- | --- |
| `frontend\` | GUI 是 hotreload 构建，前端从 `<exe>\frontend\` 读盘，不读内嵌资源 |
| `resources-system\` | 语言包 / 图标 / 主题的查找顺序是 `resources-system` → `resources` → 内嵌，内置层优先级最高 |

脚本会把 4 个 exe、`frontend\`、`config\resources\`（→ `resources-system\`）一起同步，
逐文件比对字节数，不一致非 0 退出。外置层 `resources\`（用户自己的语言包 / 主题）不碰。

> 改了语言包记得**重启 GUI**：i18n 只在启动和切换语言时加载，hotreload 不盯
> `resources-system\`。

## 源代码目录

```text
serial-tool/
├── main.go / app.go / app_perm.go      # GUI 入口、绑定后端、串口权限体检
├── settings.go / dialog.go / instance.go  # gui.ini、对话框窗口、单实例
├── platform_windows.go / platform_other.go # GUI 平台桩（openFolder 等）
├── devui_release.go / devui_hotreload.go  # 前端资源：嵌入 vs 从磁盘读（build tag）
│
├── daemon/          # serial-daemon：进程管理、串口 I/O、IPC 服务端、历史 / .stlog 录制
├── cmd/serial-cli/  # serial-cli：49 条命令 + 交互 REPL
├── cmd/serial-mcp/  # serial-mcp：stdio JSON-RPC，29 个工具
├── client/          # DaemonClient（3 管道 + 共享内存）
├── contract/        # IPC 方法名常量（单一事实来源）
├── protocol/        # JSON-RPC 换行分帧
├── pipe/            # Windows 命名管道 / Linux Unix 域套接字（成对文件）
├── ringbuf/         # 共享内存环形缓冲区（Windows / Linux 成对文件）
├── sessionfile/     # .stlog v1.4 单文件与 v2 工程目录的读写
├── sysperm/         # Linux 串口权限：access(2) 探测 + pkexec 临时授权（其它平台桩）
├── decode/          # UTF-8 / GBK / ASCII 容错解码，产 []Segment
├── config/          # 内置资源：i18n（9 语言）、icons、themes、probe.toml
├── version/         # 版本号（改一处全部同步）
├── frontend/        # 纯 HTML/JS/CSS（无构建链）＋ bindings/（wails3 生成，随仓库提交）
├── build/           # 构建资源与产物：windows/、linux/（.run 打包与安装脚本）、bin/、pkg/
├── docs/format/     # .stlog 格式规范与版本演进
├── winres/          # Windows 资源（go-winres 生成 .syso）
└── *.md             # README / BUILD / ARCHITECTURE / LAYOUT-RULES / TODO / CHANGELOG …
```

## 开发调试

```bash
go run ./daemon/                      # 守护进程
go run ./cmd/serial-cli/ status       # CLI
go run ./cmd/serial-mcp/              # MCP
.\build.ps1                           # GUI 热重载（dev 构建，前端从磁盘读）
```

没有真实串口时，可用虚拟串口对联调（Windows 下 com0com 等工具），或用两块 USB 转串口板互连。

## 图标

内置图标位于 `config/resources/icons/system/`（32 个 24×24 SVG + icons.json），编译时 `//go:embed` 嵌入。外部图标主题放在 `resources/icons/<包名>/`（文件夹 = 主题，内含 icons.json + 平铺 SVG）。

Windows 可执行文件图标（ICO）：`daemon/`、`cmd/serial-cli/`、`cmd/serial-mcp/` 三个目录各有 `winres/`，统一用 go-winres 生成：

```bash
go install github.com/tc-hib/go-winres@latest
cd daemon && go-winres make && cd ../..
cd cmd/serial-cli && go-winres make && cd ../..
cd cmd/serial-mcp && go-winres make && cd ../..
```

生成的 `.syso` 文件（`rsrc_windows_*.syso`）随仓库提交，正常构建无需重新生成。

## 注意事项

- GUI 统一走 `.\build.ps1`（内部 `wails3 build`）；直接 `go build .` 必须带 `-tags production`，否则前端资源不内嵌（hotreload 构建反而从 `<exe>/frontend/` 读盘）
- Linux 分体版**不写系统文件**：串口权限只做临时授权（GUI 横幅 / `serial-cli fix-permissions`，pkexec 给当前设备加 ACL），拔插或重启后失效；要永久解决走 `sudo usermod -aG dialout $USER` 或 `build/linux/install.sh --fix-permissions`（写 `/etc/udev/rules.d/60-serial-tool.rules` 的 uaccess 规则）。授权必须由**桌面会话里**的进程发起，SSH / 后台启动的 GUI 弹不出系统密码框（会返回 `no_agent`）
- 守护进程注册管道 `\\.\pipe\serial-tool-daemon`，字节模式 + 同步 I/O
- 3 管道架构：持久客户端额外创建 `\\.\pipe\st-{clientId}-resp`（响应）和 `\\.\pipe\st-{clientId}-sub`（事件推送）
- 管道超时：`WaitNamedPipeW` 5s，IPC Call 10s，串口打开 8s，写入 3s
- 守护进程 `--silent` 参数后台静默启动
- 心跳 ping 每 5s 一次（静默不记日志），3 次失败触发断连回调（防御性兜底）
- 管道读端即时断连检测：`readSubLoop` / `readRespLoop` 读错误（如管道断裂）立即触发 `OnDisconnect`
- 字节解码由 Go 端 per-tab goroutine 完成（`decode/decode.go`），产 `[]Segment` 分段结构；JS `renderSegments()` 纯 DOM 渲染
- 编码切换时 `BatchDecodeForTab` 批量重解码所有缓存 hex 条目
- 状态栏由 `frontend/statusbar.js` 组件化：StatusDaemon / StatusClients / StatusEncoding / StatusStats，通过 `_statusBar` API 更新
- 设置页通过菜单「设置 → 软件设置」打开（独立标签页），左侧导航（编码与显示/外观/存储与行为/语言/关于），右侧气泡卡片布局
- 主题/语言气泡底部"创建示例"按键通过 Go 后端 `CreateExampleFiles` 创建示例文件夹和文件
- 设置页全部文字已 i18n 化（168 个 `settings.*` 键；9 个语言包各 593 个键，键集必须一致）
- 所有可执行文件放在同一目录下，GUI 通过 `os.Executable()` 自动查找 `serial-daemon.exe`
- 设备探测配置文件 `probe.toml` 搜索路径（`daemon/probe.go`）：显式指定 → exe 同目录 → `exe/../config/`（发布包布局）→ `exe/../../config/`（开发布局：exe 在 `build/bin`，配置在仓库根）→ 工作目录 `probe.toml` / `config/probe.toml`，候选去重。全部未命中时回退**内置规则**（`config/probe.toml` 经 `//go:embed` 嵌入二进制），因此发布包即使不含 `config/` 目录，`probe` 也可直接使用
- GUI 设置文件 `settings/gui.ini`（INI 格式），与 `serial-gui.exe` 同级的 `settings/` 目录；同目录还有 `cli.ini`（CLI 只读：`[general] language/color`）与 `daemon.ini`（守护进程只读：`[log] language/color`），一个文件只有一个写者。MCP 不落盘
- 外部资源目录是 `<exe>/resources/`，与 `serial-gui.exe` 同目录：`i18n/<lang>.json`（语言包）、`themes/<id>/`（一个主题一个文件夹，含 `theme.json` 与自带素材）、`icons/<包名>/`（`icons.json` + 平铺 `.svg`）。查找顺序：内置 `resources-system/`（仅动态开发版有）→ 外置 `resources/` → 编译进 exe 的那份（`config/resources.go`）。旧布局 `<exe>/i18n/`、`<exe>/themes/` 已不再读取
- 图标主题文件夹中 `.svg` 文件名 = 内置图标键名，覆盖对应内置图标
- i18n 文件 `_font` 字段指定首选字体（如 `"SimSun", "宋体", serif`），系统无此字体时回退默认
- 内置语言字体：zh=微软雅黑, zh-Hant=微軟正黑體, ja=Yu Gothic UI, ko=Malgun Gothic, ar=Noto Naskh Arabic, 其余=Segoe UI
- 版本号统一在 `version/version.go`，改一处全部同步（GUI / CLI / MCP 引用该包）

## MCP 配置

在 MCP 客户端（如 Claude Desktop）配置文件中加入：

```json
{
  "mcpServers": {
    "serial-tool": {
      "command": "path\\to\\serial-mcp.exe"
    }
  }
}
```

29 个工具：`serial_start_daemon`, `serial_list_ports`, `serial_refresh_ports`, `serial_create`, `serial_open`, `serial_connect`, `serial_disconnect`, `serial_switch`, `serial_forward_create`, `serial_declare`, `serial_set_mode`, `serial_close`, `serial_rename`, `serial_send`, `serial_sessions`, `serial_history`, `serial_status`, `serial_monitor`, `serial_shutdown`, `serial_stats`, `serial_port_watch`, `serial_autosend_start`, `serial_autosend_stop`, `serial_autosend_status`, `serial_sendqueue`, `serial_multistr_save`, `serial_multistr_load`, `serial_multistr_status`, `serial_probe_ports`
