#!/usr/bin/env bash
# 端到端测试：本机启动两个内核实例（A/B），局域网发现 -> 配对 -> 同步文件（双向）。
# 用法: scripts/e2e-two-nodes.sh   需要先 scripts/fetch-syncthing.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ST="$(ls "$ROOT"/app/src-tauri/binaries/syncthing-* | grep -v LICENSE | head -1)"
W="$(mktemp -d)"; T="$W/teamsyncd"
(cd "$ROOT/core" && go build -o "$T" ./cmd/teamsyncd)
cleanup(){ kill $PA $PB 2>/dev/null || true; wait 2>/dev/null || true; rm -rf "$W"; }
trap cleanup EXIT
"$T" serve --home "$W/A" --folders-dir "$W/A-folders" --syncthing "$ST" --listen tcp://0.0.0.0:22101 >"$W/A.log" 2>&1 & PA=$!
"$T" serve --home "$W/B" --folders-dir "$W/B-folders" --syncthing "$ST" --listen tcp://0.0.0.0:22102 >"$W/B.log" 2>&1 & PB=$!
until [ -f "$W/A/daemon.json" ] && [ -f "$W/B/daemon.json" ]; do sleep 0.5; done
id(){ "$T" status --home "$W/$1" | sed -n 's/.*"myID": "\(.*\)".*/\1/p'; }
IA=$(id A); IB=$(id B); echo "A=$IA"; echo "B=$IB"
wait_for(){ for i in $(seq 1 ${3:-90}); do if eval "$1"; then echo "✔ $2"; return 0; fi; sleep 1; done; echo "✘ $2"; exit 1; }
wait_for "\"$T\" nearby --home $W/B | grep -q $IA" "B 通过局域网发现找到 A"
"$T" add-device "$IB" 电脑B --home "$W/A" >/dev/null
"$T" add-folder 设计文档 "$W/A-docs" --home "$W/A" >/dev/null
echo "# 接口设计 v1" > "$W/A-docs/接口.md"
wait_for "\"$T\" nearby --home $W/B | grep -q '\"pending\": true'" "B 收到 A 的配对请求"
"$T" accept "$IA" --home "$W/B" >/dev/null
wait_for "[ -f '$W/B-folders/设计文档/接口.md' ]" "A -> B 同步 接口.md（自动接收团队文件夹）" 120
echo "B 的修改" > "$W/B-folders/设计文档/会议纪要.md"
wait_for "[ -f '$W/A-docs/会议纪要.md' ]" "B -> A 同步 会议纪要.md" 60
wait_for "\"$T\" devices --home $W/A | grep -q '\"connected\": true'" "A 显示 B 在线" 10
echo "全部通过"
