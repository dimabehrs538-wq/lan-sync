# Windows：编译 Go 内核为 sidecar（teamsyncd-x86_64-pc-windows-msvc.exe）
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$out = Join-Path $root "app\src-tauri\binaries"
New-Item -ItemType Directory -Force $out | Out-Null
Push-Location (Join-Path $root "core")
$env:CGO_ENABLED = "0"; $env:GOOS = "windows"; $env:GOARCH = "amd64"
go build -trimpath -ldflags "-s -w -H windowsgui" -o "$out\teamsyncd-x86_64-pc-windows-msvc.exe" ./cmd/teamsyncd
Pop-Location
Write-Host "已生成 $out\teamsyncd-x86_64-pc-windows-msvc.exe"
