# 本机数据存放位置与上限（v0.2.4）

数据目录默认为 `%APPDATA%\TeamSync`（即 `C:\Users\<用户名>\AppData\Roaming\TeamSync`）。

| 数据 | Windows 路径 | 是否有上限 |
|---|---|---|
| 最近动态（上下线、文件变更、邀请等记录） | `%APPDATA%\TeamSync\activity.json` | 有：最多 500 条，且只保留最近 7 天 |
| 内核日志（teamsyncd） | `%APPDATA%\TeamSync\teamsyncd.log`（+ `.1`） | 有：单个 5 MiB，保留 1 个旧文件，最多约 10 MiB |
| 同步引擎日志（Syncthing） | `%APPDATA%\TeamSync\syncthing.log`（+ `.1`、`.2`） | 有：单个 10 MiB，保留 2 个旧文件，最多约 30 MiB；旧版本遗留的超大日志在启动时会自动轮换 |
| 同步索引数据库 | `%APPDATA%\TeamSync\st\index-v2\` | 随同步的文件数量增长（大约每个文件几百字节），不会无限增长；已删除文件的记录由 Syncthing 默认保留约 15 个月后清理 |
| 引擎配置与设备证书 | `%APPDATA%\TeamSync\st\config.xml`、`cert.pem`、`key.pem` 等 | 有，很小（KB 级）。**请勿删除证书，否则设备 ID 会变** |
| 应用设置（默认位置、忽略的邀请、备注标记） | `%APPDATA%\TeamSync\settings.json` | 有，很小；忽略记录在对方撤回邀请时自动清除 |
| 历史版本 | 每个团队文件夹下的 `.stversions\` | 按数量与时间：每个文件最多 10 个旧版本，超过 30 天自动清理；总大小取决于文件本身大小与修改频率 |
| 界面缓存（WebView2） | `%LOCALAPPDATA%\com.teamsync.desktop\EBWebView\` | 有，很小（主题、开关等少量设置） |
| 安装更新下载 | 系统临时目录 | 安装完成后由系统临时目录清理 |
