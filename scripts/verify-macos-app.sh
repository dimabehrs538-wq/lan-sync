#!/usr/bin/env bash
# 在 macOS 上校验 Tauri 构建出的 TeamSync.app / .dmg / 更新包（CI 中调用，也可在本机执行）。
# 用法: scripts/verify-macos-app.sh <bundle 目录> <arm64|x86_64> <版本>
# 只做结构、架构、最低系统、ad-hoc 代码签名与打包资源检查；更新包的 minisign 签名由 release 流程用公钥校验。
set -euo pipefail
B="$1"; ARCH="$2"; VER="$3"; MINOS="${MINOS:-12.0}"
APP="$B/macos/TeamSync.app"
fail(){ echo "✘ $*"; exit 1; }
ok(){ echo "✔ $*"; }
[ -d "$APP" ] || fail "缺少 $APP"
PL="$APP/Contents/Info.plist"
pl(){ /usr/libexec/PlistBuddy -c "Print :$1" "$PL"; }
[ "$(pl CFBundleIdentifier)" = com.teamsync.desktop ] || fail "CFBundleIdentifier=$(pl CFBundleIdentifier)"
[ "$(pl CFBundleShortVersionString)" = "$VER" ] || fail "版本 $(pl CFBundleShortVersionString) ≠ $VER"
[ "$(pl LSMinimumSystemVersion)" = "$MINOS" ] || fail "LSMinimumSystemVersion=$(pl LSMinimumSystemVersion)"
pl NSLocalNetworkUsageDescription >/dev/null || fail "缺少 NSLocalNetworkUsageDescription"
pl NSDocumentsFolderUsageDescription >/dev/null || fail "缺少 NSDocumentsFolderUsageDescription"
ok "Info.plist：com.teamsync.desktop ${VER}，最低 macOS ${MINOS}，本地网络 / 文件夹访问说明齐全"
EXE="$(pl CFBundleExecutable)"
verlte(){ [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -1)" = "$1" ]; }
for f in "$EXE" teamsyncd syncthing; do
  p="$APP/Contents/MacOS/$f"
  [ -x "$p" ] || fail "缺少可执行文件 Contents/MacOS/$f"
  a="$(lipo -archs "$p")"
  [ "$a" = "$ARCH" ] || fail "$f 架构为 ${a}，应为 $ARCH"
  m="$(otool -l "$p" | awk '/LC_BUILD_VERSION/{f=1} f&&/minos/{print $2; exit}')"
  [ -n "$m" ] && verlte "$m" "$MINOS" || fail "$f 最低系统 $m 高于 $MINOS"
  codesign -dv "$p" 2>&1 | grep -q "Signature=adhoc" || fail "$f 不是 ad-hoc 签名"
  ok "Contents/MacOS/${f}：Mach-O ${a}，minos ${m}，ad-hoc 签名"
done
[ "$(ls "$APP/Contents/MacOS" | wc -l | tr -d ' ')" = 3 ] || fail "Contents/MacOS 中有多余文件：$(ls "$APP/Contents/MacOS")"
"$APP/Contents/MacOS/syncthing" --version | grep -q "v2.1.6" || fail "syncthing 版本不是 v2.1.6"
otool -L "$APP/Contents/MacOS/syncthing" | grep -q CoreServices.framework || fail "syncthing 未链接 CoreServices：FSEvents 文件监视不可用（需 CGO_ENABLED=1 编译）"
ok "syncthing 已编译 FSEvents 文件监视（链接 CoreServices.framework）"
ok "内置 $("$APP/Contents/MacOS/syncthing" --version | head -1)"
[ -f "$APP/Contents/Resources/SYNCTHING-LICENSE.txt" ] || fail "缺少 Resources/SYNCTHING-LICENSE.txt"
[ -f "$APP/Contents/Resources/icon.icns" ] || fail "缺少 Resources/icon.icns"
ok "Resources：SYNCTHING-LICENSE.txt、icon.icns"
codesign --verify --deep --strict --verbose=2 "$APP" 2>&1 | tail -2
codesign -dv "$APP" 2>&1 | grep -E "^(Identifier|Format|Signature|TeamIdentifier)=" || true
codesign -dv "$APP" 2>&1 | grep -q "Signature=adhoc" || fail "TeamSync.app 不是 ad-hoc 签名"
ok "codesign --verify --deep --strict 通过（ad-hoc，无 Apple 开发者证书、未公证）"
echo "Gatekeeper 评估（预期因未公证而拒绝，仅供参考）：$(spctl -a -t exec -vv "$APP" 2>&1 | head -1 || true)"
# 更新包
TGZ="$(ls "$B"/macos/*.app.tar.gz)"; [ -f "$TGZ.sig" ] || fail "缺少 $TGZ.sig（未配置更新签名私钥？）"
tar -tzf "$TGZ" | grep -q "^TeamSync.app/Contents/MacOS/syncthing$" || fail "更新包结构不正确"
ok "更新包 $(basename "$TGZ") 与 .sig 存在，结构包含 TeamSync.app"
# DMG
DMG="$(ls "$B"/dmg/*.dmg)"
case "$DMG" in *"_${VER}_$( [ "$ARCH" = arm64 ] && echo aarch64 || echo x64 ).dmg") ;; *) fail "DMG 文件名 $DMG";; esac
hdiutil verify "$DMG" >/dev/null 2>&1 || fail "hdiutil verify 失败"
MNT="$(mktemp -d)"; hdiutil attach -nobrowse -readonly -mountpoint "$MNT" "$DMG" >/dev/null
trap 'hdiutil detach "$MNT" -quiet || true' EXIT
[ -d "$MNT/TeamSync.app" ] && [ -L "$MNT/Applications" ] || { ls -la "$MNT"; fail "DMG 内容不正确"; }
codesign --verify --deep --strict "$MNT/TeamSync.app" || fail "DMG 内 app 签名无效"
[ "$(lipo -archs "$MNT/TeamSync.app/Contents/MacOS/syncthing")" = "$ARCH" ] || fail "DMG 内 syncthing 架构"
ok "$(basename "$DMG")：hdiutil verify 通过，含 TeamSync.app 与「应用程序」快捷方式，签名有效"
