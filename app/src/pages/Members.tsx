import { useState } from "react";
import { Check, QrCode, Radar, Trash2, UserPlus, Users, X } from "lucide-react";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Avatar, CopyButton, DeviceIdText, Empty, PageHeader } from "@/components/common";
import { api, short } from "@/lib/api";
import { useData } from "@/lib/store";

function AddDeviceDialog({ open, onOpenChange, preset }: { open: boolean; onOpenChange: (v: boolean) => void; preset?: string }) {
  const { run } = useData();
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const deviceID = preset ?? id;
  return (
    <Dialog open={open} onOpenChange={(v) => { onOpenChange(v); if (!v) { setId(""); setName(""); } }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{preset ? "添加附近设备" : "手动添加成员"}</DialogTitle>
          <DialogDescription>添加后对方会收到加入请求；对方同意后即成为成员，之后可以在“团队文件夹”里邀请他加入指定文件夹。</DialogDescription>
        </DialogHeader>
        <form id="add-dev" className="space-y-4" onSubmit={async (e) => {
          e.preventDefault();
          if (await run(() => api("POST", "/api/devices", { deviceID, name }), "已添加，等待对方同意")) { onOpenChange(false); setId(""); setName(""); }
        }}>
          <div className="space-y-2">
            <Label>设备 ID</Label>
            {preset ? <div className="rounded-md border bg-muted/40 px-3 py-2"><DeviceIdText id={preset} /></div>
              : <Input value={id} onChange={(e) => setId(e.target.value)} placeholder="XXXXXXX-XXXXXXX-…" className="font-mono text-xs" required autoFocus />}
          </div>
          <div className="space-y-2"><Label>备注名</Label><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：张三" autoFocus={!!preset} /></div>
        </form>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          <Button type="submit" form="add-dev" disabled={!deviceID}><UserPlus />添加</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PairDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { status } = useData();
  const id = status?.myID ?? "";
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>邀请同事加入</DialogTitle>
          <DialogDescription>同一局域网内，同事打开本软件后会在“附近设备”里看到你，点“添加”即可。也可以把下面的设备 ID 发给他手动添加。</DialogDescription>
        </DialogHeader>
        <div className="rounded-lg border bg-muted/40 p-4 text-center">
          <div className="mb-2 text-xs text-muted-foreground">本机设备 ID</div>
          <div className="font-mono text-sm leading-relaxed tracking-wide break-all">{id}</div>
          <div className="mt-2 text-xs text-muted-foreground">短码 <span className="font-mono font-semibold text-foreground">{short(id)}</span></div>
        </div>
        <DialogFooter><CopyButton text={id} label="复制设备 ID" /><Button onClick={() => onOpenChange(false)}>完成</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function Members() {
  const { devices, nearby, run } = useData();
  const [adding, setAdding] = useState(false);
  const [preset, setPreset] = useState<string | undefined>();
  const [pairing, setPairing] = useState(false);
  const [removing, setRemoving] = useState<{ id: string; name: string } | null>(null);
  const members = devices.filter((d) => !d.self);
  const pending = nearby.filter((n) => n.pending);
  const found = nearby.filter((n) => !n.pending);
  const addr = (a: string[]) => a.filter((x) => !x.includes("[")).map((x) => x.replace(/^\w+:\/\//, ""))[0] ?? "";

  return (
    <>
      <PageHeader title="成员" desc="每位成员都是一台装了本软件的电脑，点对点直连，无需服务器。"
        actions={<>
          <Button variant="outline" onClick={() => setPairing(true)}><QrCode />我的设备 ID</Button>
          <Button onClick={() => { setPreset(undefined); setAdding(true); }}><UserPlus />手动添加</Button>
        </>} />

      {pending.length > 0 && (
        <Card className="mb-4 border-sky-500/40 bg-sky-500/5">
          <CardHeader><CardTitle className="text-sm">加入请求</CardTitle><CardDescription>以下设备请求加入你的团队</CardDescription></CardHeader>
          <CardContent className="space-y-2">
            {pending.map((n) => (
              <div key={n.deviceID} className="flex items-center gap-3 rounded-lg border bg-card p-3">
                <Avatar name={n.name || n.deviceID} id={n.deviceID} />
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{n.name || short(n.deviceID)}</div>
                  <div className="truncate text-xs text-muted-foreground">{addr(n.addresses)} · <span className="font-mono">{short(n.deviceID)}</span></div>
                </div>
                <Button variant="outline" size="sm" onClick={() => run(() => api("DELETE", `/api/pending/devices/${n.deviceID}`), "已忽略")}><X />忽略</Button>
                <Button size="sm" onClick={() => run(() => api("POST", `/api/pending/devices/${n.deviceID}/accept`), "已同意，对方已成为成员")}><Check />同意</Button>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <Card className="mb-4">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm"><Radar className="size-4 text-sky-600" />附近设备
            <span className="relative ml-1 inline-flex size-2"><span className="absolute inline-flex size-full animate-ping rounded-full bg-sky-500 opacity-60" /><span className="relative size-2 rounded-full bg-sky-500" /></span>
          </CardTitle>
          <CardDescription>通过局域网广播自动发现的、尚未添加的设备</CardDescription>
        </CardHeader>
        <CardContent>
          {found.length === 0 ? <p className="text-sm text-muted-foreground">正在搜索… 请确认同事已打开本软件并处于同一局域网。</p> : (
            <div className="grid grid-cols-2 gap-2">
              {found.map((n) => (
                <div key={n.deviceID} className="flex items-center gap-3 rounded-lg border p-3">
                  <Avatar name={n.deviceID} id={n.deviceID} />
                  <div className="min-w-0 flex-1">
                    <div className="font-mono text-sm">{short(n.deviceID)}</div>
                    <div className="truncate text-xs text-muted-foreground">{addr(n.addresses)}</div>
                  </div>
                  <Button size="sm" variant="secondary" onClick={() => { setPreset(n.deviceID); setAdding(true); }}><UserPlus />添加</Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-sm">已添加成员（{members.length}）</CardTitle></CardHeader>
        <CardContent>
          {members.length === 0 ? <Empty icon={<Users className="size-5" />} title="还没有成员" desc="从附近设备中添加，或者手动输入同事的设备 ID。" /> : (
            <Table>
              <TableHeader><TableRow><TableHead>成员</TableHead><TableHead>设备 ID</TableHead><TableHead>连接</TableHead><TableHead className="w-12" /></TableRow></TableHeader>
              <TableBody>
                {members.map((d) => (
                  <TableRow key={d.deviceID}>
                    <TableCell>
                      <div className="flex items-center gap-3">
                        <Avatar name={d.name} id={d.deviceID} online={d.connected} />
                        <span className="font-medium">{d.name || short(d.deviceID)}</span>
                      </div>
                    </TableCell>
                    <TableCell><span className="font-mono text-xs text-muted-foreground">{d.deviceID.slice(0, 23)}…</span></TableCell>
                    <TableCell>
                      {d.connected
                        ? <div><Badge variant="secondary" className="bg-emerald-500/10 text-emerald-700 dark:text-emerald-400">在线</Badge><div className="mt-1 text-[11px] text-muted-foreground">{d.address?.replace(/^\w+:\/\//, "")}</div></div>
                        : <Badge variant="outline">离线</Badge>}
                    </TableCell>
                    <TableCell>
                      <Button variant="ghost" size="icon" className="text-muted-foreground hover:text-red-600" onClick={() => setRemoving({ id: d.deviceID, name: d.name || short(d.deviceID) })}><Trash2 /></Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <AddDeviceDialog open={adding} onOpenChange={setAdding} preset={preset} />
      <PairDialog open={pairing} onOpenChange={setPairing} />
      <AlertDialog open={!!removing} onOpenChange={(v) => !v && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移除成员「{removing?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>移除后将不再与该设备同步。本地文件不会被删除。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction className="bg-red-600 text-white hover:bg-red-700" onClick={() => removing && run(() => api("DELETE", `/api/devices/${removing.id}`), "已移除成员")}>移除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
