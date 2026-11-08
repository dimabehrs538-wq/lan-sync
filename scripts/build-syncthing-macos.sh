#!/usr/bin/env bash
# macOS 版 Syncthing：用官方 v2.1.6 源码包（按官方 sha256sum.txt.asc 校验）以 CGO_ENABLED=0 编译，放到 app/src-tauri/binaries。
# 原因：官方 syncthing-macos-amd64-v2.1.6 二进制声明的最低系统为 macOS 26（LC_BUILD_VERSION minos=26.0），
#       在 macOS 12–15 的 Intel Mac 上无法启动；arm64 与 amd64 统一从同一份源码编译，最低系统由 Go 版本决定（Go 1.26 = macOS 12）。
# 与 Windows / 手机端使用的是同一 Syncthing 版本（v2.1.6），同步协议一致。
# 用法: scripts/build-syncthing-macos.sh <arm64|amd64> [版本]
set -euo pipefail
ARCH="${1:?用法: build-syncthing-macos.sh <arm64|amd64> [版本]}"
VER="${2:-v2.1.6}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/app/src-tauri/binaries"; mkdir -p "$OUT"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
BASE="https://github.com/syncthing/syncthing/releases/download/$VER"
FILE="syncthing-source-$VER.tar.gz"
sha256() { if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | awk '{print $1}'; }
echo "下载 $FILE ..."
curl -fsSL -o "$TMP/$FILE" "$BASE/$FILE"
curl -fsSL -o "$TMP/sha256sum.txt.asc" "$BASE/sha256sum.txt.asc"
EXPECTED="$(grep " $FILE\$" "$TMP/sha256sum.txt.asc" | awk '{print $1}')"
ACTUAL="$(sha256 "$TMP/$FILE")"
[ -n "$EXPECTED" ] && [ "$EXPECTED" = "$ACTUAL" ] || { echo "sha256 校验失败"; exit 1; }
echo "sha256 校验通过：$ACTUAL"
tar xzf "$TMP/$FILE" -C "$TMP"
cd "$TMP/syncthing"
[ "$(cat RELEASE)" = "$VER" ] || { echo "源码版本 $(cat RELEASE) 与 $VER 不一致"; exit 1; }
# 使用源码自带的 vendor 目录离线编译；noupgrade：禁用自升级（内核另外设置 STNOUPGRADE=1）
CGO_ENABLED=0 go run build.go -goos darwin -goarch "$ARCH" -no-upgrade build syncthing
TRIPLE="$( [ "$ARCH" = arm64 ] && echo aarch64 || echo x86_64 )-apple-darwin"
cp syncthing "$OUT/syncthing-$TRIPLE"
cp LICENSE "$OUT/SYNCTHING-LICENSE.txt"
chmod +x "$OUT/syncthing-$TRIPLE"
echo "已生成: $OUT/syncthing-$TRIPLE（$(go version | awk '{print $3}')）"
