# 应用图标源文件

本目录存放 serial-tool 各可执行文件图标的**矢量源文件**（SVG）与 256×256 PNG 导出。

| 文件 | 对应产物 |
| --- | --- |
| `daemon.svg` / `daemon_256.png` | `serial-daemon.exe` |
| `gui.svg` / `gui_256.png` | `serial-gui.exe`（Wails，经 `build/windows/icon.ico`）|
| `cli.svg` / `cli_256.png` | `serial-cli.exe` |
| `mcp.svg` / `mcp_256.png` | `serial-mcp.exe` |
| `web.svg` / `web_256.png` | 网页版（预留，可用作 favicon）|
| `standalone.svg` / `standalone_256.png` | 独立软件（预留）|
| `_body-only.svg` | 仅主体（无徽章）的设计参考 |

## 设计说明

统一主体为 **DB9 串口接头的侧面剪影**：

```
线缆 → 阶梯尾管 → 两根长螺丝条夹着壳体（带防滑纹）→ 竖直法兰 → 方形金属对接面
                    螺丝穿过法兰，两侧只露出螺丝尖
```

- 螺丝条与壳体间保留细缝以保持部件可读；螺丝尖与金属对接面的左端为直角，与法兰干净合并
- 右下角白底圆徽章承载各形态标记：六边形（daemon）/ 窗口+波形（GUI）/ `>_`（CLI）/ 三节点（MCP）/ 地球（Web）/ 立方体（独立软件）
- 底色为上亮下暗的线性渐变，配色沿用项目既有色系（daemon 蓝 / CLI 绿 / GUI 琥珀），MCP 为紫色

## 重新生成 ICO

`assets/icon-*.ico` 是多尺寸容器（16/32/48/64/128/256）。修改图标后：

1. 编辑 `assets/icons/<key>.svg`
2. 导出 256×256 透明 PNG，覆盖 `<key>_256.png`
3. 生成多尺寸 ICO（需要 Pillow）：

   ```python
   from PIL import Image
   img = Image.open("assets/icons/daemon_256.png").convert("RGBA")
   img.save("assets/icon-daemon.ico",
            sizes=[(16, 16), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)])
   ```

4. 重新生成资源文件，否则改动的图标不会生效：

   ```bash
   cd cmd/serial-cli && go-winres make
   cd cmd/serial-mcp && go-winres make
   cd daemon         && go-winres make
   ```

   随后 GUI 需 `wails build` 才会用上新的 `build/windows/icon.ico`。

> **注**：原先的 `gen_icons.go` 已移除。它只能输出 32×32 纯色方块（占位图），
> 无法生成多尺寸 ICO —— Go 标准库不提供图像缩放。

## 资源引用位置

| 可执行文件 | 资源文件 | 引用配置 |
| --- | --- | --- |
| `serial-cli.exe` | `cmd/serial-cli/rsrc_windows_*.syso` | `cmd/serial-cli/winres/winres.json` → `assets/icon-cli.ico` |
| `serial-mcp.exe` | `cmd/serial-mcp/rsrc_windows_*.syso` | `cmd/serial-mcp/winres/winres.json` → `assets/icon-mcp.ico` |
| `serial-daemon.exe` | `daemon/rsrc_windows_*.syso` | `daemon/winres/winres.json` → `assets/icon-daemon.ico` |
| `serial-gui.exe` | 由 Wails 打包 | `build/windows/icon.ico`（= `icon-gui.ico`）|
