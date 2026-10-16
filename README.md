# TeamSync 团队同步（内网 P2P 文件夹同步）

面向小团队的**纯内网、无服务器**文件夹同步桌面工具：每人电脑装一个，团队文件夹里有人新增或修改文档，其他在线成员自动拉取。

- 同步引擎：内嵌官方 [Syncthing](https://syncthing.net) v2.1.6（不自研同步协议），由 Go 内核通过 REST + 事件 API 控制（思路同 SyncTrayzor）
- 内核：Go（`core/`，`teamsyncd`），作为 Tauri sidecar 运行，对前端暴露本地 HTTP API
- 桌面端：Tauri v2 + 原生 JS（`app/`），简体中文界面，托盘、通知、开机自启
- 默认纯内网：关闭全局发现、中继、NAT 穿透和使用统计，只用局域网广播发现同事

![概览](docs/screenshot-overview.png)

## 功能（MVP）

| 功能 | 说明 |
|---|---|
| 引擎管理 | 隐藏启动 syncthing（`--no-browser --no-restart`），REST 仅监听 `127.0.0.1` 随机端口，每次启动随机 API Key（`STGUIAPIKEY`），`STNOUPGRADE=1` 禁止自升级；崩溃自动重启（指数退避）；退出时先 REST shutdown，超时再强杀；Windows 用 Job Object、Linux 用 Pdeathsig 保证不留孤儿进程 |
| 团队文件夹 | 一键新建并共享给所有成员；默认开启实时文件监视（fsWatcher）和简单版本控制（保留 10 份/30 天）；写入默认忽略规则（Office 临时文件 `~$*`、`*.tmp` 等） |
| 局域网配对 | 显示本机设备 ID；"附近设备"列出局域网发现到的设备和请求加入的设备，点"添加/同意"即可；成员间开启自动接收文件夹，对方共享的团队文件夹自动出现在 `~/TeamSync/<名称>` |
| 状态与通知 | 概览（同步状态/在线成员/文件夹数）、文件夹列表（状态、文件数、暂停/扫描）、成员在线状态；"张三 修改了 设计/接口.md"类动态与系统通知；托盘提示同步状态 |
| 冲突中心 | 扫描 `*.sync-conflict-*` 副本，选择保留原文件或冲突版本 |
| 托盘/自启 | 关闭窗口最小化到托盘；单实例；开机自启（`--minimized` 只驻留托盘） |

## 目录结构

```
core/                 Go 内核（无第三方依赖）
  cmd/teamsyncd/      入口：serve 守护进程 + CLI 子命令
  internal/engine/    syncthing 进程管理
  internal/stclient/  Syncthing REST/事件客户端
  internal/team/      团队文件夹/成员模型、内网默认配置、附近设备
  internal/conflict/  冲突副本扫描与处理
  internal/api/       给前端的本地 HTTP API + SSE 事件（中文翻译）
app/                  Tauri v2 桌面端
  src/                前端（index.html 在 app/ 下）
  src-tauri/          Rust 壳：启动 sidecar、托盘、通知、自启
  src-tauri/binaries/ sidecar 二进制（脚本生成，不入库）
scripts/              下载 syncthing、编译内核、两节点端到端测试
docs/design.md        设计文档（含 SyncTrayzor 原理分析）
```

## 环境要求

- Go ≥ 1.22，Node ≥ 18，Rust（stable，建议用 rustup 安装最新版）
- Windows：Visual Studio 2022 生成工具（"使用 C++ 的桌面开发"）+ WebView2（Win10/11 一般自带）
- Linux：`libwebkit2gtk-4.1-dev libayatana-appindicator3-dev librsvg2-dev build-essential`

## Windows 构建（主平台）

在 PowerShell 中（仓库根目录）：

```powershell
# 1. 下载并校验官方 syncthing.exe
powershell -ExecutionPolicy Bypass -File scripts\fetch-syncthing.ps1
# 2. 编译 Go 内核 sidecar
powershell -ExecutionPolicy Bypass -File scripts\build-core.ps1
# 3. 构建安装包（NSIS 中文安装程序 + MSI）
cd app
npm install
npx tauri build
# 产物：app\src-tauri\target\release\bundle\nsis\TeamSync_0.1.0_x64-setup.exe
```

开发调试：`cd app && npx tauri dev`。

**防火墙**：首次运行 Windows 会询问是否允许 syncthing 访问网络，请勾选"专用网络"。或管理员执行：

```powershell
New-NetFirewallRule -DisplayName "TeamSync TCP" -Direction Inbound -Protocol TCP -LocalPort 22000 -Action Allow -Profile Private,Domain
New-NetFirewallRule -DisplayName "TeamSync UDP" -Direction Inbound -Protocol UDP -LocalPort 22000,21027 -Action Allow -Profile Private,Domain
```

## Linux / macOS 构建

```bash
scripts/fetch-syncthing.sh            # 默认当前系统，可指定：scripts/fetch-syncthing.sh v2.1.6 windows amd64
scripts/build-core.sh                 # 可指定：scripts/build-core.sh windows amd64
cd app && npm install && npx tauri build --bundles deb   # macOS: --bundles dmg
```

## 只用内核 / CLI

```bash
cd core && go build -o teamsyncd ./cmd/teamsyncd
./teamsyncd serve --syncthing /path/to/syncthing      # 数据目录默认 <用户配置目录>/TeamSync
# 另开终端：
./teamsyncd status                      # 本机设备 ID 和状态
./teamsyncd add-folder 设计文档 ~/设计文档
./teamsyncd nearby                      # 局域网发现的设备 / 待确认设备
./teamsyncd add-device <设备ID> 张三
./teamsyncd accept <设备ID>
./teamsyncd folders | devices | conflicts | scan <文件夹ID>
```

`serve` 参数：`--home` 数据目录、`--folders-dir` 自动接收的文件夹存放处（默认 `~/TeamSync`）、`--listen` 同步监听地址（默认 22000）、`--api-port`/`--token` 本地 API、`--exit-on-stdin-close`（sidecar 模式）。

## 测试

```bash
cd core && go test ./...          # 单元测试
scripts/e2e-two-nodes.sh          # 本机起两个实例：局域网发现 → 配对 → 双向同步文件
```

## 使用建议

- 两人同时改同一文档只会产生冲突副本，不会自动合并：建议"一人一文档/一人一子目录"，AI 生成文档按"作者_主题"命名。
- 只有在线的电脑之间才同步；建议每个人都互加所有成员，或放一台常开电脑当"中心节点"。
- 误删/误覆盖可在团队文件夹的 `.stversions` 中找回旧版本。

## 许可

Syncthing 为 MPL-2.0 许可，安装包附带其 LICENSE（`SYNCTHING-LICENSE.txt`），源码见 https://github.com/syncthing/syncthing 。
