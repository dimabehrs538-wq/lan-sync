import { useState, type ReactNode } from "react";
import { Check, Copy } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export function PageHeader({ title, desc, actions }: { title: string; desc?: string; actions?: ReactNode }) {
  return (
    <div className="mb-6 flex items-start justify-between gap-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {desc && <p className="mt-1 text-sm text-muted-foreground">{desc}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}

export function Dot({ tone = "gray", pulse }: { tone?: "green" | "amber" | "red" | "gray" | "blue"; pulse?: boolean }) {
  const c = { green: "bg-emerald-500", amber: "bg-amber-500", red: "bg-red-500", gray: "bg-zinc-400", blue: "bg-sky-500" }[tone];
  return (
    <span className="relative inline-flex size-2">
      {pulse && <span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-60", c)} />}
      <span className={cn("relative inline-flex size-2 rounded-full", c)} />
    </span>
  );
}

const palette = ["bg-sky-500", "bg-violet-500", "bg-emerald-500", "bg-amber-500", "bg-rose-500", "bg-indigo-500", "bg-teal-500"];
export function Avatar({ name, id, online }: { name: string; id: string; online?: boolean }) {
  const h = [...id].reduce((a, c) => a + c.charCodeAt(0), 0);
  return (
    <div className="relative">
      <div className={cn("flex size-9 items-center justify-center rounded-full text-sm font-medium text-white", palette[h % palette.length])}>
        {(name || id).slice(0, 1).toUpperCase()}
      </div>
      {online !== undefined && (
        <span className={cn("absolute -right-0.5 -bottom-0.5 size-3 rounded-full border-2 border-card", online ? "bg-emerald-500" : "bg-zinc-400")} />
      )}
    </div>
  );
}

export function Empty({ icon, title, desc, action }: { icon: ReactNode; title: string; desc?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center rounded-xl border border-dashed px-6 py-12 text-center">
      <div className="mb-3 flex size-11 items-center justify-center rounded-full bg-muted text-muted-foreground">{icon}</div>
      <div className="text-sm font-medium">{title}</div>
      {desc && <div className="mt-1 max-w-sm text-xs text-muted-foreground">{desc}</div>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

export function CopyButton({ text, label = "复制" }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  return (
    <Button variant="outline" size="sm" onClick={async () => {
      await navigator.clipboard.writeText(text);
      setDone(true); toast.success("已复制到剪贴板"); setTimeout(() => setDone(false), 1500);
    }}>
      {done ? <Check /> : <Copy />}{label}
    </Button>
  );
}

export function DeviceIdText({ id, className }: { id: string; className?: string }) {
  return <code className={cn("font-mono text-xs break-all text-muted-foreground", className)}>{id}</code>;
}
