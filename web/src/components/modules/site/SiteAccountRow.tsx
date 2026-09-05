"use client";

import { SiteAccount, Site as SiteRecord } from "@/api/endpoints/site";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/animate-ui/components/animate/tooltip";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { useSettingStore } from "@/stores/setting";
import {
  CalendarCheck2,
  FileJson,
  MoreHorizontal,
  Pencil,
  RefreshCw,
  Trash2,
  Waypoints,
} from "lucide-react";
import { useTranslations } from "next-intl";
import { accountHasCheckinEnabled, sitePlatformSupportsCheckin } from "./checkin-status";
import { translateSiteMessage } from "./site-message";
import { siteAccountHasActivePartialSync } from "./sync-health";

import {
  CREDENTIAL_LABELS,
  MENU_BUTTON_CLASS,
  cardToneClass,
  formatBalance,
  formatDateTime,
  normalizedStatus,
} from "./site-display";

import { accountHasHealthFailure } from "./site-summary";
import { CompactMetric, ExecutionSummary, IconActionButton, StaticSummary } from "./SiteStatus";

import { HealthTone, SiteEditorActions } from "./types";
import { SiteActions } from "./useSiteActions";
import { SiteLayout } from "./useSiteLayout";

export function SiteAccountRow({
  site,
  account,
  actions,
  editors,
  layout,
}: {
  site: SiteRecord;
  account: SiteAccount;
  actions: SiteActions;
  editors: SiteEditorActions;
  layout: SiteLayout;
}) {
  const t = useTranslations();
  const locale = useSettingStore((state) => state.locale);
  const tProxy = useTranslations("proxyPool");
  const {
    enableSiteAccount,
    syncingAccountIds,
    checkinAccountIds,
    handleToggleAccount,
    handleSyncAccount,
    handleCheckinAccount,
    handleDeleteAccount,
    jumpToSiteChannelAccount,
  } = actions;
  const { openEditAccountDialog, openManualSyncDialog } = editors;
  const { setAccountElementRef, highlightedAccountId } = layout;
  const accountFailed = accountHasHealthFailure(site, account);
  const accountPartial = siteAccountHasActivePartialSync(site, account);
  const accountTone: HealthTone = accountFailed
    ? "danger"
    : accountPartial
      ? "warning"
      : account.enabled
        ? "default"
        : "muted";
  const supportsCheckin = sitePlatformSupportsCheckin(site.platform);
  const canShowManualCheckin = supportsCheckin && accountHasCheckinEnabled(account, site.platform);

  return (
    <article
      key={account.id}
      ref={(node) => setAccountElementRef(account.id, node)}
      className={cn(
        "rounded-2xl border px-4 py-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.04)] transition-colors",
        cardToneClass(accountTone),
        highlightedAccountId === account.id &&
          "ring-2 ring-primary/35 ring-offset-2 ring-offset-background",
      )}
    >
      <div className="space-y-3">
        <div className="flex items-start gap-3">
          <div className="min-w-0 flex-1 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <div className="text-sm font-semibold">{account.name}</div>
              <Badge variant="outline">{CREDENTIAL_LABELS[account.credential_type]}</Badge>
              <Badge
                variant="outline"
                className={account.enabled ? "text-emerald-600" : "text-muted-foreground"}
              >
                {account.enabled ? "启用中" : "已停用"}
              </Badge>
            </div>

            <div className="flex flex-wrap gap-x-4 gap-y-1">
              <CompactMetric label="分组" value={account.user_groups.length} />
              <CompactMetric label="模型" value={account.models.length} />
              <CompactMetric label="余额" value={formatBalance(account.balance)} />
              <CompactMetric label="今日收入" value={formatBalance(account.today_income)} />
            </div>

            <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
              <span>{account.auto_sync ? "自动同步" : "手动同步"}</span>
              <span>
                {account.auto_checkin
                  ? account.random_checkin
                    ? "随机签到"
                    : "自动签到"
                  : "手动签到"}
              </span>
              <span>
                {account.proxy_mode === "inherit"
                  ? tProxy("site.inherit")
                  : account.proxy_mode === "pool"
                    ? tProxy("mode.pool")
                    : account.proxy_mode === "system"
                      ? tProxy("mode.system")
                      : tProxy("mode.direct")}
              </span>
            </div>
          </div>

          <div className="flex shrink-0 items-center gap-2 self-start">
            <Tooltip>
              <TooltipTrigger asChild>
                <span>
                  <Switch
                    checked={account.enabled}
                    disabled={enableSiteAccount.isPending}
                    onCheckedChange={() => handleToggleAccount(account)}
                  />
                </span>
              </TooltipTrigger>
              <TooltipContent>{account.enabled ? "停用账号" : "启用账号"}</TooltipContent>
            </Tooltip>

            <IconActionButton
              label="同步账号"
              disabled={syncingAccountIds.has(account.id)}
              onClick={() => handleSyncAccount(account)}
            >
              <RefreshCw
                className={cn("size-4", syncingAccountIds.has(account.id) && "animate-spin")}
              />
            </IconActionButton>

            <Popover>
              <PopoverTrigger asChild>
                <Button
                  type="button"
                  size="icon-sm"
                  variant="outline"
                  className="rounded-xl"
                  aria-label="更多账号操作"
                  title="更多账号操作"
                >
                  <MoreHorizontal className="size-4" />
                </Button>
              </PopoverTrigger>
              <PopoverContent
                align="end"
                className="w-44 rounded-2xl border border-border/60 bg-card p-2"
              >
                <div className="grid gap-1">
                  <button
                    type="button"
                    className={MENU_BUTTON_CLASS}
                    onClick={() => jumpToSiteChannelAccount(site.id, account.id)}
                  >
                    <Waypoints className="size-4" />
                    <span>查看站点渠道</span>
                  </button>
                  <button
                    type="button"
                    className={cn(
                      MENU_BUTTON_CLASS,
                      "disabled:cursor-not-allowed disabled:opacity-50",
                    )}
                    onClick={() => handleCheckinAccount(account)}
                    disabled={checkinAccountIds.has(account.id)}
                    hidden={!canShowManualCheckin}
                  >
                    <CalendarCheck2 className="size-4" />
                    <span>立即签到</span>
                  </button>
                  <button
                    type="button"
                    className={MENU_BUTTON_CLASS}
                    onClick={() => openEditAccountDialog(site, account)}
                  >
                    <Pencil className="size-4" />
                    <span>编辑账号</span>
                  </button>
                  <button
                    type="button"
                    className={MENU_BUTTON_CLASS}
                    onClick={() => openManualSyncDialog(site, account)}
                  >
                    <FileJson className="size-4" />
                    <span>手动导入同步数据</span>
                  </button>
                  <button
                    type="button"
                    className={cn(MENU_BUTTON_CLASS, "text-destructive")}
                    onClick={() => handleDeleteAccount(account)}
                  >
                    <Trash2 className="size-4" />
                    <span>删除账号</span>
                  </button>
                </div>
              </PopoverContent>
            </Popover>
          </div>
        </div>

        <div className="space-y-1">
          <ExecutionSummary
            label="同步"
            status={normalizedStatus(account.last_sync_status)}
            at={account.last_sync_at}
            message={translateSiteMessage(locale, account.last_sync_message, t) || "等待首次同步"}
          />
          {supportsCheckin ? (
            accountHasCheckinEnabled(account, site.platform) ? (
              <ExecutionSummary
                label="签到"
                status={normalizedStatus(account.last_checkin_status)}
                at={account.last_checkin_at}
                message={account.last_checkin_message || "等待首次签到"}
              />
            ) : (
              <StaticSummary text="签到未启用" />
            )
          ) : (
            <StaticSummary tone="warning" text="当前平台不支持签到" />
          )}
          {account.auto_checkin ? (
            <div className="pl-4 text-xs text-muted-foreground">
              下次自动签到{" "}
              {account.next_auto_checkin_at
                ? formatDateTime(account.next_auto_checkin_at)
                : "待调度"}{" "}
              · 间隔 {account.checkin_interval_hours} 小时
              {account.random_checkin
                ? ` · 随机延迟 0-${account.checkin_random_window_minutes} 分钟`
                : ""}
              {account.last_checkin_success_at
                ? ` · 上次成功 ${formatDateTime(account.last_checkin_success_at)}`
                : ""}
              {account.checkin_failure_count > 0
                ? ` · 连续失败 ${account.checkin_failure_count} 次`
                : ""}
            </div>
          ) : null}
        </div>
      </div>
    </article>
  );
}
