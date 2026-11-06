# 贡献指南

感谢您对 serial-debugger 的关注！欢迎通过以下方式参与贡献。

## 报告问题

- 使用 [GitHub Issues](https://github.com/nienieai/serial-debugger/issues) 提交
- 请提供：复现步骤、期望行为、实际行为、环境信息（操作系统、Go 版本、串口硬件型号等）
- 若涉及崩溃或异常，请附上 `serial-daemon` 的日志输出

## 提交代码

1. Fork 本仓库并创建特性分支：`git checkout -b feat/your-feature`
2. 修改代码，遵循以下规范：
   - Go 代码使用 `gofmt` / `go vet` 检查通过
   - 前端 JS/CSS 遵循 `o-` / `c-` / `t-` / `is-` 命名体系（见 [LAYOUT-RULES.md](LAYOUT-RULES.md)）
   - 新增功能需同步更新 [README.md](README.md) 与 [REQUIREMENTS.md](REQUIREMENTS.md)
3. 提交信息建议遵循 `<type>: <描述>` 格式（如 `feat:` / `fix:` / `docs:` / `refactor:`）
4. 发起 Pull Request，说明改动内容与测试情况

## 构建与测试

```bash
go build -ldflags="-s -w" -o build/bin/serial-daemon.exe ./daemon/
go build -ldflags="-s -w" -o build/bin/serial-cli.exe    ./cmd/serial-cli/
go build -ldflags="-s -w" -o build/bin/serial-mcp.exe    ./cmd/serial-mcp/
.\build.ps1             # GUI（Wails v3，内部走 wails3 build；Linux 用 go build -tags production）
```

自检（提交前本地跑一遍）：

```bash
gofmt -l .              # 应无输出
go vet ./...
go test ./...           # 单元测试；pipe / ringbuf 的部分用例带 //go:build windows，只在 Windows 编译
node --check frontend/app.js   # 改前端 JS 时逐个检查
```

详细构建说明见 [BUILD.md](BUILD.md)。

## 翻译贡献

多语言翻译（`config/resources/i18n/`，9 种语言，每包 593 个键）由 AI 辅助生成，可能存在不准确之处。

- **修正翻译**：直接改 `config/resources/i18n/{lang}.json` 并提交 PR。键集必须与 `zh.json` 完全一致（键名扁平、按字母序），增删键时 9 个包要一起改
- **新增外置语言**：把 `<lang>.json` 放到 `<exe>/resources/i18n/`，不用改代码也不用重新编译，界面会自动发现并追加到语言菜单
- **新增内置语言**：除 `config/resources/i18n/<lang>.json`（含 `_font`，RTL 语言还要 `_dir: "rtl"`）外，还要改前端四处：`frontend/index.html` 的 `menuLang*` 按钮、`frontend/i18n.js` 的 `builtinLangs`（L150）与 `updateLangMenu` 列表（L183）、`frontend/settingspage.js` 的语言下拉（L1012）与 `builtinLangs`（L1365）

## 行为准则

- 保持友善与专业，尊重不同背景的贡献者
- 技术讨论对事不对人
