# 虚窗日志视图（logview-engine 分支）进度与交接

> 分支：`feat/logview-engine`（从 `0.9.1` 拉出）
> 目标：把 `stlog-viewer` 的「虚窗 + 泳道滚动条」引擎并入主工程，替换**所有**显示面
> （`.stlog` 文件页 / 串口监视器 / 转发监视器），最后删掉 `history.js` 的窗口渲染。
> 本文件记录**已完成 / 未完成 / 怎么验**，接手照这个继续。

## 架构

- **Go 侧 `stlogview` 包**：文档模型。静态文件（扫页建 arena 索引）与实时会话
  （`Append` 增量索引、按字节上限裁头、`Prepend` 补历史）共用同一套
  「像素窗口 / 滚动条桶 / 选区复制」。
- **服务壳 `main.LogView`**（`logview.go`）：Wails 服务是全局单例，而一个进程里会有
  多个文档（每个标签页一个），所以按 id 分文档；`OpenLive(sessionID)` 额外维护
  「会话 → 文档」路由，`App.emitSession` 每帧喂一次。
- **前端 `frontend/logview.js` + `logview.css`**：只做三件事——按像素取窗口、
  把可见的几十行 `translate3d` 定位、画滚动条。行高与折行全在 Go 算。
- 行的 DOM 复用会话页的 `.ts-line` / `.data-line` / `.hex-*` 类，配色主题跟着走。

## 已完成（均有提交）

| 提交 | 内容 |
|---|---|
| `9e56e39` | Go 侧引擎：索引 / 格子度量 / 选区复制 / 滚动条桶 + 测试 |
| `ca5ede1` | `.stlog` 文件页换成虚窗：删分页与「加载更多」，拖选/复制/Hex↔文本/年份 |
| `0252f12` | 实时文档：增量追加、按 8 MB 裁头（绝对帧号不回收）、补历史、清空 |
| `d36105e` | 会话页挂引擎：贴底跟随、未读胶囊、清空、系统消息 |
| `b3b36a9` | 渲染修复（引擎单开 `.lv-mount`，不再与老渲染器抢 DOM）；**行首两条 5px 竖条**与**三条 5px 泳道**恢复原语义，未实现的两道按占位画 |
| `73296bf` | 顶部让位改成量 `.config-bar` 实际底边 |
| `0d61a11` | 底部让位改成每次 `layout()` 现量（挂载时标签页还没布局，`reserveBottom` 被算成 8px） |
| `d2a68bc` | 未读胶囊 / 滚动条轨道不再被选区抢指针（只接管 `.lv-view` 里的按下） |
| `bc9045a` | 打开即回放历史；裁头时锁定视口（`trimmedPx`）、滚动条缓慢上推，顶到缓冲起点时在顶部提示「历史已被覆盖」 |

### 已实测可用

- **文件页**：512 KB / 14,611 帧、52 MB / 1,584,391 帧；滚轮、翻页键、End、拖选、
  Ctrl+C（剪贴板逐行核对）、Hex↔文本、年份开关、轨道贴右。
- **会话页**：`daemon` + `serial-cli connect 1 COM4` + `serial-cli send`；
  文本渲染、时间戳 / 方向 / 来源角标 / 字节数、贴底跟随、三泳道与竖条占位可见。
- **未读胶囊**：翻上去后来新数据出现「↓ N 条新数据」，点一下回底、胶囊消失。
- **打开回放**：会话已有 100 帧时重开 GUI，页面直接显示这 100 帧（Home 到顶是第 1 帧）。
- **裁头锚定 + 覆盖提示**：临时把 `DefaultLiveBytes` 调到 64 KB 验证 ——
  翻上去后持续发帧，`offset` 随 `trimmedPx` 同步减小（画面锁住、滑块上推），
  顶到缓冲起点后内容冻结、顶部出现「↑ 更早的历史数据已被覆盖」。

## 未完成（按优先级）

1. **往回翻到顶后的分页加载**：打开时只回放最近一页（500 条），再往前的历史还没接
   `GetSessionHistory` 分页（游标 + `PrependFrames` + 前插后的锚定）。有 `historyFile`
   的会话，顶部提示已经写成「向上可加载磁盘历史」，但点了还没反应。
2. **窗口取行用的是上界**：`Window` 里 `lowerBound` 找第一个 `prefix >= contentY` 的
   行，`offset` 落在行中间时窗口会从下一行开始 —— 顶部出现最多一行高的空白并跳过
   当前帧（帧很高时很明显）。应改成取「最后一个 `prefix <= contentY`」的行。
3. **会话页的 Hex↔文本 / 清空**：代码已接（`setMode()`、`clear()`），但还没逐项实测。
4. **转发页（P1/P2）**：`TabPage` 已统一挂载，但转发页的双方向配色与三泳道口径没验；
   老的分方向字号按钮（字 / H）切换要接到 `view.setMode`。
5. **收尾**：删掉 `history.js` 的窗口渲染（`_flushAppends` / `renderHistoryLines` /
   `_trimCache` / 自绘滚动条 / 未读块），把搜索、统计、磁盘提示并进引擎，
   同步 README / ARCHITECTURE / TODO / CHANGELOG。
6. **Modbus 校验道**：本轮按用户要求不做（将来是「可自定义规则文件」的校验引擎，
   不是硬编码检查），泳道先占位。

## 怎么验

```powershell
# 起守护进程 + 建会话 + 灌数据
.\daemon.exe --silent
.\serial-cli.exe connect 1 COM4 115200
1..20 | % { .\serial-cli.exe send ("row-{0:D2}" -f $_) | Out-Null; Start-Sleep -Milliseconds 100 }

# 构建（前端热加载，改 JS/CSS 不用重编）
.\build.ps1
```

- 截图工具：`C:\Users\ns\AppData\Local\Temp\screencap.ps1`（先 `Stop-Process` 掉旧的
  `serial-gui` / `stlogviewer`，否则会拍到别人的窗口）。
- 日志：启动时把 stderr 重定向到文件，`App.Log` 会以 `[js]` 前缀落在里面；
  `logview.js` 把未捕获异常重定向到它（`[logview] JSERR …`）。

## 红线（用户明确要求）

- 行首两条竖条（读取指示）**必须保留**，读取记录未实现时留占位。
- 滚动条**三条泳道按原语义**：① 读取 ② 校验 ③ 落盘；未实现的两道留占位，
  **不要**改成语义别的道，也**不要**塞假数据。
- 没实现的功能留占位，不伪装成“已支持”。
