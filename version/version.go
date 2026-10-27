package version

// Version 是产品版本号，同时用于 GUI 标题、CLI 横幅、MCP ServerInfo
// 以及 IPC 的 daemon.info（客户端据此核对守护进程是否为同一次构建）。
//
// 唯一来源是 wails.json 的 info.productVersion：build.ps1 会把它通过
// -ldflags "-X ...Version=x.y.z" 注入进来，所以这里是构建时的默认值，
// 不是需要手改的地方（保留 var 而不是 const 才能被 ldflags 覆盖）。
var Version = "0.8.3"
