import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { api, eventSourceURL, notify, setTray, type Conflict, type Device, type Folder, type Invitation, type Nearby, type Settings, type Status, type UIEvent } from "./api";

interface Activity { id: number; message: string; time: string; type: string }
interface Data {
  ready: boolean; status: Status | null; folders: Folder[]; devices: Device[]; nearby: Nearby[];
  invitations: Invitation[]; settings: Settings | null; conflicts: Conflict[]; activity: Activity[];
  refresh: () => Promise<void>;
  run: (fn: () => Promise<unknown>, ok?: string) => Promise<boolean>;
}

const Ctx = createContext<Data>(null!);
export const useData = () => useContext(Ctx);

export function DataProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);
  const [status, setStatus] = useState<Status | null>(null);
  const [folders, setFolders] = useState<Folder[]>([]);
  const [devices, setDevices] = useState<Device[]>([]);
  const [nearby, setNearby] = useState<Nearby[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [conflicts, setConflicts] = useState<Conflict[]>([]);
  const [activity, setActivity] = useState<Activity[]>([]);
  const seq = useRef(0);

  const refresh = useCallback(async () => {
    try {
      const [s, f, d, n, p, c, st] = await Promise.all([
        api<Status>("GET", "/api/status"), api<Folder[]>("GET", "/api/folders"), api<Device[]>("GET", "/api/devices"),
        api<Nearby[]>("GET", "/api/nearby"), api<Invitation[]>("GET", "/api/invitations"), api<Conflict[]>("GET", "/api/conflicts"),
        api<Settings>("GET", "/api/settings"),
      ]);
      setStatus(s); setFolders(f); setDevices(d); setNearby(n); setInvitations(p || []); setConflicts(c); setSettings(st); setReady(true);
      setTray(s.error ? "引擎未就绪" : s.syncing ? "同步中" : `已同步 · ${s.connected} 人在线`);
    } catch (e) {
      setStatus((old) => ({ ...(old ?? {}), engine: "unreachable", error: String(e) }));
    }
  }, []);

  const run = useCallback(async (fn: () => Promise<unknown>, ok?: string) => {
    try { await fn(); if (ok) toast.success(ok); await refresh(); return true; }
    catch (e) { toast.error("操作失败", { description: (e as Error).message }); return false; }
  }, [refresh]);

  useEffect(() => {
    let es: EventSource | null = null;
    let timer: ReturnType<typeof setTimeout>;
    let closed = false;
    const connect = async () => {
      es = new EventSource(await eventSourceURL());
      es.onmessage = (m) => {
        const e: UIEvent = JSON.parse(m.data);
        if (e.message) {
          setActivity((a) => [{ id: ++seq.current, message: e.message!, time: e.time, type: e.type }, ...a].slice(0, 100));
          if (e.notify) { notify(e.message); toast(e.message); }
        }
        clearTimeout(timer);
        timer = setTimeout(refresh, 400);
      };
      es.onerror = () => { es?.close(); if (!closed) setTimeout(connect, 3000); };
    };
    (async () => {
      for (let i = 0; i < 60; i++) {
        try { await api("GET", "/api/status"); break; } catch { await new Promise((r) => setTimeout(r, 1000)); }
      }
      await refresh();
      connect();
    })();
    const iv = setInterval(refresh, 8000);
    return () => { closed = true; es?.close(); clearInterval(iv); };
  }, [refresh]);

  return <Ctx.Provider value={{ ready, status, folders, devices, nearby, invitations, settings, conflicts, activity, refresh, run }}>{children}</Ctx.Provider>;
}
