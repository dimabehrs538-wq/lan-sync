#!/usr/bin/env bash
# 端到端测试（邀请制）：本机启动三个内核实例 A/B/C。
#  1. 局域网发现 + 配对（A 添加 B、C，B、C 同意）
#  2. A 新建文件夹只共享给 B：B 收到邀请且不会自动创建；C 收不到
#  3. B 忽略邀请 -> 邀请消失；A 重新邀请 -> B 再次收到
#  4. B 接受到自定义路径 -> 双向同步
#  5. 移除：B 仅停止同步（文件保留、不再同步、不会再作为邀请出现，重新邀请后出现）；
#     C 同时删除本地文件（进回收站，A 不受影响）
# 用法: scripts/e2e-two-nodes.sh   需要先 scripts/fetch-syncthing.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ST="$(ls "$ROOT"/app/src-tauri/binaries/syncthing-* | grep -v LICENSE | head -1)"
W="$(mktemp -d)"; T="$W/teamsyncd"
(cd "$ROOT/core" && go build -o "$T" ./cmd/teamsyncd)
PIDS=()
cleanup(){ kill "${PIDS[@]}" 2>/dev/null || true; wait 2>/dev/null || true; rm -rf "$W"; }
trap cleanup EXIT
declare -A PORT=([A]=22101 [B]=22102 [C]=22103) PID
start(){ TEAMSYNC_GUARD_INTERVAL_S=3 XDG_DATA_HOME="$W/$1-xdg" "$T" serve --home "$W/$1" --folders-dir "$W/$1-base" --listen "tcp://0.0.0.0:${PORT[$1]}" --syncthing "$ST" >>"$W/$1.log" 2>&1 & PID[$1]=$!; PIDS+=($!); }
for n in A B C; do start $n; done
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
kill "${PID[B]}"; wait "${PID[B]}" 2>/dev/null || true; rm -f "$W/B/daemon.json"; start B
until [ -f "$W/B/daemon.json" ]; do sleep 0.5; done
wait_for "t A devices | grep -c '\"connected\": true' | grep -q 2" "B 重启后重新连上 A" 60
sleep 8
t B invites | grep -q "$FID" && { echo "✘ 重连后忽略的邀请又出现"; exit 1; } || echo "✔ 重连后忽略的邀请仍然隐藏"
t A reinvite "$FID" "$IB" >/dev/null
wait_for "t B invites | grep -q $FID" "A 重新邀请后 B 再次收到邀请" 60

t B accept-invite "$FID" "$IA" "$W/B-自定义/设计" >/dev/null
wait_for "[ -f '$W/B-自定义/设计/接口.md' ]" "B 接受到自定义路径并同步 接口.md" 90
echo "B 的修改" > "$W/B-自定义/设计/会议纪要.md"
wait_for "[ -f '$W/A-docs/会议纪要.md' ]" "B -> A 同步 会议纪要.md" 60
wait_for "t A folders | grep -q '\"remoteState\": \"valid\"'" "A 看到 B 已接受（remoteState=valid）" 30

t A share "$FID" "$IB" "$IC" >/dev/null
wait_for "t C invites | grep -q $FID" "A 编辑成员加入 C 后，C 收到邀请" 60
t C accept-invite "$FID" "$IA" "$W/C-设计" >/dev/null
wait_for "[ -f '$W/C-设计/接口.md' ]" "C 接受并同步" 90

# ---- 修改成员备注名（仅本机）----
t A rename-device "$IB" "  李四（设计） " | grep -q '"李四（设计）"' && echo "✔ 修改备注成功（已去除首尾空格）"
t A rename-device "$IB" " " >/dev/null 2>&1 && { echo "✘ 空备注应被拒绝"; exit 1; } || echo "✔ 空备注被拒绝"
t A devices | grep -q '"李四（设计）"' && echo "✔ 成员列表显示新备注"
t A folders | grep -q '"name": "李四（设计）"' && echo "✔ 文件夹卡片成员显示新备注"
t B devices | grep -q '李四（设计）' && { echo "✘ 备注不应同步到对方"; exit 1; } || echo "✔ 备注只影响本机"

# ---- 我的名称：A 改名后其他成员看到；对方的备注优先 ----
t C rename-device "$IA" 老张 >/dev/null
t A my-name 张经理 | grep -q '"张经理"' && echo "✔ A 修改我的名称"
t A my-name | grep -q '"name": "张经理"' && echo "✔ A 读取我的名称"
wait_for "t B devices | python3 -c \"import json,sys; d=[x for x in json.load(sys.stdin) if x['deviceID']=='$IA'][0]; sys.exit(0 if d['name']=='张经理' else 1)\"" "B（无备注）看到 A 的新名称" 60
wait_for "t C devices | python3 -c \"import json,sys; d=[x for x in json.load(sys.stdin) if x['deviceID']=='$IA'][0]; sys.exit(0 if d['name']=='老张' and d.get('announcedName')=='张经理' else 1)\"" "C 的备注「老张」优先，同时记录 A 自称张经理" 60
t C rename-device "$IA" --announced | grep -q '"张经理"' && echo "✔ C 清除备注后改用 A 的名称"
t C devices | grep -q '"name": "张经理"' && echo "✔ C 显示张经理"

# ---- 取消最后的成员：文件夹变为仅本机（回归：members=[]）----
FID2=$("$T" add-folder 临时 "$W/A-tmp" "$IB" --home "$W/A" | sed -n 's/.*"id": "\(.*\)".*/\1/p' | head -1)
t A share "$FID2" | grep -q '"ok": true' && echo "✔ 取消最后一位成员成功"
t A folders | python3 -c "import json,sys; f=[x for x in json.load(sys.stdin) if x['id']=='$FID2'][0]; sys.exit(0 if f['members']==[] else 1)" && echo "✔ 文件夹变为仅本机"

# ---- .stfolder 被删除（git clean 等）→ 卡片报错 → 修复 → 恢复同步 ----
rm -rf "$W/A-docs/.stfolder"
t A scan "$FID" >/dev/null 2>&1 || true
wait_for "t A folders | python3 -c \"import json,sys; f=[x for x in json.load(sys.stdin) if x['id']=='$FID'][0]; sys.exit(0 if f.get('problem',{}).get('kind')=='markerMissing' and f['status']['state']=='error' else 1)\"" "删除 .stfolder 后引擎报错，卡片显示需要修复" 90
t A scan "$FID" 2>&1 | grep -q "missing" && { echo "✘ 错误未翻译"; exit 1; } || echo "✔ 错误提示为中文"
t A repair "$FID" | grep -q '"ok": true' && echo "✔ 修复成功"
[ -d "$W/A-docs/.stfolder" ] && echo "✔ .stfolder 已重建"
wait_for "t A folders | python3 -c \"import json,sys; f=[x for x in json.load(sys.stdin) if x['id']=='$FID'][0]; sys.exit(0 if 'problem' not in f and f['status']['state']!='error' else 1)\"" "修复后状态恢复正常" 60
echo "修复后新增" > "$W/A-docs/修复后.md"
wait_for "[ -f '$W/C-设计/修复后.md' ]" "修复后继续同步到 C" 60

# ---- 移除（仅停止同步）----
t B remove-folder "$FID" >/dev/null
! t B folders | grep -q "$FID" && echo "✔ B 已移除文件夹"
[ -f "$W/B-自定义/设计/接口.md" ] && echo "✔ B 本地文件保留"
echo "移除后新增" > "$W/A-docs/新文件.md"
wait_for "[ -f '$W/C-设计/新文件.md' ]" "C 继续同步新文件" 60
sleep 5
[ ! -e "$W/B-自定义/设计/新文件.md" ] && echo "✔ B 不再同步" || { echo "✘ B 仍在同步"; exit 1; }
t B folders | grep -q "$FID" && { echo "✘ 文件夹被自动重建"; exit 1; } || echo "✔ 未被自动重建"
t B invites | grep -q "$FID" && { echo "✘ 移除后又作为邀请出现"; exit 1; } || echo "✔ 移除后没有再弹出邀请"
wait_for "t A folders | grep -q '\"remoteState\": \"notSharing\"'" "A 看到 B 未加入（notSharing）" 30
t A reinvite "$FID" "$IB" >/dev/null
wait_for "t B invites | grep -q $FID" "A 重新邀请后 B 收到邀请" 60

# ---- 移除并删除本地文件 ----
t C remove-folder "$FID" --delete-files | grep -q '"trashed": true' && echo "✔ C 删除文件（进回收站）"
[ ! -e "$W/C-设计" ] && echo "✔ C 本地目录已删除"
ls "$W/C-xdg/Trash/files/" | grep -q "C-设计" && echo "✔ 回收站中可找回"
sleep 8
[ -f "$W/A-docs/接口.md" ] && [ -f "$W/A-docs/新文件.md" ] && echo "✔ A 的文件不受影响" || { echo "✘ A 文件受影响"; ls -la "$W/A-docs"; exit 1; }
# ---- v0.2.4：上下线防抖、断线合并、存储上限 ----
grep -q '<reconnectionIntervalS>10</reconnectionIntervalS>' "$W/A/st/config.xml" && echo "✔ 重连间隔 10 秒"
grep -q '<alwaysLocalNet>192.168.0.0/16</alwaysLocalNet>' "$W/A/st/config.xml" && echo "✔ 私有网段视为局域网"
grep -q '<relaysEnabled>false</relaysEnabled>' "$W/A/st/config.xml" && grep -q '<globalAnnounceEnabled>false</globalAnnounceEnabled>' "$W/A/st/config.xml" && echo "✔ 仍为纯内网（中继/全局发现关闭）"
[ -s "$W/A/syncthing.log" ] && [ -s "$W/A/teamsyncd.log" ] && echo "✔ 日志写入可轮换的日志文件"
t A activity | grep -q '已上线' && echo "✔ 最近动态由内核记录"
t A activity | grep -q '已离线' && { echo "✘ B 重启后 60 秒内重连，不应出现离线动态"; exit 1; } || echo "✔ 60 秒内重连不提示离线"
t A settings | grep -q '"presenceNotify": false' && echo "✔ 上下线通知默认关闭"
t A presence-notify on >/dev/null && t A settings | grep -q '"presenceNotify": true' && echo "✔ 上下线通知可开启"
restartB(){ kill "${PID[B]}"; wait "${PID[B]}" 2>/dev/null || true; rm -f "$W/B/daemon.json"; start B
  until [ -f "$W/B/daemon.json" ]; do sleep 0.5; done
  wait_for "t A devices | python3 -c \"import json,sys; sys.exit(0 if [x for x in json.load(sys.stdin) if x['deviceID']=='$IB'][0]['connected'] else 1)\"" "B 重新连上 A" 60; }
for i in 1 2 3; do restartB; done
t A activity | python3 -c "
import json,sys
a=json.load(sys.stdin); u=[x for x in a if x.get('key')=='unstable:$IB']
assert len(u)==1, u
assert '连接不稳定（5 分钟内断开' in u[0]['message'] and a[0]['key']=='unstable:$IB', a[:3]
print('✔ 反复断线合并为一条：'+u[0]['message'])"
t A activity | grep -q '已离线' && { echo "✘ 抖动期间不应逐条提示离线"; exit 1; } || echo "✔ 抖动期间没有逐条离线动态"
sleep 3; [ -s "$W/A/activity.json" ] && echo "✔ 最近动态已保存到 activity.json"
# ---- v0.2.6：文件夹位置不能相同/嵌套；默认放到 默认位置\文件夹名；重叠时自动暂停 ----
fails(){ "$@" 2>&1 || true; }  # 预期失败的命令（pipefail 下避免整条管道失败）
F3=$("$T" add-folder 周报 "$W/A-周报" "$IC" --home "$W/A" | sed -n 's/.*"id": "\(.*\)".*/\1/p' | head -1)
F4=$("$T" add-folder 方案 "$W/A-方案" "$IC" --home "$W/A" | sed -n 's/.*"id": "\(.*\)".*/\1/p' | head -1)
F5=$("$T" add-folder 周报 "$W/A-周报2" "$IC" --home "$W/A" | sed -n 's/.*"id": "\(.*\)".*/\1/p' | head -1)
wait_for "t C invites | grep -q $F3 && t C invites | grep -q $F4 && t C invites | grep -q $F5" "C 收到 3 个文件夹邀请" 60
t C invites | grep -q "\"suggestPath\": \"$W/C-base/周报\"" && echo "✔ 默认位置为 默认目录/文件夹名" || { echo "✘ 默认位置为 默认目录/文件夹名"; exit 1; }
fails t C accept-invite "$F3" "$IA" "$W/C-base" | grep -q '默认位置' && echo "✔ 拒绝直接使用默认目录本身" || { echo "✘ 拒绝直接使用默认目录本身"; exit 1; }
t C accept-invite "$F3" "$IA" >/dev/null && t C accept-invite "$F4" "$IA" >/dev/null && echo "✔ 两个邀请按默认位置接受" || { echo "✘ 两个邀请按默认位置接受"; exit 1; }
fails t C accept-invite "$F5" "$IA" "$W/C-base/周报" | grep -q '已被团队文件夹「周报」使用' && echo "✔ 拒绝与已有文件夹相同的位置" || { echo "✘ 拒绝与已有文件夹相同的位置"; exit 1; }
fails t C accept-invite "$F5" "$IA" "$W/C-base/周报/子目录" | grep -q '之内' && echo "✔ 拒绝放在已有文件夹之内" || { echo "✘ 拒绝放在已有文件夹之内"; exit 1; }
t C invites | grep -q "\"suggestPath\": \"$W/C-base/周报 (2)\"" && echo "✔ 同名文件夹默认位置自动变为「周报 (2)」" || { echo "✘ 同名文件夹默认位置自动变为「周报 (2)」"; exit 1; }
t C accept-invite "$F5" "$IA" >/dev/null && echo "✔ 同名文件夹按「周报 (2)」接受" || { echo "✘ 同名文件夹按「周报 (2)」接受"; exit 1; }
t C folders | python3 -c "
import json,sys
p=sorted(f['path'] for f in json.load(sys.stdin) if f['id'] in ('$F3','$F4','$F5'))
assert p==sorted(['$W/C-base/周报','$W/C-base/方案','$W/C-base/周报 (2)']), p
print('✔ 3 个文件夹各自独立：'+'、'.join(x.rsplit('/',1)[1] for x in p))"
fails t A add-folder 嵌套 "$W/A-docs/子" | grep -q '之内' && echo "✔ 新建文件夹不能放在已有文件夹之内" || { echo "✘ 新建文件夹不能放在已有文件夹之内"; exit 1; }
fails t A add-folder 包含 "$W" | grep -q '包含了团队文件夹' && echo "✔ 新建文件夹不能包含已有文件夹" || { echo "✘ 新建文件夹不能包含已有文件夹"; exit 1; }
t A check-path "$W/A-docs" "$FID" | grep -q '"empty": false' && echo "✔ 位置检查报告目录非空（界面据此确认）" || { echo "✘ 位置检查报告目录非空（界面据此确认）"; exit 1; }
# 模拟旧版本留下的重叠：直接改配置把「临时」指向「设计文档」内部，重启后应被自动暂停
kill "${PID[A]}"; wait "${PID[A]}" 2>/dev/null || true; rm -f "$W/A/daemon.json"
mkdir -p "$W/A-docs/嵌套/.stfolder"
python3 - "$W/A/st/config.xml" "$FID2" "$W/A-docs/嵌套" <<'PY'
import sys,re
p,fid,new=sys.argv[1:]; s=open(p,encoding='utf-8').read()
s2=re.sub(r'(<folder id="%s"[^>]*path=")[^"]*(")'%re.escape(fid), lambda m:m.group(1)+new+m.group(2), s)
assert s2!=s; open(p,'w',encoding='utf-8').write(s2)
PY
start A; until [ -f "$W/A/daemon.json" ]; do sleep 0.5; done
wait_for "t A folders | python3 -c \"import json,sys; f=[x for x in json.load(sys.stdin) if x['id']=='$FID2'][0]; sys.exit(0 if f['paused'] and f.get('problem',{}).get('kind')=='overlap' else 1)\"" "重叠的文件夹被自动暂停并提示" 60
t A folders | python3 -c "import json,sys; f=[x for x in json.load(sys.stdin) if x['id']=='$FID'][0]; sys.exit(0 if not f['paused'] and 'problem' not in f else 1)" && echo "✔ 先添加的「设计文档」不受影响" || { echo "✘ 先添加的「设计文档」不受影响"; exit 1; }
t A activity | grep -q '位置重叠，已暂停同步' && echo "✔ 动态中有重叠提示" || { echo "✘ 动态中有重叠提示"; exit 1; }
fails t A resume "$FID2" | grep -q '互相同步' && echo "✔ 重叠时不能直接恢复" || { echo "✘ 重叠时不能直接恢复"; exit 1; }
fails t A relocate "$FID2" "$W/A-docs/嵌套2" | grep -q '之内' && echo "✔ 重新选择位置仍会检查" || { echo "✘ 重新选择位置仍会检查"; exit 1; }
t A relocate "$FID2" "$W/A-临时新" | grep -q '"ok": true' && echo "✔ 重新选择独立位置" || { echo "✘ 重新选择独立位置"; exit 1; }
wait_for "t A folders | python3 -c \"import json,sys; f=[x for x in json.load(sys.stdin) if x['id']=='$FID2'][0]; sys.exit(0 if not f['paused'] and 'problem' not in f else 1)\"" "换位置后自动恢复同步" 30
# ---- v0.2.9：只读共享、删除保护、历史版本 ----
fj(){ local n=$1 id=$2; shift 2; t "$n" folders | python3 -c "import json,sys; f=[x for x in json.load(sys.stdin) if x['id']=='$id'][0]; sys.exit(0 if ($*) else 1)"; }
ok(){ if eval "$1"; then echo "✔ $2"; else echo "✘ $2"; exit 1; fi; }
mkdir -p "$W/A-只读"; echo "规范 v1" > "$W/A-只读/规范.md"
F6=$(t A add-folder 制度规范 "$W/A-只读" "$IC" --readonly | sed -n 's/.*"id": "\(.*\)".*/\1/p' | head -1)
ok "fj A $F6 \"f['type']=='sendonly' and f['mode']=='readonly' and f['role']=='owner' and f['label']=='制度规范' and f['deleteProtection']\"" "共享者一侧为仅发送（只读共享），名称不显示标记"
wait_for "t C invites | python3 -c \"import json,sys; i=[x for x in json.load(sys.stdin) if x['folderID']=='$F6']; sys.exit(0 if i and i[0]['mode']=='readonly' and i[0]['label']=='制度规范' else 1)\"" "C 收到的邀请带有只读模式" 60
t C accept-invite "$F6" "$IA" >/dev/null
ok "fj C $F6 \"f['type']=='receiveonly' and f['role']=='member' and f['path'].endswith('/C-base/制度规范')\"" "C 自动以仅接收方式加入"
wait_for "[ \"\$(cat '$W/C-base/制度规范/规范.md' 2>/dev/null)\" = '规范 v1' ]" "只读文件同步到 C" 90
echo "C 私自修改" > "$W/C-base/制度规范/规范.md"; t C scan "$F6" >/dev/null
wait_for "fj C $F6 \"f['status']['receiveOnlyChangedFiles']>=1\"" "C 的本机改动被识别为本机改动" 30
sleep 6
ok "[ \"\$(cat '$W/A-只读/规范.md')\" = '规范 v1' ]" "C 的本机改动不会同步到共享者"
t C revert "$F6" >/dev/null
wait_for "[ \"\$(cat '$W/C-base/制度规范/规范.md')\" = '规范 v1' ]" "恢复为共享版本" 60
wait_for "fj C $F6 \"f['status']['receiveOnlyChangedFiles']==0\"" "恢复后本机改动数归零" 30
echo "规范 v2" > "$W/A-只读/规范.md"; t A scan "$F6" >/dev/null
wait_for "[ \"\$(cat '$W/C-base/制度规范/规范.md')\" = '规范 v2' ]" "共享者的更新继续同步到 C" 60
ok "fails t C set-mode $F6 collab | grep -q '只有共享者'" "成员不能修改共享模式"
t A set-mode "$F6" collab >/dev/null
ok "fj A $F6 \"f['type']=='sendreceive' and f['mode']=='collab'\"" "共享者切换为协作"
wait_for "fj C $F6 \"f['type']=='sendreceive'\"" "C 自动跟随切换为协作" 60
wait_for "t C activity | grep -q '改为协作模式'" "C 动态提示模式变化" 10
t A set-mode "$F6" readonly >/dev/null
wait_for "fj C $F6 \"f['type']=='receiveonly'\"" "再切回只读后 C 自动变为仅接收" 60
ok "! t C activity | grep -q 'teamsync.json'" "模式文件不出现在动态中"
# 删除保护：成员删除的文件可一键恢复
echo "第 1 周内容" > "$W/A-周报/第1周.md"; t A scan "$F3" >/dev/null
wait_for "[ -f '$W/C-base/周报/第1周.md' ]" "协作文件夹同步 第1周.md" 60
ok "fj C $F3 \"f['deleteProtection']\"" "新文件夹默认开启删除保护"
rm "$W/A-周报/第1周.md"; t A scan "$F3" >/dev/null
wait_for "[ ! -e '$W/C-base/周报/第1周.md' ]" "A 删除后 C 也被删除" 60
wait_for "t C activity | grep -q '成员 张经理 删除了 第1周.md'" "C 动态显示「成员 张经理 删除了 第1周.md」" 20
t C activity | python3 -c "
import json,sys
a=[x for x in json.load(sys.stdin) if '删除了 第1周.md' in x['message']][0]
assert a['data']['folderID']=='$F3' and a['data']['path']=='第1周.md', a
print('✔ 删除记录带有文件夹和路径（可一键恢复）')"
t C versions "$F3" | python3 -c "
import json,sys,os
v=[x for x in json.load(sys.stdin) if x['path']=='第1周.md'][0]
assert v['deleted'] and v['versions'] and os.path.isfile(v['versions'][0]['file']), v
print('✔ 历史版本中有已删除的 第1周.md（可打开版本文件）')"
t C restore "$F3" 第1周.md | grep -q '"ok": true' && echo "✔ 一键恢复最近版本" || { echo "✘ 恢复失败"; exit 1; }
ok "[ \"\$(cat '$W/C-base/周报/第1周.md')\" = '第 1 周内容' ]" "C 本机文件已恢复"
wait_for "[ -f '$W/A-周报/第1周.md' ]" "恢复的文件重新同步给 A" 60
# 历史版本列表与指定版本恢复
for v in 二 三; do sleep 1.2; echo "第 1 周 修订$v" > "$W/A-周报/第1周.md"; t A scan "$F3" >/dev/null; wait_for "grep -q 修订$v '$W/C-base/周报/第1周.md'" "C 收到修订$v" 60; done
VT=$(t C versions "$F3" | python3 -c "
import json,sys
v=[x for x in json.load(sys.stdin) if x['path']=='第1周.md'][0]
assert len(v['versions'])>=2 and not v['deleted'], v
old=[x for x in v['versions'] if open(x['file']).read().strip()=='第 1 周内容']
assert old, v; print(old[0]['versionTime'])")
ok "[ -n '$VT' ]" "历史版本列出多个版本（时间/大小/文件）"
t C restore "$F3" 第1周.md "$VT" | grep -q '"ok": true' && echo "✔ 恢复指定版本" || { echo "✘ 恢复指定版本失败"; exit 1; }
ok "[ \"\$(cat '$W/C-base/周报/第1周.md')\" = '第 1 周内容' ]" "C 文件为所选版本"
ok "fails t C restore $F3 不存在.md | grep -q '没有找到'" "不存在的文件提示中文错误"
t C delete-protection "$F3" off >/dev/null
ok "fj C $F3 \"not f['deleteProtection']\"" "可关闭删除保护"
# ---- v0.3.0：历史版本保留规则（份数 / 天数 / 总容量） ----
vp(){ fj "$1" "$2" "f['versioning']['params']['keep']=='$3' and f['versioning']['params']['cleanoutDays']=='$4'"; }
t C settings | python3 -c "import json,sys; r=json.load(sys.stdin)['retention']; assert r=={'keep':10,'days':30,'maxMB':2048}, r; print('✔ 默认保留规则 10 份 / 30 天 / 2 GB')"
t C delete-protection "$F3" on >/dev/null
ok "fj C $F3 \"f['deleteProtection'] and f['retention']=={'keep':10,'days':30,'maxMB':2048,'custom':False}\" && vp C $F3 10 30" "重新开启删除保护，使用默认规则"
ok "fails t C retention-default 0 7 10 | grep -q '保留份数需在 1–100'" "份数超出范围被拒绝"
ok "fails t C retention-default 10 400 10 | grep -q '保留天数需在 1–365'" "天数超出范围被拒绝"
t C retention-default 5 7 1024 >/dev/null
ok "vp C $F3 5 7 && vp C $F6 5 7 && fj C $F3 \"f['retention']['days']==7 and f['retention']['maxMB']==1024\"" "修改默认值后，使用默认值的文件夹立即生效（引擎参数 keep/cleanoutDays）"
t C retention "$F3" 2 3 1 >/dev/null
ok "vp C $F3 2 3 && fj C $F3 \"f['retention']=={'keep':2,'days':3,'maxMB':1,'custom':True}\" && vp C $F6 5 7" "文件夹单独设置只影响该文件夹"
t C retention-default 8 9 2048 >/dev/null
ok "vp C $F3 2 3 && vp C $F6 8 9" "再改默认值：自定义的文件夹不受影响"
for v in 甲 乙 丙 丁; do sleep 1.2; echo "第 1 周 $v" > "$W/A-周报/第1周.md"; t A scan "$F3" >/dev/null; wait_for "grep -q $v '$W/C-base/周报/第1周.md'" "C 收到修改 $v" 60; done
ok "t C versions $F3 | python3 -c \"import json,sys; v=[x for x in json.load(sys.stdin) if x['path']=='第1周.md'][0]; sys.exit(0 if len(v['versions'])<=2 else 1)\"" "每个文件最多保留 2 份"
head -c 1600000 /dev/urandom > "$W/A-周报/大文件.bin"; t A scan "$F3" >/dev/null
wait_for "[ \"\$(stat -c %s '$W/C-base/周报/大文件.bin' 2>/dev/null)\" = 1600000 ]" "C 收到 1.6 MB 大文件" 60
head -c 1600000 /dev/urandom > "$W/A-周报/大文件.bin"; t A scan "$F3" >/dev/null
wait_for "cmp -s '$W/A-周报/大文件.bin' '$W/C-base/周报/大文件.bin'" "C 收到大文件的新内容" 60
wait_for "! ls '$W/C-base/周报/.stversions/' | grep -q '大文件~' && fj C $F3 \"f['versionsSize']<=1048576\"" "超过 1 MB 总容量后，内核自动删除最旧的版本" 30
OLDV="$W/C-base/周报/.stversions/过期~$(date -d '-10 days' +%Y%m%d-%H%M%S).md"; echo 旧 > "$OLDV"
t C cleanup-versions "$F3" | python3 -c "import json,sys; r=json.load(sys.stdin); assert r['removed']>=1 and r['size']<=1048576, r; print('✔ 立即清理删除超过 3 天的版本，返回清理结果')"
ok "[ ! -e '$OLDV' ]" "过期版本文件已删除"
sleep 1.2; echo "第 1 周 戊" > "$W/A-周报/第1周.md"; t A scan "$F3" >/dev/null
wait_for "fj C $F3 \"f['versionsFiles']>=1 and 0<f['versionsSize']<=1048576\"" "卡片显示当前历史版本占用（份数与大小）" 60
t C retention "$F3" default >/dev/null
ok "vp C $F3 8 9 && fj C $F3 \"not f['retention']['custom']\"" "改回使用默认值"
echo "全部通过"
