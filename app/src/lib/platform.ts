import { invoke, isTauri } from "@tauri-apps/api/core";

/** Android 原生桥（gen/android/.../TeamSyncBridge.kt），只在 Android App 中存在 */
interface AndroidBridge {
  shareFile(path: string): string;
  openFile(path: string): string;
  hasAllFilesAccess(): boolean;
  requestAllFilesAccess(): string;
  sharedStorageRoot(): string;
  setBackgroundSync(on: boolean): string;
  isBackgroundSync(): boolean;
}
declare global { interface Window { TeamSyncAndroid?: AndroidBridge } }

const ua = typeof navigator !== "undefined" ? navigator.userAgent : "";
// 浏览器调试移动端界面：?mobile=android / ?mobile=ios
const forced = typeof location !== "undefined" ? new URLSearchParams(location.search).get("mobile") : null;

export const isAndroid = forced === "android" || (isTauri() && /Android/i.test(ua));
export const isIOS = forced === "ios" || (!isAndroid && isTauri() && /iPhone|iPad|iPod/i.test(ua));
/** Android / iOS App：内核在 App 进程内运行，没有托盘、开机自启、资源管理器与回收站 */
export const isMobile = isAndroid || isIOS;
/** 桌面 App（Windows/macOS/Linux） */
export const isDesktop = isTauri() && !isMobile;

export const android = (): AndroidBridge | undefined => (typeof window !== "undefined" ? window.TeamSyncAndroid : undefined);

export interface MobileStatus { state: "starting" | "running" | "error"; error?: string; foldersDir: string; home: string }
export async function mobileStatus(): Promise<MobileStatus | null> {
  if (!isMobile || !isTauri()) return null;
  try { return await invoke<MobileStatus>("mobile_status"); } catch { return null; }
}

function bridgeResult(r: string | undefined) {
  if (!r) throw new Error("当前设备不支持");
  if (r.startsWith("error:")) throw new Error(r.slice(6));
}

/** 用系统中的其它应用打开文件（Android：FileProvider；桌面：默认程序） */
export async function openFile(abs: string) {
  const a = android();
  if (a) return bridgeResult(a.openFile(abs));
  const m = await import("@tauri-apps/plugin-opener");
  await m.openPath(abs);
}

/** 通过系统分享面板导出文件。Android 走原生桥；iOS 使用 Web Share（失败时提示到“文件”App 查看） */
export async function shareFile(abs: string, blob: () => Promise<Blob>, name: string) {
  const a = android();
  if (a) return bridgeResult(a.shareFile(abs));
  const file = new File([await blob()], name, { type: "application/octet-stream" });
  const nav = navigator as Navigator & { canShare?: (d: ShareData) => boolean };
  if (nav.share && (!nav.canShare || nav.canShare({ files: [file] }))) {
    await nav.share({ files: [file], title: name });
    return;
  }
  throw new Error(isIOS ? "请在“文件”App → 我的 iPhone → 团队同步 中查看和分享该文件" : "当前设备不支持分享");
}

export async function openURL(url: string) {
  if (!isTauri()) { window.open(url, "_blank"); return; }
  const m = await import("@tauri-apps/plugin-opener");
  await m.openUrl(url);
}
