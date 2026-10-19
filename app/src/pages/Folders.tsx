import { useEffect, useState } from "react";
import { toast } from "sonner";
import { AlertTriangle, Check, Eye, Info, Lock, ShieldCheck, Settings2, Undo2, Upload, FolderInput, FolderMinus, MoreHorizontal, Trash2, FolderOpen, FolderPlus, FolderSync, History, Mail, Pause, Play, RefreshCw, Send, UserCog, Users, Wrench, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Switch } from "@/components/ui/switch";
import { VersionsDialog } from "@/components/VersionsDialog";
import { Avatar, Dot, Empty, PageHeader } from "@/components/common";
import { api, ApiError, formatBytes, isTauri, short, STATE_CN, type Device, type Folder, type FolderMember, type Invitation, type PathInfo } from "@/lib/api";
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

/** 在资源管理器中打开文件夹；文件夹不存在时中文提示 */
async function openFolder(f: Folder) {
  try {
    const info = await checkPath(f.path, f.id);
    if (!info.exists) { toast.error("文件夹不存在", { description: `${f.path}\n可能已被移动、改名或删除，或所在磁盘未连接。` }); return; }
    if (!isTauri()) { toast.info(f.path); return; }
    const m = await import("@tauri-apps/plugin-opener");
    await m.openPath(f.path);
  } catch (e) { toast.error("无法打开文件夹", { description: (e as Error).message ?? String(e) }); }
}

type Mode = "collab" | "readonly";
/** 共享模式选择：协作（大家都能修改）/ 只读共享（只有我能修改） */
function ModePicker({ value, onChange, disabled }: { value: Mode; onChange: (m: Mode) => void; disabled?: boolean }) {
  const opts: { v: Mode; icon: React.ReactNode; title: string; desc: string }[] = [
    { v: "collab", icon: <Users className="size-4" />, title: "协作", desc: "所有成员都可以修改，改动互相同步" },
    { v: "readonly", icon: <Lock className="size-4" />, title: "只读共享", desc: "只有你的修改会同步；成员在本机的改动不会同步出去" },
  ];
  return (
    <div className="grid grid-cols-2 gap-2">
      {opts.map((o) => (
        <button key={o.v} type="button" disabled={disabled} onClick={() => onChange(o.v)}
          className={cn("flex items-start gap-2.5 rounded-lg border p-3 text-left transition-colors hover:bg-muted/50 disabled:cursor-not-allowed disabled:opacity-60",
            value === o.v && "border-primary ring-2 ring-primary/15")}>
          <span className="mt-0.5 text-muted-foreground">{o.icon}</span>
          <span><span className="block text-sm font-medium">{o.title}</span><span className="mt-0.5 block text-xs text-muted-foreground">{o.desc}</span></span>
        </button>
      ))}
    </div>
  );
}

function ProtectRow({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <div className="flex items-center gap-3 rounded-lg border p-3">
      <ShieldCheck className="size-4 text-emerald-600" />
      <div className="flex-1"><div className="text-sm font-medium">删除保护</div><div className="text-xs text-muted-foreground">文件被成员删除或覆盖时，在本机保留旧版本（每个文件 10 份、30 天），可一键恢复</div></div>
      <Switch checked={checked} onCheckedChange={onChange} />
    </div>
  );
}

/** 文件夹设置：共享模式（仅共享者可改）与删除保护 */
function FolderSettingsDialog({ folder, onClose }: { folder: Folder | null; onClose: () => void }) {
  const { run } = useData();
  const [mode, setMode] = useState<Mode>("collab");
  const [protect, setProtect] = useState(true);
  useEffect(() => { if (folder) { setMode(folder.mode); setProtect(folder.deleteProtection); } }, [folder]);
  const member = folder?.role === "member";
  const save = async () => {
    if (!folder) return;
    const tasks: (() => Promise<unknown>)[] = [];
    if (mode !== folder.mode) tasks.push(() => api("PUT", `/api/folders/${folder.id}/mode`, { mode }));
    if (protect !== folder.deleteProtection) tasks.push(() => api("PUT", `/api/folders/${folder.id}/delete-protection`, { enabled: protect }));
    if (await run(async () => { for (const t of tasks) await t(); }, "已保存文件夹设置")) onClose();
  };
  return (
    <Dialog open={!!folder} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>「{folder?.label}」的设置</DialogTitle>
          <DialogDescription>{member ? "这是同事共享给你的只读文件夹，共享模式由共享者决定。" : "切换共享模式后，已加入的成员会自动跟随，无需重新邀请。"}</DialogDescription>
        </DialogHeader>
        <div className="space-y-2"><Label>共享模式</Label><ModePicker value={mode} onChange={setMode} disabled={member} /></div>
        <ProtectRow checked={protect} onChange={setProtect} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button onClick={save} disabled={!folder || (mode === folder.mode && protect === folder.deleteProtection)}><Check />保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function AddFolderDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { run, devices, settings } = useData();
  const members = devices.filter((d) => !d.self);
  const [label, setLabel] = useState("");
  const [path, setPath] = useState("");
  const [sel, setSel] = useState<string[]>([]);
  const [mode, setMode] = useState<Mode>("collab");
  const [protect, setProtect] = useState(true);
  useEffect(() => { if (open) { setSel(members.map((m) => m.deviceID)); setLabel(""); setPath(""); setMode("collab"); setProtect(true); } }, [open]); // 默认全选
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>新建团队文件夹</DialogTitle>
          <DialogDescription>被选中的成员会收到邀请，由他们自己决定是否加入以及保存位置。默认开启实时监视和历史版本。</DialogDescription>
        </DialogHeader>
        <form id="add-folder" className="space-y-4" onSubmit={async (e) => {
          e.preventDefault();
          if (await run(() => api("POST", "/api/folders", { label, path, members: sel, mode, deleteProtection: protect }), sel.length ? `已创建「${label}」，已邀请 ${sel.length} 位成员` : `已创建「${label}」`)) onOpenChange(false);
        }}>
          <div className="space-y-2"><Label>名称</Label><Input value={label} onChange={(e) => { setLabel(e.target.value); }} placeholder="例如：设计文档" required autoFocus /></div>
          <div className="space-y-2"><Label>本地路径</Label>
            <PathField value={path} onChange={setPath} placeholder={settings?.defaultBaseDir ? `${settings.defaultBaseDir}${settings.defaultBaseDir.includes("\\") ? "\\" : "/"}${label || "设计文档"}` : "例如：D:\\TeamSync\\设计文档"} />
            {!path && label && settings?.defaultBaseDir && (
              <button type="button" className="text-xs text-sky-600 hover:underline" onClick={() => setPath(`${settings.defaultBaseDir}${settings.defaultBaseDir.includes("\\") ? "\\" : "/"}${label}`)}>使用默认目录</button>
            )}
          </div>
          <div className="space-y-2"><Label>共享模式</Label><ModePicker value={mode} onChange={setMode} /></div>
          <div className="space-y-2"><Label>邀请成员</Label><MemberPicker members={members} selected={sel} onChange={setSel} /></div>
          <ProtectRow checked={protect} onChange={setProtect} />
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

/** 提交前检查位置：不能与已有文件夹相同/嵌套等（内核返回中文原因），并返回是否非空 */
export const checkPath = (path: string, folderID?: string) => api<PathInfo>("POST", "/api/path-check", { path, folderID });

function AcceptDialog({ inv, onClose }: { inv: Invitation | null; onClose: () => void }) {
  const { run, settings } = useData();
  const [path, setPath] = useState("");
  const [error, setError] = useState("");
  const [nonEmpty, setNonEmpty] = useState(0); // >0：目录非空，等待确认
  const [busy, setBusy] = useState(false);
  useEffect(() => { if (inv) { setPath(inv.suggestPath); setError(""); setNonEmpty(0); } }, [inv]);
  const change = (p: string) => { setPath(p); setError(""); setNonEmpty(0); };
  const submit = async (confirmed: boolean) => {
    if (!inv) return;
    setBusy(true);
    try {
      const info = await checkPath(path, inv.folderID);
      if (!info.ok) { setError(info.error || "该位置不可用"); return; }
      if (info.exists && !info.empty && !confirmed) { setNonEmpty(info.entries); return; }
      if (await run(() => api("POST", "/api/invitations/accept", { folderID: inv.folderID, deviceID: inv.deviceID, label: inv.label, path }), `已加入「${inv.label}」，开始同步`)) onClose();
    } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  };
  return (
    <Dialog open={!!inv} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>加入「{inv?.label}」</DialogTitle>
          <DialogDescription>{inv?.deviceName || short(inv?.deviceID)} 邀请你加入这个团队文件夹。每个团队文件夹需要单独的位置，默认放在“接收文件夹的默认位置”下以文件夹名命名的子文件夹中。</DialogDescription>
        </DialogHeader>
        {inv?.mode === "readonly" && (
          <p className="flex gap-1.5 rounded-md border border-sky-500/30 bg-sky-500/5 p-2.5 text-xs text-sky-800 dark:text-sky-300"><Eye className="mt-0.5 size-3.5 shrink-0" />这是<b>只读共享</b>：你会收到共享者的最新文件，但你在本机的修改不会同步给其他人。</p>
        )}
        <div className="space-y-2"><Label>保存位置</Label><PathField value={path} onChange={change} /></div>
        {error ? (
          <p className="flex gap-1.5 rounded-md border border-red-500/30 bg-red-500/5 p-2.5 text-xs text-red-700 dark:text-red-400" data-testid="path-error"><AlertTriangle className="mt-0.5 size-3.5 shrink-0" />{error}</p>
        ) : nonEmpty > 0 ? (
          <p className="flex gap-1.5 rounded-md border border-amber-400/40 bg-amber-500/10 p-2.5 text-xs text-amber-800 dark:text-amber-300" data-testid="path-nonempty"><AlertTriangle className="mt-0.5 size-3.5 shrink-0" />该文件夹不是空的（已有 {nonEmpty} 项）。加入后，其中的文件会与「{inv?.label}」的其他成员互相同步。确定要使用这个位置吗？</p>
        ) : (
          <p className="text-xs text-muted-foreground">默认位置{settings?.defaultBaseDir ? <>（<span className="font-mono">{settings.defaultBaseDir}</span>）</> : ""}可在“设置 → 接收文件夹的默认位置”中修改。</p>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          {nonEmpty > 0
            ? <Button variant="destructive" disabled={busy} onClick={() => submit(true)}><FolderInput />仍然使用该位置</Button>
            : <Button disabled={!path || busy} onClick={() => submit(false)}><FolderInput />加入并同步</Button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RemoveDialog({ folder, onClose }: { folder: Folder | null; onClose: () => void }) {
  const { run } = useData();
  const [mode, setMode] = useState<"keep" | "delete">("keep");
  const [confirm, setConfirm] = useState("");
  const [permanent, setPermanent] = useState(false);
  useEffect(() => { if (folder) { setMode("keep"); setConfirm(""); setPermanent(false); } }, [folder]);
  const name = folder?.label || folder?.id || "";
  const ok = mode === "keep" || confirm.trim() === name;
  const Option = ({ v, icon, title, desc, danger }: { v: "keep" | "delete"; icon: React.ReactNode; title: string; desc: string; danger?: boolean }) => (
    <button type="button" onClick={() => setMode(v)} className={cn("flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-muted/50",
      mode === v && (danger ? "border-red-500 bg-red-500/5 ring-2 ring-red-500/20" : "border-primary ring-2 ring-primary/15"))}>
      <span className={cn("mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border", mode === v && (danger ? "border-red-500" : "border-primary"))}>
        {mode === v && <span className={cn("size-2 rounded-full", danger ? "bg-red-500" : "bg-primary")} />}
      </span>
      <span className="flex-1">
        <span className={cn("flex items-center gap-1.5 text-sm font-medium", danger && "text-red-600")}>{icon}{title}</span>
        <span className="mt-0.5 block text-xs text-muted-foreground">{desc}</span>
      </span>
    </button>
  );
  return (
    <Dialog open={!!folder} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>移除「{name}」</DialogTitle>
          <DialogDescription>只影响本机：其他成员会继续同步这个文件夹，他们那里会显示你“未加入”。之后对方重新邀请时，会以邀请的形式出现。</DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          <Option v="keep" icon={<FolderMinus className="size-4" />} title="仅停止同步（保留本地文件）" desc={`本机不再同步，${folder?.path ?? ""} 中的文件原样保留。`} />
          <Option v="delete" danger icon={<Trash2 className="size-4" />} title="同时删除本地文件" desc="停止同步并把本机的这个文件夹移到回收站。其他成员的文件不受影响。" />
        </div>
        {mode === "delete" && (
          <div className="space-y-3 rounded-lg border border-red-500/30 bg-red-500/5 p-3">
            <div className="flex gap-2 text-xs text-red-700 dark:text-red-400"><AlertTriangle className="size-4 shrink-0" />将删除 <span className="font-mono break-all">{folder?.path}</span> 及其中所有文件。</div>
            <div className="space-y-1.5">
              <Label className="text-xs">请输入文件夹名称 <b>{name}</b> 以确认</Label>
              <Input value={confirm} onChange={(e) => setConfirm(e.target.value)} placeholder={name} autoFocus />
            </div>
            <label className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground">
              <Checkbox checked={permanent} onCheckedChange={(v) => setPermanent(!!v)} />
              回收站不可用时直接永久删除（无法恢复）
            </label>
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button disabled={!ok} className={mode === "delete" ? "bg-red-600 text-white hover:bg-red-700" : ""} onClick={async () => {
            if (!folder) return;
            const q = mode === "delete" ? `?deleteFiles=1${permanent ? "&permanent=1" : ""}` : "";
            let msg = "";
            const done = await run(async () => {
              const r = await api<{ filesDeleted: boolean; trashed: boolean }>("DELETE", `/api/folders/${folder.id}${q}`);
              msg = !r.filesDeleted ? `已移除「${name}」，本地文件已保留` : r.trashed ? `已移除「${name}」，本地文件已移到回收站` : `已移除「${name}」，本地文件已永久删除`;
            });
            if (done) { toast.success(msg); onClose(); }
          }}>{mode === "delete" ? <><Trash2 />移除并删除文件</> : <><FolderMinus />移除</>}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 只读共享：成员一侧显示本机改动并可恢复为共享版本；共享者一侧在成员有不同版本时可“以我的版本为准” */
function ReadOnlyBar({ folder }: { folder: Folder }) {
  const { run } = useData();
  if (folder.mode !== "readonly" || folder.paused) return null;
  const changed = folder.status?.receiveOnlyChangedFiles ?? 0;
  if (folder.role === "member" && changed > 0) return (
    <div className="mt-3 flex items-center gap-2 rounded-lg border border-amber-400/40 bg-amber-500/10 p-2.5 text-xs text-amber-800 dark:text-amber-300" data-testid="readonly-local-changes">
      <AlertTriangle className="size-4 shrink-0" />
      <span className="flex-1">本机有 <b>{changed}</b> 个文件与共享版本不同（只读文件夹中的修改不会同步给其他人）。</span>
      <Button size="sm" variant="outline" className="h-7" onClick={() => run(() => api("POST", `/api/folders/${folder.id}/revert`), "已恢复为共享版本")}><Undo2 />恢复为共享版本</Button>
    </div>
  );
  if (folder.role === "owner" && (folder.status?.needFiles ?? 0) > 0) return (
    <div className="mt-3 flex items-center gap-2 rounded-lg border border-sky-500/30 bg-sky-500/5 p-2.5 text-xs text-sky-800 dark:text-sky-300">
      <Info className="size-4 shrink-0" />
      <span className="flex-1">有 {folder.status!.needFiles} 个文件在其他成员处版本不同（只读共享不会采用他们的改动）。</span>
      <Button size="sm" variant="outline" className="h-7" onClick={() => run(() => api("POST", `/api/folders/${folder.id}/override`), "已以你的版本为准")}><Upload />以我的版本为准</Button>
    </div>
  );
  return null;
}

/** 文件夹故障（.stfolder 丢失 / 路径丢失）提示与修复 */
function ProblemBanner({ folder, onRemove }: { folder: Folder; onRemove: () => void }) {
  const { run, refresh } = useData();
  const p = folder.problem!;
  const [confirmEmpty, setConfirmEmpty] = useState(false);
  const [busy, setBusy] = useState(false);
  const repair = async (force: boolean) => {
    setBusy(true);
    try {
      await api("POST", `/api/folders/${folder.id}/repair`, { force });
      toast.success(`已修复「${folder.label || folder.id}」，正在重新扫描`);
      setConfirmEmpty(false);
      void refresh();
    } catch (e) {
      if (e instanceof ApiError && e.data.needConfirm) setConfirmEmpty(true);
      else toast.error("修复失败", { description: (e as Error).message });
    } finally { setBusy(false); }
  };
  const [pendingPath, setPendingPath] = useState<{ path: string; entries: number } | null>(null);
  const doRelocate = (path: string) => run(() => api("POST", `/api/folders/${folder.id}/relocate`, { path }), `已将「${folder.label || folder.id}」指向新位置`).then(() => setPendingPath(null));
  const relocate = async () => {
    const path = await pickDir();
    if (!path) return;
    try {
      const info = await checkPath(path, folder.id);
      if (!info.ok) { toast.error("不能使用该位置", { description: info.error }); return; }
      if (info.exists && !info.empty) { setPendingPath({ path, entries: info.entries }); return; }
      await doRelocate(path);
    } catch (e) { toast.error("操作失败", { description: (e as Error).message }); }
  };
  return (
    <div className="mt-3 rounded-lg border border-red-500/30 bg-red-500/5 p-3 text-xs" data-testid="folder-problem">
      <div className="flex gap-2 text-red-700 dark:text-red-400"><AlertTriangle className="mt-0.5 size-4 shrink-0" /><span>{p.message}</span></div>
      {p.kind === "markerMissing" && p.inGitRepo && (
        <p className="mt-2 pl-6 text-muted-foreground">该文件夹位于 git 仓库内。建议在仓库的 <span className="font-mono">.gitignore</span> 中加入 <span className="font-mono">.stfolder/</span> 和 <span className="font-mono">.stversions/</span>，并避免执行 <span className="font-mono">git clean -x</span>（会删除被忽略的文件）。</p>
      )}
      {confirmEmpty && (
        <p className="mt-2 pl-6 font-medium text-red-700 dark:text-red-400">文件夹是空的。如果文件确实丢失，修复后其他成员那里的文件也可能被删除。建议先确认文件是否被移走；确定这是全新的空文件夹再继续。</p>
      )}
      {pendingPath && (
        <p className="mt-2 pl-6 font-medium text-amber-700 dark:text-amber-400">所选文件夹 <span className="font-mono">{pendingPath.path}</span> 不是空的（已有 {pendingPath.entries} 项），其中的文件会与其他成员互相同步。确定使用？</p>
      )}
      <div className="mt-2 flex flex-wrap gap-2 pl-6">
        {pendingPath && <><Button size="sm" variant="destructive" onClick={() => doRelocate(pendingPath.path)}>仍然使用该位置</Button><Button size="sm" variant="outline" onClick={() => setPendingPath(null)}>取消</Button></>}
        {p.kind === "markerMissing" && (confirmEmpty
          ? <><Button size="sm" variant="destructive" disabled={busy} onClick={() => repair(true)}><Wrench />仍然修复</Button><Button size="sm" variant="outline" onClick={() => setConfirmEmpty(false)}>取消</Button></>
          : <Button size="sm" disabled={busy} onClick={() => repair(false)}><Wrench />修复</Button>)}
        {p.kind === "overlap" && !pendingPath && <>
          {isTauri() && <Button size="sm" onClick={relocate}><FolderOpen />重新选择位置</Button>}
          <Button size="sm" variant="outline" onClick={onRemove}><FolderMinus />移除</Button>
        </>}
        {p.kind === "pathMissing" && <>
          {isTauri() && <Button size="sm" onClick={relocate}><FolderOpen />重新选择位置</Button>}
          <Button size="sm" variant="outline" onClick={onRemove}><FolderMinus />移除</Button>
        </>}
      </div>
    </div>
  );
}

const MEMBER_STATE: Record<FolderMember["remoteState"], [string, string]> = {
  valid: ["已加入", "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"],
  notSharing: ["未加入", "bg-amber-500/10 text-amber-700 dark:text-amber-400"],
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
  const [removing, setRemoving] = useState<Folder | null>(null);
  const [settingsOf, setSettings] = useState<Folder | null>(null);
  const [versionsOf, setVersions] = useState<Folder | null>(null);

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
            const tone = f.problem ? "red" : st === "idle" ? "green" : st === "error" ? "red" : st === "paused" ? "gray" : "amber";
            return (
              <Card key={f.id} className="py-4">
                <CardContent className="px-5">
                  <div className="flex items-start gap-4">
                    <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-violet-500/10 text-violet-600"><FolderSync className="size-5" /></div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{f.label || f.id}</span>
                        <Badge variant="outline" className="gap-1.5"><Dot tone={tone} pulse={tone === "amber"} />{f.problem ? (f.problem.kind === "overlap" ? "位置重叠" : f.problem.kind === "pathMissing" ? "路径丢失" : f.problem.kind === "markerMissing" ? "需要修复" : "出错") : STATE_CN[st] ?? st}</Badge>
                        {f.mode === "readonly" && (
                          <Tooltip><TooltipTrigger asChild>
                            <Badge variant="outline" className="gap-1 border-sky-500/40 text-sky-700 dark:text-sky-300" data-testid="readonly-badge"><Lock className="size-3" />只读</Badge>
                          </TooltipTrigger><TooltipContent>{f.role === "owner" ? "只读共享：只有你的修改会同步给成员" : "只读共享：本机的修改不会同步给其他人"}</TooltipContent></Tooltip>
                        )}
                        {f.conflicts > 0 && <Badge className="bg-red-500 text-white">{f.conflicts} 个冲突</Badge>}
                      </div>
                      <Tooltip><TooltipTrigger asChild>
                        <button className="mt-0.5 block max-w-full truncate text-left font-mono text-xs text-muted-foreground hover:text-foreground hover:underline" onClick={() => openFolder(f)} data-testid="folder-path">{f.path}</button>
                      </TooltipTrigger><TooltipContent>在资源管理器中打开</TooltipContent></Tooltip>
                      <div className="mt-3 flex items-center gap-3">
                        <Progress value={pct} className={`h-1.5 flex-1 ${pct === 100 ? "[&_[data-slot=progress-indicator]]:bg-emerald-500" : "[&_[data-slot=progress-indicator]]:bg-sky-500"}`} />
                        <span className="w-10 text-right text-xs tabular-nums text-muted-foreground">{pct}%</span>
                      </div>
                      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                        <span>{f.status?.localFiles ?? 0} 个文件 · {formatBytes(f.status?.globalBytes)}</span>
                        {f.status?.needFiles ? <span className="text-amber-600">还差 {f.status.needFiles} 个文件</span> : null}
                        {f.versioning?.type
                          ? <button className="flex items-center gap-1 hover:text-foreground hover:underline" onClick={() => setVersions(f)}><History className="size-3" />历史版本 保留 {f.versioning.params?.keep ?? "-"} 份</button>
                          : <span className="flex items-center gap-1 text-amber-600"><ShieldCheck className="size-3" />未开启删除保护</span>}
                        {f.fsWatcherEnabled && <span>实时监视</span>}
                      </div>
                      <div className="mt-3 flex flex-wrap items-center gap-1.5">
                        <Users className="mr-1 size-3.5 text-muted-foreground" />
                        {f.members.length === 0 && <span className="text-xs text-muted-foreground">仅本机，未共享给成员</span>}
                        {f.members.map((m) => <MemberChip key={m.deviceID} folder={f} m={m} />)}
                      </div>
                      <ReadOnlyBar folder={f} />
                      {f.problem && <ProblemBanner folder={f} onRemove={() => setRemoving(f)} />}
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
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild><Button variant="ghost" size="icon"><MoreHorizontal /></Button></DropdownMenuTrigger>
                        <DropdownMenuContent align="end" className="w-44">
                          <DropdownMenuItem onClick={() => openFolder(f)}><FolderOpen />打开文件夹</DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem onClick={() => setEditing(f)}><UserCog />管理成员</DropdownMenuItem>
                          <DropdownMenuItem onClick={() => setSettings(f)}><Settings2 />共享设置</DropdownMenuItem>
                          <DropdownMenuItem onClick={() => setVersions(f)}><History />历史版本</DropdownMenuItem>
                          <DropdownMenuItem onClick={() => run(() => api("POST", `/api/folders/${f.id}/scan`), "已开始扫描")}><RefreshCw />立即扫描</DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem variant="destructive" onClick={() => setRemoving(f)}><FolderMinus />移除…</DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
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
      <RemoveDialog folder={removing} onClose={() => setRemoving(null)} />
      <FolderSettingsDialog folder={settingsOf} onClose={() => setSettings(null)} />
      <VersionsDialog folder={versionsOf} onClose={() => setVersions(null)} />
    </>
  );
}
