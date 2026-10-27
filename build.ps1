# Build script: version comes from wails.json info.productVersion only.
#
#   .\build.ps1              # dev build (devtools + hotreload)
#   .\build.ps1 -Release     # release build
#
# Injects the version into both the exe resources (wails.json) and
# Go's version.Version (ldflags) so the two cannot drift.
param(
    [switch]$Release
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$ver = (Get-Content wails.json -Raw -Encoding UTF8 | ConvertFrom-Json).info.productVersion
if (-not $ver) { throw 'info.productVersion missing in wails.json' }
Write-Host "version: $ver" -ForegroundColor Cyan

$ldflags = "-X github.com/nienieai/serial-debugger/version.Version=$ver"

if ($Release) {
    $wailsArgs = @('build', '-ldflags', $ldflags)
} else {
    $wailsArgs = @('build', '-devtools', '-tags', 'hotreload', '-ldflags', $ldflags)
}

& wails @wailsArgs
if ($LASTEXITCODE -ne 0) { throw "wails build failed (exit $LASTEXITCODE)" }

$out = Join-Path $PSScriptRoot 'build\bin\serial-gui.exe'
if (-not (Test-Path $out)) { throw "artifact missing: $out" }
Write-Host "artifact: $out" -ForegroundColor Green
(Get-Item $out) | Select-Object Name, Length, LastWriteTime | Format-List
