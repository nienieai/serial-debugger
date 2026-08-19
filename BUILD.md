# 构建说明

## 环境

- Go 1.26+ / Wails CLI v2 / Windows: WebView2 运行时

## 构建

```bash
cd 串口调试工具-0.6.4
go mod tidy

# 守护进程 / CLI / MCP
go build -ldflags="-s -w" -o build/bin/serial-daemon.exe ./daemon/
go build -ldflags="-s -w" -o build/bin/serial-cli.exe ./cmd/serial-cli/
go build -ldflags="-s -w" -o build/bin/serial-mcp.exe ./cmd/serial-mcp/

# GUI（必须用 wails build，加 -devtools 启用 DevTools）
wails build -devtools
```

产物：`build/bin/serial-daemon.exe`、`serial-cli.exe`、`serial-mcp.exe`、`serial-gui.exe`

## 按需构建

并非每次修改都需要全量构建，按变更范围选择对应目标：

### 仅修改前端 (JS/CSS/HTML)

```bash
wails build -devtools
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

### 修改共享包 (client / protocol / ringbuf / pipe / decode / config / version)

共享包被 daemon、CLI、MCP、GUI 共同依赖，需重编所有引用方：

```bash
go build -ldflags="-s -w" -o build/bin/serial-daemon.exe ./daemon/
go build -ldflags="-s -w" -o build/bin/serial-cli.exe ./cmd/serial-cli/
go build -ldflags="-s -w" -o build/bin/serial-mcp.exe ./cmd/serial-mcp/
wails build -devtools
```

### 仅修改 GUI Go 后端 (app.go / settings.go / main.go / platform_*.go)

```bash
wails build -devtools
```

### 依赖关系速查

```
daemon ────── client, pipe, protocol, ringbuf, config, version, decode
CLI    ────── client, pipe, protocol, ringbuf,         version
MCP    ────── client, pipe, protocol,                   version
GUI    ────── client, pipe, protocol, ringbuf, config, version, decode, frontend/
```

## 源代码目录

```
串口调试工具-0.6.4/
├── main.go                    # GUI 入口 (Wails)
├── app.go                     # GUI Go 后端 (Wails 绑定、daemon 通信、TabDecoder 管理)
├── settings.go                # GUI 设置持久化 (INI)
├── gen_icons.go               # 图标生成 (go:build ignore)
├── platform_windows.go        # GUI Windows 平台代码
├── platform_other.go          # GUI 其他平台桩
├── wails.json                 # Wails 配置
│
├── decode/                    # 字节解码包
│   ├── decode.go              #   Segment 类型、UTF-8/GBK/ASCII 容忍解码器
│   └── decode_test.go         #   25 个单元测试
│
├── daemon/                    # 守护进程 serial-daemon.exe
│   ├── main.go                #   入口
│   ├── serial.go              #   进程管理、串口 I/O、自动发送引擎
│   ├── multistr.go            #   多字符串条目序列化/持久化
│   ├── ipc.go                 #   命名管道 IPC 服务端
│   ├── probe.go               #   设备端口探测引擎
│   ├── history.go             #   历史记录持久化
│   ├── platform_windows.go    #   Windows 平台
│   ├── platform_other.go      #   其他平台桩
│   ├── portdesc_windows.go    #   端口描述 (Windows)
│   ├── portdesc_other.go      #   端口描述 (其他平台)
│   └── winres/                #   Windows 资源文件
│
├── cmd/
│   ├── serial-cli/            # CLI 工具 serial-cli.exe
│   │   ├── main.go            #   入口、27 条命令
│   │   └── winres/
│   └── serial-mcp/            # MCP 工具 serial-mcp.exe
│       ├── main.go            #   入口 (stdio JSON-RPC)
│       ├── tools.go           #   25 个 MCP 工具定义和处理器
│       └── winres/
│
├── client/                    # IPC 客户端库 (DaemonClient)
│   ├── client.go              #   3 管道客户端、共享内存操作、便捷方法
│   ├── process_windows.go     #   进程检测 (Windows)
│   └── process_other.go       #   进程检测 (其他平台)
│
├── pipe/                      # Windows 命名管道封装
│   ├── pipe.go                #   通用接口
│   ├── pipe_windows.go        #   CreateNamedPipe / CreateFile
│   └── pipe_other.go          #   其他平台桩
│
├── protocol/                  # IPC 协议 (JSON-RPC 换行分帧)
│   └── protocol.go
│
├── ringbuf/                   # 共享内存环形缓冲区
│   ├── ringbuf.go             #   跨平台逻辑
│   └── ringbuf_windows.go     #   CreateFileMappingW / MapViewOfFile
│
├── config/                    # 配置 (embed 嵌入二进制)
│   ├── i18n.go                #   内置 i18n 翻译加载
│   ├── i18n/                  #   9 种语言翻译 JSON
│   ├── icons.go               #   内置图标加载 (embed icons/system/*.svg)
│   ├── icons/system/          #   内置图标主题（19 个 24×24 SVG + icons.json）
│   ├── theme.go               #   主题系统 (颜色+图标)
│   ├── themes/system.json     #   内置颜色主题
│   └── probe.toml             #   设备探测规则配置
│
├── version/                   # 版本号 (改一处全部同步)
│   └── version.go             #   const Version = "0.6.4"
│
├── frontend/                  # GUI 前端 (Wails 嵌入)
│   ├── index.html             #   HTML 骨架（仅菜单栏/标签栏/容器/状态栏，其余 JS 构建）
│   ├── style.css              #   样式 (o-/c-/t-/is- 命名体系，.tab-page-inner 布局约束)
│   ├── app.js                 #   主逻辑 — 状态管理/事件驱动/标签页管理/弹窗构建/发送/主题
│   ├── decoder.js             #   编解码 (Hex、renderSegments 分段渲染)
│   ├── history.js             #   历史渲染缓存 (pageEl 访问活动标签页元素，ts-text/ts-stat 分色)
│   ├── i18n.js                #   多语言系统 (t()、外部 i18n、_i18nFallback)
│   ├── icons.js               #   SVG 图标库 (icon()、_populateIcons，Go embed 加载)
│   ├── statusbar.js           #   状态栏组件 (StatusDaemon/Clients 靠左、Encoding/Stats 靠右)
│   ├── tabpage.js             #   TabPage + 欢迎页 — 每标签页独立 DOM 子树 + createWelcomePage()
│   ├── settingspage.js        #   设置页组件 (buildSettingsPage + createSettingsOverlay + _buildColorRow + _renderColorPreview)
│   ├── color-picker.js        #   调色板 Web Component (Shadow DOM, RGBA 滑块, EyeDropper)
│   ├── color-picker.html      #   调色板 Shadow DOM 模板
│   └── wailsjs/               #   Wails 自动生成的 JS 绑定
│
├── assets/                    # 图标资源 (ICO 生成)
├── build/                     # 构建产物
│   └── bin/                   #   可执行文件
│       ├── i18n/              #   外部 i18n 翻译文件 (exe 同目录)
│       └── themes/            #   外部主题 (颜色+图标)
├── go.mod / go.sum            # Go 模块定义
├── README.md                  # 项目说明
├── REQUIREMENTS.md            # 需求文档
├── BUILD.md                   # 本文档
├── LAYOUT-RULES.md            # 布局约束文档
└── 框架文档.md                 # 架构设计文档
```

## 开发

```bash
go run ./daemon/                      # 守护进程
go run ./cmd/serial-cli/ status       # CLI
go run ./cmd/serial-mcp/              # MCP
wails dev                             # GUI 热重载
```

## 图标

内置图标位于 `config/icons/system/`（19 个 24×24 SVG + icons.json），编译时 `//go:embed` 嵌入。外部图标主题放在 `themes/icons/<主题名>/`（文件夹 = 主题，内含 icons.json + 平铺 SVG）。

Windows 可执行文件图标（ICO）：
```bash
go install github.com/tc-hib/go-winres@latest
cd daemon && go-winres make && cd ../..
```

## 注意事项

- GUI 必须 `wails build`，`go build .` 不支持 WebView2 集成
- 守护进程注册管道 `\\.\pipe\serial-tool-daemon`，字节模式 + 同步 I/O
- 3 管道架构：持久客户端额外创建 `\\.\pipe\st-{clientId}-resp`（响应）和 `\\.\pipe\st-{clientId}-sub`（事件推送）
- 管道超时：`WaitNamedPipeW` 5s，IPC Call 10s，串口打开 8s，写入 3s
- 守护进程 `--silent` 参数后台静默启动
- 心跳 ping 每 5s 一次（静默不记日志），3 次失败触发断连回调（防御性兜底）
- 管道读端即时断连检测：`readSubLoop` / `readRespLoop` 检测到 `ERROR_BROKEN_PIPE` 立即触发 `OnDisconnect`
- 字节解码由 Go 端 per-tab goroutine 完成（`decode/decode.go`），产 `[]Segment` 分段结构；JS `renderSegments()` 纯 DOM 渲染
- 编码切换时 `BatchDecodeForTab` 批量重解码所有缓存 hex 条目
- 状态栏由 `frontend/statusbar.js` 组件化：StatusDaemon / StatusClients / StatusEncoding / StatusStats，通过 `_statusBar` API 更新
- 设置页通过菜单「设置 → 软件设置」打开，左侧导航（编码与显示/外观/存储与行为/语言），右侧气泡卡片布局
- 主题/语言气泡底部"创建示例"按键通过 Go 后端 `CreateExampleFiles` 创建示例文件夹和文件
- 设置页全部文字已 i18n 化，新增 31 个 `settings.*` 翻译键
- 所有可执行文件放在同一目录下，GUI 通过 `os.Executable()` 自动查找 `serial-daemon.exe`
- 设备探测配置文件 `probe.toml` 放在 daemon 可执行文件同目录下（开发模式放 `config/probe.toml`）
- GUI 设置文件 `settings.ini`（INI 格式），与 `serial-gui.exe` 同目录
- 外部 i18n 目录 `build/bin/i18n/` 与 `serial-gui.exe` 同目录，文件名格式 `{lang}.json`，详见 README
- 外部主题目录 `build/bin/themes/colors/`（颜色主题 .json）和 `build/bin/themes/icons/<名称>/`（图标主题文件夹，含 icons.json + .svg）
- 图标主题文件夹中 `.svg` 文件名 = 内置图标键名，覆盖对应内置图标
- i18n 文件 `_font` 字段指定首选字体（如 `"SimSun", "宋体", serif`），系统无此字体时回退默认
- 内置语言字体：zh=微软雅黑, zh-Hant=微軟正黑體, ja=Yu Gothic UI, ko=Malgun Gothic, ar=Noto Naskh Arabic, 其余=Segoe UI
- 版本号统一在 `version/version.go`，改一处全部同步

## MCP 配置

```json
{
  "mcpServers": {
    "serial-tool": {
      "command": "path\\to\\serial-mcp.exe"
    }
  }
}
```

28 个工具：`serial_start_daemon`, `serial_list_ports`, `serial_refresh_ports`, `serial_create`, `serial_open`, `serial_connect`, `serial_disconnect`, `serial_close`, `serial_switch`, `serial_forward_create`, `serial_set_mode`, `serial_declare`, `serial_send`, `serial_sessions`, `serial_history`, `serial_status`, `serial_monitor`, `serial_shutdown`, `serial_stats`, `serial_port_watch`, `serial_autosend_start`, `serial_autosend_stop`, `serial_autosend_status`, `serial_sendqueue`, `serial_probe_ports`, `serial_multistr_save`, `serial_multistr_load`, `serial_multistr_status`
