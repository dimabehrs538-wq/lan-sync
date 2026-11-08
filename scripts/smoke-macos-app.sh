#!/usr/bin/env bash
# macOS CI 运行时冒烟测试（在 GitHub macOS runner 上执行，不等于真机界面验收）：
#  1. 直接运行 .app 内的 teamsyncd sidecar：拉起内置 syncthing，本地 API 健康，SIGTERM 后 syncthing 一并退出
#  2. 孤儿进程回收：强杀 teamsyncd 后 syncthing 被 launchd 收养，再次启动内核时会先结束旧 syncthing 再正常启动
#  3. 启动 TeamSync.app 主程序：自动拉起内核与 syncthing；通过 Apple 事件退出（等同 ⌘Q）后两者都被清理
# 用法: scripts/smoke-macos-app.sh <TeamSync.app> <arm64|x86_64>
set -euo pipefail
APP="$(cd "$1" && pwd)"; ARCH="$2"
RUN=(arch "-$ARCH")
M="$APP/Contents/MacOS"; T="$M/teamsyncd"; ST="$M/syncthing"
W="$(mktemp -d)"
fail(){ echo "✘ $*"; for f in "$W"/*.log "$W"/node/*.log "$HOME/Library/Application Support/TeamSync"/*.log; do [ -f "$f" ] || continue; echo "--- $f"; tail -n 40 "$f"; done
  echo "--- 进程"; ps -axo pid,ppid,stat,comm | grep -E 'syncthing|teamsyncd|TeamSync' | grep -v grep || true; exit 1; }
ok(){ echo "✔ $*"; }
alive(){ kill -0 "$1" 2>/dev/null; }
st_pid(){ pgrep -f "^$ST serve .*--home=$1/st" | head -1 || true; }
st_pids(){ pgrep -f "^$ST serve .*--home=$1/st" || true; }
no_st(){ for i in $(seq 1 20); do [ -z "$(pgrep -f "^$ST serve" || true)" ] && return 0; sleep 0.5; done; return 1; } # 监控进程与工作进程都已退出
wait_status(){ # home
  for i in $(seq 1 90); do
    if out="$("${RUN[@]}" "$T" status --home "$1" 2>/dev/null)" && echo "$out" | grep -q '"engine": "running"' && echo "$out" | grep -q '"myID"'; then echo "$out"; return 0; fi
    sleep 1
  done; return 1; }
start(){ # home port
  "${RUN[@]}" "$T" serve --home "$1" --folders-dir "$1-base" --listen "tcp://127.0.0.1:$2" --syncthing "$ST" >>"$W/$(basename "$1").log" 2>&1 &
  echo $!; }

# 1. sidecar
H="$W/node"; P=$(start "$H" 22611)
S="$(wait_status "$H")" || fail "内核未就绪"
echo "$S" | grep -q '"syncthingVersion": "v2.1.6"' || fail "syncthing 版本：$S"
SP="$(st_pid "$H")"; [ -n "$SP" ] || fail "找不到 syncthing 子进程"
[ "$(lipo -archs "$T")" = "$ARCH" ] && ok "teamsyncd（${ARCH}）启动内置 syncthing v2.1.6，本地 API 正常：$(echo "$S" | grep -o '"myID": "[A-Z0-9-]*"' | cut -c1-30)…"
kill -TERM "$P"; for i in $(seq 1 30); do alive "$P" || break; sleep 0.5; done
alive "$P" && fail "teamsyncd 未退出"
no_st || fail "teamsyncd 退出后 syncthing 仍在运行"
[ ! -f "$H/daemon.json" ] || fail "daemon.json 未清理"
ok "SIGTERM：teamsyncd 优雅退出，syncthing 一并退出，daemon.json 已清理"

# 2. 孤儿回收（Syncthing v2 为“监控进程 + 工作进程”两层，两者都要被结束）
P=$(start "$H" 22611); wait_status "$H" >/dev/null || fail "内核二次启动失败"
OLD="$(st_pids "$H" | tr '\n' ' ')"; [ -n "$OLD" ] || fail "找不到 syncthing 进程"
kill -KILL "$P"; sleep 2
LEFT=""; for p in $OLD; do alive "$p" && LEFT="$LEFT $p"; done
for p in $LEFT; do [ "$(ps -o ppid= -p "$p" | tr -d ' ')" = 1 ] || fail "syncthing（PID ${p}）未被 launchd 收养"; done
[ -n "$LEFT" ] || echo "（强杀内核后 syncthing 已因输出管道断开自行退出）"
rm -f "$H/daemon.json"
P=$(start "$H" 22611); wait_status "$H" >/dev/null || fail "强杀后重新启动内核失败"
for p in $LEFT; do alive "$p" && fail "旧的孤儿 syncthing（PID ${p}）未被结束"; done
NSP="$(st_pid "$H")"; [ -n "$NSP" ] || fail "新 syncthing 未启动"
for p in $OLD; do [ "$p" != "$NSP" ] || fail "新 syncthing PID 与旧进程相同"; done
if [ -n "$LEFT" ]; then ok "孤儿回收：强杀内核后残留的 syncthing（PID${LEFT}）在下次启动时被结束，新实例（PID ${NSP}）正常"
else ok "强杀内核后重新启动正常（新 syncthing PID ${NSP}）"; fi
kill -TERM "$P"; for i in $(seq 1 30); do alive "$P" || break; sleep 0.5; done; no_st || fail "syncthing 未退出"

# 3. 主程序
HOME_DIR="$HOME/Library/Application Support/TeamSync"
rm -f "$HOME_DIR/daemon.json"
EXE="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$APP/Contents/Info.plist")"
"${RUN[@]}" "$M/$EXE" >"$W/app.log" 2>&1 & AP=$!
S="$(wait_status "$HOME_DIR")" || fail "TeamSync.app 启动后内核未就绪"
CP="$(pgrep -f "^$T serve" | head -1)"; SP="$(pgrep -f "^$ST serve" | head -1)"
[ -n "$CP" ] && [ -n "$SP" ] || fail "未找到 teamsyncd / syncthing 进程"
[ "$(ps -o ppid= -p "$CP" | tr -d ' ')" = "$AP" ] || fail "teamsyncd 不是由 TeamSync.app 启动"
ok "TeamSync.app 启动：拉起 teamsyncd（PID ${CP}）与 syncthing（PID ${SP}），内核 API 正常"
HOW="Apple 事件 quit，等同 ⌘Q"
osascript -e 'tell application id "com.teamsync.desktop" to quit' >/dev/null 2>&1 || { HOW="SIGTERM（osascript 不可用）"; kill -TERM "$AP"; }
for i in $(seq 1 40); do alive "$AP" || alive "$CP" || alive "$SP" || break; sleep 0.5; done
alive "$AP" && fail "TeamSync.app 未退出"
alive "$CP" && fail "退出后 teamsyncd 仍在运行"
no_st || fail "退出后 syncthing 仍在运行"
ok "退出 TeamSync.app（${HOW}）后 teamsyncd 与 syncthing 均已清理"
rm -rf "$W"
