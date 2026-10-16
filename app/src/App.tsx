import { useState } from "react";
import { AlertTriangle, FolderSync, LayoutDashboard, Moon, Settings as SettingsIcon, Sun, Users } from "lucide-react";
import { useTheme } from "next-themes";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dot } from "@/components/common";
import { useData } from "@/lib/store";
import { cn } from "@/lib/utils";
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

  return (
    <div className="flex h-screen overflow-hidden bg-background text-foreground">
      <aside className="flex w-56 shrink-0 flex-col border-r bg-sidebar">
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
          <Button variant="ghost" size="sm" className="w-full justify-start text-muted-foreground"
            onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}>
            {resolvedTheme === "dark" ? <Sun /> : <Moon />}{resolvedTheme === "dark" ? "浅色模式" : "深色模式"}
          </Button>
        </div>
      </aside>
      <main className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-5xl px-8 py-7">
          {page === "overview" && <Overview go={setPage} />}
          {page === "folders" && <Folders />}
          {page === "members" && <Members />}
          {page === "conflicts" && <Conflicts />}
          {page === "settings" && <Settings />}
        </div>
      </main>
    </div>
  );
}
