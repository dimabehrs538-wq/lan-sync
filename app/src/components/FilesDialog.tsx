import { useEffect, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, ExternalLink, File as FileIcon, Folder as FolderIcon, Loader2, Lock, RefreshCw, Share2, Upload } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { api, downloadFile, formatBytes, uploadFile, type FileList, type Folder } from "@/lib/api";
import { isAndroid, isIOS, openFile, shareFile } from "@/lib/platform";
import { cn } from "@/lib/utils";

const fmt = (t: string) => new Date(t).toLocaleString("zh-CN", { hour12: false, month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });

/**
 * 浏览团队文件夹中的文件（手机上没有资源管理器可以直接打开团队文件夹）：
 * 进入子目录、从系统文件选择器导入文件（不覆盖同名文件）、把文件交给系统分享或用其它应用打开。
 */
export function FilesDialog({ folder, onClose }: { folder: Folder | null; onClose: () => void }) {
  const [dir, setDir] = useState("");
  const [list, setList] = useState<FileList | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const readonly = folder?.type === "receiveonly";

  const load = async (d: string) => {
    if (!folder) return;
    setLoading(true);
    try { setList(await api<FileList>("GET", `/api/folders/${folder.id}/files?${new URLSearchParams({ dir: d })}`)); setDir(d); }
    catch (e) { toast.error("无法读取文件列表", { description: (e as Error).message }); }
    finally { setLoading(false); }
  };
  useEffect(() => { if (folder) { setList(null); setDir(""); void load(""); } }, [folder?.id]);

  const importFiles = async (files: FileList_ | null) => {
    if (!folder || !files?.length) return;
    let ok = 0;
    for (const f of Array.from(files)) {
      setBusy(`正在导入 ${f.name}…`);
      try { const r = await uploadFile(folder.id, dir, f); ok++; if (r.name !== f.name) toast.info(`已存在同名文件，另存为「${r.name}」`); }
      catch (e) { toast.error(`导入 ${f.name} 失败`, { description: (e as Error).message }); }
    }
    setBusy("");
    if (input.current) input.current.value = "";
    if (ok) toast.success(`已导入 ${ok} 个文件，将同步给其他成员`);
    void load(dir);
  };

  const crumbs = dir ? dir.split("/") : [];
  return (
    <Dialog open={!!folder} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="flex max-h-[85dvh] flex-col gap-3 sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>「{folder?.label || folder?.id}」中的文件</DialogTitle>
          <DialogDescription className="font-mono text-[11px] break-all">{list?.root ?? folder?.path}</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-1 text-sm">
          <Button variant="ghost" size="icon" className="size-9" disabled={!dir} aria-label="上一级" onClick={() => load(crumbs.slice(0, -1).join("/"))}><ChevronLeft /></Button>
          <div className="flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto whitespace-nowrap text-xs text-muted-foreground">
            <button className="hover:text-foreground" onClick={() => load("")}>根目录</button>
            {crumbs.map((c, i) => (
              <span key={i} className="flex items-center gap-0.5"><ChevronRight className="size-3" />
                <button className="hover:text-foreground" onClick={() => load(crumbs.slice(0, i + 1).join("/"))}>{c}</button></span>
            ))}
          </div>
          <Button variant="ghost" size="icon" className="size-9" aria-label="刷新" onClick={() => load(dir)}><RefreshCw className={loading ? "animate-spin" : ""} /></Button>
        </div>
        <div className="-mx-6 min-h-40 flex-1 overflow-y-auto border-y px-3" data-testid="file-list">
          {!list ? <div className="flex justify-center py-10"><Loader2 className="animate-spin text-muted-foreground" /></div>
            : list.entries.length === 0 ? <p className="py-10 text-center text-xs text-muted-foreground">这个目录是空的</p>
            : list.entries.map((e) => (
              <div key={e.path} className="flex items-center gap-3 border-b py-2 last:border-b-0">
                <button className={cn("flex min-w-0 flex-1 items-center gap-3 py-1 text-left", !e.dir && "cursor-default")} onClick={() => e.dir && load(e.path)}>
                  {e.dir ? <FolderIcon className="size-5 shrink-0 text-violet-600" /> : <FileIcon className="size-5 shrink-0 text-muted-foreground" />}
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm">{e.name}</span>
                    <span className="block text-[11px] text-muted-foreground">{e.dir ? "文件夹" : formatBytes(e.size)} · {fmt(e.modTime)}</span>
                  </span>
                  {e.dir && <ChevronRight className="size-4 text-muted-foreground" />}
                </button>
                {!e.dir && <>
                  {isAndroid && <Button variant="ghost" size="icon" className="size-9" aria-label="打开" onClick={() => openFile(e.abs).catch((x) => toast.error("无法打开", { description: (x as Error).message }))}><ExternalLink /></Button>}
                  <Button variant="ghost" size="icon" className="size-9" aria-label="分享 / 导出" onClick={() =>
                    shareFile(e.abs, () => downloadFile(folder!.id, e.path), e.name).catch((x) => { if ((x as Error).name !== "AbortError") toast.error("无法分享", { description: (x as Error).message }); })}><Share2 /></Button>
                </>}
              </div>
            ))}
        </div>
        {readonly ? (
          <p className="flex gap-1.5 text-xs text-muted-foreground"><Lock className="mt-0.5 size-3.5 shrink-0" />只读共享文件夹：本机添加的文件不会同步给其他成员，因此不能导入。</p>
        ) : (
          <div className="flex items-center gap-2">
            <p className="flex-1 text-xs text-muted-foreground">{busy || (isIOS ? "导入的文件会同步给已加入的成员；也可在“文件”App → 我的 iPhone → 团队同步 中管理。" : "导入的文件会同步给已加入的成员，同名文件不会被覆盖。")}</p>
            <input ref={input} type="file" multiple className="hidden" onChange={(e) => importFiles(e.target.files)} />
            <Button disabled={!!busy || !list} onClick={() => input.current?.click()}>{busy ? <Loader2 className="animate-spin" /> : <Upload />}导入文件</Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

type FileList_ = globalThis.FileList;
