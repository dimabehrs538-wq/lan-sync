import { useEffect, useMemo, useState } from "react";
import { ChevronDown, ChevronRight, ExternalLink, FileClock, FolderSearch, History, Loader2, RotateCcw, Search, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { api, formatBytes, isTauri, type Folder, type VersionedFile } from "@/lib/api";
import { useData } from "@/lib/store";
import { formatCap, VersionsUsage } from "@/components/Retention";
import { cn } from "@/lib/utils";

const fmt = (t: string) => new Date(t).toLocaleString("zh-CN", { hour12: false });

async function openPath(p: string) {
  try { const m = await import("@tauri-apps/plugin-opener"); await m.openPath(p); }
  catch (e) { toast.error("无法打开文件", { description: String(e) }); }
}
async function reveal(p: string) {
  try { const m = await import("@tauri-apps/plugin-opener"); await m.revealItemInDir(p); }
  catch (e) { toast.error("无法打开所在文件夹", { description: String(e) }); }
}

/** 历史版本：列出文件夹中有历史版本的文件（含已删除的），可恢复、打开或在文件夹中显示。 */
export function VersionsDialog({ folder, onClose }: { folder: Folder | null; onClose: () => void }) {
  const { refresh, folders } = useData();
  const live = folders.find((f) => f.id === folder?.id) ?? folder;
  const [list, setList] = useState<VersionedFile[] | null>(null);
  const [q, setQ] = useState("");
  const [tab, setTab] = useState<"all" | "deleted">("all");
  const [open, setOpen] = useState<string | null>(null);
  const [busy, setBusy] = useState("");
  const load = async (f: Folder) => {
    try { setList(await api<VersionedFile[]>("GET", `/api/folders/${f.id}/versions`)); }
    catch (e) { setList([]); toast.error("读取历史版本失败", { description: (e as Error).message }); }
  };
  useEffect(() => { if (folder) { setList(null); setQ(""); setTab("all"); setOpen(null); void load(folder); } }, [folder?.id]);
  const shown = useMemo(() => (list ?? []).filter((f) => (tab === "all" || f.deleted) && (!q || f.path.toLowerCase().includes(q.toLowerCase()))), [list, q, tab]);
  const deletedCount = (list ?? []).filter((f) => f.deleted).length;
  const restore = async (path: string, versionTime?: string) => {
    if (!folder) return;
    setBusy(path + (versionTime ?? ""));
    try {
      await api("POST", `/api/folders/${folder.id}/versions/restore`, { path, versionTime });
      toast.success(`已恢复 ${path}`, { description: versionTime ? `恢复为 ${fmt(versionTime)} 的版本` : "已恢复为最近的版本" });
      await load(folder); void refresh();
    } catch (e) { toast.error("恢复失败", { description: (e as Error).message }); }
    finally { setBusy(""); }
  };
  return (
    <Dialog open={!!folder} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><History className="size-4" />「{folder?.label}」的历史版本</DialogTitle>
          <DialogDescription>被修改或删除的文件会保留旧版本（每个文件最多 {folder?.retention?.keep ?? 10} 份，保留 {folder?.retention?.days ?? 30} 天，总共不超过 {formatCap(folder?.retention?.maxMB ?? 2048)}），只保存在本机。恢复时当前文件会先存为一个新版本。</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2">
          <div className="relative flex-1">
            <Search className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="搜索文件名或路径" className="pl-8" />
          </div>
          <Tabs value={tab} onValueChange={(v) => setTab(v as "all" | "deleted")}>
            <TabsList>
              <TabsTrigger value="all">全部 {list ? list.length : ""}</TabsTrigger>
              <TabsTrigger value="deleted">已删除 {list ? deletedCount : ""}</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
        <ScrollArea className="h-[420px] rounded-md border">
          {list === null ? (
            <div className="flex h-40 items-center justify-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />正在读取…</div>
          ) : shown.length === 0 ? (
            <div className="flex h-40 flex-col items-center justify-center gap-1 text-sm text-muted-foreground">
              <FileClock className="size-6" />{list.length === 0 ? (folder?.deleteProtection ? "还没有历史版本。文件被修改或删除后会出现在这里。" : "该文件夹未开启删除保护，不会保留历史版本。") : "没有匹配的文件"}
            </div>
          ) : (
            <ul className="divide-y" data-testid="versions-list">
              {shown.map((f) => {
                const expanded = open === f.path;
                return (
                  <li key={f.path}>
                    <div className="flex items-center gap-2 px-3 py-2 hover:bg-muted/40">
                      <button className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => setOpen(expanded ? null : f.path)}>
                        {expanded ? <ChevronDown className="size-4 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-4 shrink-0 text-muted-foreground" />}
                        <span className={cn("truncate text-sm", f.deleted && "text-muted-foreground line-through decoration-red-400/70")}>{f.path}</span>
                        {f.deleted && <Badge variant="outline" className="shrink-0 gap-1 border-red-500/30 text-red-600"><Trash2 className="size-3" />已删除</Badge>}
                        <span className="ml-auto shrink-0 text-xs text-muted-foreground">{f.versions.length} 个版本 · 最近 {fmt(f.latest)}</span>
                      </button>
                      <Button size="sm" variant={f.deleted ? "default" : "outline"} disabled={!!busy} onClick={() => restore(f.path)}>
                        {busy === f.path ? <Loader2 className="animate-spin" /> : <RotateCcw />}{f.deleted ? "恢复" : "恢复最近版本"}
                      </Button>
                    </div>
                    {expanded && (
                      <table className="mb-2 ml-9 w-[calc(100%-3rem)] text-xs">
                        <thead className="text-muted-foreground"><tr><th className="py-1 text-left font-normal">保存时间</th><th className="text-left font-normal">文件修改时间</th><th className="text-right font-normal">大小</th><th /></tr></thead>
                        <tbody>
                          {f.versions.map((v) => (
                            <tr key={v.versionTime} className="border-t">
                              <td className="py-1.5 tabular-nums">{fmt(v.versionTime)}</td>
                              <td className="tabular-nums text-muted-foreground">{fmt(v.modTime)}</td>
                              <td className="text-right tabular-nums">{formatBytes(v.size)}</td>
                              <td className="w-0 whitespace-nowrap pl-3 text-right">
                                <Button size="sm" variant="ghost" className="h-7" disabled={!!busy} onClick={() => restore(f.path, v.versionTime)}><RotateCcw />恢复</Button>
                                <Tooltip><TooltipTrigger asChild>
                                  <Button size="sm" variant="ghost" className="h-7" disabled={!v.file || !isTauri()} onClick={() => v.file && openPath(v.file)}><ExternalLink />打开</Button>
                                </TooltipTrigger><TooltipContent>用默认程序打开这个版本（只读查看，不影响当前文件）</TooltipContent></Tooltip>
                                <Button size="sm" variant="ghost" className="h-7" disabled={!v.file || !isTauri()} onClick={() => v.file && reveal(v.file)}><FolderSearch />在文件夹中显示</Button>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </ScrollArea>
        {live && <VersionsUsage folder={live} compact onCleaned={() => void load(live)} />}
      </DialogContent>
    </Dialog>
  );
}
