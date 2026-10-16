# Windows：下载官方 Syncthing 并放到 app\src-tauri\binaries（sidecar 命名）
# 用法: powershell -ExecutionPolicy Bypass -File scripts\fetch-syncthing.ps1 [-Version v2.1.6] [-Arch amd64]
param([string]$Version = "v2.1.6", [string]$Arch = "amd64")
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$out = Join-Path $root "app\src-tauri\binaries"
New-Item -ItemType Directory -Force $out | Out-Null
$name = "syncthing-windows-$Arch-$Version"
$base = "https://github.com/syncthing/syncthing/releases/download/$Version"
$tmp = Join-Path $env:TEMP ([guid]::NewGuid())
New-Item -ItemType Directory $tmp | Out-Null
Invoke-WebRequest "$base/$name.zip" -OutFile "$tmp\$name.zip"
Invoke-WebRequest "$base/sha256sum.txt.asc" -OutFile "$tmp\sha.txt"
$expected = (Select-String -Path "$tmp\sha.txt" -Pattern " $name.zip$").Line.Split(" ")[0]
$actual = (Get-FileHash "$tmp\$name.zip" -Algorithm SHA256).Hash.ToLower()
if ($expected -ne $actual) { throw "sha256 校验失败" }
Expand-Archive "$tmp\$name.zip" -DestinationPath $tmp
$triple = if ($Arch -eq "arm64") { "aarch64-pc-windows-msvc" } else { "x86_64-pc-windows-msvc" }
Copy-Item "$tmp\$name\syncthing.exe" "$out\syncthing-$triple.exe" -Force
Copy-Item "$tmp\$name\LICENSE.txt" "$out\SYNCTHING-LICENSE.txt" -Force
Remove-Item -Recurse -Force $tmp
Write-Host "已放置: $out\syncthing-$triple.exe"
