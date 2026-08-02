'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useChannelList } from '@/api/endpoints/channel';
import { useLogs, useLogSiteActionTargets, type LogKeywordMode, type LogKeywordScope } from '@/api/endpoints/log';
import { LogCard, type LogSiteActionTargets } from './Item';
import { Columns3, Loader2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useSearchStore } from '@/components/modules/toolbar';
import { useToolbarViewOptionsStore } from '@/components/modules/toolbar/view-options-store';
import { useLogFieldVisibilityStore, useLogUIStore, type LogFieldName } from './ui-store';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';

type LogFilters = {
    keyword: string;
    keywordMode: LogKeywordMode;
    keywordScope: LogKeywordScope;
    channelIds: number[];
    startTime?: number;
    endTime?: number;
};

const LOG_PAGE_SIZE = 10;

function useDebouncedValue<T>(value: T, delay = 200) {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const handle = setTimeout(() => setDebounced(value), delay);
        return () => clearTimeout(handle);
    }, [value, delay]);
    return debounced;
}

function filtersActive(filters: LogFilters) {
    return (
        !!filters.keyword.trim() ||
        filters.channelIds.length > 0 ||
        !!filters.startTime ||
        !!filters.endTime
    );
}

/**
 * 日志页面组件
 * - 初始加载 pageSize 条历史日志
 * - SSE 实时推送新日志（无过滤时）
 * - 过滤模式使用 cursor 分页，滚动加载更多
 */
export function Log() {
    const t = useTranslations('log');
    const pageKey = 'log' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const refreshRequestId = useLogUIStore((s) => s.refreshRequestId);
    const setRefreshing = useLogUIStore((s) => s.setRefreshing);
    const lastHandledRefreshRequestIdRef = useRef(refreshRequestId);
    const logDateRange = useToolbarViewOptionsStore((s) => s.logDateRange);
    const logChannelIds = useToolbarViewOptionsStore((s) => s.logChannelIds);
    const logKeywordMode = useToolbarViewOptionsStore((s) => s.logKeywordMode);
    const logKeywordScope = useToolbarViewOptionsStore((s) => s.logKeywordScope);
    const filters = useMemo<LogFilters>(() => ({
        keyword: searchTerm,
        keywordMode: logKeywordMode,
        keywordScope: logKeywordScope,
        channelIds: logChannelIds,
        startTime: logDateRange.start,
        endTime: logDateRange.end,
    }), [logDateRange.end, logDateRange.start, logChannelIds, searchTerm, logKeywordMode, logKeywordScope]);
    const debouncedFilters = useDebouncedValue(filters, 200);
    const filterMode = filtersActive(debouncedFilters);
    const logFilters = useMemo(() => ({
        keyword: debouncedFilters.keyword.trim() || undefined,
        keyword_mode: debouncedFilters.keyword.trim() ? debouncedFilters.keywordMode : undefined,
        keyword_scope: debouncedFilters.keyword.trim() ? debouncedFilters.keywordScope : undefined,
        channel_ids: debouncedFilters.channelIds.length > 0 ? debouncedFilters.channelIds : undefined,
        start_time: debouncedFilters.startTime,
        end_time: debouncedFilters.endTime,
    }), [debouncedFilters]);
    const liveLogsQuery = useLogs({ pageSize: LOG_PAGE_SIZE, filters: logFilters, mode: filterMode ? 'paged' : 'stream' });
    const logs = liveLogsQuery.logs;
    const hasMore = liveLogsQuery.hasMore;
    const isLoading = liveLogsQuery.isLoading;
    const isLoadingMore = liveLogsQuery.isLoadingMore;
    const loadMore = liveLogsQuery.loadMore;
    const warning = liveLogsQuery.warning;
    const visibility = useLogFieldVisibilityStore((state) => state.visibility);
    const tView = useTranslations('log.viewOptions');
    const { data: channels = [] } = useChannelList();
    const channelNameById = useMemo(() => {
        const map = new Map<number, string>();
        for (const channel of channels) map.set(channel.raw.id, channel.raw.name);
        return map;
    }, [channels]);

    const logIDs = useMemo(() => logs.map((log) => log.id), [logs]);
    const siteActionTargetsQuery = useLogSiteActionTargets(logIDs, logs.length > 0);
    const siteActionTargets = useMemo(() => {
        const next = new Map<number, LogSiteActionTargets>();
        const data = siteActionTargetsQuery.data ?? {};
        for (const [id, targets] of Object.entries(data)) {
            next.set(Number(id), targets);
        }
        return next;
    }, [siteActionTargetsQuery.data]);

    const canLoadMore = hasMore && !isLoading && !isLoadingMore && logs.length > 0;
    const handleReachEnd = useCallback(() => {
        if (!canLoadMore) return;
        void loadMore();
    }, [canLoadMore, loadMore]);

    const refreshIdRef = useRef(0);
    const handleRefresh = useCallback(async () => {
        refreshIdRef.current += 1;
        const myId = refreshIdRef.current;
        setRefreshing(true);
        const startedAt = Date.now();
        try {
            await liveLogsQuery.refetch();
        } finally {
            const elapsed = Date.now() - startedAt;
            const remaining = Math.max(0, 500 - elapsed);
            setTimeout(() => {
                if (refreshIdRef.current === myId) setRefreshing(false);
            }, remaining);
        }
    }, [liveLogsQuery, setRefreshing]);

    useEffect(() => {
        if (refreshRequestId === lastHandledRefreshRequestIdRef.current) return;
        lastHandledRefreshRequestIdRef.current = refreshRequestId;
        void handleRefresh();
    }, [handleRefresh, refreshRequestId]);

    const footer = useMemo(() => {
        if (hasMore) {
            return (
                <div className="flex justify-center py-4">
                    <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
            );
        }
        if (logs.length > 0) {
            return (
                <div className="flex justify-center py-4">
                    <span className="text-sm text-muted-foreground">{t('list.noMore')}</span>
                </div>
            );
        }
        return null;
    }, [hasMore, logs.length, t]);

    return (
        <div className="flex h-full min-h-0 flex-col gap-3 overflow-hidden">
            <div className="flex justify-end">
                <Popover>
                    <PopoverTrigger asChild>
                        <button type="button" className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground">
                            <Columns3 className="size-3.5" />{tView('title')}
                        </button>
                    </PopoverTrigger>
                    <PopoverContent align="end" className="w-56 p-3">
                        <div className="mb-2 text-xs font-medium text-muted-foreground">{tView('title')}</div>
                        <div className="flex flex-col gap-1">
                            {(['endpointType', 'channelName', 'actualModel', 'apiKeyName', 'clientIP', 'cost', 'tps', 'cacheHitRate', 'reasoningEffort', 'reasoningTokens'] as LogFieldName[]).map((field) => (
                                <label key={field} className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-xs transition-colors hover:bg-muted">
                                    <input type="checkbox" checked={visibility[field]} onChange={() => useLogFieldVisibilityStore.getState().toggleField(field)} className="size-3 rounded" />
                                    {tView(field)}
                                </label>
                            ))}
                        </div>
                        <button type="button" onClick={() => useLogFieldVisibilityStore.getState().resetFields()} className="mt-2 w-full rounded-md border border-border/50 px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground">{tView('reset')}</button>
                    </PopoverContent>
                </Popover>
            </div>
            {warning ? (
                <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
                    {warning}
                </div>
            ) : null}
            <div className="relative min-h-0 flex-1">
                <VirtualizedGrid
                    items={logs}
                    layout="list"
                    columns={{ default: 1 }}
                    estimateItemHeight={180}
                    overscan={8}
                    getItemKey={(log) => `log-${log.id}`}
                    renderItem={(log) => <LogCard log={log} siteTargets={siteActionTargets.get(log.id) ?? null} channelNameById={channelNameById} />}
                    footer={footer}
                    onReachEnd={handleReachEnd}
                    reachEndEnabled={canLoadMore}
                    reachEndOffset={2}
                />
            </div>
        </div>
    );
}
