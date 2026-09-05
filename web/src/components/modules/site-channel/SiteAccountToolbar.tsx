'use client';

import { type SiteChannelAccount, type SiteChannelGroup, type SiteModelRouteType } from '@/api/endpoints/site-channel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { cn } from '@/lib/utils';
import {
    Check,
    CircleAlert,
    CirclePause,
    KeyRound,
    MoreHorizontal,
    Plus,
    Power,
    RefreshCw,
    Search,
    Settings,
    SlidersHorizontal,
    Waypoints,
} from 'lucide-react';
import { motion } from 'motion/react';
import { SITE_ROUTE_COLUMN_ORDER } from './constants';
import {
    QUICK_FILTER_OPTIONS,
    SITE_GROUP_FILTER_ALL_VALUE,
    STALE_MODEL_SYNC_STATUSES,
    getGroupStatusBadge,
} from './model-view';
import { routeTypeLabel } from './utils';

import type { SiteChannelModelState } from './useSiteChannelModels';
export type SiteChannelGroupActions = {
    creatingGroup: SiteChannelGroup | null;
    isCreatingKey: boolean;
    handleOpenCreateKey: (group: SiteChannelGroup) => void;
    handleOpenProjectedKeys: (group: SiteChannelGroup) => void;
    handleOpenAdvancedSettings: (group: SiteChannelGroup) => void;
    handleOpenAddManualModels: (group: SiteChannelGroup) => void;
};

export function SiteAccountToolbar({
    account,
    accounts,
    activeAccountId,
    onSelectAccount,
    highlightedAccountId,
    registerAccountTabRef,
    modelState,
    groupActions,
}: {
    account: SiteChannelAccount;
    accounts: SiteChannelAccount[];
    activeAccountId: number | null;
    onSelectAccount: (accountId: number) => void;
    highlightedAccountId: number | null;
    registerAccountTabRef: (accountId: number, node: HTMLButtonElement | null) => void;
    modelState: SiteChannelModelState;
    groupActions: SiteChannelGroupActions;
}) {
    const {
        setSelectedModelKeys,
        modelSearchTerm,
        setModelSearchTerm,
        bulkMoveTarget,
        setBulkMoveTarget,
        panelKey,
        panelPreferences,
        setCompactMode,
        groupProjectionMutation,
        resetMutation,
        enableSiteAccount,
        visibleGroups,
        selectedModels,
        hasPendingChanges,
        applyRouteChange,
        applyDisabledChange,
        handleToggleGroupProjection,
        handleResetRoutes,
        toggleQuickFilter,
        selectedVisibleCount,
        activeGroupValue,
        activeGroup,
        activeGroupLabel,
        activeGroupProjectionSuspended,
        activeGroupProjectionStale,
        activeGroupSuspensionReason,
        activeGroupStaleReason,
        activeQuickFilterCount,
        pendingKeyGroups,
        projectedGroups,
        unsupportedRouteCount,
        handleGroupFilterChange,
        handleClearQuickFilters,
        handleFocusAttention,
    } = modelState;
    const {
        creatingGroup,
        isCreatingKey,
        handleOpenCreateKey,
        handleOpenProjectedKeys,
        handleOpenAdvancedSettings,
        handleOpenAddManualModels,
    } = groupActions;
    return (
        <div className="flex flex-none flex-col gap-2 rounded-2xl border border-border/70 bg-card/70 p-2.5">
            {accounts.length >= 2 ? (
                <div className="flex items-center justify-between gap-3 border-b border-border/60 pb-2">
                    <div className="-mb-px max-w-full overflow-x-auto">
                        <div className="flex min-w-max items-baseline gap-5 px-0.5 pb-1">
                            {accounts.map((acc) => {
                                const isActive = acc.account_id === activeAccountId;
                                return (
                                    <button
                                        key={acc.account_id}
                                        ref={(node) => registerAccountTabRef(acc.account_id, node)}
                                        type="button"
                                        onClick={() => onSelectAccount(acc.account_id)}
                                        className={cn(
                                            'relative inline-flex items-baseline gap-1.5 pb-1 text-sm font-medium transition-colors',
                                            isActive
                                                ? 'text-foreground'
                                                : 'text-muted-foreground hover:text-foreground',
                                            highlightedAccountId === acc.account_id &&
                                                'rounded-md ring-2 ring-primary/35 ring-offset-2 ring-offset-background',
                                        )}
                                    >
                                        <span className="truncate">{acc.account_name}</span>
                                        <span
                                            className={cn(
                                                'size-1.5 shrink-0 rounded-full',
                                                acc.enabled ? 'bg-emerald-500' : 'bg-destructive',
                                            )}
                                            aria-hidden
                                        />
                                        {isActive && (
                                            <motion.span
                                                layoutId="site-account-tab-underline"
                                                className="absolute -bottom-px left-0 right-0 h-0.5 rounded-full bg-primary"
                                                transition={{ type: 'spring', stiffness: 320, damping: 30, mass: 0.8 }}
                                            />
                                        )}
                                    </button>
                                );
                            })}
                        </div>
                    </div>

                    <button
                        type="button"
                        onClick={() =>
                            enableSiteAccount.mutate({
                                id: account.account_id,
                                enabled: !account.enabled,
                            })
                        }
                        disabled={enableSiteAccount.isPending}
                        className={cn(
                            'inline-flex h-7 shrink-0 cursor-pointer items-center gap-1 rounded-full border px-2.5 text-[11px] font-medium transition hover:opacity-80',
                            account.enabled
                                ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                                : 'border-destructive/30 bg-destructive/10 text-destructive',
                        )}
                    >
                        <Power className={cn('size-3', enableSiteAccount.isPending && 'animate-spin')} />
                        {account.enabled ? '账号启用' : '账号停用'}
                    </button>
                </div>
            ) : null}

            <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
                <div className="flex flex-1 flex-col gap-2 sm:flex-row sm:items-center">
                    <Select value={activeGroupValue} onValueChange={handleGroupFilterChange}>
                        <SelectTrigger className="h-8 w-full rounded-2xl border-border/70 bg-background/80 sm:w-[18rem]">
                            <div className="flex min-w-0 items-center gap-2">
                                <span className="text-xs text-muted-foreground">分组</span>
                                <span className="truncate text-sm font-medium">{activeGroupLabel}</span>
                            </div>
                        </SelectTrigger>
                        <SelectContent align="start" className="rounded-2xl border border-border/70 bg-card">
                            <SelectItem value={SITE_GROUP_FILTER_ALL_VALUE} className="rounded-xl py-2">
                                <div className="flex w-full min-w-0 items-center justify-between gap-3">
                                    <span className="truncate">全部分组</span>
                                    <span className="text-[11px] text-muted-foreground">
                                        {account.groups.length} 组
                                    </span>
                                </div>
                            </SelectItem>
                            {account.groups.map((group) => (
                                <SelectItem key={group.group_key} value={group.group_key} className="rounded-xl py-2">
                                    <div className="flex w-full min-w-0 items-start justify-between gap-3">
                                        <div className="min-w-0">
                                            <div className="truncate">{group.group_name || group.group_key}</div>
                                            <div className="text-[11px] text-muted-foreground">
                                                {group.models.length} 模型 · Key {group.enabled_key_count}/
                                                {group.key_count}
                                                {group.projection_disabled ? ' · 不投影' : ''}
                                                {group.projection_suspended
                                                    ? ' · 已暂停'
                                                    : STALE_MODEL_SYNC_STATUSES.includes(group.model_sync_status)
                                                      ? ' · 沿用历史'
                                                      : ''}
                                                {group.masked_pending_key_count > 0
                                                    ? ` · 待补全 ${group.masked_pending_key_count}`
                                                    : ''}
                                                {group.has_projected_channel
                                                    ? ` · 投影 ${group.projected_keys.length}`
                                                    : ''}
                                            </div>
                                        </div>
                                        {(() => {
                                            const statusBadge = getGroupStatusBadge(group);
                                            return statusBadge ? (
                                                <span className={statusBadge.className}>{statusBadge.label}</span>
                                            ) : null;
                                        })()}
                                    </div>
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>

                    <div className="relative min-w-0 flex-1">
                        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                        <Input
                            value={modelSearchTerm}
                            onChange={(event) => setModelSearchTerm(event.target.value)}
                            placeholder="搜索模型名称、分组..."
                            className="h-8 rounded-2xl pl-9"
                        />
                    </div>
                </div>

                {activeGroupProjectionSuspended ? (
                    <div className="flex items-start gap-2 rounded-2xl border border-destructive/25 bg-destructive/10 px-3 py-2 text-xs text-destructive">
                        <CircleAlert className="mt-0.5 size-4 shrink-0" />
                        <div className="min-w-0">
                            <div className="font-medium">该分组投影已由系统暂停</div>
                            <div className="mt-0.5 break-words text-destructive/80">
                                {activeGroupSuspensionReason ||
                                    '该分组缺少可用 Key 或上游当前无可用模型。重新同步成功后会自动恢复投影。'}
                            </div>
                        </div>
                    </div>
                ) : activeGroupProjectionStale ? (
                    <div className="flex items-start gap-2 rounded-2xl border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-200">
                        <CircleAlert className="mt-0.5 size-4 shrink-0" />
                        <div className="min-w-0">
                            <div className="font-medium">该分组正在沿用上次成功投影</div>
                            <div className="mt-0.5 break-words text-amber-800/80 dark:text-amber-100/80">
                                {activeGroupStaleReason ||
                                    '最近一次同步未能确认最新模型，当前 managed channel 保持启用。'}
                            </div>
                        </div>
                    </div>
                ) : null}

                <div className="flex flex-wrap items-center gap-2">
                    <Button
                        type="button"
                        variant="outline"
                        className="h-8 rounded-2xl px-3"
                        onClick={() => activeGroup && handleOpenAddManualModels(activeGroup)}
                        disabled={!activeGroup}
                        title={activeGroup ? undefined : '请先选择具体分组'}
                    >
                        <Plus className="size-4" />
                        添加
                    </Button>

                    <Button
                        type="button"
                        variant="outline"
                        className={cn(
                            'h-8 rounded-2xl px-3',
                            activeGroup?.projection_disabled &&
                                'border-amber-500/30 bg-amber-500/10 text-amber-800 hover:bg-amber-500/15 hover:text-amber-900 dark:text-amber-200 dark:hover:text-amber-100',
                            activeGroupProjectionSuspended &&
                                'border-destructive/30 bg-destructive/10 text-destructive hover:bg-destructive/15',
                        )}
                        onClick={() => activeGroup && handleToggleGroupProjection(activeGroup)}
                        disabled={!activeGroup || activeGroupProjectionSuspended || groupProjectionMutation.isPending}
                        title={
                            !activeGroup
                                ? '请先选择具体分组'
                                : activeGroupProjectionSuspended
                                  ? `系统已暂停投影：${activeGroupSuspensionReason || '最近模型同步失败，请重新同步恢复'}`
                                  : activeGroup.projection_disabled
                                    ? '恢复生成投影渠道并显示到分组编辑'
                                    : '停止生成投影渠道并从分组编辑中移除'
                        }
                    >
                        {activeGroupProjectionSuspended ? (
                            <CirclePause className="size-4" />
                        ) : (
                            <Waypoints className={cn('size-4', groupProjectionMutation.isPending && 'animate-spin')} />
                        )}
                        {activeGroupProjectionSuspended
                            ? '已暂停'
                            : activeGroup?.projection_disabled
                              ? '不投影'
                              : '投影'}
                    </Button>

                    <Popover>
                        <PopoverTrigger asChild>
                            <Button type="button" variant="outline" className="h-8 rounded-2xl px-3">
                                <SlidersHorizontal className="size-4" />
                                {activeQuickFilterCount > 0 ? `筛选(${activeQuickFilterCount})` : '筛选'}
                            </Button>
                        </PopoverTrigger>
                        <PopoverContent
                            align="end"
                            className="w-60 rounded-2xl border border-border/70 bg-card p-3 shadow-xl"
                        >
                            <div className="space-y-3">
                                <div className="text-xs font-medium text-muted-foreground">快速筛选</div>
                                <div className="grid gap-2">
                                    {QUICK_FILTER_OPTIONS.map((option) => {
                                        const active = panelPreferences.quickFilters.includes(option.key);
                                        return (
                                            <button
                                                key={option.key}
                                                type="button"
                                                onClick={() => toggleQuickFilter(option.key)}
                                                className={cn(
                                                    'flex items-center justify-between rounded-xl border px-3 py-2 text-left text-sm transition',
                                                    active
                                                        ? 'border-primary/30 bg-primary/10 text-foreground'
                                                        : 'border-border bg-background hover:bg-muted/60',
                                                )}
                                            >
                                                <span>{option.label}</span>
                                                {active ? <Check className="size-4 text-primary" /> : null}
                                            </button>
                                        );
                                    })}
                                </div>
                                {activeQuickFilterCount > 0 ? (
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        className="h-8 rounded-xl px-2"
                                        onClick={handleClearQuickFilters}
                                    >
                                        清空筛选
                                    </Button>
                                ) : null}
                            </div>
                        </PopoverContent>
                    </Popover>

                    <Button
                        type="button"
                        variant="outline"
                        className="h-8 rounded-2xl px-3"
                        onClick={() => activeGroup && handleOpenAdvancedSettings(activeGroup)}
                        disabled={!activeGroup || activeGroup.projected_channels.length === 0}
                        title={
                            !activeGroup
                                ? '请先选择具体分组'
                                : activeGroup.projected_channels.length === 0
                                  ? '当前分组暂无投影渠道'
                                  : undefined
                        }
                    >
                        <Settings className="size-4" />
                        高级
                    </Button>

                    <Popover>
                        <PopoverTrigger asChild>
                            <Button type="button" variant="outline" className="h-8 rounded-2xl px-3">
                                <MoreHorizontal className="size-4" />
                                更多
                            </Button>
                        </PopoverTrigger>
                        <PopoverContent
                            align="end"
                            className="w-64 rounded-2xl border border-border/70 bg-card p-2 shadow-xl"
                        >
                            <div className="space-y-1">
                                <button
                                    type="button"
                                    onClick={() => setCompactMode(panelKey, !panelPreferences.compactMode)}
                                    className="flex w-full items-center justify-between rounded-xl px-3 py-2 text-left transition hover:bg-muted/60"
                                >
                                    <div>
                                        <div className="text-sm font-medium text-foreground">紧凑模式</div>
                                        <div className="text-[11px] text-muted-foreground">压缩模型卡片和表格行高</div>
                                    </div>
                                    {panelPreferences.compactMode ? <Check className="size-4 text-primary" /> : null}
                                </button>
                            </div>
                            <Button
                                type="button"
                                variant="outline"
                                className="mt-2 h-8 w-full justify-start rounded-xl px-3"
                                onClick={handleResetRoutes}
                                disabled={resetMutation.isPending || hasPendingChanges}
                            >
                                <RefreshCw className={cn('size-4', resetMutation.isPending && 'animate-spin')} />
                                {resetMutation.isPending ? '重置中...' : '重置模型端点格式'}
                            </Button>
                        </PopoverContent>
                    </Popover>
                </div>
            </div>

            {pendingKeyGroups.length > 0 ||
            projectedGroups.length > 0 ||
            unsupportedRouteCount > 0 ||
            selectedVisibleCount > 0 ? (
                <div className="flex min-h-8 flex-wrap items-center gap-2">
                    {pendingKeyGroups.length > 0 ? (
                        <Popover>
                            <PopoverTrigger asChild>
                                <button
                                    type="button"
                                    className="inline-flex h-8 items-center gap-2 rounded-full border border-amber-500/30 bg-amber-500/10 px-3 text-xs font-medium text-amber-800 transition hover:bg-amber-500/15 dark:text-amber-200"
                                >
                                    <CircleAlert className="size-3.5" />
                                    待建 Key {pendingKeyGroups.length} 组
                                </button>
                            </PopoverTrigger>
                            <PopoverContent
                                align="start"
                                className="w-72 rounded-2xl border border-amber-500/30 bg-card p-3 shadow-xl"
                            >
                                <div className="space-y-2">
                                    <div className="text-xs font-medium text-muted-foreground">未创建 Key 的分组</div>
                                    <div className="flex flex-wrap gap-2">
                                        {pendingKeyGroups.map((group) => (
                                            <Button
                                                key={group.group_key}
                                                type="button"
                                                variant="outline"
                                                size="sm"
                                                className="rounded-full border-amber-500/30 bg-white/60 text-amber-800 hover:bg-white dark:bg-background/40 dark:text-amber-200"
                                                onClick={() => handleOpenCreateKey(group)}
                                                disabled={isCreatingKey}
                                            >
                                                {group.group_name || group.group_key}
                                                <span className="text-[10px] text-amber-700/80 dark:text-amber-200/80">
                                                    {isCreatingKey && creatingGroup?.group_key === group.group_key
                                                        ? '创建中...'
                                                        : '快捷创建'}
                                                </span>
                                            </Button>
                                        ))}
                                    </div>
                                </div>
                            </PopoverContent>
                        </Popover>
                    ) : null}

                    {visibleGroups.some(
                        (group) => group.masked_pending_key_count > 0 && group.enabled_key_count === 0,
                    ) ? (
                        <button
                            type="button"
                            onClick={handleFocusAttention}
                            className="inline-flex h-8 items-center gap-2 rounded-full border border-amber-500/30 bg-amber-500/10 px-3 text-xs font-medium text-amber-800 transition hover:bg-amber-500/15 dark:text-amber-200"
                        >
                            <CircleAlert className="size-3.5" />
                            待补全文明文 Key
                        </button>
                    ) : null}

                    {projectedGroups.length > 0 ? (
                        <Popover>
                            <PopoverTrigger asChild>
                                <button
                                    type="button"
                                    className="inline-flex h-8 items-center gap-2 rounded-full border border-border/70 bg-background/70 px-3 text-xs font-medium text-foreground transition hover:bg-muted/60"
                                >
                                    <KeyRound className="size-3.5 text-primary" />
                                    投影 Key {projectedGroups.length} 组
                                </button>
                            </PopoverTrigger>
                            <PopoverContent
                                align="start"
                                className="w-72 rounded-2xl border border-border/70 bg-card p-3 shadow-xl"
                            >
                                <div className="space-y-2">
                                    <div className="text-xs font-medium text-muted-foreground">投影渠道 Key 管理</div>
                                    <div className="flex flex-wrap gap-2">
                                        {projectedGroups.map((group) => (
                                            <Button
                                                key={`projected-${group.group_key}`}
                                                type="button"
                                                variant="outline"
                                                size="sm"
                                                className="rounded-full"
                                                onClick={() => handleOpenProjectedKeys(group)}
                                            >
                                                {group.group_name || group.group_key}
                                                <span className="text-[10px] text-muted-foreground">
                                                    {group.projected_keys.length} Keys
                                                </span>
                                            </Button>
                                        ))}
                                    </div>
                                </div>
                            </PopoverContent>
                        </Popover>
                    ) : null}

                    {unsupportedRouteCount > 0 ? (
                        <button
                            type="button"
                            onClick={handleFocusAttention}
                            className="inline-flex h-8 items-center gap-2 rounded-full border border-amber-500/30 bg-amber-500/10 px-3 text-xs font-medium text-amber-800 transition hover:bg-amber-500/15 dark:text-amber-200"
                        >
                            <CircleAlert className="size-3.5" />
                            未识别端点 {unsupportedRouteCount}
                        </button>
                    ) : null}

                    {selectedVisibleCount > 0 ? (
                        <div className="ml-auto flex flex-wrap items-center gap-2">
                            <span className="text-xs font-medium text-foreground">已选 {selectedVisibleCount} 个</span>
                            <Select
                                value={bulkMoveTarget}
                                onValueChange={(value) => setBulkMoveTarget(value as SiteModelRouteType)}
                            >
                                <SelectTrigger className="h-7 w-[10rem] rounded-xl text-xs">
                                    <SelectValue placeholder="目标端点" />
                                </SelectTrigger>
                                <SelectContent className="rounded-xl">
                                    {SITE_ROUTE_COLUMN_ORDER.map((routeType) => (
                                        <SelectItem key={routeType} value={routeType}>
                                            {routeTypeLabel(routeType)}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                            <Button
                                type="button"
                                size="sm"
                                className="h-7 rounded-xl px-2 text-xs"
                                onClick={() => applyRouteChange(selectedModels, bulkMoveTarget)}
                                disabled={hasPendingChanges}
                            >
                                移动
                            </Button>
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-7 rounded-xl px-2 text-xs"
                                onClick={() => applyDisabledChange(selectedModels, false)}
                                disabled={hasPendingChanges}
                            >
                                启用
                            </Button>
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-7 rounded-xl px-2 text-xs"
                                onClick={() => applyDisabledChange(selectedModels, true)}
                                disabled={hasPendingChanges}
                            >
                                停用
                            </Button>
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                className="h-7 rounded-xl px-2 text-xs"
                                onClick={() => setSelectedModelKeys(new Set())}
                            >
                                清空
                            </Button>
                        </div>
                    ) : null}
                </div>
            ) : null}
        </div>
    );
}
