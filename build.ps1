# Build script: version comes from wails.json info.productVersion only.
#
#   .\build.ps1              # dev build（hotreload：前端从磁盘读、保存即刷新）
#   .\build.ps1 -Release     # release build（前端嵌进 exe，可单文件分发）
#
# Wails v3 改用 Taskfile 构建（`wails3 build` = `wails3 task build`），这里只做两件事：
#   1. 把 wails.json 的版本号读出来，作为 APP_VERSION 传给 Taskfile
#   2. 同一份版本号既进 ldflags（程序内版本/窗口标题），也进 --file-version
#      （exe 属性），两处不会漂移
param(
    [switch]$Release
)

Set-Location $PSScriptRoot

$ver = (Get-Content wails.json -Raw -Encoding UTF8 | ConvertFrom-Json).info.productVersion
if (-not $ver) { throw 'info.productVersion missing in wails.json' }
Write-Host "version: $ver" -ForegroundColor Cyan

$ErrorActionPreference = 'Continue'
if ($Release) {
    & wails3 build APP_VERSION=$ver
} else {
    # hotreload 只影响 uiAssets（磁盘 vs 嵌入），assetserver 仍走 production 版，
    # 两种构建除了这一处之外行为一致。
    #
    # devtools：production 构建默认把 WebView2 的开发者工具关掉
    # （webview_window_windows_production.go 里 openDevTools 是空实现、
    # AreDevToolsEnabled(false)），「帮助 → 开发人员工具」点了没反应。
    # 开发构建带上这个 tag 才能开（release 构建不加，保持禁用）。
    & wails3 build APP_VERSION=$ver EXTRA_TAGS=hotreload,devtools
}
$code = $LASTEXITCODE
$ErrorActionPreference = 'Stop'
if ($code -ne 0) { throw "wails3 build failed (exit $code)" }

$out = Join-Path $PSScriptRoot 'build\bin\serial-gui.exe'
if (-not (Test-Path $out)) { throw "artifact missing: $out" }
Write-Host "artifact: $out" -ForegroundColor Green
(Get-Item $out) | Select-Object Name, Length, LastWriteTime | Format-List
