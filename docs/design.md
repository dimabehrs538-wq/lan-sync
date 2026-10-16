# 内网 P2P 文件夹同步工具 设计文档（Go 内核 + Tauri 桌面端）

## 一、SyncTrayzor 实现原理（源码 + REA 分析）

分析对象：GermanCoding/SyncTrayzor（C#/.NET 10, WPF + Stylet MVVM），源码 `src/SyncTrayzor`，以及 v2.2.0 便携版 `SyncTrayzor.dll`（REA `inspect-artifact` 确认为 .NET PE 托管程序集）。

**核心结论：SyncTrayzor 自己不做任何同步，它只是 Syncthing 的"外壳 + 进程管家 + REST 客户端"。** 同步、发现、加密、冲突副本、版本控制全由 syncthing.exe 完成。

| 模块 | 关键文件 | 原理 |
|---|---|---|
| 进程管理 | `Syncthing/SyncthingProcessRunner.cs` | `Process.Start(syncthing.exe --no-browser --no-restart --gui-address=127.0.0.1:<空闲端口> [--home=...])`，隐藏窗口，重定向 stdout/stderr 收日志；崩溃自动重启；退出时先 REST `shutdown`，超时再 Kill |
| API Key | `SyncthingManager.GenerateApiKey()` | 每次启动随机生成，通过环境变量 `STGUIAPIKEY` 注入给 Syncthing，HTTP 请求头 `X-API-Key` 带上；`STNOUPGRADE=1` 禁止 Syncthing 自升级 |
| 端口 | `FreePortFinder.cs` | 找空闲端口，避免 8384 冲突 |
| REST 客户端 | `Syncthing/ApiClient/ISyncthingApi.cs`（Refit） | `/rest/config`、`/rest/system/status|version|connections|shutdown|restart|pause|resume`、`/rest/db/status`、`/rest/db/scan` |
| 事件 | `Syncthing/EventWatcher/` | 长轮询 `GET /rest/events?since=<lastId>`，解析 DeviceConnected/Disconnected、ItemStarted/Finished、FolderSummary、StateChanged、DeviceRejected/FolderRejected、ConfigSaved 等，驱动托盘气泡和 UI |
| 轮询 | `SyncthingPoller`、`SyncthingConnectionsWatcher` | 定时取连接统计（速度） |
| 文件监视 | `Services/WatchedFolderMonitor.cs`、`DirectoryWatcher.cs` | 旧版 Syncthing 没有内建监视时用 .NET FileSystemWatcher 带退避合并后调 `/rest/db/scan?folder=&sub=`；现在 Syncthing 自带 fsWatcher，这层基本可省 |
| 冲突处理 | `Services/Conflicts/ConflictFileManager.cs` | 扫描 `*.sync-conflict-YYYYMMDD-HHMMSS-<设备ID>.ext` 文件（正则解析），UI 列出原文件与各冲突版本，用户选"保留哪个"，其余删除/移入回收站 |
| 托盘/通知 | `NotifyIcon/`、`Pages/Tray` | 托盘图标状态（同步中/空闲/离线）、气泡通知、内嵌浏览器（CefSharp/WebView）显示 Syncthing 原生 Web UI |
| 配置 | `Services/Config/` | 自身 XML 配置（Syncthing 路径、端口、环境变量、通知开关等），Syncthing 自己的 config.xml 由其管理 |
| 开机自启 | `Services/AutostartProvider.cs` | 写注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` |
| 单实例/IPC | `Services/Ipc/` | 命名管道，第二个实例把命令转给第一个 |
| 更新 | `Services/UpdateManagement/` | 查询 GitHub Release，下载安装包 + `sha512sum.txt.asc` 校验签名；Syncthing 本体用 `syncthing upgrade` |
| 计量网络 | `Services/Metering/` | 按流量计费网络时暂停设备 |

## 二、我们的方案选型

**结论：内嵌 Syncthing 作为同步引擎（推荐"捆绑二进制 + REST 控制"），自己只做 Go 控制层 + Tauri 界面。不要自研同步协议。**

| 方案 | 优点 | 缺点 |
|---|---|---|
| A. 捆绑 syncthing.exe，REST 控制（SyncTrayzor 同款） | 最稳，可独立升级，MPL-2.0 许可友好，进程隔离 | 多一个进程 |
| B. Go 里 import `github.com/syncthing/syncthing/lib/syncthing` 编成一个二进制 | 单进程、可深度定制 | 内部 API 不稳定，升级成本高 |
| C. 自研（fsnotify + 块哈希 + 自定义协议） | 完全可控 | 冲突、断点续传、发现、NAT、索引一致性坑极深，至少数月 |

MVP 选 A，后期如需单文件可演进到 B（控制层代码基本不变，因为都走同一套 REST/事件）。

## 三、架构

```
Tauri 前端 (Vue/React)  ──invoke/事件──>  Tauri Rust 壳（托盘、通知、窗口、自启）
                                             │ sidecar 启动 + 本地 HTTP/WebSocket
                                    Go 内核 teamsyncd.exe
                                    ├─ engine: 启停 syncthing.exe、API Key、端口、健康检查
                                    ├─ stclient: REST + 事件长轮询
                                    ├─ team: 团队/成员/共享文件夹简化模型
                                    ├─ pairing: 局域网发现与配对
                                    ├─ conflict: 冲突扫描与解决
                                    ├─ history: 版本浏览/恢复（.stversions）
                                    └─ api: 给前端的本地 API（127.0.0.1 + token）
                                             │ REST (X-API-Key)
                                    syncthing.exe（BEP 协议，TLS，局域网发现）
```

### 模块与关键 API
- **engine**：`exec.Command(syncthing, "serve", "--no-browser", "--no-restart", "--gui-address=127.0.0.1:PORT", "--home=%APPDATA%/TeamSync/st")`，env `STGUIAPIKEY`、`STNOUPGRADE=1`；Windows 用 Job Object（`KILL_ON_JOB_CLOSE`）保证父进程退出子进程同死；`/rest/noauth/health` 健康检查。
- **stclient**：`GET/PUT /rest/config`（及 `/rest/config/devices`、`/rest/config/folders`）、`GET /rest/events?since=`、`/rest/db/completion`、`/rest/db/status`、`/rest/system/connections`、`/rest/cluster/pending/devices|folders`、`POST /rest/db/scan`。
- **team**：一键"创建团队文件夹"——生成 folder ID、默认开启 `fsWatcherEnabled`、`versioning: staggered`（或 simple 保留 N 份）、忽略规则（`~$*`, `*.tmp`, `.DS_Store`）。
- **pairing（局域网发现/配对）**：Syncthing 自带本地发现（UDP 21027 广播）。我们关闭全局发现/中继（`globalAnnounceEnabled=false`、`relaysEnabled=false`，纯内网）。配对体验：
  1. 每台机显示自己的设备ID二维码/短码；或
  2. 内核另开 mDNS（`_teamsync._tcp`）广播"团队名 + 设备ID"，界面列出同网段成员，点"加入"；对方收到 `pending device` 事件弹窗确认后，双方自动互加并共享团队文件夹（`autoAcceptFolders` 仅对已信任团队成员开启）。
  3. 全互联拓扑（每人互加所有人），可设一台常开机器 `introducer=true` 做引荐者，新人只需加它一个。
- **conflict**：同 SyncTrayzor，扫描 `*.sync-conflict-*`，界面并排显示（Markdown/Word 可做 diff 预览），选择保留版本；建议规范"一人一文档/子目录"减少冲突。可选：md 文本三方合并。
- **history**：读 `.stversions`，或 `GET /rest/folder/versions`、`POST /rest/folder/versions` 恢复。
- **通知/托盘**：Tauri `tray-icon` + `notification` 插件，订阅内核事件："张三 更新了 设计/接口.md"（ItemFinished + 设备名映射）。
- **自启**：Tauri `autostart` 插件（HKCU Run），内核作为 sidecar 随 UI 启动；也可选注册为 Windows 服务（不登录也同步）。
- **防火墙**：安装时添加 22000/TCP+UDP、21027/UDP 入站规则。

## 四、打包（Windows）
- Tauri `bundle` 输出 NSIS/MSI；`externalBin` 放 `teamsyncd.exe` 与 `syncthing.exe`（官方 release，校验 sha256）。
- Go：`GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -s -w"`。
- 代码签名（避免 SmartScreen）；更新用 Tauri updater 插件 + 自建内网静态文件服务器或共享目录。
- 许可：Syncthing 为 MPL-2.0，捆绑分发需附带许可与源码链接。SyncTrayzor 为 MIT，可参考。

## 五、MVP 里程碑
1. **M1（1 周）**：Go 内核启停 syncthing、REST 客户端、事件流；CLI 能创建文件夹、加设备。
2. **M2（1–2 周）**：Tauri 界面：状态面板、成员列表、团队文件夹、托盘与通知、开机自启。
3. **M3（1 周）**：局域网 mDNS 发现 + 一键配对/确认；关闭全局发现与中继。
4. **M4（1 周）**：冲突中心、历史版本恢复、忽略规则模板。
5. **M5（1 周）**：安装包、防火墙规则、自动更新、内测。
