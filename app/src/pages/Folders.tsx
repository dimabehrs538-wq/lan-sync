import { useEffect, useState } from "react";
import { Check, FolderInput, FolderOpen, FolderPlus, FolderSync, History, Mail, Pause, Play, RefreshCw, Send, UserCog, Users, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Avatar, Dot, Empty, PageHeader } from "@/components/common";
import { api, formatBytes, isTauri, short, STATE_CN, type Device, type Folder, type FolderMember, type Invitation } from "@/lib/api";
import { useData } from "@/lib/store";
import { cn } from "@/lib/utils";

async function pickDir(defaultPath?: string): Promise<string | null> {
  if (!isTauri()) return null;
  const { open } = await import("@tauri-apps/plugin-dialog");
  const p = await open({ directory: true, title: "选择文件夹", defaultPath });
  return typeof p === "string" ? p : null;
}

export function PathField({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <div className="flex gap-2">
      <Input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} className="font-mono text-xs" />
      {isTauri() && <Button type="button" variant="outline" onClick={async () => { const p = await pickDir(value || undefined); if (p) onChange(p); }}><FolderOpen />浏览</Button>}
    </div>
  );
}

/** 成员多选列表 */
function MemberPicker({ members, selected, onChange }: { members: Device[]; selected: string[]; onChange: (v: string[]) => void }) {
  if (members.length === 0) return <p className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">还没有成员，可以先创建，之后在“成员”页添加同事后再共享。</p>;
  const all = selected.length === members.length;
  return (
    <div className="rounded-lg border">
      <label className="flex cursor-pointer items-center gap-3 border-b px-3 py-2 text-xs text-muted-foreground">
        <Checkbox checked={all ? true : selected.length ? "indeterminate" : false} onCheckedChange={(v) => onChange(v ? members.map((m) => m.deviceID) : [])} />
        全选（已选 {selected.length}/{members.length}）
      </label>
      <div className="max-h-48 overflow-y-auto">
        {members.map((m) => (
          <label key={m.deviceID} className="flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-muted/50">
            <Checkbox checked={selected.includes(m.deviceID)} onCheckedChange={(v) => onChange(v ? [...selected, m.deviceID] : selected.filter((x) => x !== m.deviceID))} />
            <Avatar name={m.name} id={m.deviceID} online={m.connected} />
            <div className="min-w-0 flex-1">
              <div className="text-sm">{m.name || short(m.deviceID)}</div>
              <div className="font-mono text-[11px] text-muted-foreground">{short(m.deviceID)}</div>
            </div>
            <span className="text-[11px] text-muted-foreground">{m.connected ? "在线" : "离线"}</span>
          </label>
        ))}
      </div>
    </div>
  );
}

function AddFolderDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { run, devices, settings } = useData();
  const members = devices.filter((d) => !d.self);
  const [label, setLabel] = useState("");
  const [path, setPath] = useState("");
  const [sel, setSel] = useState<string[]>([]);
  useEffect(() => { if (open) { setSel(members.map((m) => m.deviceID)); setLabel(""); setPath(""); } }, [open]); // 默认全选
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>新建团队文件夹</DialogTitle>
          <DialogDescription>被选中的成员会收到邀请，由他们自己决定是否加入以及保存位置。默认开启实时监视和历史版本。</DialogDescription>
        </DialogHeader>
        <form id="add-folder" className="space-y-4" onSubmit={async (e) => {
          e.preventDefault();
          if (await run(() => api("POST", "/api/folders", { label, path, members: sel }), sel.length ? `已创建「${label}」，已邀请 ${sel.length} 位成员` : `已创建「${label}」`)) onOpenChange(false);
        }}>
          <div className="space-y-2"><Label>名称</Label><Input value={label} onChange={(e) => { setLabel(e.target.value); }} placeholder="例如：设计文档" required autoFocus /></div>
          <div className="space-y-2"><Label>本地路径</Label>
            <PathField value={path} onChange={setPath} placeholder={settings?.defaultBaseDir ? `${settings.defaultBaseDir}${settings.defaultBaseDir.includes("\\") ? "\\" : "/"}${label || "设计文档"}` : "例如：D:\\TeamSync\\设计文档"} />
            {!path && label && settings?.defaultBaseDir && (
              <button type="button" className="text-xs text-sky-600 hover:underline" onClick={() => setPath(`${settings.defaultBaseDir}${settings.defaultBaseDir.includes("\\") ? "\\" : "/"}${label}`)}>使用默认目录</button>
            )}
          </div>
          <div className="space-y-2"><Label>邀请成员</Label><MemberPicker members={members} selected={sel} onChange={setSel} /></div>
        </form>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          <Button type="submit" form="add-folder" disabled={!label || !path}><FolderPlus />创建</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function MembersDialog({ folder, onClose }: { folder: Folder | null; onClose: () => void }) {
  const { run, devices } = useData();
  const members = devices.filter((d) => !d.self);
  const [sel, setSel] = useState<string[]>([]);
  useEffect(() => { if (folder) setSel(folder.members.map((m) => m.deviceID)); }, [folder]);
  return (
    <Dialog open={!!folder} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>管理「{folder?.label}」的成员</DialogTitle>
          <DialogDescription>新勾选的成员会收到邀请；取消勾选后将停止与该成员同步此文件夹（对方的本地文件不会被删除）。</DialogDescription>
        </DialogHeader>
        <MemberPicker members={members} selected={sel} onChange={setSel} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button onClick={async () => { if (folder && await run(() => api("PUT", `/api/folders/${folder.id}/members`, { members: sel }), "已更新共享成员")) onClose(); }}><Check />保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function AcceptDialog({ inv, onClose }: { inv: Invitation | null; onClose: () => void }) {
  const { run } = useData();
  const [path, setPath] = useState("");
  useEffect(() => { if (inv) setPath(inv.suggestPath); }, [inv]);
  return (
    <Dialog open={!!inv} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>加入「{inv?.label}」</DialogTitle>
          <DialogDescription>{inv?.deviceName || short(inv?.deviceID)} 邀请你加入这个团队文件夹。请选择保存到本机的位置，可以是已有文件夹。</DialogDescription>
        </DialogHeader>
        <div className="space-y-2"><Label>保存位置</Label><PathField value={path} onChange={setPath} /></div>
        <p className="text-xs text-muted-foreground">默认位置可在“设置 → 接收文件夹的默认位置”中修改。</p>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button disabled={!path} onClick={async () => {
            if (inv && await run(() => api("POST", "/api/invitations/accept", { folderID: inv.folderID, deviceID: inv.deviceID, label: inv.label, path }), `已加入「${inv.label}」，开始同步`)) onClose();
          }}><FolderInput />加入并同步</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const MEMBER_STATE: Record<FolderMember["remoteState"], [string, string]> = {
  valid: ["已加入", "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"],
  notSharing: ["待接受", "bg-amber-500/10 text-amber-700 dark:text-amber-400"],
  paused: ["已暂停", "bg-zinc-500/10 text-muted-foreground"],
  unknown: ["离线", "bg-zinc-500/10 text-muted-foreground"],
};

function MemberChip({ folder, m }: { folder: Folder; m: FolderMember }) {
  const { run } = useData();
  const [label, cls] = MEMBER_STATE[m.remoteState] ?? MEMBER_STATE.unknown;
  return (
    <div className="flex items-center gap-2 rounded-full border bg-card py-0.5 pr-1 pl-0.5">
      <div className="scale-75"><Avatar name={m.name} id={m.deviceID} online={m.connected} /></div>
      <span className="-ml-1.5 text-xs">{m.name || short(m.deviceID)}</span>
      <span className={cn("rounded-full px-1.5 py-0.5 text-[10px]", cls)}>{label}{m.remoteState === "valid" && m.completion < 100 ? ` ${Math.floor(m.completion)}%` : ""}</span>
      {m.remoteState === "notSharing" && m.connected && (
        <Tooltip><TooltipTrigger asChild>
          <Button variant="ghost" size="icon-xs" onClick={() => run(() => api("POST", `/api/folders/${folder.id}/reinvite`, { deviceID: m.deviceID }), `已重新邀请 ${m.name || short(m.deviceID)}`)}><Send /></Button>
        </TooltipTrigger><TooltipContent>重新邀请（对方忽略过也会再次收到）</TooltipContent></Tooltip>
      )}
    </div>
  );
}

export default function Folders() {
  const { folders, invitations, run } = useData();
  const [adding, setAdding] = useState(false);
  const [accepting, setAccepting] = useState<Invitation | null>(null);
  const [editing, setEditing] = useState<Folder | null>(null);

  return (
    <>
      <PageHeader title="团队文件夹" desc="文件夹只会同步给被邀请且已接受的成员；每个人自己决定保存位置。"
        actions={<Button onClick={() => setAdding(true)}><FolderPlus />新建文件夹</Button>} />

      {invitations.length > 0 && (
        <Card className="mb-4 border-violet-500/40 bg-violet-500/5">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm"><Mail className="size-4 text-violet-600" />文件夹邀请（{invitations.length}）</CardTitle>
            <CardDescription>接受后选择本机保存位置；忽略后不再提醒，除非对方重新邀请。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {invitations.map((i) => (
              <div key={i.folderID + i.deviceID} className="flex items-center gap-3 rounded-lg border bg-card p-3">
                <div className="flex size-9 items-center justify-center rounded-lg bg-violet-500/10 text-violet-600"><FolderInput className="size-4" /></div>
                <div className="min-w-0 flex-1">
                  <div className="text-sm"><b>{i.deviceName || short(i.deviceID)}</b> 邀请你加入「<b>{i.label}</b>」</div>
                  <div className="text-xs text-muted-foreground">{i.time ? new Date(i.time).toLocaleString("zh-CN") : ""} · 文件夹 ID <span className="font-mono">{i.folderID}</span></div>
                </div>
                <Button variant="outline" size="sm" onClick={() => run(() => api("POST", "/api/invitations/ignore", { folderID: i.folderID, deviceID: i.deviceID, offerTime: i.offerTime }), "已忽略，不会再提醒")}><X />忽略</Button>
                <Button size="sm" onClick={() => setAccepting(i)}><Check />接受</Button>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      {folders.length === 0 ? (
        <Empty icon={<FolderSync className="size-5" />} title="还没有团队文件夹" desc="新建一个文件夹并邀请成员，或者等待同事邀请你。"
          action={<Button onClick={() => setAdding(true)}><FolderPlus />新建文件夹</Button>} />
      ) : (
        <div className="grid gap-3">
          {folders.map((f) => {
            const st = f.paused ? "paused" : f.status?.state ?? "";
            const g = f.status?.globalBytes || 0;
            const pct = g ? Math.round(((g - (f.status?.needBytes || 0)) / g) * 100) : 100;
            const tone = st === "idle" ? "green" : st === "error" ? "red" : st === "paused" ? "gray" : "amber";
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
                        {f.versioning?.type && <span className="flex items-center gap-1"><History className="size-3" />历史版本 保留 {f.versioning.params?.keep ?? "-"} 份</span>}
                        {f.fsWatcherEnabled && <span>实时监视</span>}
                      </div>
                      <div className="mt-3 flex flex-wrap items-center gap-1.5">
                        <Users className="mr-1 size-3.5 text-muted-foreground" />
                        {f.members.length === 0 && <span className="text-xs text-muted-foreground">仅本机，未共享给成员</span>}
                        {f.members.map((m) => <MemberChip key={m.deviceID} folder={f} m={m} />)}
                      </div>
                    </div>
                    <div className="flex shrink-0 gap-1">
                      <Tooltip><TooltipTrigger asChild>
                        <Button variant="ghost" size="icon" onClick={() => setEditing(f)}><UserCog /></Button>
                      </TooltipTrigger><TooltipContent>管理成员</TooltipContent></Tooltip>
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
      <AcceptDialog inv={accepting} onClose={() => setAccepting(null)} />
      <MembersDialog folder={editing} onClose={() => setEditing(null)} />
    </>
  );
}
