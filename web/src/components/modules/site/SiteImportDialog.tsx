"use client";

import { useImportAllAPIHub, useImportMetAPI } from "@/api/endpoints/site";
import { toast } from "@/components/common/Toast";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { useSettingStore } from "@/stores/setting";
import { FileJson, TriangleAlert, Upload, X } from "lucide-react";
import { useTranslations } from "next-intl";
import { useRef, useState, type DragEvent } from "react";
import { getSiteErrorMessage } from "./site-display";
import { IconActionButton, SiteMetric } from "./SiteStatus";
import { ImportSource, SiteImportResult } from "./types";

export function SiteImportDialog({
  open: importDialogOpen,
  onOpenChange: setImportDialogOpen,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useTranslations();
  const locale = useSettingStore((state) => state.locale);
  const importAllAPIHub = useImportAllAPIHub();

  const importMetAPI = useImportMetAPI();

  const [importPayloadText, setImportPayloadText] = useState("");

  const [importFile, setImportFile] = useState<File | null>(null);

  const importFileInputRef = useRef<HTMLInputElement | null>(null);

  const importDragDepthRef = useRef(0);

  const [isImportDragging, setIsImportDragging] = useState(false);

  const [importSource, setImportSource] = useState<ImportSource>("all-api-hub");

  const [lastImportResult, setLastImportResult] = useState<SiteImportResult | null>(null);

  async function handleImportSites() {
    const hasFile = !!importFile;
    const hasText = !!importPayloadText.trim();
    if (!hasFile && !hasText) {
      toast.error("请选择 JSON 文件或粘贴导出内容");
      return;
    }

    try {
      const payload = {
        file: importFile,
        text: importPayloadText,
      };
      const result =
        importSource === "metapi"
          ? await importMetAPI.mutateAsync(payload)
          : await importAllAPIHub.mutateAsync(payload);
      setLastImportResult(result);
      setImportFile(null);
      setImportPayloadText("");
      toast.success(
        `导入完成：新增 ${result.created_sites} 个站点，新增 ${result.created_accounts} 个账号，更新 ${result.updated_accounts} 个账号`,
      );
    } catch (importError) {
      toast.error(getSiteErrorMessage(locale, importError, t));
    }
  }

  function setSelectedImportFile(file: File | null) {
    setImportFile(file);
    setLastImportResult(null);
    setIsImportDragging(false);
    importDragDepthRef.current = 0;
    if (!file && importFileInputRef.current) {
      importFileInputRef.current.value = "";
    }
  }

  function isImportFileDrag(event: DragEvent<HTMLDivElement>) {
    return Array.from(event.dataTransfer.types).includes("Files");
  }

  function handleImportDragEnter(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
    importDragDepthRef.current += 1;
    setIsImportDragging(true);
  }

  function handleImportDragLeave(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
    importDragDepthRef.current = Math.max(0, importDragDepthRef.current - 1);
    if (importDragDepthRef.current === 0) {
      setIsImportDragging(false);
    }
  }

  function handleImportDragOver(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
  }

  function handleImportDrop(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
    setSelectedImportFile(event.dataTransfer.files?.[0] ?? null);
  }

  return (
    <Dialog
      open={importDialogOpen}
      onOpenChange={(open) => {
        setImportDialogOpen(open);
        if (!open) setLastImportResult(null);
      }}
    >
      <DialogContent className="max-w-3xl rounded-3xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FileJson className="size-5" />
            导入站点数据
          </DialogTitle>
          <DialogDescription>
            支持上传或粘贴 All API Hub / Metapi 导出的
            JSON。导入会按平台和站点地址自动创建或复用站点。
          </DialogDescription>
        </DialogHeader>

        <div
          className="space-y-5"
          onDragEnter={handleImportDragEnter}
          onDragLeave={handleImportDragLeave}
          onDragOver={handleImportDragOver}
          onDrop={handleImportDrop}
        >
          <div className="grid gap-2 text-sm">
            <span className="font-medium">导入来源</span>
            <Select
              value={importSource}
              onValueChange={(value) => {
                setImportSource(value as ImportSource);
                setLastImportResult(null);
              }}
            >
              <SelectTrigger className="rounded-xl">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all-api-hub">All API Hub</SelectItem>
                <SelectItem value="metapi">Metapi</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="grid gap-2 text-sm">
            <div className="text-sm font-medium">上传 JSON 文件</div>
            <div className="flex items-center gap-2">
              <Input
                ref={importFileInputRef}
                type="file"
                accept=".json,application/json"
                onChange={(event) => {
                  setSelectedImportFile(event.target.files?.[0] ?? null);
                }}
                className="hidden"
              />
              <button
                type="button"
                onClick={() => importFileInputRef.current?.click()}
                className={cn(
                  "flex min-w-0 flex-1 items-center justify-center rounded-xl border border-dashed px-3 text-center text-sm transition-all hover:bg-muted/30",
                  isImportDragging
                    ? "min-h-28 border-primary bg-primary/10 text-primary"
                    : "min-h-10 border-border bg-muted/20",
                )}
              >
                <span
                  className={cn(
                    "min-w-0 truncate",
                    importFile ? "text-foreground" : "text-muted-foreground",
                  )}
                >
                  {isImportDragging
                    ? "松开即可上传 JSON 文件"
                    : (importFile?.name ?? "点击选择或拖拽 JSON 文件到这里")}
                </span>
              </button>
              <IconActionButton
                label="清除文件"
                onClick={() => {
                  setSelectedImportFile(null);
                }}
                disabled={!importFile}
                className={!importFile ? "opacity-50" : undefined}
              >
                <X className="size-4" />
              </IconActionButton>
            </div>
            <div className="text-xs text-muted-foreground">
              {importFile
                ? `已选择：${importFile.name}`
                : `支持 ${importSource === "metapi" ? "Metapi" : "All API Hub"} 导出的 .json 文件`}
            </div>
          </div>

          <label className="grid gap-2 text-sm">
            <span className="font-medium">或粘贴导出 JSON</span>
            <textarea
              value={importPayloadText}
              onChange={(event) => {
                setImportPayloadText(event.target.value);
                setLastImportResult(null);
              }}
              placeholder={
                importSource === "metapi"
                  ? '粘贴类似 {"version":"2.1","accounts":{"sites":[...],"accounts":[...]}} 的完整导出内容'
                  : '粘贴类似 {"accounts":{"accounts":[...]}} 的完整导出内容'
              }
              className="min-h-40 rounded-2xl border border-input bg-background px-4 py-3 font-mono text-xs outline-none transition focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/20"
            />
            <span className="text-xs text-muted-foreground">
              {importSource === "metapi"
                ? "Metapi 导入只迁移站点、账号、Key、分组和模型；路由策略与下游 Key 会跳过。"
                : "导入会保留已存在站点的本地配置；同一分组下的多个 key 后续仍会聚合到同一个托管 channel。"}
            </span>
          </label>

          {lastImportResult ? (
            <div className="space-y-4 rounded-2xl border border-border/60 bg-muted/10 p-4">
              <div className="grid gap-3 sm:grid-cols-3">
                <SiteMetric label="新增站点" value={lastImportResult.created_sites} />
                <SiteMetric label="复用站点" value={lastImportResult.reused_sites} />
                <SiteMetric label="新增账号" value={lastImportResult.created_accounts} />
                <SiteMetric label="更新账号" value={lastImportResult.updated_accounts} />
                <SiteMetric label="跳过账号" value={lastImportResult.skipped_accounts} />
                {typeof lastImportResult.scheduled_sync_accounts === "number" ? (
                  <SiteMetric label="后台同步" value={lastImportResult.scheduled_sync_accounts} />
                ) : null}
                {typeof lastImportResult.imported_tokens === "number" ? (
                  <>
                    <SiteMetric label="导入 Key" value={lastImportResult.imported_tokens} />
                    <SiteMetric label="导入分组" value={lastImportResult.imported_groups ?? 0} />
                    <SiteMetric label="导入模型" value={lastImportResult.imported_models ?? 0} />
                    <SiteMetric label="禁用模型" value={lastImportResult.disabled_models ?? 0} />
                  </>
                ) : null}
              </div>

              {lastImportResult.warnings.length > 0 ? (
                <div className="rounded-2xl border border-border/60 bg-background/70 p-4">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <TriangleAlert className="size-4 text-muted-foreground" />
                    <span>导入告警</span>
                  </div>
                  <div className="mt-3 space-y-2 text-sm text-muted-foreground">
                    {lastImportResult.warnings.map((warning) => (
                      <div
                        key={warning}
                        className="break-all rounded-xl border border-border/60 bg-muted/20 px-3 py-2"
                      >
                        {warning}
                      </div>
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          ) : null}
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            className="rounded-xl"
            onClick={() => setImportDialogOpen(false)}
          >
            关闭
          </Button>
          <Button
            onClick={handleImportSites}
            disabled={importAllAPIHub.isPending || importMetAPI.isPending}
            className="rounded-xl"
          >
            <Upload
              className={cn(
                "size-4",
                importAllAPIHub.isPending || importMetAPI.isPending ? "animate-pulse" : "",
              )}
            />
            {importAllAPIHub.isPending || importMetAPI.isPending ? "导入中..." : "开始导入"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
