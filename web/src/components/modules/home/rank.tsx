'use client';

import { useChannelList } from '@/api/endpoints/channel';
import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { ChartNoAxesColumnIncreasing } from 'lucide-react';
import { Tabs, TabsList, TabsTrigger, TabsContents, TabsContent } from '@/components/animate-ui/components/animate/tabs';
import { useHomeViewStore, type RankSortMode } from '@/components/modules/home/store';
import { cn } from '@/lib/utils';

type ChannelData = NonNullable<ReturnType<typeof useChannelList>['data']>[number];

export function Rank() {
    const { data: channelData, isLoading, isError } = useChannelList();
    const t = useTranslations('home.rank');
    const rankSortMode = useHomeViewStore((state) => state.rankSortMode);
    const setRankSortMode = useHomeViewStore((state) => state.setRankSortMode);

    const rankedByCost = useMemo<ChannelData[]>(() => {
        if (!channelData) return [];
        return [...channelData].sort((a, b) => b.formatted.total_cost.raw - a.formatted.total_cost.raw);
    }, [channelData]);

    const rankedByCount = useMemo<ChannelData[]>(() => {
        if (!channelData) return [];
        return [...channelData].sort((a, b) => b.formatted.request_count.raw - a.formatted.request_count.raw);
    }, [channelData]);

    const rankedByTokens = useMemo<ChannelData[]>(() => {
        if (!channelData) return [];
        return [...channelData].sort((a, b) => b.formatted.total_token.raw - a.formatted.total_token.raw);
    }, [channelData]);

    const renderList = (channels: ChannelData[], mode: RankSortMode) => {
        if (isLoading) {
            return <div className="space-y-3 py-3" aria-busy="true">{Array.from({ length: 3 }, (_, index) => <div key={index} className="h-13 animate-pulse rounded-xl bg-muted/60" />)}</div>;
        }
        if (channels.length === 0) {
            return (
                <div className="flex h-48 flex-col items-center justify-center gap-3 text-muted-foreground">
                    <ChartNoAxesColumnIncreasing className="size-8 opacity-50" />
                    <p className="text-sm">{isError ? t('loadFailed') : t('noData')}</p>
                </div>
            );
        }
        return (
            <div className="max-h-59 space-y-1 overflow-y-auto pr-1">
                {channels.map((channel, index) => {
                    const rank = index + 1;
                    const field = mode === 'cost' ? 'total_cost' : mode === 'tokens' ? 'total_token' : 'request_count';
                    const maxValue = channels[0].formatted[field].raw;
                    const share = maxValue > 0 ? channel.formatted[field].raw / maxValue * 100 : 0;

                    return (
                        <div
                            key={channel.raw.id}
                            className="flex w-full items-center gap-3 rounded-xl px-2 py-3 text-left transition-colors hover:bg-muted/50"
                        >
                            <div className={cn('flex size-7 shrink-0 items-center justify-center rounded-lg text-xs font-semibold tabular-nums', index === 0 ? 'bg-primary/10 text-primary' : 'bg-muted/70 text-muted-foreground')}>
                                {String(rank).padStart(2, '0')}
                            </div>

                            <div className="flex-1 min-w-0">
                                <p className="truncate text-sm font-medium" title={channel.raw.name}>{channel.raw.name}</p>
                                <div className="mt-2 h-1 overflow-hidden rounded-full bg-muted" aria-hidden="true">
                                    <div className="h-full rounded-full bg-primary/60" style={{ width: `${share}%` }} />
                                </div>
                                {mode === 'count' && (() => {
                                    const successCount = channel.formatted.request_success.raw;
                                    const failedCount = channel.formatted.request_failed.raw;
                                    const totalCount = successCount + failedCount;
                                    const successRate = totalCount > 0 ? (successCount / totalCount) * 100 : 0;

                                    return (
                                        <div className="flex items-center gap-1 text-xs text-muted-foreground mt-1">
                                            <span>{t('successRate')}:</span>
                                            <span>{successRate.toFixed(1)}%</span>
                                        </div>
                                    );
                                })()}
                            </div>

                            <div className="flex items-center gap-1 text-right shrink-0">
                                {mode === 'count' ? (
                                    <div className="flex items-center gap-1 text-sm font-medium tabular-nums">
                                        <span className="text-accent">
                                            {channel.formatted.request_success.formatted.value}
                                            <span className="text-xs text-muted-foreground">
                                                {channel.formatted.request_success.formatted.unit}
                                            </span>
                                        </span>
                                        <span className="text-muted-foreground/40 font-light">/</span>
                                        <span className="text-destructive">
                                            {channel.formatted.request_failed.formatted.value}
                                            <span className="text-xs text-muted-foreground">
                                                {channel.formatted.request_failed.formatted.unit}
                                            </span>
                                        </span>
                                    </div>
                                ) : mode === 'tokens' ? (
                                    <span className="font-semibold text-base">
                                        {channel.formatted.total_token.formatted.value}
                                        <span className="text-xs text-muted-foreground">
                                            {channel.formatted.total_token.formatted.unit}
                                        </span>
                                    </span>
                                ) : (
                                    <span className="font-semibold text-base">
                                        <span className="mr-0.5 text-xs font-normal text-muted-foreground">$</span>
                                        {channel.formatted.total_cost.formatted.value}
                                        <span className="text-xs text-muted-foreground">
                                            {channel.formatted.total_cost.formatted.unit.replace('$', '')}
                                        </span>
                                    </span>
                                )}
                            </div>
                        </div>
                    );
                })}
            </div>
        );
    };

    return (
        <section aria-label={t('title')} className="min-w-0 rounded-2xl border border-border/70 bg-card p-5 shadow-sm">
            <Tabs value={rankSortMode} onValueChange={(value) => setRankSortMode(value as RankSortMode)}>
                <div className="mb-2 flex items-center justify-between gap-3">
                    <h3 className="font-semibold">{t('title')}</h3>
                    <span className="rounded-full border border-border/60 px-2 py-0.5 text-[11px] text-muted-foreground">{t('allTime')}</span>
                </div>
                <TabsList aria-label={t('sortLabel')} className="mb-2 w-full bg-muted/60">
                    <TabsTrigger value="cost">{t('sortByCost')}</TabsTrigger>
                    <TabsTrigger value="count">{t('sortByCount')}</TabsTrigger>
                    <TabsTrigger value="tokens">{t('sortByTokens')}</TabsTrigger>
                </TabsList>
                <TabsContents>
                    <TabsContent value="cost">
                        {rankSortMode === 'cost' && renderList(rankedByCost, 'cost')}
                    </TabsContent>
                    <TabsContent value="count">
                        {rankSortMode === 'count' && renderList(rankedByCount, 'count')}
                    </TabsContent>
                    <TabsContent value="tokens">
                        {rankSortMode === 'tokens' && renderList(rankedByTokens, 'tokens')}
                    </TabsContent>
                </TabsContents>
            </Tabs>
        </section>
    );
}
