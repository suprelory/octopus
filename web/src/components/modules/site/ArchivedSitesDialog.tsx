"use client";

import { useArchivedSiteList, useRestoreSite } from "@/api/endpoints/site";
import { toast } from "@/components/common/Toast";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useSettingStore } from "@/stores/setting";
import { ArchiveRestore } from "lucide-react";
import { useTranslations } from "next-intl";
import { getSiteErrorMessage } from "./site-display";

export function ArchivedSitesDialog({
  open: archivedDialogOpen,
  onOpenChange: setArchivedDialogOpen,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useTranslations();
  const locale = useSettingStore((state) => state.locale);
  const restoreSite = useRestoreSite();

  const {
    data: archivedSites,
    isLoading: archivedLoading,
    error: archivedError,
  } = useArchivedSiteList(archivedDialogOpen);

  async function handleRestoreSite(siteId: number, siteName: string) {
    try {
      await restoreSite.mutateAsync(siteId);
      toast.success(`站点「${siteName}」已恢复，请在列表中启用`);
    } catch (err) {
      toast.error(getSiteErrorMessage(locale, err, t));
    }
  }

  return (
    <Dialog open={archivedDialogOpen} onOpenChange={setArchivedDialogOpen}>
      <DialogContent className="flex h-[min(85vh,42rem)] max-w-3xl flex-col overflow-hidden rounded-3xl border-border/70 p-0 sm:max-w-3xl">
        <DialogHeader className="shrink-0 border-b border-border/60 px-6 py-4">
          <DialogTitle>归档站点</DialogTitle>
          <DialogDescription>
            归档的站点仍保留账号、Key
            和模型配置，托管渠道会被下线。点击恢复会还原到主列表（默认保持禁用状态，启用后会自动重建托管渠道）。
          </DialogDescription>
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto px-6 py-4">
          {archivedLoading ? (
            <div className="py-10 text-center text-sm text-muted-foreground">
              正在加载归档站点...
            </div>
          ) : archivedError ? (
            <div className="rounded-2xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
              加载失败：{getSiteErrorMessage(locale, archivedError, t)}
            </div>
          ) : !archivedSites || archivedSites.length === 0 ? (
            <div className="py-10 text-center text-sm text-muted-foreground">
              当前没有归档的站点。
            </div>
          ) : (
            <div className="space-y-2">
              {archivedSites.map((site) => (
                <div
                  key={site.id}
                  className="flex flex-wrap items-center gap-3 rounded-2xl border border-border bg-card/60 p-3"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="truncate font-medium">{site.name}</span>
                      <Badge variant="outline" className="rounded-full text-xs">
                        {site.platform}
                      </Badge>
                      <span className="truncate text-xs text-muted-foreground">
                        {site.base_url}
                      </span>
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                      归档于 {site.archived_at ? new Date(site.archived_at).toLocaleString() : "-"}
                      {" · "}
                      {site.accounts.length} 个账号已保留
                    </div>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    className="rounded-xl"
                    onClick={() => handleRestoreSite(site.id, site.name)}
                    disabled={restoreSite.isPending}
                  >
                    <ArchiveRestore className="size-4" />
                    恢复
                  </Button>
                </div>
              ))}
            </div>
          )}
        </div>
        <DialogFooter className="shrink-0 border-t border-border/60 px-6 py-4">
          <Button
            variant="outline"
            className="rounded-xl"
            onClick={() => setArchivedDialogOpen(false)}
          >
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
