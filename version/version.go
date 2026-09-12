package version

// Version 是产品版本号，同时用于 GUI 标题、CLI 横幅、MCP ServerInfo
// 以及 IPC 的 daemon.info（客户端据此核对守护进程是否为同一次构建）。
const Version = "0.7.3"
