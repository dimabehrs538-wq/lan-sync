import { Activity, AlertTriangle, ArrowRight, CheckCircle2, FolderSync, Loader2, ShieldCheck, Users } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Avatar, CopyButton, DeviceIdText, Dot, Empty, PageHeader } from "@/components/common";
import { formatUptime, STATE_CN } from "@/lib/api";
import { useData } from "@/lib/store";
import type { Page } from "@/App";

function Stat({ icon: Icon, label, value, sub, tone }: { icon: typeof Users; label: string; value: React.ReactNode; sub?: React.ReactNode; tone: string }) {
  return (
    <Card className="gap-0 py-4">
      <CardContent className="flex items-center gap-3.5 px-4">
        <div className={`flex size-10 items-center justify-center rounded-lg ${tone}`}><Icon className="size-5" /></div>
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">{label}</div>
          <div className="text-xl font-semibold tabular-nums">{value}</div>
          {sub && <div className="truncate text-[11px] text-muted-foreground">{sub}</div>}
        </div>
      </CardContent>
    </Card>
  );
}

export default function Overview({ go }: { go: (p: Page) => void }) {
  const { status, folders, devices, conflicts, activity, nearby } = useData();
  const members = devices.filter((d) => !d.self);
  const online = members.filter((d) => d.connected);
  const pending = nearby.filter((n) => n.pending);
  const syncing = status?.syncing;

  return (
    <>
      <PageHeader title="概览" desc="团队文件夹在局域网内自动同步，数据不经过任何外部服务器。" />
      <div className="grid grid-cols-4 gap-3">
        <Stat icon={syncing ? Loader2 : CheckCircle2} label="同步状态" value={status?.error ? "未就绪" : syncing ? "同步中" : "已同步"}
          sub={status?.uptime ? `已运行 ${formatUptime(status.uptime)}` : undefined}
          tone={syncing ? "bg-amber-500/10 text-amber-600 [&_svg]:animate-spin" : "bg-emerald-500/10 text-emerald-600"} />
        <Stat icon={Users} label="在线成员" value={<>{online.length}<span className="text-sm font-normal text-muted-foreground"> / {members.length}</span></>} tone="bg-sky-500/10 text-sky-600" />
        <Stat icon={FolderSync} label="团队文件夹" value={folders.length} tone="bg-violet-500/10 text-violet-600" />
        <Stat icon={AlertTriangle} label="待处理冲突" value={conflicts.length} tone={conflicts.length ? "bg-red-500/10 text-red-600" : "bg-muted text-muted-foreground"} />
      </div>

      {pending.length > 0 && (
        <Card className="mt-4 border-sky-500/40 bg-sky-500/5 py-3">
          <CardContent className="flex items-center gap-3 px-4 text-sm">
            <Dot tone="blue" pulse />
            <span className="flex-1">有 <b>{pending.length}</b> 台设备请求加入团队</span>
            <Button size="sm" onClick={() => go("members")}>去处理 <ArrowRight /></Button>
          </CardContent>
        </Card>
      )}

      <Card className="mt-4">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm"><ShieldCheck className="size-4 text-emerald-600" />本机设备 ID</CardTitle>
          <CardDescription>把它发给同事；或者让同事在“成员 → 附近设备”里直接添加你。</CardDescription>
        </CardHeader>
        <CardContent className="flex items-center gap-3">
          <div className="flex-1 rounded-md border bg-muted/40 px-3 py-2"><DeviceIdText id={status?.myID ?? "—"} className="text-foreground" /></div>
          {status?.myID && <CopyButton text={status.myID} />}
        </CardContent>
      </Card>

      <div className="mt-4 grid grid-cols-5 gap-4">
        <Card className="col-span-3">
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle className="text-sm">文件夹进度</CardTitle>
            <Button variant="ghost" size="sm" onClick={() => go("folders")}>全部 <ArrowRight /></Button>
          </CardHeader>
          <CardContent className="space-y-4">
            {folders.length === 0 && <Empty icon={<FolderSync className="size-5" />} title="还没有团队文件夹" action={<Button size="sm" onClick={() => go("folders")}>新建文件夹</Button>} />}
            {folders.slice(0, 5).map((f) => {
              const st = f.paused ? "paused" : f.status?.state ?? "";
              const g = f.status?.globalBytes || 0;
              const pct = g ? Math.round(((g - (f.status?.needBytes || 0)) / g) * 100) : 100;
              return (
                <div key={f.id}>
                  <div className="mb-1.5 flex items-center justify-between text-sm">
                    <span className="font-medium">{f.label || f.id}</span>
                    <span className="flex items-center gap-1.5 text-xs text-muted-foreground"><Dot tone={st === "idle" ? "green" : st === "error" ? "red" : "amber"} />{STATE_CN[st] ?? st}</span>
                  </div>
                  <Progress value={pct} className={`h-1.5 ${pct === 100 ? "[&_[data-slot=progress-indicator]]:bg-emerald-500" : "[&_[data-slot=progress-indicator]]:bg-sky-500"}`} />
                  <div className="mt-1 text-[11px] text-muted-foreground">{f.status?.localFiles ?? 0} 个文件 · {f.devices.length - 1} 位成员共享</div>
                </div>
              );
            })}
          </CardContent>
        </Card>
        <Card className="col-span-2">
          <CardHeader><CardTitle className="text-sm">成员</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            {members.length === 0 && <p className="text-sm text-muted-foreground">还没有成员</p>}
            {members.slice(0, 6).map((d) => (
              <div key={d.deviceID} className="flex items-center gap-3">
                <Avatar name={d.name} id={d.deviceID} online={d.connected} />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{d.name || d.deviceID.slice(0, 7)}</div>
                  <div className="truncate text-[11px] text-muted-foreground">{d.connected ? d.address?.replace(/^tcp:\/\//, "") : "离线"}</div>
                </div>
                <Badge variant={d.connected ? "secondary" : "outline"} className={d.connected ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400" : ""}>{d.connected ? "在线" : "离线"}</Badge>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>

      <Card className="mt-4">
        <CardHeader><CardTitle className="flex items-center gap-2 text-sm"><Activity className="size-4" />最近动态</CardTitle></CardHeader>
        <CardContent>
          {activity.length === 0 ? <p className="text-sm text-muted-foreground">暂无动态，成员上线或修改文件时会显示在这里。</p> : (
            <ScrollArea className="h-56 pr-3">
              <ol className="relative ml-1.5 border-l">
                {activity.map((a) => (
                  <li key={a.id} className="mb-3 ml-4 last:mb-0">
                    <span className={`absolute -left-[5px] mt-1.5 size-2.5 rounded-full border-2 border-card ${a.type === "RemoteChangeDetected" ? "bg-sky-500" : a.type.startsWith("Device") ? "bg-emerald-500" : "bg-amber-500"}`} />
                    <div className="text-sm">{a.message}</div>
                    <time className="text-[11px] text-muted-foreground">{new Date(a.time).toLocaleTimeString("zh-CN")}</time>
                  </li>
                ))}
              </ol>
            </ScrollArea>
          )}
        </CardContent>
      </Card>
    </>
  );
}
