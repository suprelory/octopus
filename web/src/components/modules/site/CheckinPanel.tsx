"use client";

import { useCallback, useMemo, type ReactNode } from "react";
import {
  AlertTriangle,
  CalendarCheck2,
  ExternalLink,
  FilterX,
  Layers3,
  Tag,
  TrendingUp,
  Wallet,
} from "lucide-react";
import { type Site } from "@/api/endpoints/site";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useTranslations } from 'next-intl';
import {
  buildCheckinSummary,
  type CheckinActiveFilterStatus,
  type CheckinFilterStatus,
} from "./checkin-status";

const FILTERS: Array<{ key: CheckinFilterStatus; label: string }> = [
  { key: "all", label: "全部" },
  { key: "success", label: "成功" },
  { key: "failed", label: "签到失败" },
  { key: "sync_failed", label: "同步失败" },
  { key: "idle", label: "未执行" },
  { key: "disabled", label: "禁用" },
];

function filterTone(status: CheckinFilterStatus, active: boolean) {
  if (active) {
    switch (status) {
      case "success":
        return "border-emerald-500/30 bg-emerald-500 text-white";
      case "failed":
      case "sync_failed":
        return "border-destructive/30 bg-destructive text-white";
      case "idle":
        return "border-border bg-foreground text-background";
      case "disabled":
        return "border-slate-500/30 bg-slate-700 text-white dark:bg-slate-200 dark:text-slate-900";
      case "all":
      default:
        return "border-primary/30 bg-primary text-primary-foreground";
    }
  }

  switch (status) {
    case "success":
      return "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
    case "failed":
    case "sync_failed":
      return "border-destructive/20 bg-destructive/10 text-destructive";
    case "idle":
      return "border-border bg-muted/40 text-muted-foreground";
    case "disabled":
      return "border-slate-500/20 bg-slate-500/10 text-slate-700 dark:text-slate-300";
    case "all":
    default:
      return "border-border bg-background text-foreground";
  }
}

function formatCurrency(value: number) {
  const safe = Number.isFinite(value) ? value : 0;
  return `$${safe.toFixed(2)}`;
}

function OverviewMetric({
  icon,
  label,
  value,
  tone,
}: {
  icon: ReactNode;
  label: string;
  value: string;
  tone?: "default" | "warning";
}) {
  return (
    <div className="page-card relative min-w-0 p-4 sm:p-5">
      <span
        className={cn(
          "absolute right-4 top-4 flex size-5 items-center justify-center sm:right-5 sm:top-5",
          tone === "warning"
            ? "text-amber-600 dark:text-amber-400"
            : "text-muted-foreground",
        )}
      >
        {icon}
      </span>
      <div className="min-w-0">
        <div className="pr-6 text-xs text-muted-foreground">{label}</div>
        <div className="mt-4 truncate text-2xl font-semibold tracking-tight tabular-nums" title={value}>{value}</div>
      </div>
    </div>
  );
}

export function CheckinPanel({
  sites,
  inventory,
  statusDayKey,
  visibleSiteCount,
  visibleAccountCount,
  searchTerm,
  hasActiveFilters,
  onClearFilters,
  activeFilterStatuses,
  onFilterChange,
  allTags,
  activeTags,
  onTagFilterChange,
}: {
  sites: Site[] | undefined;
  inventory: {
    totalBalance: number;
    totalBalanceUsed: number;
    enabledAccounts: number;
    totalAccounts: number;
  };
  statusDayKey: string;
  visibleSiteCount: number;
  visibleAccountCount: number;
  searchTerm: string;
  hasActiveFilters: boolean;
  onClearFilters: () => void;
  activeFilterStatuses: CheckinActiveFilterStatus[];
  onFilterChange: (status: CheckinFilterStatus) => void;
  allTags: Array<{ tag: string; count: number }>;
  activeTags: string[];
  onTagFilterChange: (tag: string) => void;
}) {
  const t = useTranslations('workspace.site');
  const summaryNow = useMemo(() => {
    const [year = "", month = "", day = ""] = statusDayKey.split("-");
    const parsed = new Date(Number(year), Number(month), Number(day));
    return Number.isNaN(parsed.getTime()) ? new Date() : parsed;
  }, [statusDayKey]);

  const summary = useMemo(
    () => buildCheckinSummary(sites, summaryNow),
    [sites, summaryNow],
  );
  const hasContextBadges = Boolean(searchTerm);

  const manualCheckinUrls = useMemo(
    () =>
      (sites ?? [])
        .filter((s) => s.external_checkin_url?.trim())
        .map((s) => s.external_checkin_url!.trim()),
    [sites],
  );

  const openAllManualCheckin = useCallback(() => {
    for (const url of manualCheckinUrls) {
      window.open(url, "_blank", "noopener,noreferrer");
    }
  }, [manualCheckinUrls]);

  return (
    <section aria-label={t('title')} className="space-y-4">
      <div>
        <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h2 className="flex items-center gap-2 text-lg font-semibold tracking-tight"><CalendarCheck2 aria-hidden className="size-4 text-primary" />{t('title')}</h2>
            <p className="mt-1 text-sm text-muted-foreground">{t('description')}</p>
          </div>

          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>当前结果</span>
            <span className="font-medium text-foreground">
              {visibleSiteCount} 站点 / {visibleAccountCount} 账号
            </span>
          </div>
        </div>

        <div className="mt-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
          <OverviewMetric
            icon={<Wallet className="size-4" />}
            label="当前余额"
            value={formatCurrency(inventory.totalBalance)}
          />
          <OverviewMetric
            icon={<TrendingUp className="size-4" />}
            label="累计消耗"
            value={formatCurrency(inventory.totalBalanceUsed)}
          />
          <OverviewMetric
            icon={<Layers3 className="size-4" />}
            label="启用账号"
            value={`${inventory.enabledAccounts} / ${inventory.totalAccounts}`}
          />
          <OverviewMetric
            icon={<AlertTriangle className="size-4" />}
            label="今日签到异常"
            value={`${summary.failed}`}
            tone={summary.failed > 0 ? "warning" : "default"}
          />
        </div>

        {hasActiveFilters && hasContextBadges ? (
          <div className="mt-4 flex flex-wrap gap-2">
            {searchTerm ? <Badge variant="outline">搜索：{searchTerm}</Badge> : null}
          </div>
        ) : null}
      </div>

      <div className="rounded-2xl border border-border/60 bg-card/60 px-4 py-4 sm:px-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap gap-2">
            {FILTERS.map((filter) => {
              const count =
                filter.key === "all" ? summary.total : summary[filter.key];
              const active =
                filter.key === "all"
                  ? activeFilterStatuses.length === 0
                  : activeFilterStatuses.includes(filter.key);
              return (
                <button
                  key={filter.key}
                  type="button"
                  aria-pressed={active}
                  onClick={() => onFilterChange(filter.key)}
                  className={cn(
                    "inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors focus-visible:outline-2 focus-visible:outline-ring",
                    filterTone(filter.key, active),
                  )}
                >
                  <span>{count}</span>
                  <span>{filter.label}</span>
                </button>
              );
            })}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {hasActiveFilters ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="rounded-xl text-xs"
                onClick={onClearFilters}
              >
                <FilterX className="size-4" />
                清空筛选
              </Button>
            ) : null}
            {manualCheckinUrls.length > 0 ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="rounded-xl text-xs"
                onClick={openAllManualCheckin}
              >
                <ExternalLink className="size-4" />
                打开手动签到 ({manualCheckinUrls.length})
              </Button>
            ) : null}
          </div>
        </div>

        {allTags.length > 0 ? (
          <div className="mt-3 flex flex-wrap gap-2">
            {allTags.map(({ tag, count }) => {
              const active = activeTags.includes(tag);
              return (
                <button
                  key={tag}
                  type="button"
                  aria-pressed={active}
                  onClick={() => onTagFilterChange(tag)}
                  title={active ? `取消按「${tag}」筛选` : `按「${tag}」筛选`}
                  className={cn(
                    "inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors",
                    active
                      ? "border-primary/30 bg-primary text-primary-foreground"
                      : "border-border bg-background text-foreground hover:bg-muted/40",
                  )}
                >
                  <Tag className="size-3" />
                  <span>{tag}</span>
                  <span className="text-[10px] opacity-70">{count}</span>
                </button>
              );
            })}
          </div>
        ) : null}
      </div>
    </section>
  );
}
