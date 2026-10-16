import { useEffect, useState } from "react";
import { Bell, Info, Laptop, Moon, Power, ShieldCheck, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { toast } from "sonner";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { PageHeader } from "@/components/common";
import { formatUptime, isTauri } from "@/lib/api";
import { useData } from "@/lib/store";
import { cn } from "@/lib/utils";

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
  const { status } = useData();
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
          <Row icon={Bell} title="文件变更通知" desc="成员新增或修改文件、请求加入时弹出系统通知">
            <Switch checked={notify} onCheckedChange={(v) => { setNotify(v); localStorage.setItem("notify", v ? "1" : "0"); }} />
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
          <span className="text-muted-foreground">应用版本</span><span>TeamSync 0.1.0</span>
          <span className="text-muted-foreground">同步引擎</span><span>Syncthing {status?.syncthingVersion ?? "-"}（MPL-2.0）</span>
          <span className="text-muted-foreground">引擎运行时间</span><span>{status?.uptime ? formatUptime(status.uptime) : "-"}</span>
        </CardContent>
      </Card>
    </>
  );
}
