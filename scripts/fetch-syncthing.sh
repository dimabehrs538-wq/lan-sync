#!/usr/bin/env bash
# 下载官方 Syncthing 二进制并放到 app/src-tauri/binaries（Tauri sidecar 命名规则）
# 用法: scripts/fetch-syncthing.sh [版本] [os] [arch]   例: scripts/fetch-syncthing.sh v2.1.6 windows amd64
set -euo pipefail
VER="${1:-v2.1.6}"
OS="${2:-$(uname -s | tr A-Z a-z)}"
ARCH="${3:-amd64}"
case "$OS" in darwin) OS=macos;; esac
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/app/src-tauri/binaries"; mkdir -p "$OUT"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
BASE="https://github.com/syncthing/syncthing/releases/download/$VER"
case "$OS" in
  windows) NAME="syncthing-windows-$ARCH-$VER"; FILE="$NAME.zip"; TRIPLE="x86_64-pc-windows-msvc"; EXE=".exe";;
  linux)   NAME="syncthing-linux-$ARCH-$VER";   FILE="$NAME.tar.gz"; TRIPLE="x86_64-unknown-linux-gnu"; EXE="";;
  macos)   NAME="syncthing-macos-$ARCH-$VER";   FILE="$NAME.zip"; TRIPLE="$( [ "$ARCH" = arm64 ] && echo aarch64 || echo x86_64 )-apple-darwin"; EXE="";;
  *) echo "不支持的系统: $OS"; exit 1;;
esac
[ "$ARCH" = arm64 ] && TRIPLE="${TRIPLE/x86_64/aarch64}"
echo "下载 $FILE ..."
curl -fsSL -o "$TMP/$FILE" "$BASE/$FILE"
curl -fsSL -o "$TMP/sha256sum.txt.asc" "$BASE/sha256sum.txt.asc"
EXPECTED="$(grep " $FILE\$" "$TMP/sha256sum.txt.asc" | awk '{print $1}')"
ACTUAL="$(sha256sum "$TMP/$FILE" | awk '{print $1}')"
[ -n "$EXPECTED" ] && [ "$EXPECTED" = "$ACTUAL" ] || { echo "sha256 校验失败"; exit 1; }
echo "sha256 校验通过"
cd "$TMP"
if [[ "$FILE" == *.zip ]]; then unzip -q "$FILE"; else tar xzf "$FILE"; fi
cp "$TMP/$NAME/syncthing$EXE" "$OUT/syncthing-$TRIPLE$EXE"
cp "$TMP/$NAME/LICENSE.txt" "$OUT/SYNCTHING-LICENSE.txt" 2>/dev/null || true
chmod +x "$OUT/syncthing-$TRIPLE$EXE"
echo "已放置: $OUT/syncthing-$TRIPLE$EXE"
