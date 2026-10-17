import { useState } from "react";
import { WifiOff, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useData } from "@/lib/store";

/** 成员 10 分钟内断开 >=3 次时显示：多半是代理软件 TUN 模式接管了局域网流量。 */
export function UnstableBanner() {
  const { status } = useData();
  const list = status?.unstable ?? [];
  const sig = list.map((u) => u.device).sort().join(",");
  const [dismissed, setDismissed] = useState("");
  if (list.length === 0 || dismissed === sig) return null;
  const who = list.map((u) => `${u.name}（10 分钟内断开 ${u.count} 次）`).join("、");
  return (
    <div role="alert" className="mb-5 flex gap-3 rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200">
      <WifiOff className="mt-0.5 size-4 shrink-0" />
      <div className="flex-1 space-y-1.5">
        <div className="font-medium">与成员的连接频繁断开：{who}</div>
        <p className="text-xs leading-relaxed opacity-90">
          可能是本机或对方开启了代理软件的 <b>TUN 模式</b>（虚拟网卡接管全部流量）。请在代理软件中把局域网设为直连：
        </p>
        <ul className="list-disc space-y-0.5 pl-5 text-xs leading-relaxed opacity-90">
          <li>绕过局域网网段 <code className="font-mono">192.168.0.0/16</code>、<code className="font-mono">10.0.0.0/8</code>、<code className="font-mono">172.16.0.0/12</code>（Clash 类软件可开启“绕过局域网”或配置 route-exclude-address）</li>
          <li>或为同步进程添加直连规则：<code className="font-mono">PROCESS-NAME,syncthing.exe,DIRECT</code></li>
          <li>确保端口 TCP/UDP <code className="font-mono">22000</code> 与 UDP <code className="font-mono">21027</code> 走直连且防火墙已放行</li>
        </ul>
      </div>
      <Button variant="ghost" size="icon" className="size-7 shrink-0 text-amber-900 hover:bg-amber-100 dark:text-amber-200 dark:hover:bg-amber-900/40" aria-label="关闭提示" onClick={() => setDismissed(sig)}><X /></Button>
    </div>
  );
}
