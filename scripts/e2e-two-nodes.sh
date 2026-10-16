#!/usr/bin/env bash
# 端到端测试（邀请制）：本机启动三个内核实例 A/B/C。
#  1. 局域网发现 + 配对（A 添加 B、C，B、C 同意）
#  2. A 新建文件夹只共享给 B：B 收到邀请且不会自动创建；C 收不到
#  3. B 忽略邀请 -> 邀请消失；A 重新邀请 -> B 再次收到
#  4. B 接受到自定义路径 -> 双向同步
# 用法: scripts/e2e-two-nodes.sh   需要先 scripts/fetch-syncthing.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ST="$(ls "$ROOT"/app/src-tauri/binaries/syncthing-* | grep -v LICENSE | head -1)"
W="$(mktemp -d)"; T="$W/teamsyncd"
(cd "$ROOT/core" && go build -o "$T" ./cmd/teamsyncd)
PIDS=()
cleanup(){ kill "${PIDS[@]}" 2>/dev/null || true; wait 2>/dev/null || true; rm -rf "$W"; }
trap cleanup EXIT
port=22101
for n in A B C; do
  "$T" serve --home "$W/$n" --folders-dir "$W/$n-base" --listen "tcp://0.0.0.0:$port" --syncthing "$ST" >"$W/$n.log" 2>&1 & PIDS+=($!)
  port=$((port+1))
done
for n in A B C; do until [ -f "$W/$n/daemon.json" ]; do sleep 0.5; done; done
id(){ "$T" status --home "$W/$1" | sed -n 's/.*"myID": "\(.*\)".*/\1/p'; }
IA=$(id A); IB=$(id B); IC=$(id C)
wait_for(){ for i in $(seq 1 ${3:-90}); do if eval "$1"; then echo "✔ $2"; return 0; fi; sleep 1; done; echo "✘ $2"; exit 1; }
t(){ local n=$1; shift; "$T" "$@" --home "$W/$n"; }

"$T" add-device "$IB" 李四 --home "$W/A" >/dev/null
"$T" add-device "$IC" 王五 --home "$W/A" >/dev/null
wait_for "t B nearby | grep -q '\"pending\": true'" "B 收到 A 的配对请求"
wait_for "t C nearby | grep -q '\"pending\": true'" "C 收到 A 的配对请求"
t B accept "$IA" >/dev/null; t C accept "$IA" >/dev/null
wait_for "t A devices | grep -c '\"connected\": true' | grep -q 2" "A 与 B、C 均已连接"

FID=$("$T" add-folder 设计文档 "$W/A-docs" "$IB" --home "$W/A" | sed -n 's/.*"id": "\(.*\)".*/\1/p' | head -1)
echo "# 接口设计 v1" > "$W/A-docs/接口.md"
wait_for "t B invites | grep -q $FID" "B 收到文件夹邀请"
sleep 3
t B folders | grep -q "$FID" && { echo "✘ B 不应自动创建文件夹"; exit 1; } || echo "✔ B 没有自动创建文件夹"
[ ! -e "$W/B-base/设计文档" ] && echo "✔ B 默认目录下没有自动出现文件夹"
t C invites | grep -q "$FID" && { echo "✘ C 不应收到邀请"; exit 1; } || echo "✔ C 未被选中，没有收到邀请"

t B ignore-invite "$FID" "$IA" >/dev/null
wait_for "! t B invites | grep -q $FID" "B 忽略后邀请消失" 10
sleep 5
t B invites | grep -q "$FID" && { echo "✘ 忽略后又出现"; exit 1; } || echo "✔ 忽略后不再提示"
t A reinvite "$FID" "$IB" >/dev/null
wait_for "t B invites | grep -q $FID" "A 重新邀请后 B 再次收到邀请" 60

t B accept-invite "$FID" "$IA" "$W/B-自定义/设计" >/dev/null
wait_for "[ -f '$W/B-自定义/设计/接口.md' ]" "B 接受到自定义路径并同步 接口.md" 90
echo "B 的修改" > "$W/B-自定义/设计/会议纪要.md"
wait_for "[ -f '$W/A-docs/会议纪要.md' ]" "B -> A 同步 会议纪要.md" 60
wait_for "t A folders | grep -q '\"remoteState\": \"valid\"'" "A 看到 B 已接受（remoteState=valid）" 30

t A share "$FID" "$IB" "$IC" >/dev/null
wait_for "t C invites | grep -q $FID" "A 编辑成员加入 C 后，C 收到邀请" 60
echo "全部通过"
