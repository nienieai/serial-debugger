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
wails build -devtools   # GUI
```

详细构建说明见 [BUILD.md](BUILD.md)。

## 翻译贡献

多语言翻译（`config/i18n/`，9 种语言）由 AI 辅助生成，可能存在不准确之处。
- 修正翻译：直接修改 `config/i18n/{lang}.json` 并提交 PR
- 新增语言：参考 `config/i18n/zh.json` 创建新文件，并在 `config/i18n.go` 中登记

## 行为准则

- 保持友善与专业，尊重不同背景的贡献者
- 技术讨论对事不对人
