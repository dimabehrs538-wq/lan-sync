import { useEffect, useState } from "react";
import { AlertTriangle, FolderSync, LayoutDashboard, Moon, Settings as SettingsIcon, Sun, Users } from "lucide-react";
import { useTheme } from "next-themes";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dot } from "@/components/common";
import { useData } from "@/lib/store";
import { cn } from "@/lib/utils";
import { isMobile, mobileStatus } from "@/lib/platform";
import Overview from "@/pages/Overview";
import Folders from "@/pages/Folders";
import Members from "@/pages/Members";
import Conflicts from "@/pages/Conflicts";
import Settings from "@/pages/Settings";

export type Page = "overview" | "folders" | "members" | "conflicts" | "settings";

const ENGINE: Record<string, [string, "green" | "amber" | "red" | "gray"]> = {
  running: ["引擎运行中", "green"], starting: ["引擎启动中", "amber"], crashed: ["引擎异常，重启中", "red"],
  stopped: ["引擎已停止", "red"], unreachable: ["正在连接内核…", "amber"],
};

export default function App() {
  const [page, setPage] = useState<Page>(() => (new URLSearchParams(location.search).get("page") as Page) || "overview");
  const { status, conflicts, nearby, invitations } = useData();
  const { resolvedTheme, setTheme } = useTheme();
  const pendingDevices = nearby.filter((n) => n.pending).length;
  const nav: { id: Page; label: string; icon: typeof Users; badge?: number; tone?: "red" | "blue" }[] = [
    { id: "overview", label: "概览", icon: LayoutDashboard },
    { id: "folders", label: "团队文件夹", icon: FolderSync, badge: invitations.length || undefined, tone: "blue" },
    { id: "members", label: "成员", icon: Users, badge: pendingDevices || undefined, tone: "blue" },
    { id: "conflicts", label: "冲突", icon: AlertTriangle, badge: conflicts.length || undefined, tone: "red" },
    { id: "settings", label: "设置", icon: SettingsIcon },
  ];
  const [engText, engTone] = ENGINE[status?.engine ?? "unreachable"] ?? [status?.engine ?? "", "gray"];
  // 移动端：内核在 App 进程内启动，失败时（例如存储不可写）显示原因而不是一直“正在连接”
  const [startErr, setStartErr] = useState("");
  useEffect(() => {
    if (!isMobile || status?.engine === "running") { setStartErr(""); return; }
    const t = setInterval(async () => { const m = await mobileStatus(); setStartErr(m?.state === "error" ? m.error || "未知错误" : ""); }, 1500);
    return () => clearInterval(t);
  }, [status?.engine]);
  const toggleTheme = () => setTheme(resolvedTheme === "dark" ? "light" : "dark");
  const content = (
    <>
      {startErr && (
        <div className="mb-4 flex gap-2 rounded-lg border border-red-500/30 bg-red-500/5 p-3 text-xs text-red-700 dark:text-red-400">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" /><span>同步内核启动失败：{startErr}。请重新打开 App；如果仍然失败，可在系统设置中清除本 App 的缓存后重试。</span>
        </div>
      )}
      {page === "overview" && <Overview go={setPage} />}
      {page === "folders" && <Folders />}
      {page === "members" && <Members />}
      {page === "conflicts" && <Conflicts />}
      {page === "settings" && <Settings />}
    </>
  );

  return (
    <div className="flex h-dvh overflow-hidden bg-background text-foreground">
      <aside className="hidden w-56 shrink-0 flex-col border-r bg-sidebar sm:flex">
        <div className="flex items-center gap-2.5 px-4 pt-5 pb-6">
          <div className="flex size-8 items-center justify-center rounded-lg bg-gradient-to-br from-sky-500 to-indigo-600 text-white shadow-sm">
            <FolderSync className="size-4" />
          </div>
          <div>
            <div className="text-sm font-semibold leading-tight">团队同步</div>
            <div className="text-[11px] text-muted-foreground">内网 P2P · 无服务器</div>
          </div>
        </div>
        <nav className="flex flex-col gap-0.5 px-2">
          {nav.map((n) => (
            <button key={n.id} onClick={() => setPage(n.id)}
              className={cn("group flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm transition-colors",
                page === n.id ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground" : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground")}>
              <n.icon className="size-4" />
              <span className="flex-1 text-left">{n.label}</span>
              {n.badge !== undefined && (
                <Badge variant={n.tone ? "default" : "secondary"} className={cn("h-5 min-w-5 justify-center px-1.5 tabular-nums",
                  n.tone === "red" && "bg-red-500 text-white", n.tone === "blue" && "bg-sky-500 text-white")}>{n.badge}</Badge>
              )}
            </button>
          ))}
        </nav>
        <div className="mt-auto space-y-3 border-t p-3">
          <div className="flex items-center gap-2 px-1 text-xs text-muted-foreground">
            <Dot tone={engTone} pulse={engTone !== "green"} /> <span>{engText}</span>
            {status?.syncthingVersion && <span className="ml-auto font-mono text-[10px]">{status.syncthingVersion}</span>}
          </div>
          <Button variant="ghost" size="sm" className="w-full justify-start text-muted-foreground" onClick={toggleTheme}>
            {resolvedTheme === "dark" ? <Sun /> : <Moon />}{resolvedTheme === "dark" ? "浅色模式" : "深色模式"}
          </Button>
        </div>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        {/* 手机：紧凑顶栏（安全区）+ 底部 5 个标签 */}
        <header className="pt-safe pl-safe pr-safe shrink-0 border-b bg-sidebar sm:hidden" data-testid="mobile-topbar">
          <div className="flex h-12 items-center gap-2.5 px-4">
            <div className="flex size-7 items-center justify-center rounded-lg bg-gradient-to-br from-sky-500 to-indigo-600 text-white shadow-sm">
              <FolderSync className="size-3.5" />
            </div>
            <div className="text-sm font-semibold">团队同步</div>
            <div className="ml-auto flex items-center gap-1.5 text-xs text-muted-foreground"><Dot tone={engTone} pulse={engTone !== "green"} /><span>{engText}</span></div>
            <Button variant="ghost" size="icon" className="-mr-2 size-10" aria-label="切换主题" onClick={toggleTheme}>{resolvedTheme === "dark" ? <Sun /> : <Moon />}</Button>
          </div>
        </header>
        <main className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          <div className="mx-auto max-w-5xl py-4 pl-[max(1rem,env(safe-area-inset-left))] pr-[max(1rem,env(safe-area-inset-right))] sm:px-8 sm:py-7">
            {content}
          </div>
        </main>
        <nav className="pb-safe pl-safe pr-safe shrink-0 border-t bg-sidebar sm:hidden" data-testid="mobile-tabbar">
          <div className="grid grid-cols-5">
            {nav.map((n) => (
              <button key={n.id} onClick={() => setPage(n.id)} aria-label={n.label}
                className={cn("relative flex h-14 flex-col items-center justify-center gap-0.5 text-[11px] transition-colors",
                  page === n.id ? "font-medium text-foreground" : "text-muted-foreground")}>
                <n.icon className={cn("size-5", page === n.id && "text-sky-600")} />
                <span>{n.id === "folders" ? "文件夹" : n.label}</span>
                {n.badge !== undefined && (
                  <span className={cn("absolute top-1.5 left-1/2 ml-2 flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] text-white tabular-nums",
                    n.tone === "red" ? "bg-red-500" : "bg-sky-500")}>{n.badge}</span>
                )}
              </button>
            ))}
          </div>
        </nav>
      </div>
    </div>
  );
}
