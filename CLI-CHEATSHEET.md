# serial-cli 命令速查表

`serial-cli.exe` 支持两种模式：

- **命令行模式**：`serial-cli <cmd>`，一次性执行后退出（CallOnce 临时管道）
- **交互模式**：不带参数启动 REPL，3 管道持久连接，事件实时推送，支持引号参数（`send "hello world"`）

## 命令一览（34 条，含 help）

| 类别 | 命令 |
|------|------|
| 守护进程 | `start` `check` `status` `shutdown` |
| 端口 | `ports` `refresh` `probe [ports...] [--config <path>]` |
| 进程 | `create` `declare` `open` `connect` `disconnect` `close` `switch` `setmode` `forward` |
| 数据 | `send [--hex]` `stats` `history` |
| 自动发送 | `autosend start/stop/status/interval` `sendqueue` |
| 多字符串 | `multistr save/load/reload/status` |
| 历史 | `history-files` `history-search` `history-enable` `history-status` `history-attach` `history-new` `history-detach` |
| 诊断 | `sessions` `monitor` `threads` `goroutines` |
| 其他 | `help` |

## 常用示例

### 守护进程生命周期

```bash
serial-cli start            # 启动守护进程（已运行则返回状态）
serial-cli check            # 三层检测：进程 + 管道 + IPC
serial-cli status           # 健康检查
serial-cli shutdown         # 优雅关闭（先断开全部串口）
```

### 端口与设备

```bash
serial-cli ports            # 可用串口列表（缓存）
serial-cli refresh          # 刷新串口列表
serial-cli probe            # 设备端口探测（TOML 规则识别设备类型）
serial-cli probe COM3 COM4  # 只探测指定端口
serial-cli probe --config my-rules.toml   # 自定义规则文件
```

### 进程与串口

```bash
serial-cli open COM4 9600           # 创建进程并打开串口
serial-cli create                   # 创建空闲进程（不打开串口）
serial-cli declare COM3 115200      # 声明端口配置（不打开串口，全客户端可见）
serial-cli connect 1 COM3 115200    # 空闲进程绑定串口
serial-cli switch 1 COM5 115200     # 切换串口（保留进程与历史）
serial-cli disconnect 1             # 断开（进程保留）
serial-cli close 1                  # 销毁进程（清空历史）
serial-cli forward COM3 COM4 115200 115200  # 双串口双向转发
serial-cli setmode 1 forward        # 切换进程模式（需 idle）
serial-cli sessions                 # 查看全部进程（idle + connected）
```

### 数据收发

```bash
serial-cli send "AT\r\n"            # 文本发送
serial-cli send 01030008000105C8 --hex   # Hex 发送（空格自动去除）
serial-cli history                  # 查看历史（毫秒时间戳 + Hex）
serial-cli stats                    # I/O 统计（速率/字节/错误）
```

### 自动发送与多字符串

```bash
serial-cli autosend start 1000 single     # 每 1000ms 发送一次
serial-cli autosend start 500 queue --loop  # 队列循环发送
serial-cli autosend interval 2000         # 运行中修改间隔
serial-cli autosend status                # 查看状态
serial-cli autosend stop                  # 停止
serial-cli sendqueue entries.json 1       # 从 JSON 文件写入多条到发送队列
serial-cli multistr save / load / reload / status   # 多字符串持久化与同步
```

### 历史记录文件

```bash
serial-cli history-files                 # 列出 history/*.log
serial-cli history-search 20260801_1.log "READY"  # 文件内搜索关键字
serial-cli history-enable false          # 关闭自动保存
serial-cli history-status                # 查看自动保存状态
serial-cli history-attach 1 20260801_1.log  # 为进程附加历史文件
serial-cli history-new 1                 # 新建历史文件
serial-cli history-detach 1              # 分离历史文件
```

### 诊断

```bash
serial-cli monitor [timeout]     # 实时监听事件（3 管道）
serial-cli threads               # 线程/会话详情
serial-cli goroutines            # Goroutine 调用栈
```

## 交互模式示例

```text
> start
> open COM14 115200
> send "hello world"
> history
> close
> exit
```

> 提示：`serial-cli help` 可随时查看分组帮助；命令行模式退出码 0 表示成功。
