//! TeamSync 团队同步：桌面端以 sidecar 运行 Go 内核；Android/iOS 在 App 进程内运行同一份 Go 内核
//! （core/mobile，嵌入同版本 Syncthing），两端对前端暴露相同的本地 HTTP API。
use std::net::TcpListener;

use rand::{distributions::Alphanumeric, Rng};
use serde::Serialize;

#[cfg(desktop)]
mod desktop;
#[cfg(mobile)]
mod mobile;

#[derive(Clone, Serialize)]
pub struct CoreInfo {
    pub port: u16,
    pub token: String,
    /// 运行平台：windows / macos / linux / android / ios（前端据此调整界面）
    pub platform: &'static str,
}

/// 前端获取本地内核 API 地址与 token
#[tauri::command]
fn core_info(state: tauri::State<CoreInfo>) -> CoreInfo {
    state.inner().clone()
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .and_then(|l| l.local_addr())
        .map(|a| a.port())
        .unwrap_or(38384)
}

fn platform() -> &'static str {
    if cfg!(target_os = "android") {
        "android"
    } else if cfg!(target_os = "ios") {
        "ios"
    } else if cfg!(target_os = "macos") {
        "macos"
    } else if cfg!(windows) {
        "windows"
    } else {
        "linux"
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let core = CoreInfo {
        port: free_port(),
        token: rand::thread_rng()
            .sample_iter(&Alphanumeric)
            .take(40)
            .map(char::from)
            .collect(),
        platform: platform(),
    };
    #[cfg(desktop)]
    desktop::run(core);
    #[cfg(mobile)]
    mobile::run(core);
}
