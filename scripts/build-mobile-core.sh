#!/usr/bin/env bash
# 编译移动端 Go 内核（core/mobile：进程内嵌入 Syncthing + 与桌面相同的本地 API）。
# 用法:
#   scripts/build-mobile-core.sh android [aarch64-linux-android ...]   需要 ANDROID_NDK_HOME（或 NDK_HOME）
#   scripts/build-mobile-core.sh ios [aarch64-apple-ios]               需要在 macOS + Xcode 上运行
# 产物: app/src-tauri/mobile-libs/<rust-target>/libteamsynccore.{so,a}
#      Android 还会复制到 app/src-tauri/gen/android/app/src/main/jniLibs/<abi>/（随 APK 打包）
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PLATFORM="${1:?android|ios}"; shift || true
ST_VERSION="${SYNCTHING_VERSION:-v2.1.6}"   # 必须与桌面捆绑的 syncthing 版本一致（scripts/fetch-syncthing.*）
LDFLAGS="-s -w -checklinkname=0 -X github.com/syncthing/syncthing/lib/build.Version=$ST_VERSION -X github.com/syncthing/syncthing/lib/build.User=teamsync -X github.com/syncthing/syncthing/lib/build.Host=teamsync"
TAGS="noupgrade,noassets"
OUT="$ROOT/app/src-tauri/mobile-libs"
cd "$ROOT/core/mobile"
case "$PLATFORM" in
android)
  NDK="${ANDROID_NDK_HOME:-${NDK_HOME:-}}"; [ -d "$NDK" ] || { echo "请设置 ANDROID_NDK_HOME"; exit 1; }
  HOST="$(uname -s | tr A-Z a-z)-x86_64"; BIN="$NDK/toolchains/llvm/prebuilt/$HOST/bin"
  TARGETS=("$@"); [ ${#TARGETS[@]} -gt 0 ] || TARGETS=(aarch64-linux-android)
  for T in "${TARGETS[@]}"; do
    case "$T" in
      aarch64-linux-android) GOARCH=arm64; GOARM=; ABI=arm64-v8a; CC="$BIN/aarch64-linux-android24-clang";;
      x86_64-linux-android)  GOARCH=amd64; GOARM=; ABI=x86_64;    CC="$BIN/x86_64-linux-android24-clang";;
      armv7-linux-androideabi) GOARCH=arm; GOARM=7; ABI=armeabi-v7a; CC="$BIN/armv7a-linux-androideabi24-clang";;
      *) echo "不支持的目标 $T"; exit 1;;
    esac
    mkdir -p "$OUT/$T"
    echo "编译 $T ($ABI) ..."
    CGO_ENABLED=1 GOOS=android GOARCH=$GOARCH GOARM=$GOARM CC="$CC" \
      go build -buildmode=c-shared -trimpath -tags "$TAGS" -ldflags "$LDFLAGS -extldflags=-Wl,-soname,libteamsynccore.so" \
      -o "$OUT/$T/libteamsynccore.so" ./cmd/libteamsync
    JNI="$ROOT/app/src-tauri/gen/android/app/src/main/jniLibs/$ABI"
    if [ -d "$ROOT/app/src-tauri/gen/android" ]; then mkdir -p "$JNI"; cp "$OUT/$T/libteamsynccore.so" "$JNI/"; fi
    ls -la "$OUT/$T/libteamsynccore.so"
  done;;
ios)
  [ "$(uname -s)" = Darwin ] || { echo "iOS 内核需要在 macOS 上编译"; exit 1; }
  TARGETS=("$@"); [ ${#TARGETS[@]} -gt 0 ] || TARGETS=(aarch64-apple-ios)
  for T in "${TARGETS[@]}"; do
    case "$T" in
      aarch64-apple-ios) SDK=iphoneos; GOARCH=arm64; EXTRA="-miphoneos-version-min=14.0";;
      aarch64-apple-ios-sim) SDK=iphonesimulator; GOARCH=arm64; EXTRA="-mios-simulator-version-min=14.0 -target arm64-apple-ios14.0-simulator";;
      *) echo "不支持的目标 $T"; exit 1;;
    esac
    SDKPATH="$(xcrun --sdk $SDK --show-sdk-path)"; CLANG="$(xcrun --sdk $SDK --find clang)"
    mkdir -p "$OUT/$T"
    echo "编译 $T ..."
    CGO_ENABLED=1 GOOS=ios GOARCH=$GOARCH CC="$CLANG" CXX="$(xcrun --sdk $SDK --find clang++)" \
      CGO_CFLAGS="-isysroot $SDKPATH -arch arm64 $EXTRA" CGO_LDFLAGS="-isysroot $SDKPATH -arch arm64 $EXTRA" \
      go build -buildmode=c-archive -trimpath -tags "$TAGS" -ldflags "$LDFLAGS" -o "$OUT/$T/libteamsynccore.a" ./cmd/libteamsync
    ls -la "$OUT/$T/libteamsynccore.a"
  done;;
*) echo "用法: $0 android|ios [目标...]"; exit 1;;
esac
