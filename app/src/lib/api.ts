import { invoke, isTauri } from "@tauri-apps/api/core";

export interface CoreInfo { port: number; token: string }
export interface Status {
  engine: string; myID?: string; uptime?: number; syncthingVersion?: string;
  connected?: number; folders?: number; syncing?: boolean; error?: string;
}
export interface FolderStatus { state: string; globalFiles: number; localFiles: number; needFiles: number; needBytes: number; globalBytes: number; errors: number }
export interface Folder {
  id: string; label: string; path: string; type: string; paused: boolean;
  devices: { deviceID: string }[]; fsWatcherEnabled: boolean;
  versioning: { type: string; params: Record<string, string> };
  status?: FolderStatus; conflicts: number;
}
export interface Device { deviceID: string; name: string; connected: boolean; address?: string; self: boolean; paused: boolean }
export interface Nearby { deviceID: string; addresses: string[]; name?: string; pending: boolean }
export interface PendingFolder { offeredBy: Record<string, { time: string; label: string }> }
export interface Conflict { path: string; original: string; time: string; device: string; folderID: string; folderLabel: string }
export interface UIEvent { type: string; message?: string; notify: boolean; time: string; data?: unknown }

let core: CoreInfo | null = null;

export async function getCore(): Promise<CoreInfo> {
  if (core) return core;
  if (isTauri()) core = await invoke<CoreInfo>("core_info");
  else {
    // 浏览器调试：http://localhost:5173/?port=xxx&token=yyy（见 daemon.json）
    const q = new URLSearchParams(location.search);
    core = { port: Number(q.get("port")), token: q.get("token") ?? "" };
  }
  return core;
}

export async function api<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  const c = await getCore();
  const r = await fetch(`http://127.0.0.1:${c.port}${path}`, {
    method,
    headers: { Authorization: `Bearer ${c.token}`, "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error((data as { error?: string }).error || `HTTP ${r.status}`);
  return data as T;
}

export async function eventSourceURL() {
  const c = await getCore();
  return `http://127.0.0.1:${c.port}/api/events?token=${encodeURIComponent(c.token)}`;
}

export const short = (id = "") => id.slice(0, 7);

export const STATE_CN: Record<string, string> = {
  idle: "已同步", scanning: "扫描中", syncing: "同步中", "sync-preparing": "准备同步", "sync-waiting": "等待同步",
  error: "出错", paused: "已暂停", "scan-waiting": "等待扫描", cleaning: "清理中",
};

export function formatBytes(n = 0) {
  if (n < 1024) return `${n} B`;
  const u = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return `${n.toFixed(n < 10 ? 1 : 0)} ${u[i]}`;
}

export function formatUptime(s = 0) {
  if (s < 60) return `${s} 秒`;
  if (s < 3600) return `${Math.floor(s / 60)} 分钟`;
  if (s < 86400) return `${Math.floor(s / 3600)} 小时 ${Math.floor((s % 3600) / 60)} 分`;
  return `${Math.floor(s / 86400)} 天 ${Math.floor((s % 86400) / 3600)} 小时`;
}

let notifyMod: typeof import("@tauri-apps/plugin-notification") | null = null;
export async function notify(body: string) {
  if (localStorage.getItem("notify") === "0") return;
  try {
    if (isTauri()) {
      notifyMod ??= await import("@tauri-apps/plugin-notification");
      let ok = await notifyMod.isPermissionGranted();
      if (!ok) ok = (await notifyMod.requestPermission()) === "granted";
      if (ok) notifyMod.sendNotification({ title: "团队同步", body });
    }
  } catch (e) { console.warn(e); }
}

export function setTray(text: string) {
  if (isTauri()) invoke("set_tray_status", { text }).catch(() => {});
}
export { isTauri };
