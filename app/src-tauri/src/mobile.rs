//! Android/iOS：在 App 进程内启动移动端 Go 内核（libteamsynccore，见 core/mobile/cmd/libteamsync）。
//! 不再拉起任何子进程：iOS 禁止 exec，Android 也不适合常驻子进程。
use std::ffi::{c_char, c_int, CStr, CString};
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::{Manager, RunEvent};

use crate::CoreInfo;

extern "C" {
    fn TeamSyncStart(home: *const c_char, folders_dir: *const c_char, token: *const c_char, api_port: c_int) -> *mut c_char;
    fn TeamSyncStop();
    fn TeamSyncFree(p: *mut c_char);
}

#[derive(Default, Clone, Serialize, Deserialize)]
pub struct StartResult {
    #[serde(default)]
    port: u16,
    #[serde(default, skip_serializing)]
    token: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    error: Option<String>,
}

#[derive(Default, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct MobileStatus {
    /// starting / running / error
    state: &'static str,
    error: Option<String>,
    /// 团队文件夹默认存放位置（App 沙盒内，可在设置里修改）
    folders_dir: String,
    /// 内核数据目录（设备证书、同步数据库）
    home: String,
}

struct MobileState(Mutex<MobileStatus>);

/// 前端查询移动端内核启动状态（启动失败时显示原因）
#[tauri::command]
fn mobile_status(state: tauri::State<MobileState>) -> MobileStatus {
    state.0.lock().unwrap().clone()
}

/// 移动端没有托盘：保留同名命令，前端无需区分
#[tauri::command]
fn set_tray_status(_text: String) {}

fn start_core(home: &str, folders: &str, core: &CoreInfo) -> StartResult {
    let (Ok(h), Ok(f), Ok(t)) = (CString::new(home), CString::new(folders), CString::new(core.token.as_str())) else {
        return StartResult { error: Some("路径无效".into()), ..Default::default() };
    };
    unsafe {
        let p = TeamSyncStart(h.as_ptr(), f.as_ptr(), t.as_ptr(), core.port as c_int);
        if p.is_null() {
            return StartResult { error: Some("内核没有返回结果".into()), ..Default::default() };
        }
        let s = CStr::from_ptr(p).to_string_lossy().into_owned();
        TeamSyncFree(p);
        serde_json::from_str(&s).unwrap_or(StartResult { error: Some(s), ..Default::default() })
    }
}

pub fn run(core: CoreInfo) {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_opener::init())
        .manage(core.clone())
        .manage(MobileState(Mutex::new(MobileStatus { state: "starting", ..Default::default() })))
        .invoke_handler(tauri::generate_handler![crate::core_info, set_tray_status, mobile_status])
        .setup(move |app| {
            let home = app.path().app_data_dir()?.join("TeamSync");
            // Android: /storage/emulated/0/Android/data/<包名>/files/Documents/TeamSync（USB/文件管理器可见）
            // iOS: App 的 Documents/TeamSync（“文件”App → 我的 iPhone → 团队同步 中可见）
            let folders = app.path().document_dir()?.join("TeamSync");
            let (home, folders) = (home.to_string_lossy().into_owned(), folders.to_string_lossy().into_owned());
            {
                let st = app.state::<MobileState>();
                let mut s = st.0.lock().unwrap();
                s.home = home.clone();
                s.folders_dir = folders.clone();
            }
            let handle = app.handle().clone();
            let core = core.clone();
            std::thread::spawn(move || {
                let r = start_core(&home, &folders, &core);
                let st = handle.state::<MobileState>();
                let mut s = st.0.lock().unwrap();
                match r.error {
                    Some(e) => {
                        eprintln!("[teamsync] 内核启动失败: {e}");
                        s.state = "error";
                        s.error = Some(e);
                    }
                    None if r.port != core.port || r.token != core.token => {
                        s.state = "error";
                        s.error = Some("内核端口或令牌与预期不一致".into());
                    }
                    None => s.state = "running",
                }
            });
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("启动 Tauri 失败");

    app.run(|_handle, event| {
        if let RunEvent::Exit = event {
            unsafe { TeamSyncStop() };
        }
    });
}
