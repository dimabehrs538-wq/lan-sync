import { CheckCircle2, FileWarning, GitCompare } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Empty, PageHeader } from "@/components/common";
import { api } from "@/lib/api";
import { useData } from "@/lib/store";

export default function Conflicts() {
  const { conflicts, devices, folders, run } = useData();
  const devName = (s: string) => devices.find((d) => d.deviceID.startsWith(s))?.name || s;
  const rel = (folderID: string, p: string) => {
    const root = folders.find((f) => f.id === folderID)?.path ?? "";
    return p.startsWith(root) ? p.slice(root.length).replace(/^[\\/]/, "") : p;
  };
  return (
    <>
      <PageHeader title="冲突中心" desc="两人同时修改同一文件时会生成冲突副本。选择保留哪个版本，另一个会被删除（可在历史版本 .stversions 中找回）。" />
      {conflicts.length === 0 ? (
        <Empty icon={<CheckCircle2 className="size-5 text-emerald-600" />} title="没有冲突" desc="建议团队约定“一人一文档/一人一子目录”，可以大幅减少冲突。" />
      ) : (
        <Card className="py-0">
          <CardContent className="px-0">
            <Table>
              <TableHeader>
                <TableRow><TableHead className="pl-5">文件</TableHead><TableHead>文件夹</TableHead><TableHead>冲突来源</TableHead><TableHead>时间</TableHead><TableHead className="pr-5 text-right">处理</TableHead></TableRow>
              </TableHeader>
              <TableBody>
                {conflicts.map((c) => (
                  <TableRow key={c.path}>
                    <TableCell className="pl-5">
                      <div className="flex items-center gap-2.5">
                        <div className="flex size-8 items-center justify-center rounded-md bg-red-500/10 text-red-600"><FileWarning className="size-4" /></div>
                        <div>
                          <div className="font-medium">{rel(c.folderID, c.original)}</div>
                          <div className="text-[11px] text-muted-foreground">冲突副本：{c.path.split(/[\\/]/).pop()}</div>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell><Badge variant="outline">{c.folderLabel}</Badge></TableCell>
                    <TableCell>{devName(c.device)}</TableCell>
                    <TableCell className="text-muted-foreground">{new Date(c.time).toLocaleString("zh-CN")}</TableCell>
                    <TableCell className="pr-5 text-right">
                      <div className="flex justify-end gap-2">
                        <Button size="sm" variant="outline" onClick={() => run(() => api("POST", "/api/conflicts/resolve", { folderID: c.folderID, path: c.path, keep: "original" }), "已保留原文件")}>保留原文件</Button>
                        <Button size="sm" onClick={() => run(() => api("POST", "/api/conflicts/resolve", { folderID: c.folderID, path: c.path, keep: "conflict" }), "已采用冲突版本")}><GitCompare />采用冲突版本</Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </>
  );
}
