import { useState } from "react";
import { FolderInput, FolderOpen, FolderPlus, FolderSync, History, Pause, Play, RefreshCw, Users } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Dot, Empty, PageHeader } from "@/components/common";
import { api, formatBytes, isTauri, short, STATE_CN } from "@/lib/api";
import { useData } from "@/lib/store";

async function pickDir(): Promise<string | null> {
  if (!isTauri()) return null;
  const { open } = await import("@tauri-apps/plugin-dialog");
  const p = await open({ directory: true, title: "选择要同步的文件夹" });
  return typeof p === "string" ? p : null;
}

function PathField({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <div className="flex gap-2">
      <Input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} className="font-mono text-xs" />
      {isTauri() && <Button type="button" variant="outline" onClick={async () => { const p = await pickDir(); if (p) onChange(p); }}><FolderOpen />浏览</Button>}
    </div>
  );
}

function AddFolderDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { run, devices } = useData();
  const [label, setLabel] = useState("");
  const [path, setPath] = useState("");
  const n = devices.filter((d) => !d.self).length;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新建团队文件夹</DialogTitle>
          <DialogDescription>创建后会自动共享给全部 {n} 位成员，默认开启实时监视和历史版本（保留 10 份）。</DialogDescription>
        </DialogHeader>
        <form id="add-folder" className="space-y-4" onSubmit={async (e) => {
          e.preventDefault();
          if (await run(() => api("POST", "/api/folders", { label, path }), `已创建「${label}」`)) { setLabel(""); setPath(""); onOpenChange(false); }
        }}>
          <div className="space-y-2"><Label>名称</Label><Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="例如：设计文档" required autoFocus /></div>
          <div className="space-y-2"><Label>本地路径</Label><PathField value={path} onChange={setPath} placeholder="例如：D:\项目\设计文档" /></div>
        </form>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          <Button type="submit" form="add-folder" disabled={!label || !path}><FolderPlus />新建并共享</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function AcceptFolderDialog({ offer, onClose }: { offer: { id: string; label: string; dev: string } | null; onClose: () => void }) {
  const { run } = useData();
  const [path, setPath] = useState("");
  return (
    <Dialog open={!!offer} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>接收共享文件夹</DialogTitle>
          <DialogDescription>成员 {short(offer?.dev)} 共享了「{offer?.label}」，请选择保存到本机的位置。</DialogDescription>
        </DialogHeader>
        <div className="space-y-2"><Label>保存路径</Label><PathField value={path} onChange={setPath} placeholder="例如：D:\TeamSync\设计文档" /></div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button disabled={!path} onClick={async () => {
            if (offer && await run(() => api("POST", "/api/pending/folders/accept", { folderID: offer.id, label: offer.label, path, devices: [offer.dev] }), "已接收，开始同步")) { setPath(""); onClose(); }
          }}><FolderInput />接收</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function Folders() {
  const { folders, pendingFolders, run, devices } = useData();
  const [adding, setAdding] = useState(false);
  const [offer, setOffer] = useState<{ id: string; label: string; dev: string } | null>(null);
  const name = (id: string) => devices.find((d) => d.deviceID === id)?.name || short(id);
  const offers = Object.entries(pendingFolders).map(([id, p]) => { const [dev, info] = Object.entries(p.offeredBy)[0]; return { id, label: info.label || id, dev }; });

  return (
    <>
      <PageHeader title="团队文件夹" desc="文件夹内任何人新增或修改的文档，都会自动同步到所有在线成员。"
        actions={<Button onClick={() => setAdding(true)}><FolderPlus />新建文件夹</Button>} />

      {offers.map((o) => (
        <Card key={o.id} className="mb-3 border-sky-500/40 bg-sky-500/5 py-3">
          <CardContent className="flex items-center gap-3 px-4 text-sm">
            <FolderInput className="size-4 text-sky-600" />
            <span className="flex-1"><b>{name(o.dev)}</b> 向你共享了文件夹「{o.label}」</span>
            <Button size="sm" onClick={() => setOffer(o)}>接收</Button>
          </CardContent>
        </Card>
      ))}

      {folders.length === 0 ? (
        <Empty icon={<FolderSync className="size-5" />} title="还没有团队文件夹" desc="新建一个文件夹并放入文档，团队成员就会自动收到。"
          action={<Button onClick={() => setAdding(true)}><FolderPlus />新建文件夹</Button>} />
      ) : (
        <div className="grid gap-3">
          {folders.map((f) => {
            const st = f.paused ? "paused" : f.status?.state ?? "";
            const g = f.status?.globalBytes || 0;
            const pct = g ? Math.round(((g - (f.status?.needBytes || 0)) / g) * 100) : 100;
            const tone = st === "idle" ? "green" : st === "error" ? "red" : st === "paused" ? "gray" : "amber";
            const members = f.devices.filter((d) => devices.some((x) => x.deviceID === d.deviceID && !x.self));
            return (
              <Card key={f.id} className="py-4">
                <CardContent className="px-5">
                  <div className="flex items-start gap-4">
                    <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-violet-500/10 text-violet-600"><FolderSync className="size-5" /></div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{f.label || f.id}</span>
                        <Badge variant="outline" className="gap-1.5"><Dot tone={tone} pulse={tone === "amber"} />{STATE_CN[st] ?? st}</Badge>
                        {f.conflicts > 0 && <Badge className="bg-red-500 text-white">{f.conflicts} 个冲突</Badge>}
                      </div>
                      <div className="mt-0.5 truncate font-mono text-xs text-muted-foreground">{f.path}</div>
                      <div className="mt-3 flex items-center gap-3">
                        <Progress value={pct} className={`h-1.5 flex-1 ${pct === 100 ? "[&_[data-slot=progress-indicator]]:bg-emerald-500" : "[&_[data-slot=progress-indicator]]:bg-sky-500"}`} />
                        <span className="w-10 text-right text-xs tabular-nums text-muted-foreground">{pct}%</span>
                      </div>
                      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                        <span>{f.status?.localFiles ?? 0} 个文件 · {formatBytes(f.status?.globalBytes)}</span>
                        {f.status?.needFiles ? <span className="text-amber-600">还差 {f.status.needFiles} 个文件</span> : null}
                        <span className="flex items-center gap-1"><Users className="size-3" />{members.length ? members.map((m) => name(m.deviceID)).join("、") : "未共享给成员"}</span>
                        {f.versioning?.type && <span className="flex items-center gap-1"><History className="size-3" />历史版本 保留 {f.versioning.params?.keep ?? "-"} 份</span>}
                        {f.fsWatcherEnabled && <span>实时监视</span>}
                      </div>
                    </div>
                    <div className="flex shrink-0 gap-1">
                      <Tooltip><TooltipTrigger asChild>
                        <Button variant="ghost" size="icon" onClick={() => run(() => api("POST", `/api/folders/${f.id}/scan`), "已开始扫描")}><RefreshCw /></Button>
                      </TooltipTrigger><TooltipContent>立即扫描</TooltipContent></Tooltip>
                      <Tooltip><TooltipTrigger asChild>
                        <Button variant="ghost" size="icon" onClick={() => run(() => api("POST", `/api/folders/${f.id}/${f.paused ? "resume" : "pause"}`), f.paused ? "已恢复同步" : "已暂停同步")}>{f.paused ? <Play /> : <Pause />}</Button>
                      </TooltipTrigger><TooltipContent>{f.paused ? "恢复同步" : "暂停同步"}</TooltipContent></Tooltip>
                    </div>
                  </div>
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}
      <AddFolderDialog open={adding} onOpenChange={setAdding} />
      <AcceptFolderDialog offer={offer} onClose={() => setOffer(null)} />
    </>
  );
}
