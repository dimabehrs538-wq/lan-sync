#!/usr/bin/env bash
# 移动端 ⇄ 桌面端互通测试（只用本机临时目录与回环地址，不连接任何外部服务或真实局域网设备）：
#   D = 桌面内核 teamsyncd + 官方 syncthing 二进制（与 Windows 安装包相同的组合）
#   M = 移动端内核 teamsync-mobile-node（与 Android/iOS App 相同的 core/mobile 代码，进程内嵌入 syncthing）
# 覆盖：按地址手动配对（iPhone 场景）、手机作为共享者邀请桌面、文件导入/列表/导出接口、双向同步、5 MB 大文件、
#       并发修改产生冲突并在手机端解决、删除保护与历史版本恢复、只读共享时手机不能导入、手机内核重启后身份与文件夹保持、
#       首次启动即为纯内网（不使用全局发现/中继）。
# 用法: scripts/e2e-mobile-desktop.sh   需要先 scripts/fetch-syncthing.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ST="$(ls "$ROOT"/app/src-tauri/binaries/syncthing-* | grep -v LICENSE | head -1)"
W="$(mktemp -d)"; T="$W/teamsyncd"; M="$W/teamsync-mobile-node"
(cd "$ROOT/core" && go build -o "$T" ./cmd/teamsyncd)
(cd "$ROOT/core/mobile" && go build -tags noupgrade,noassets -ldflags "-X github.com/syncthing/syncthing/lib/build.Version=${SYNCTHING_VERSION:-v2.1.6}" -o "$M" ./cmd/teamsync-mobile-node)
PIDS=()
cleanup(){ kill "${PIDS[@]}" 2>/dev/null || true; wait 2>/dev/null || true; [ -n "${KEEP:-}" ] && echo "保留 $W" || rm -rf "$W"; }
trap cleanup EXIT
declare -A PID
DPORT=22301; MPORT=22302
startD(){ TEAMSYNC_GUARD_INTERVAL_S=3 XDG_DATA_HOME="$W/D-xdg" "$T" serve --home "$W/D" --folders-dir "$W/D-base" --listen "tcp://127.0.0.1:$DPORT" --syncthing "$ST" >>"$W/D.log" 2>&1 & PID[D]=$!; PIDS+=($!); }
startM(){ TEAMSYNC_GUARD_INTERVAL_S=3 "$M" --home "$W/M" --folders-dir "$W/M-base" --listen "tcp://127.0.0.1:$MPORT" >>"$W/M.log" 2>&1 & PID[M]=$!; PIDS+=($!); }
startD; startM
for n in D M; do until [ -f "$W/$n/daemon.json" ]; do sleep 0.5; done; done
t(){ local n=$1; shift; "$T" "$@" --home "$W/$n"; }
id(){ t "$1" status | sed -n 's/.*"myID": "\(.*\)".*/\1/p'; }
wait_for(){ for i in $(seq 1 ${3:-90}); do if eval "$1"; then echo "✔ $2"; return 0; fi; sleep 1; done; echo "✘ $2"; exit 1; }
ok(){ if eval "$1"; then echo "✔ $2"; else echo "✘ $2"; exit 1; fi; }
# 直接调用本地 API（与 App 前端相同的接口）
apiurl(){ python3 -c "import json;d=json.load(open('$W/$1/daemon.json'));print('http://127.0.0.1:%d'%d['port'])"; }
tok(){ python3 -c "import json;print(json.load(open('$W/$1/daemon.json'))['token'])"; }
call(){ local n=$1 m=$2 p=$3; shift 3; curl -sS -X "$m" -H "Authorization: Bearer $(tok $n)" "$(apiurl $n)$p" "$@"; }
code(){ local n=$1 m=$2 p=$3; shift 3; curl -s -o /dev/null -w '%{http_code}' -X "$m" -H "Authorization: Bearer $(tok $n)" "$(apiurl $n)$p" "$@"; }
fj(){ t "$1" folders | python3 -c "import json,sys; f=[x for x in json.load(sys.stdin) if x['id']=='$2'][0]; sys.exit(0 if ($3) else 1)"; }

ID_D=$(id D); ID_M=$(id M)
ok "[ -n '$ID_D' ] && [ -n '$ID_M' ] && [ '$ID_D' != '$ID_M' ]" "桌面与移动端内核均已启动（设备 ID 不同）"
ok "t M status | grep -q '\"syncthingVersion\": \"v2.1.6\"' && t D status | grep -q '\"syncthingVersion\": \"v2.1.6\"'" "两端同步引擎版本一致（Syncthing v2.1.6）"
ok "grep -q '<globalAnnounceEnabled>false</globalAnnounceEnabled>' '$W/M/st/config.xml' && grep -q '<relaysEnabled>false</relaysEnabled>' '$W/M/st/config.xml'" "移动端为纯内网配置"
ok "! grep -q 'global discovery server' '$W/M.log' && ! grep -qi 'relay listener starting\|Joined relay' '$W/M.log'" "移动端首次启动即不使用全局发现/中继（不连接公网服务）"
ok "call M GET /api/status | python3 -c \"import json,sys; a=json.load(sys.stdin)['addresses']; sys.exit(0 if 'tcp://127.0.0.1:$MPORT' not in a else 1)\"" "本机地址列表排除回环地址"

# ---- 按地址手动配对（iPhone 无法使用局域网广播发现）----
ok "[ \"\$(code M POST /api/devices -d '{\"deviceID\":\"$ID_D\",\"name\":\"办公室电脑\",\"address\":\"bad address\"}')\" = 400 ]" "地址格式错误时拒绝"
call M POST /api/devices -d "{\"deviceID\":\"$ID_D\",\"name\":\"办公室电脑\",\"address\":\"127.0.0.1:$DPORT\"}" >/dev/null
ok "grep -q '<address>tcp://127.0.0.1:$DPORT</address>' '$W/M/st/config.xml'" "手机端保存了对方地址（tcp://127.0.0.1:$DPORT + dynamic）"
wait_for "t D nearby | grep -q '\"pending\": true'" "桌面端收到手机的加入请求" 60
t D accept "$ID_M" >/dev/null
wait_for "t M devices | python3 -c \"import json,sys; sys.exit(0 if [d for d in json.load(sys.stdin) if d['deviceID']=='$ID_D'][0]['connected'] else 1)\"" "手机与桌面已连接（TLS 设备证书互相认证）" 60
t D my-name 设计部电脑 >/dev/null
wait_for "t M devices | grep -q '\"announcedName\": \"设计部电脑\"'" "手机看到桌面设置的名称（备注优先显示）" 60

# ---- 手机作为共享者邀请桌面 ----
mkdir -p "$W/M-base/现场照片"
FID=$(t M add-folder 现场照片 "$W/M-base/现场照片" "$ID_D" | sed -n 's/.*"id": "\(.*\)".*/\1/p' | head -1)
ok "[ -n '$FID' ]" "手机新建团队文件夹并邀请桌面"
wait_for "t D invites | grep -q $FID" "桌面收到手机的邀请" 60
t D accept-invite "$FID" "$ID_M" >/dev/null
wait_for "fj D $FID \"f['owner']=='$ID_M'\"" "桌面记录共享者为手机" 30
printf '现场记录 v1\n' > "$W/up.txt"
R=$(call M POST "/api/folders/$FID/files?name=%E8%AE%B0%E5%BD%95.txt" --data-binary @"$W/up.txt")
ok "echo '$R' | grep -q '\"name\":\"记录.txt\"'" "手机通过导入接口写入 记录.txt"
wait_for "[ -f '$W/D-base/现场照片/记录.txt' ] && cmp -s '$W/up.txt' '$W/D-base/现场照片/记录.txt'" "导入的文件同步到桌面" 60
R=$(call M POST "/api/folders/$FID/files?name=%E8%AE%B0%E5%BD%95.txt" --data-binary @"$W/up.txt")
ok "echo '$R' | grep -q '\"name\":\"记录 (2).txt\"'" "再次导入同名文件不覆盖（另存为 记录 (2).txt）"
mkdir -p "$W/D-base/现场照片/图纸"; head -c 5242880 /dev/urandom > "$W/D-base/现场照片/图纸/大图.bin"; t D scan "$FID" >/dev/null
wait_for "cmp -s '$W/D-base/现场照片/图纸/大图.bin' '$W/M-base/现场照片/图纸/大图.bin'" "桌面 5 MB 文件完整同步到手机" 90
ok "call M GET '/api/folders/$FID/files' | python3 -c \"import json,sys; e={x['name']:x for x in json.load(sys.stdin)['entries']}; sys.exit(0 if e['图纸']['dir'] and '记录.txt' in e and '.stfolder' not in e else 1)\"" "手机文件列表：目录在前、隐藏引擎内部文件"
call M GET "/api/folders/$FID/file?path=%E5%9B%BE%E7%BA%B8/%E5%A4%A7%E5%9B%BE.bin" -o "$W/down.bin"
ok "cmp -s '$W/down.bin' '$W/D-base/现场照片/图纸/大图.bin'" "手机导出（读取）文件内容与桌面一致"
ok "[ \"\$(code M GET '/api/folders/$FID/file?path=../../M/st/key.pem')\" = 400 ]" "导出接口拒绝访问文件夹之外（设备私钥）"

# ---- 并发修改 → 冲突 → 在手机端解决 ----
t M pause "$FID" >/dev/null; t D pause "$FID" >/dev/null
sleep 2
echo "手机上的修改" > "$W/M-base/现场照片/记录.txt"; echo "电脑上的修改" > "$W/D-base/现场照片/记录.txt"
touch -d '+5 seconds' "$W/D-base/现场照片/记录.txt"
t M resume "$FID" >/dev/null; t D resume "$FID" >/dev/null
t M scan "$FID" >/dev/null 2>&1 || true; t D scan "$FID" >/dev/null 2>&1 || true
wait_for "t M conflicts | grep -q '记录.sync-conflict-' || t D conflicts | grep -q '记录.sync-conflict-'" "两端同时修改同一文件后产生冲突副本" 90
wait_for "t M conflicts | grep -q '记录.sync-conflict-'" "手机冲突中心列出冲突（冲突副本已同步到手机）" 60
CP=$(t M conflicts | python3 -c "import json,sys; print([c['path'] for c in json.load(sys.stdin) if '记录.sync-conflict-' in c['path']][0])")
call M POST /api/conflicts/resolve -d "{\"folderID\":\"$FID\",\"path\":\"$CP\",\"keep\":\"conflict\"}" >/dev/null
wait_for "! t M conflicts | grep -q '记录.sync-conflict-' && ! t D conflicts | grep -q '记录.sync-conflict-'" "手机选择保留冲突版本后，两端冲突均消失" 60
wait_for "[ \"\$(cat '$W/M-base/现场照片/记录.txt')\" = \"\$(cat '$W/D-base/现场照片/记录.txt')\" ]" "解决后两端内容一致：$(cat "$W/M-base/现场照片/记录.txt" 2>/dev/null || true)" 60

# ---- 删除保护：桌面删除，手机保留历史版本并恢复 ----
rm "$W/D-base/现场照片/记录 (2).txt"; t D scan "$FID" >/dev/null
wait_for "[ ! -e '$W/M-base/现场照片/记录 (2).txt' ]" "桌面删除后手机同步删除" 60
wait_for "t M versions $FID | grep -q '记录 (2).txt'" "手机的历史版本中保留了被删除的文件" 30
t M restore "$FID" "记录 (2).txt" >/dev/null
wait_for "[ -f '$W/D-base/现场照片/记录 (2).txt' ]" "手机一键恢复后重新同步给桌面" 60

# ---- 只读共享：桌面为共享者，手机不能导入 ----
mkdir -p "$W/D-只读"; echo "制度 v1" > "$W/D-只读/制度.md"
F2=$(t D add-folder 公司制度 "$W/D-只读" "$ID_M" --readonly | sed -n 's/.*"id": "\(.*\)".*/\1/p' | head -1)
wait_for "t M invites | grep -q $F2" "手机收到只读共享邀请" 60
t M accept-invite "$F2" "$ID_D" >/dev/null
wait_for "[ -f '$W/M-base/公司制度/制度.md' ] && fj M $F2 \"f['type']=='receiveonly'\"" "手机以只读方式加入并收到文件" 90
ok "[ \"\$(code M POST /api/folders/$F2/files?name=x.txt --data-binary @'$W/up.txt')\" = 403 ]" "只读文件夹在手机上不能导入文件"
echo "制度 v2" > "$W/D-只读/制度.md"; t D scan "$F2" >/dev/null
wait_for "grep -q 'v2' '$W/M-base/公司制度/制度.md'" "共享者更新继续同步到手机" 60

# ---- 手机内核重启（App 被系统回收后重新打开）----
kill "${PID[M]}"; wait "${PID[M]}" 2>/dev/null || true; rm -f "$W/M/daemon.json"
wait_for "t D devices | python3 -c \"import json,sys; sys.exit(0 if not [d for d in json.load(sys.stdin) if d['deviceID']=='$ID_M'][0]['connected'] else 1)\"" "手机内核停止后桌面显示离线" 60
startM; until [ -f "$W/M/daemon.json" ]; do sleep 0.5; done
ok "[ \"\$(id M)\" = '$ID_M' ]" "重启后设备 ID 不变（证书持久化在 App 数据目录）"
ok "fj M $FID 'True' && fj M $F2 \"f['type']=='receiveonly'\"" "重启后团队文件夹与只读模式保持"
wait_for "t M devices | python3 -c \"import json,sys; sys.exit(0 if [d for d in json.load(sys.stdin) if d['deviceID']=='$ID_D'][0]['connected'] else 1)\"" "重启后自动重新连接桌面" 60
echo "重启后新增" > "$W/D-base/现场照片/重启后.txt"; t D scan "$FID" >/dev/null
wait_for "[ -f '$W/M-base/现场照片/重启后.txt' ]" "重启后继续同步" 60

# ---- 移动端移除文件夹并永久删除（没有回收站）----
t M remove-folder "$F2" --delete-files --permanent | grep -q '"trashed": false' && echo "✔ 手机移除只读文件夹并永久删除本地文件" || { echo "✘ 手机移除文件夹"; exit 1; }
ok "[ ! -e '$W/M-base/公司制度' ] && [ -f '$W/D-只读/制度.md' ]" "手机本地文件已删除，桌面不受影响"
echo "全部通过"
