import { useEffect, useState } from "react";
import { ArrowUpCircle, Bell, FolderOpen, History, Wifi, Info, Laptop, Moon, Power, Save, ShieldCheck, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { PageHeader } from "@/components/common";
import { api, DEFAULT_RETENTION, formatUptime, isTauri, type Retention } from "@/lib/api";
import { RetentionFields, validateRetention } from "@/components/Retention";
import { useData } from "@/lib/store";
import { cn } from "@/lib/utils";
import { useUpdater } from "@/lib/updater";

function Row({ icon: Icon, title, desc, children }: { icon: typeof Bell; title: string; desc: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-4 py-3">
      <div className="flex size-9 items-center justify-center rounded-lg bg-muted text-muted-foreground"><Icon className="size-4" /></div>
      <div className="flex-1"><div className="text-sm font-medium">{title}</div><div className="text-xs text-muted-foreground">{desc}</div></div>
      {children}
    </div>
  );
}

export default function Settings() {
  const { theme, setTheme } = useTheme();
  const { status, settings, folders, run } = useData();
  const up = useUpdater();
  const [base, setBase] = useState("");
  useEffect(() => { if (settings) setBase(settings.defaultBaseDir); }, [settings?.defaultBaseDir]);
  const [ret, setRet] = useState<Retention>(DEFAULT_RETENTION);
  const [retKey, setRetKey] = useState(0);
  const sr = settings?.retention;
  useEffect(() => { if (sr) { setRet(sr); setRetKey((k) => k + 1); } }, [sr?.keep, sr?.days, sr?.maxMB]);
  const retErr = validateRetention(ret);
  const retSame = !!sr && sr.keep === ret.keep && sr.days === ret.days && sr.maxMB === ret.maxMB;
  const customCount = folders.filter((f) => f.retention?.custom).length;
  const [auto, setAuto] = useState(false);
  const [notify, setNotify] = useState(localStorage.getItem("notify") !== "0");
  useEffect(() => {
    if (isTauri()) import("@tauri-apps/plugin-autostart").then((m) => m.isEnabled()).then(setAuto).catch(() => {});
  }, []);
  const themes = [{ v: "light", l: "浅色", i: Sun }, { v: "dark", l: "深色", i: Moon }, { v: "system", l: "跟随系统", i: Laptop }];

  return (
    <>
      <PageHeader title="设置" />
      <Card>
        <CardHeader><CardTitle className="text-sm">外观</CardTitle><CardDescription>选择界面主题</CardDescription></CardHeader>
        <CardContent className="grid grid-cols-3 gap-3">
          {themes.map((t) => (
            <button key={t.v} onClick={() => setTheme(t.v)} className={cn("flex flex-col items-center gap-2 rounded-lg border p-4 text-sm transition-colors hover:bg-muted/60", theme === t.v && "border-primary ring-2 ring-primary/20")}>
              <t.i className="size-5" />{t.l}
            </button>
          ))}
        </CardContent>
      </Card>
      <Card className="mt-4">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm"><FolderOpen className="size-4" />接收文件夹的默认位置</CardTitle>
          <CardDescription>接受同事的文件夹邀请时，默认保存到这里的“文件夹名”子目录（每次接受时仍可修改）。</CardDescription>
        </CardHeader>
        <CardContent className="flex gap-2">
          <Input value={base} onChange={(e) => setBase(e.target.value)} placeholder="例如：D:\TeamSync" className="font-mono text-xs" />
          {isTauri() && <Button variant="outline" onClick={async () => {
            const { open } = await import("@tauri-apps/plugin-dialog");
            const p = await open({ directory: true, title: "选择默认位置", defaultPath: base || undefined });
            if (typeof p === "string") setBase(p);
          }}><FolderOpen />浏览</Button>}
          <Button disabled={!base || base === settings?.defaultBaseDir} onClick={() => run(() => api("PUT", "/api/settings", { defaultBaseDir: base }), "已保存默认位置")}><Save />保存</Button>
        </CardContent>
      </Card>
      <Card className="mt-4" data-testid="retention-settings">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm"><History className="size-4" />历史版本默认保留规则</CardTitle>
          <CardDescription>开启删除保护的文件夹在本机保留被删除或覆盖的旧版本。修改后，使用默认设置的文件夹立即生效{customCount ? `（${customCount} 个文件夹使用自定义设置，不受影响）` : ""}；单个文件夹可在卡片“…”菜单 →“历史版本设置”中自定义。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <RetentionFields key={retKey} value={ret} onChange={setRet} />
          <div className="flex items-center gap-3">
            <p className={retErr ? "flex-1 text-xs text-destructive" : "flex-1 text-xs text-muted-foreground"}>{retErr ?? "超过份数、天数或总容量上限时，内核会定期从最旧的版本开始删除。"}</p>
            {!retSame && sr && <Button variant="ghost" size="sm" onClick={() => { setRet(sr); setRetKey((k) => k + 1); }}>还原</Button>}
            <Button disabled={!!retErr || retSame} onClick={() => run(() => api("PUT", "/api/settings", { retention: ret }), "已保存默认保留规则")}><Save />保存</Button>
          </div>
        </CardContent>
      </Card>
      <Card className="mt-4">
        <CardHeader><CardTitle className="text-sm">通用</CardTitle></CardHeader>
        <CardContent>
          <Row icon={Power} title="开机自动启动" desc="登录系统后自动在后台运行，只显示托盘图标">
            <Switch checked={auto} disabled={!isTauri()} onCheckedChange={async (v) => {
              const m = await import("@tauri-apps/plugin-autostart");
              try { v ? await m.enable() : await m.disable(); setAuto(v); toast.success(v ? "已开启开机自启" : "已关闭开机自启"); }
              catch (e) { toast.error("设置失败", { description: String(e) }); }
            }} />
          </Row>
          <Separator />
          <Row icon={Bell} title="文件变更通知" desc="收到文件夹邀请、成员请求加入、成员修改文件时弹出系统通知">
            <Switch checked={notify} onCheckedChange={(v) => { setNotify(v); localStorage.setItem("notify", v ? "1" : "0"); }} />
          </Row>
          <Separator />
          <Row icon={Wifi} title="上下线通知" desc="成员上线、离线或连接不稳定时弹出系统通知（默认关闭，60 秒内重连不会提示；动态记录不受影响）">
            <Switch checked={!!settings?.presenceNotify} disabled={!settings} onCheckedChange={(v) =>
              run(() => api("PUT", "/api/settings", { presenceNotify: v }), v ? "已开启上下线通知" : "已关闭上下线通知")} />
          </Row>
        </CardContent>
      </Card>
      <Card className="mt-4">
        <CardHeader><CardTitle className="flex items-center gap-2 text-sm"><ArrowUpCircle className="size-4 text-sky-600" />软件更新</CardTitle></CardHeader>
        <CardContent>
          <div className="flex items-center gap-4 py-2">
            <div className="flex-1">
              <div className="text-sm font-medium">当前版本 {up.version || "-"}</div>
              <div className="text-xs text-muted-foreground">从 GitHub Releases 获取更新，安装包经过签名校验</div>
            </div>
            <Button variant="outline" disabled={up.checking} onClick={up.checkNow}><ArrowUpCircle className={up.checking ? "animate-spin" : ""} />{up.checking ? "检查中…" : "检查更新"}</Button>
          </div>
          <Separator />
          <Row icon={ArrowUpCircle} title="自动检查更新" desc="启动时及每隔 4 小时检查一次，有新版本时提示">
            <Switch checked={up.autoCheck} onCheckedChange={up.setAutoCheck} />
          </Row>
        </CardContent>
      </Card>
      <Card className="mt-4">
        <CardHeader><CardTitle className="flex items-center gap-2 text-sm"><ShieldCheck className="size-4 text-emerald-600" />网络与隐私</CardTitle></CardHeader>
        <CardContent className="space-y-2 text-sm text-muted-foreground">
          <p>仅在局域网内同步：已关闭全局发现、中继服务器和 NAT 穿透，数据不经过任何外部服务器，设备之间全程 TLS 加密。</p>
          <p>防火墙需放行 TCP/UDP 22000 与 UDP 21027（局域网发现）。</p>
        </CardContent>
      </Card>
      <Card className="mt-4">
        <CardHeader><CardTitle className="flex items-center gap-2 text-sm"><Info className="size-4" />关于</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-2 gap-y-2 text-sm">
          <span className="text-muted-foreground">应用版本</span><span>TeamSync {up.version || "-"}</span>
          <span className="text-muted-foreground">同步引擎</span><span>Syncthing {status?.syncthingVersion ?? "-"}（MPL-2.0）</span>
          <span className="text-muted-foreground">引擎运行时间</span><span>{status?.uptime ? formatUptime(status.uptime) : "-"}</span>
        </CardContent>
      </Card>
    </>
  );
}
