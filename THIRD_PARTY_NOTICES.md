# 第三方依赖许可声明

本项目（serial-debugger）基于 [Apache License 2.0](LICENSE) 开源（版权与归属见 [NOTICE](NOTICE)）。

以下为**实际链接进发布二进制**的第三方 Go 模块及其许可证。本文件用于满足各许可证的
"二进制分发时保留版权声明"要求（BSD-2/BSD-3 第 2 条、MIT 的版权与许可声明等）。
完整许可证文本见各依赖模块目录下的 `LICENSE` / `COPYING`。

清单来源：对发布包内 4 个可执行文件执行 `go version -m`，取其 `dep` 行的并集
（Windows 四个 exe + Linux 包内的 `serial-gui`）。`go.mod` 中声明但未被任何二进制链接的
间接依赖不计入——它们不随本项目的产物分发。

## MIT

| 依赖 | 版本 |
|------|------|
| github.com/adrg/xdg | v0.5.3 |
| github.com/BurntSushi/toml | v1.6.0 |
| github.com/creack/goselect | v0.1.2 |
| github.com/go-ole/go-ole | v1.3.0 |
| github.com/wailsapp/wails/v3 | v3.0.0 |

## BSD-3-Clause

| 依赖 | 版本 |
|------|------|
| go.bug.st/serial | v1.6.4 |
| golang.org/x/sys | v0.48.0 |
| golang.org/x/text | v0.42.0 |

## BSD-2-Clause

| 依赖 | 版本 |
|------|------|
| github.com/godbus/dbus/v5 | v5.2.2 |

## 平台运行期依赖（不随本项目分发）

以下组件由操作系统或运行库提供，本项目**不打包、不修改**，故不列入上面的清单，但运行时必须具备：

| 平台 | 组件 | 许可 |
|---|---|---|
| Windows | WebView2 Runtime（GUI 渲染） | Microsoft 专有许可，随 Edge 分发 |
| Linux | GTK4 / WebKitGTK 6.0 / libsoup-3.0（GUI 渲染） | LGPL-2.1-or-later 等，随发行版分发 |
| Linux | `setfacl`（`acl` 包，`serial-cli fix-permissions` 优先使用） | GPL-2.0-or-later，随发行版分发 |

---

*本清单最后核对：2026-11-02。读取对象：0.9.1 开发构建的 Windows 四个 exe，以及 0.9.0 Linux 包内的 `serial-gui`（Linux 版比 Windows 版多链接 `godbus/dbus`）。依赖升级后请重新执行上述 `go version -m` 核对。*
