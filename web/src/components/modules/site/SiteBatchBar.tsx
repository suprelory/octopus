"use client";

import { Button } from "@/components/ui/button";
import { CheckSquare, Square } from "lucide-react";
import { VisibleSite } from "./types";
import { SiteActions } from "./useSiteActions";

export function SiteBatchBar({
  actions,
  visibleSites,
  onEdit,
}: {
  actions: SiteActions;
  visibleSites: VisibleSite[];
  onEdit: () => void;
}) {
  const { selectedSiteIds, setSelectedSiteIds, batchAction, handleBatchAction, setDeleteConfirm } =
    actions;
  return selectedSiteIds.length > 0 ? (
    <section className="page-card sticky top-0 z-30 border-border/70 bg-card/95 p-4 backdrop-blur supports-[backdrop-filter]:bg-card/90">
      <div className="flex flex-wrap items-center gap-3">
        {(() => {
          const visibleIds = visibleSites.map((item) => item.site.id);
          const allVisibleSelected =
            visibleIds.length > 0 && visibleIds.every((id) => selectedSiteIds.includes(id));
          return (
            <button
              type="button"
              onClick={() => {
                if (allVisibleSelected) {
                  setSelectedSiteIds((prev) => prev.filter((id) => !visibleIds.includes(id)));
                } else {
                  setSelectedSiteIds((prev) => Array.from(new Set([...prev, ...visibleIds])));
                }
              }}
              disabled={visibleIds.length === 0}
              title={allVisibleSelected ? "取消全选" : "全选当前可见站点"}
              className="inline-flex items-center gap-2 text-sm font-medium text-foreground transition-colors hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {allVisibleSelected ? (
                <CheckSquare className="size-5 text-primary" />
              ) : (
                <Square className="size-5" />
              )}
              全选
            </button>
          );
        })()}
        <span className="text-sm font-medium">已选 {selectedSiteIds.length} 个站点</span>
        <Button
          variant="outline"
          size="sm"
          className="rounded-xl"
          onClick={() => handleBatchAction("enable")}
          disabled={batchAction.isPending}
        >
          批量启用
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="rounded-xl"
          onClick={() => handleBatchAction("disable")}
          disabled={batchAction.isPending}
        >
          批量禁用
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="rounded-xl"
          onClick={() => onEdit()}
          disabled={batchAction.isPending}
        >
          批量编辑
        </Button>
        <Button
          variant="destructive"
          size="sm"
          className="rounded-xl"
          onClick={() =>
            setDeleteConfirm({
              type: "batch-site",
              id: 0,
              name: String(selectedSiteIds.length),
            })
          }
          disabled={batchAction.isPending}
        >
          批量删除
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="rounded-xl"
          onClick={() => setSelectedSiteIds([])}
        >
          取消选择
        </Button>
      </div>
    </section>
  ) : null;
}
