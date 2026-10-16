#!/usr/bin/env bash
# 编译 Go 内核并按 Tauri sidecar 命名放入 app/src-tauri/binaries
# 用法: scripts/build-core.sh [linux|windows|macos] [amd64|arm64]
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OS="${1:-$(uname -s | tr A-Z a-z)}"; ARCH="${2:-amd64}"
case "$OS" in darwin|macos) GOOS=darwin; T=apple-darwin;; windows) GOOS=windows; T=pc-windows-msvc;; *) GOOS=linux; T=unknown-linux-gnu;; esac
CPU=$([ "$ARCH" = arm64 ] && echo aarch64 || echo x86_64)
EXT=$([ "$GOOS" = windows ] && echo .exe || true)
LDF="-s -w"; [ "$GOOS" = windows ] && LDF="$LDF -H windowsgui"
mkdir -p "$ROOT/app/src-tauri/binaries"
cd "$ROOT/core" && CGO_ENABLED=0 GOOS=$GOOS GOARCH=$ARCH go build -trimpath -ldflags "$LDF" -o "$ROOT/app/src-tauri/binaries/teamsyncd-$CPU-$T$EXT" ./cmd/teamsyncd
echo "已生成 app/src-tauri/binaries/teamsyncd-$CPU-$T$EXT"
