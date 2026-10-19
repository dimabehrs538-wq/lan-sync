import { useEffect, useState } from "react";
import { Check, Eraser, HardDrive, Settings2, SlidersHorizontal } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api, DEFAULT_RETENTION, formatBytes, type Folder, type PruneResult, type Retention } from "@/lib/api";
import { useData } from "@/lib/store";
import { cn } from "@/lib/utils";

/** 容量上限显示：1024 MB 以上按 GB */
export function formatCap(mb: number) {
  if (mb >= 1024) { const g = mb / 1024; return `${Number.isInteger(g) ? g : g.toFixed(1)} GB`; }
  return `${mb} MB`;
}

/** 「保留 10 份 · 30 天」 */
export function retentionText(r?: Retention) {
  const x = r ?? DEFAULT_RETENTION;
  return `保留 ${x.keep} 份 · ${x.days} 天`;
}

export function validateRetention(r: Retention): string | null {
  if (!Number.isInteger(r.keep) || r.keep < 1 || r.keep > 100) return "保留份数需在 1–100 之间";
  if (!Number.isInteger(r.days) || r.days < 1 || r.days > 365) return "保留天数需在 1–365 之间";
  if (!Number.isFinite(r.maxMB) || r.maxMB < 1 || r.maxMB > 1048576) return "总容量上限需在 1 MB – 1024 GB 之间";
  return null;
}

const same = (a?: Retention, b?: Retention) => !!a && !!b && a.keep === b.keep && a.days === b.days && a.maxMB === b.maxMB;

/** 保留份数 / 保留天数 / 总容量上限 三个输入框（外部重置时请改变 key 重新挂载） */
export function RetentionFields({ value, onChange, disabled }: { value: Retention; onChange: (r: Retention) => void; disabled?: boolean }) {
  const [unit, setUnit] = useState<"MB" | "GB">(value.maxMB >= 1024 && value.maxMB % 1024 === 0 ? "GB" : "MB");
  const [cap, setCap] = useState(String(unit === "GB" ? value.maxMB / 1024 : value.maxMB));
  const setCapMB = (c: string, u: "MB" | "GB") => {
    setCap(c); setUnit(u);
    const n = Number(c);
    onChange({ ...value, maxMB: Number.isFinite(n) && c !== "" ? Math.round(u === "GB" ? n * 1024 : n) : 0 });
  };
  const num = (v: string) => (v === "" ? 0 : Math.floor(Number(v)));
  return (
    <div className="grid grid-cols-3 gap-3" data-testid="retention-fields">
      <div className="space-y-1.5">
        <Label className="text-xs">保留份数</Label>
        <div className="relative">
          <Input type="number" min={1} max={100} value={value.keep || ""} disabled={disabled} onChange={(e) => onChange({ ...value, keep: num(e.target.value) })} className="pr-8" />
          <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs text-muted-foreground">份</span>
        </div>
        <p className="text-[11px] text-muted-foreground">每个文件，1–100</p>
      </div>
      <div className="space-y-1.5">
        <Label className="text-xs">保留天数</Label>
        <div className="relative">
          <Input type="number" min={1} max={365} value={value.days || ""} disabled={disabled} onChange={(e) => onChange({ ...value, days: num(e.target.value) })} className="pr-8" />
          <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs text-muted-foreground">天</span>
        </div>
        <p className="text-[11px] text-muted-foreground">1–365，超过自动删除</p>
      </div>
      <div className="space-y-1.5">
        <Label className="text-xs">历史版本总容量上限</Label>
        <div className="flex gap-1.5">
          <Input type="number" min={1} step="any" value={cap} disabled={disabled} onChange={(e) => setCapMB(e.target.value, unit)} className="min-w-0" />
          <Select value={unit} disabled={disabled} onValueChange={(u) => setCapMB(cap, u as "MB" | "GB")}>
            <SelectTrigger className="w-[72px]"><SelectValue /></SelectTrigger>
            <SelectContent><SelectItem value="MB">MB</SelectItem><SelectItem value="GB">GB</SelectItem></SelectContent>
          </Select>
        </div>
        <p className="text-[11px] text-muted-foreground">每个文件夹，超出先删最旧的</p>
      </div>
    </div>
  );
}

/** 当前历史版本占用 + 立即清理 */
export function VersionsUsage({ folder, compact, onCleaned }: { folder: Folder; compact?: boolean; onCleaned?: () => void }) {
  const { refresh } = useData();
  const [busy, setBusy] = useState(false);
  const r = folder.retention ?? { ...DEFAULT_RETENTION, custom: false };
  const used = folder.versionsSize ?? 0;
  const pct = Math.min(100, (used / (r.maxMB * 1024 * 1024)) * 100);
  const cleanup = async () => {
    setBusy(true);
    try {
      const res = await api<PruneResult>("POST", `/api/folders/${folder.id}/versions/cleanup`);
      if (res.removed) toast.success(`已清理 ${res.removed} 个旧版本`, { description: `释放 ${formatBytes(res.freed)}，当前占用 ${formatBytes(res.size)}` });
      else toast.success("没有需要清理的旧版本", { description: `当前占用 ${formatBytes(res.size)}，均在保留规则之内` });
      await refresh(); onCleaned?.();
    } catch (e) { toast.error("清理失败", { description: (e as Error).message }); }
    finally { setBusy(false); }
  };
  return (
    <div className={cn("flex items-center gap-3 rounded-lg border p-3", compact && "py-2")} data-testid="versions-usage">
      <HardDrive className="size-4 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2 text-sm">
          <span>当前占用 <b className="tabular-nums">{formatBytes(used)}</b><span className="text-xs text-muted-foreground"> · {folder.versionsFiles ?? 0} 个版本</span></span>
          <span className="text-xs text-muted-foreground">上限 {formatCap(r.maxMB)}</span>
        </div>
        <Progress value={pct} className={cn("mt-1.5 h-1", pct >= 90 ? "[&_[data-slot=progress-indicator]]:bg-amber-500" : "[&_[data-slot=progress-indicator]]:bg-sky-500")} />
      </div>
      <Button size="sm" variant="outline" disabled={busy} onClick={cleanup}><Eraser />{busy ? "清理中…" : "立即清理"}</Button>
    </div>
  );
}

/** 文件夹卡片「历史版本设置」：使用默认设置 / 自定义 */
export function RetentionDialog({ folder, onClose }: { folder: Folder | null; onClose: () => void }) {
  const { run, settings, folders } = useData();
  const defaults = settings?.retention ?? DEFAULT_RETENTION;
  const [custom, setCustom] = useState(false);
  const [val, setVal] = useState<Retention>(defaults);
  const [seed, setSeed] = useState(0);
  useEffect(() => {
    if (!folder) return;
    const r = folder.retention;
    setCustom(!!r?.custom);
    setVal(r ? { keep: r.keep, days: r.days, maxMB: r.maxMB } : defaults);
    setSeed((x) => x + 1);
  }, [folder?.id]);
  const live = folders.find((f) => f.id === folder?.id) ?? folder; // 清理后显示最新占用
  const err = custom ? validateRetention(val) : null;
  const cur = folder?.retention;
  const unchanged = !!cur && cur.custom === custom && (!custom || same(cur, val));
  const save = async () => {
    if (!folder) return;
    const body = custom ? { custom: true, ...val } : { custom: false };
    if (await run(() => api("PUT", `/api/folders/${folder.id}/retention`, body), "已保存历史版本设置")) onClose();
  };
  const opts = [
    { v: false, icon: <Settings2 className="size-4" />, title: "使用默认设置", desc: `${retentionText(defaults)} · 上限 ${formatCap(defaults.maxMB)}（在“设置”中修改）` },
    { v: true, icon: <SlidersHorizontal className="size-4" />, title: "自定义", desc: "只对这个文件夹生效" },
  ];
  return (
    <Dialog open={!!folder} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>「{folder?.label}」的历史版本设置</DialogTitle>
          <DialogDescription>
            {folder?.deleteProtection ? "被修改或删除的文件在本机保留的旧版本。超过份数、天数或总容量上限时，最旧的版本会被自动删除。" : "该文件夹未开启删除保护，不会产生新的历史版本；可在“共享设置”中开启。"}
          </DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-2 gap-2">
          {opts.map((o) => (
            <button key={String(o.v)} type="button" onClick={() => { setCustom(o.v); if (!o.v) setVal(defaults); }}
              className={cn("flex items-start gap-2.5 rounded-lg border p-3 text-left transition-colors hover:bg-muted/50", custom === o.v && "border-primary ring-2 ring-primary/15")}>
              <span className="mt-0.5 text-muted-foreground">{o.icon}</span>
              <span><span className="block text-sm font-medium">{o.title}</span><span className="mt-0.5 block text-xs text-muted-foreground">{o.desc}</span></span>
            </button>
          ))}
        </div>
        <RetentionFields key={`${seed}-${custom}`} value={custom ? val : defaults} onChange={setVal} disabled={!custom} />
        {err && <p className="text-xs text-destructive">{err}</p>}
        {live && <VersionsUsage folder={live} />}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button onClick={save} disabled={!folder || !!err || unchanged}><Check />保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
