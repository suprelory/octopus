'use client';

import { dayjs, DAYJS_LOCALE_MAP } from './date';

import { type SiteChannelCard } from '@/api/endpoints/site-channel';
import { Badge } from '@/components/ui/badge';
import {
    MorphingDialog,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogTrigger,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { cn, formatCount, formatMoney } from '@/lib/utils';
import { type JumpTarget } from '@/stores/jump';
import { useSettingStore } from '@/stores/setting';
import { ArrowUpRight, CheckCircle2, Clock, DollarSign, Globe2, Layers3, MessageSquare, Users, XCircle } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { memo, useCallback, useEffect, useMemo, useRef } from 'react';
import { getRouteTypeTone, SITE_ROUTE_DISPLAY_ORDER } from './constants';
import { SHORT_ROUTE_LABEL } from './model-view';
import { collectSiteRuntimeSummary, collectSiteSummary } from './site-summary';
import { SiteChannelDialog } from './SiteChannelDialog';
import { SiteChannelPendingJump } from './types';
import { platformLabel, routeTypeLabel } from './utils';

export function SiteCardImpl({
    card,
    layout,
    jumpRequest,
    highlighted,
    registerCardRef,
    onJumpHandled,
    requestJump,
}: {
    card: SiteChannelCard;
    layout: 'grid' | 'list';
    jumpRequest: SiteChannelPendingJump | null;
    highlighted: boolean;
    registerCardRef: (siteId: number, node: HTMLDivElement | null) => void;
    onJumpHandled: (requestId: number) => void;
    requestJump: (target: JumpTarget) => void;
}) {
    const summary = useMemo(() => collectSiteSummary(card), [card]);
    const runtime = useMemo(() => collectSiteRuntimeSummary(card), [card]);
    const tCard = useTranslations('siteChannel.card');
    const tMetrics = useTranslations('siteChannel.card.metrics');
    const locale = useSettingStore((s) => s.locale);
    const lastUsedText = runtime.lastRequestAt
        ? dayjs(runtime.lastRequestAt * 1000)
              .locale(DAYJS_LOCALE_MAP[locale])
              .fromNow()
        : null;
    const totalRequestsFmt = formatCount(runtime.totalRequests).formatted;
    const successFmt = formatCount(runtime.successCount).formatted;
    const failureFmt = formatCount(runtime.failureCount).formatted;
    const costFmt = formatMoney(runtime.totalCost).formatted;

    // Stable navigation callbacks. Building these inside SiteCard (rather than
    // inside SiteChannelGrid.renderCard) keeps SiteCard's prop identity stable
    // across grid re-renders, so memo() can actually skip work for cards whose
    // own data didn't change.
    const onNavigateToSite = useCallback(
        () => requestJump({ kind: 'site-card', siteId: card.site_id }),
        [requestJump, card.site_id],
    );
    const onNavigateToSiteAccount = useCallback(
        (accountId: number) => requestJump({ kind: 'site-account', siteId: card.site_id, accountId }),
        [requestJump, card.site_id],
    );
    const onNavigateToChannel = useCallback(
        (channelId: number) => requestJump({ kind: 'channel-card', channelId }),
        [requestJump],
    );

    return (
        <MorphingDialog>
            <div
                ref={(node) => registerCardRef(card.site_id, node)}
                className={cn(
                    'h-full rounded-2xl transition-shadow',
                    highlighted && 'ring-2 ring-primary/35 ring-offset-2 ring-offset-background',
                )}
            >
                <MorphingDialogTrigger aria-label={tCard('openDetails', { name: card.site_name })} className="group h-full w-full rounded-2xl outline-none focus-visible:ring-2 focus-visible:ring-ring">
                    <article className="flex h-full w-full flex-col gap-4 rounded-2xl border border-border/70 bg-card p-5 text-left shadow-sm transition-colors hover:border-primary/35">
                        <header className="flex items-center justify-between gap-3">
                            <div className="flex min-w-0 flex-1 items-center gap-3">
                                <span className={cn('flex size-10 shrink-0 items-center justify-center rounded-xl', card.enabled ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground')}><Globe2 aria-hidden className="size-5" /></span>
                                <div className="min-w-0">
                                    <h3 className="truncate text-base font-semibold tracking-tight" title={card.site_name}>{card.site_name}</h3>
                                    <span className={cn('mt-1 inline-flex items-center gap-1.5 text-[11px]', card.enabled ? 'text-primary' : 'text-muted-foreground')}>
                                        <span className={cn('size-1.5 rounded-full', card.enabled ? 'bg-primary' : 'bg-muted-foreground/50')} />
                                        {tCard(card.enabled ? 'statusEnabled' : 'statusDisabled')}
                                    </span>
                                </div>
                            </div>
                            <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
                                <Badge variant="outline" className="h-6 px-2 text-[11px]">
                                    {platformLabel(card.platform)}
                                </Badge>
                                {runtime.maskedPendingKeys > 0 ? (
                                    <Badge
                                        variant="outline"
                                        className="h-6 border-amber-500/30 bg-amber-500/10 px-2 text-[11px] text-amber-700 dark:text-amber-300"
                                    >
                                        {tCard('maskedPending', { n: runtime.maskedPendingKeys })}
                                    </Badge>
                                ) : null}
                            </div>
                        </header>

                        <dl className={cn('grid gap-x-4 gap-y-4 rounded-xl bg-muted/35 p-3.5', layout === 'list' ? 'grid-cols-2 sm:grid-cols-3 lg:grid-cols-5' : 'grid-cols-2')}>
                            {layout === 'list' ? (
                                <div className="min-w-0">
                                    <dt className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
                                        <MessageSquare className="size-3.5 text-primary" />
                                        {tMetrics('totalRequests')}
                                    </dt>
                                    <dd className="text-base font-semibold tabular-nums">
                                        {totalRequestsFmt.value}
                                        {totalRequestsFmt.unit && (
                                            <span className="ml-1 text-xs font-normal text-muted-foreground">
                                                {totalRequestsFmt.unit}
                                            </span>
                                        )}
                                    </dd>
                                </div>
                            ) : null}
                            <div className="min-w-0">
                                <dt className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
                                    <CheckCircle2 className="size-3.5 text-emerald-500" />
                                    {tMetrics('successRequests')}
                                </dt>
                                <dd className="text-base font-semibold tabular-nums">
                                    {successFmt.value}
                                    {successFmt.unit && (
                                        <span className="ml-1 text-xs font-normal text-muted-foreground">
                                            {successFmt.unit}
                                        </span>
                                    )}
                                </dd>
                            </div>
                            <div className="min-w-0">
                                <dt className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
                                    <XCircle className="size-3.5 text-destructive" />
                                    {tMetrics('failedRequests')}
                                </dt>
                                <dd className="text-base font-semibold tabular-nums">
                                    {failureFmt.value}
                                    {failureFmt.unit && (
                                        <span className="ml-1 text-xs font-normal text-muted-foreground">
                                            {failureFmt.unit}
                                        </span>
                                    )}
                                </dd>
                            </div>
                            <div className="min-w-0">
                                <dt className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
                                    <DollarSign className="size-3.5 text-primary" />
                                    {tMetrics('totalCost')}
                                </dt>
                                <dd className="text-base font-semibold tabular-nums">
                                    {costFmt.value}
                                    {costFmt.unit && (
                                        <span className="ml-1 text-xs font-normal text-muted-foreground">
                                            {costFmt.unit}
                                        </span>
                                    )}
                                </dd>
                            </div>
                            <div className="min-w-0">
                                <dt className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
                                    <Clock className="size-3.5 text-primary" />
                                    {tMetrics('lastRequestAt')}
                                </dt>
                                <dd className="text-base font-semibold tabular-nums">
                                    {lastUsedText ?? <span className="text-muted-foreground">—</span>}
                                </dd>
                            </div>
                        </dl>

                        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 text-xs text-muted-foreground">
                            <span className="inline-flex items-center gap-1"><Users aria-hidden className="size-3.5" />{tCard('accountCount', { count: card.account_count })}</span>
                            <span className="inline-flex items-center gap-1"><Layers3 aria-hidden className="size-3.5" />{tCard('modelCount', { count: summary.modelCount })}</span>
                        </div>

                        {summary.routeCounts.size > 0 ? (
                            <div className="flex flex-1 flex-wrap content-start gap-1.5">
                                {SITE_ROUTE_DISPLAY_ORDER.filter(
                                    (routeType) => (summary.routeCounts.get(routeType) ?? 0) > 0,
                                ).map((routeType) => (
                                    <Badge
                                        key={routeType}
                                        variant="outline"
                                        className={cn('h-6 shrink-0 px-2 text-[11px]', getRouteTypeTone(routeType))}
                                    >
                                        {SHORT_ROUTE_LABEL[routeType] ?? routeTypeLabel(routeType)}
                                        <span className="ml-1">{summary.routeCounts.get(routeType)}</span>
                                    </Badge>
                                ))}
                            </div>
                        ) : (
                            <div className="flex flex-1 items-center justify-center text-xs text-muted-foreground">
                                {tCard('noRouteDistribution')}
                            </div>
                        )}
                        <footer className="mt-auto flex items-center justify-between gap-2 border-t border-border/60 pt-3 text-[11px] text-muted-foreground">
                            <span>{tCard('enabledKeys', { enabled: summary.enabledKeys, total: summary.totalKeys })}</span>
                            <span className="inline-flex items-center gap-1 font-medium transition-colors group-hover:text-primary">{tCard('viewDetails')}<ArrowUpRight aria-hidden className="size-3.5" /></span>
                        </footer>
                    </article>
                </MorphingDialogTrigger>
            </div>

            <MorphingDialogContainer>
                <MorphingDialogContent className="max-h-[90vh] w-[min(96vw,92rem)] max-w-[min(96vw,92rem)] overflow-hidden rounded-xl bg-background">
                    <SiteChannelDialog
                        card={card}
                        jumpRequest={jumpRequest?.target.siteId === card.site_id ? jumpRequest : null}
                        onJumpHandled={() => {}}
                        onNavigateToSite={onNavigateToSite}
                        onNavigateToSiteAccount={onNavigateToSiteAccount}
                        onNavigateToChannel={onNavigateToChannel}
                    />
                </MorphingDialogContent>
            </MorphingDialogContainer>

            <SiteCardJumpWatcher jumpRequest={jumpRequest} siteId={card.site_id} onJumpHandled={onJumpHandled} />
        </MorphingDialog>
    );
}

export const SiteCard = memo(SiteCardImpl);

export function SiteCardJumpWatcher({
    jumpRequest,
    siteId,
    onJumpHandled,
}: {
    jumpRequest: SiteChannelPendingJump | null;
    siteId: number;
    onJumpHandled: (requestId: number) => void;
}) {
    const { isOpen, setIsOpen } = useMorphingDialog();
    const handledRequestRef = useRef<number | null>(null);
    const openedRequestRef = useRef<number | null>(null);
    const onJumpHandledRef = useRef(onJumpHandled);

    useEffect(() => {
        onJumpHandledRef.current = onJumpHandled;
    }, [onJumpHandled]);

    useEffect(() => {
        if (!jumpRequest) {
            handledRequestRef.current = null;
            return;
        }
        if (jumpRequest.target.siteId !== siteId) return;
        if (jumpRequest.target.kind === 'site-channel-card') return;
        if (isOpen) return;
        if (handledRequestRef.current === jumpRequest.requestId) return;

        const requestId = jumpRequest.requestId;
        const frameId = window.requestAnimationFrame(() => {
            handledRequestRef.current = requestId;
            openedRequestRef.current = requestId;
            setIsOpen(true);
        });
        return () => window.cancelAnimationFrame(frameId);
    }, [jumpRequest, siteId, isOpen, setIsOpen]);

    useEffect(() => {
        if (isOpen) return;
        const openedRequestId = openedRequestRef.current;
        if (openedRequestId === null) return;

        const timer = window.setTimeout(() => {
            if (openedRequestRef.current !== openedRequestId) return;
            openedRequestRef.current = null;
            onJumpHandledRef.current(openedRequestId);
        }, 260);

        return () => window.clearTimeout(timer);
    }, [isOpen]);

    useEffect(() => {
        return () => {
            const openedRequestId = openedRequestRef.current;
            if (openedRequestId === null) return;
            openedRequestRef.current = null;
            onJumpHandledRef.current(openedRequestId);
        };
    }, []);

    return null;
}
