"use client";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import {
  Archive,
  CheckSquare,
  ChevronDown,
  Link2,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  Plus,
  Power,
  Square,
  Trash2,
  Waypoints,
} from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useTranslations } from "next-intl";
import {
  MENU_BUTTON_CLASS,
  PLATFORM_LABELS,
  badgeToneClass,
  cardToneClass,
  formatBalance,
} from "./site-display";
import { SiteAccountRow } from "./SiteAccountRow";
import { CompactMetric, IconActionButton } from "./SiteStatus";
import { SiteEditorActions, VisibleSite } from "./types";
import { SiteActions } from "./useSiteActions";
import { SiteLayout } from "./useSiteLayout";

export function SiteCard({
  item,
  actions,
  editors,
  layout,
  tagFilters,
  handleTagFilterChange,
}: {
  item: VisibleSite;
  actions: SiteActions;
  editors: SiteEditorActions;
  layout: SiteLayout;
  tagFilters: string[];
  handleTagFilterChange: (tag: string) => void;
}) {
  const tProxy = useTranslations("proxyPool");
  const { site, summary, visibleAccounts, forceExpanded, hasFilteredAccounts } = item;
  const { expandedSiteIds, highlightedSiteId, toggleSiteExpanded } = layout;
  const {
    selectedSiteIds,
    toggleSiteSelection,
    handleTogglePin,
    handleToggleSite,
    handleArchiveSite,
    handleDeleteSite,
    jumpToSiteChannel,
  } = actions;
  const { openCreateAccountDialog, openEditSiteDialog } = editors;
  const isExpanded = forceExpanded || expandedSiteIds.has(site.id);

  return (
    <section
      key={site.id}
      className={cn(
        "page-card p-5 transition-colors",
        cardToneClass(summary.healthTone),
        highlightedSiteId === site.id &&
          "ring-2 ring-primary/35 ring-offset-2 ring-offset-background",
      )}
    >
      <div className="flex items-start gap-3">
        <button
          type="button"
          className="mt-1 shrink-0 text-muted-foreground transition-colors hover:text-foreground"
          title={selectedSiteIds.includes(site.id) ? "取消选择站点" : "选择站点"}
          onClick={() => toggleSiteSelection(site.id)}
        >
          {selectedSiteIds.includes(site.id) ? (
            <CheckSquare className="size-5 text-primary" />
          ) : (
            <Square className="size-5" />
          )}
        </button>

        <div className="min-w-0 flex-1">
          <div className="flex items-start gap-3">
            <div
              className="min-w-0 flex-1 cursor-pointer text-left"
              role="button"
              tabIndex={0}
              onClick={() => toggleSiteExpanded(site.id, forceExpanded)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  toggleSiteExpanded(site.id, forceExpanded);
                }
              }}
            >
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="truncate text-lg font-semibold">{site.name}</h2>
                {site.is_pinned ? (
                  <Badge variant="outline" className="text-amber-600">
                    <Pin className="mr-1 size-3" />
                    置顶
                  </Badge>
                ) : null}
                <Badge variant="outline">{PLATFORM_LABELS[site.platform]}</Badge>
                <Badge variant="outline" className={badgeToneClass(summary.healthTone)}>
                  {summary.healthLabel}
                </Badge>
              </div>

              <div className="mt-2 flex items-center gap-2 text-sm text-muted-foreground">
                <Link2 className="size-4 shrink-0" />
                <a
                  href={site.base_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="truncate hover:text-foreground hover:underline transition-colors"
                  onClick={(e) => e.stopPropagation()}
                >
                  {site.base_url}
                </a>
              </div>

              <div className="mt-3 flex flex-wrap gap-x-4 gap-y-2">
                <CompactMetric label="账号" value={summary.accountCount} />
                <CompactMetric label="Key" value={summary.keyCount} />
                <CompactMetric label="模型" value={summary.modelCount} />
                <CompactMetric label="余额" value={formatBalance(summary.balance)} />
                <CompactMetric label="今日收入" value={formatBalance(summary.todayIncome)} />
              </div>

              {site.tags.length > 0 ? (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {site.tags.map((tag) => (
                    <Badge
                      key={tag}
                      asChild
                      variant="secondary"
                      className={cn(
                        "cursor-pointer transition-colors hover:bg-secondary/70",
                        tagFilters.includes(tag) &&
                          "bg-primary text-primary-foreground hover:bg-primary/90",
                      )}
                    >
                      <button
                        type="button"
                        title={
                          tagFilters.includes(tag) ? `取消按「${tag}」筛选` : `按「${tag}」筛选`
                        }
                        aria-pressed={tagFilters.includes(tag)}
                        onClick={(event) => {
                          event.stopPropagation();
                          handleTagFilterChange(tag);
                        }}
                        onKeyDown={(event) => event.stopPropagation()}
                      >
                        {tag}
                      </button>
                    </Badge>
                  ))}
                </div>
              ) : null}

              <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                <span>
                  {site.proxy_mode === "pool"
                    ? tProxy("mode.pool")
                    : site.proxy_mode === "system"
                      ? tProxy("mode.system")
                      : tProxy("mode.direct")}
                </span>
                {site.custom_header.length > 0 ? (
                  <span>{site.custom_header.length} 个 Header</span>
                ) : null}
                {site.external_checkin_url ? <span>手动签到</span> : null}
              </div>
            </div>

            <div className="flex items-center gap-1">
              {site.accounts.length === 0 ? (
                <IconActionButton label="新增账号" onClick={() => openCreateAccountDialog(site)}>
                  <Plus className="size-4" />
                </IconActionButton>
              ) : null}

              <Popover>
                <PopoverTrigger asChild>
                  <Button
                    type="button"
                    size="icon-sm"
                    variant="outline"
                    className="rounded-xl"
                    aria-label="更多站点操作"
                    title="更多站点操作"
                  >
                    <MoreHorizontal className="size-4" />
                  </Button>
                </PopoverTrigger>
                <PopoverContent
                  align="end"
                  className="w-52 rounded-2xl border border-border/60 bg-card p-2"
                >
                  <div className="grid gap-1">
                    <button
                      type="button"
                      className={MENU_BUTTON_CLASS}
                      onClick={() => jumpToSiteChannel(site.id)}
                    >
                      <Waypoints className="size-4" />
                      <span>查看站点渠道</span>
                    </button>
                    {site.accounts.length > 0 ? (
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => openCreateAccountDialog(site)}
                      >
                        <Plus className="size-4" />
                        <span>新增账号</span>
                      </button>
                    ) : null}
                    <div className="my-1 border-t border-border/60" />
                    <button
                      type="button"
                      className={MENU_BUTTON_CLASS}
                      onClick={() => openEditSiteDialog(site)}
                    >
                      <Pencil className="size-4" />
                      <span>编辑站点</span>
                    </button>
                    <button
                      type="button"
                      className={MENU_BUTTON_CLASS}
                      onClick={() => handleTogglePin(site)}
                    >
                      {site.is_pinned ? <PinOff className="size-4" /> : <Pin className="size-4" />}
                      <span>{site.is_pinned ? "取消置顶" : "置顶"}</span>
                    </button>
                    <button
                      type="button"
                      className={MENU_BUTTON_CLASS}
                      onClick={() => handleToggleSite(site)}
                    >
                      <Power className="size-4" />
                      <span>{site.enabled ? "停用站点" : "启用站点"}</span>
                    </button>
                    <button
                      type="button"
                      className={MENU_BUTTON_CLASS}
                      onClick={() => handleArchiveSite(site)}
                    >
                      <Archive className="size-4" />
                      <span>归档站点</span>
                    </button>
                    <button
                      type="button"
                      className={cn(MENU_BUTTON_CLASS, "text-destructive")}
                      onClick={() => handleDeleteSite(site)}
                    >
                      <Trash2 className="size-4" />
                      <span>删除站点</span>
                    </button>
                  </div>
                </PopoverContent>
              </Popover>

              <IconActionButton
                label={forceExpanded ? "筛选结果已自动展开" : isExpanded ? "收起账号" : "展开账号"}
                disabled={forceExpanded || site.accounts.length === 0}
                onClick={() => toggleSiteExpanded(site.id, forceExpanded)}
              >
                <ChevronDown
                  className={cn("size-4 transition-transform", isExpanded && "rotate-180")}
                />
              </IconActionButton>
            </div>
          </div>

          <AnimatePresence initial={false}>
            {isExpanded ? (
              <motion.div
                key="site-accounts"
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: "auto", opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                transition={{ duration: 0.22, ease: "easeOut" }}
                className="overflow-hidden"
              >
                <div className="mt-4 border-t border-border/60 pt-4">
                  {hasFilteredAccounts ? (
                    <div className="mb-3 text-xs text-muted-foreground">
                      显示 {visibleAccounts.length} / {site.accounts.length} 个账号
                    </div>
                  ) : null}

                  {visibleAccounts.length === 0 ? (
                    <div className="rounded-2xl border border-dashed border-border/70 bg-muted/10 px-4 py-6 text-sm text-muted-foreground">
                      暂无账号。添加账号后即可自动同步分组、模型和渠道绑定。
                    </div>
                  ) : (
                    <div className="space-y-2">
                      {visibleAccounts.map((account) => (
                        <SiteAccountRow
                          key={account.id}
                          site={site}
                          account={account}
                          actions={actions}
                          editors={editors}
                          layout={layout}
                        />
                      ))}
                    </div>
                  )}
                </div>
              </motion.div>
            ) : null}
          </AnimatePresence>
        </div>
      </div>
    </section>
  );
}
