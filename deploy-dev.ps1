<#
.SYNOPSIS
    把构建产物部署到开发测试目录，并校验三样东西是否同步。

.DESCRIPTION
    部署目录光换 exe 是不够的，这个坑已经踩过两次：

      1) GUI 是 hotreload 构建，前端从 <exe>\frontend\ 读盘，不读内嵌资源。
         只拷 exe = 新后端配旧前端，表现为"改了没反应"。
      2) 语言包 / 图标 / 主题的查找顺序是
             resources-system  →  resources  →  exe 内嵌 embed
         resources-system 是"内置资源层"，优先级最高。不同步它就永远读到旧副本，
         表现为"语言包里明明有这个词，界面上却是空的"。

    所以这个脚本一次拷三样：4 个 exe、frontend\、config\resources\ → resources-system\。
    resources\（外置层，用户自己的语言包/主题）脚本不碰。

    拷完做逐文件字节数比对 + 文件数比对，不一致直接非 0 退出，别让"好像部署了"
    蒙混过去。

.PARAMETER Target
    目标目录。默认 <仓库>\..\串口调试工具\0.8.3-dev。

.PARAMETER Build
    先重新编译再部署：
      go build -ldflags="-s -w"  → daemon / cli / mcp
      .\build.ps1                → GUI（wails build -devtools -tags hotreload）

.EXAMPLE
    .\deploy-dev.ps1
    只同步产物（最快，改完前端/语言包用这个）

.EXAMPLE
    .\deploy-dev.ps1 -Build
    重编译 + 部署（改了 Go 代码用这个）
#>
param(
    [string]$Target = '',
    [switch]$Build
)

$ErrorActionPreference = 'Stop'
$repo = $PSScriptRoot
if (-not $repo) { $repo = (Get-Location).Path }

if (-not $Target) {
    $Target = Join-Path (Split-Path $repo -Parent) '串口调试工具\0.8.3-dev'
}
$Target = [System.IO.Path]::GetFullPath($Target)
if (-not (Test-Path -LiteralPath $Target)) {
    throw "目标目录不存在: $Target（用 -Target 指定）"
}

$bin     = Join-Path $repo 'build\bin'
$frontend = Join-Path $repo 'frontend'
$resSrc  = Join-Path $repo 'config\resources'

Write-Host "目标目录: $Target" -ForegroundColor Cyan

# ---- 0. 别在程序运行时覆盖 ----
$running = Get-Process -Name serial-gui-dev, serial-gui, serial-daemon, serial-cli, serial-mcp -ErrorAction SilentlyContinue |
    Where-Object { $_.Path -and $_.Path.StartsWith($Target, [StringComparison]::OrdinalIgnoreCase) }
if ($running) {
    Write-Host ("这些进程还在跑，先关掉再部署: " + (($running | ForEach-Object { "$($_.ProcessName)($($_.Id))" }) -join ', ')) -ForegroundColor Red
    Write-Host "（覆盖运行中的 exe 会失败；就算覆盖成功，跑的还是老代码）" -ForegroundColor DarkGray
    exit 1
}

# ---- 1. 编译（可选）----
if ($Build) {
    Write-Host "[1/5] 编译 daemon / cli / mcp ..." -ForegroundColor Cyan
    Push-Location $repo
    try {
        go build -ldflags="-s -w" -o build/bin/serial-daemon.exe ./daemon/
        if ($LASTEXITCODE -ne 0) { throw 'serial-daemon 编译失败' }
        go build -ldflags="-s -w" -o build/bin/serial-cli.exe ./cmd/serial-cli/
        if ($LASTEXITCODE -ne 0) { throw 'serial-cli 编译失败' }
        go build -ldflags="-s -w" -o build/bin/serial-mcp.exe ./cmd/serial-mcp/
        if ($LASTEXITCODE -ne 0) { throw 'serial-mcp 编译失败' }
    } finally {
        Pop-Location
    }
    Write-Host "[1/5] 编译 GUI（hotreload 开发构建）..." -ForegroundColor Cyan
    & (Join-Path $repo 'build.ps1')
    if ($LASTEXITCODE -ne 0) { throw 'GUI 编译失败' }
}

# ---- 2. exe ----
Write-Host "[2/5] 拷贝 exe ..." -ForegroundColor Cyan
$exeMap = @(
    @{ Src = 'serial-gui.exe';    Dst = 'serial-gui-dev.exe' },
    @{ Src = 'serial-cli.exe';    Dst = 'serial-cli.exe' },
    @{ Src = 'serial-daemon.exe'; Dst = 'serial-daemon.exe' },
    @{ Src = 'serial-mcp.exe';    Dst = 'serial-mcp.exe' }
)
$copiedExes = @()
foreach ($m in $exeMap) {
    $s = Join-Path $bin $m.Src
    if (-not (Test-Path -LiteralPath $s)) {
        Write-Host ("      跳过 {0}（还没构建）" -f $m.Src) -ForegroundColor Yellow
        continue
    }
    $d = Join-Path $Target $m.Dst
    Copy-Item -LiteralPath $s -Destination $d -Force
    $copiedExes += [pscustomobject]@{ Name = $m.Dst; Bytes = (Get-Item -LiteralPath $d).Length }
}

# 把一个源目录同步到目标目录：覆盖 + 删掉目标里多出来的文件（只删这个子树内）
function Sync-Tree {
    param([string]$Src, [string]$Dst, [string]$Label)
    if (-not (Test-Path -LiteralPath $Src)) { throw "源目录不存在: $Src" }
    New-Item -ItemType Directory -Path $Dst -Force | Out-Null

    $srcFiles = @(Get-ChildItem -LiteralPath $Src -Recurse -File)
    foreach ($f in $srcFiles) {
        $rel = $f.FullName.Substring($Src.Length).TrimStart('\')
        $to = Join-Path $Dst $rel
        $parent = Split-Path $to -Parent
        if (-not (Test-Path -LiteralPath $parent)) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }
        Copy-Item -LiteralPath $f.FullName -Destination $to -Force
    }

    $guard = $Dst.TrimEnd('\') + '\'
    $removed = 0
    foreach ($f in @(Get-ChildItem -LiteralPath $Dst -Recurse -File)) {
        $rel = $f.FullName.Substring($Dst.Length).TrimStart('\')
        if (Test-Path -LiteralPath (Join-Path $Src $rel)) { continue }
        if (-not $f.FullName.StartsWith($guard, [StringComparison]::OrdinalIgnoreCase)) {
            throw "拒绝删除子树外的文件: $($f.FullName)"
        }
        Remove-Item -LiteralPath $f.FullName -Force
        $removed++
    }
    $bytes = ($srcFiles | Measure-Object Length -Sum).Sum
    Write-Host ("      {0}: 覆盖 {1} 个文件 / {2:N0} B，删除多余 {3} 个" -f $Label, $srcFiles.Count, $bytes, $removed)
    return [pscustomobject]@{ Label = $Label; Files = $srcFiles.Count; Bytes = $bytes }
}

Write-Host "[3/5] 拷贝 frontend（hotreload 直接读这里）..." -ForegroundColor Cyan
$feStat = Sync-Tree -Src $frontend -Dst (Join-Path $Target 'frontend') -Label 'frontend\'

Write-Host "[4/5] 拷贝 config\resources → resources-system（内置层，优先级最高）..." -ForegroundColor Cyan
$resStat = Sync-Tree -Src $resSrc -Dst (Join-Path $Target 'resources-system') -Label 'resources-system\'

# ---- 5. 校验 ----
Write-Host "[5/5] 校验 ..." -ForegroundColor Cyan
$script:problems = New-Object System.Collections.Generic.List[string]

function Compare-Tree {
    param([string]$Src, [string]$Dst, [string]$Label)
    $srcFiles = @(Get-ChildItem -LiteralPath $Src -Recurse -File)
    foreach ($f in $srcFiles) {
        $rel = $f.FullName.Substring($Src.Length).TrimStart('\')
        $to = Join-Path $Dst $rel
        if (-not (Test-Path -LiteralPath $to)) { $script:problems.Add("$Label 缺文件 $rel"); continue }
        if ((Get-Item -LiteralPath $to).Length -ne $f.Length) { $script:problems.Add("$Label 大小不一致 $rel") }
    }
    $dstCount = @(Get-ChildItem -LiteralPath $Dst -Recurse -File).Count
    if ($dstCount -ne $srcFiles.Count) {
        $script:problems.Add("$Label 文件数 $dstCount <> 源 $($srcFiles.Count)")
    }
}

Compare-Tree -Src $frontend -Dst (Join-Path $Target 'frontend') -Label 'frontend\'
Compare-Tree -Src $resSrc   -Dst (Join-Path $Target 'resources-system') -Label 'resources-system\'
foreach ($m in $exeMap) {
    $s = Join-Path $bin $m.Src
    $d = Join-Path $Target $m.Dst
    if (-not (Test-Path -LiteralPath $s)) { continue }
    if (-not (Test-Path -LiteralPath $d)) { $script:problems.Add("$($m.Dst) 不存在"); continue }
    if ((Get-Item -LiteralPath $s).Length -ne (Get-Item -LiteralPath $d).Length) {
        $script:problems.Add("$($m.Dst) 大小与 build\bin 不一致")
    }
}

Write-Host ""
Write-Host "部署结果: $Target" -ForegroundColor Cyan
foreach ($e in $copiedExes) { Write-Host ("  {0,-20} {1,12:N0} B" -f $e.Name, $e.Bytes) }
Write-Host ("  {0,-20} {1,12:N0} 个文件  {2:N0} B" -f $feStat.Label, $feStat.Files, $feStat.Bytes)
Write-Host ("  {0,-20} {1,12:N0} 个文件  {2:N0} B" -f $resStat.Label, $resStat.Files, $resStat.Bytes)

if ($script:problems.Count -gt 0) {
    Write-Host ""
    Write-Host "校验失败:" -ForegroundColor Red
    foreach ($p in $script:problems) { Write-Host "  - $p" -ForegroundColor Red }
    exit 1
}
Write-Host ""
Write-Host "校验通过：exe / frontend / resources-system 都与仓库一致" -ForegroundColor Green
Write-Host "提醒：i18n 只在启动和切换语言时加载，改过语言包要重启 GUI。" -ForegroundColor Yellow
exit 0
