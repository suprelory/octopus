'use client';

import { useEnableSiteAccount } from '@/api/endpoints/site';
import { type SiteChannelCard } from '@/api/endpoints/site-channel';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { MorphingDialogDescription, MorphingDialogTitle, useMorphingDialog } from '@/components/ui/morphing-dialog';
import { cn } from '@/lib/utils';
import { ExternalLink, Globe2, Power, Waypoints } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { SiteAccountPanel } from './SiteAccountPanel';
import { SiteChannelPendingJump } from './types';
import { platformLabel } from './utils';

export function SiteChannelDialog({
    card,
    jumpRequest,
    onJumpHandled,
    onNavigateToSite,
    onNavigateToSiteAccount,
    onNavigateToChannel,
}: {
    card: SiteChannelCard;
    jumpRequest: SiteChannelPendingJump | null;
    onJumpHandled: (requestId: number) => void;
    onNavigateToSite: () => void;
    onNavigateToSiteAccount: (accountId: number) => void;
    onNavigateToChannel: (channelId: number) => void;
}) {
    const { setIsOpen } = useMorphingDialog();
    const [activeAccountId, setActiveAccountId] = useState<number | null>(card.accounts[0]?.account_id ?? null);
    const [highlightedAccountId, setHighlightedAccountId] = useState<number | null>(null);
    const handledJumpRequestRef = useRef<number | null>(null);
    // Mount the panel after the dialog has measured and started its animation.
    const [panelReady, setPanelReady] = useState(false);
    const accountTabRefs = useRef<Map<number, HTMLButtonElement>>(new Map());

    useEffect(() => {
        // Two-frame defer: frame 1 lets the morph animation start + first paint,
        // frame 2 actually mounts the panel content.
        let raf2 = 0;
        const raf1 = window.requestAnimationFrame(() => {
            raf2 = window.requestAnimationFrame(() => setPanelReady(true));
        });
        return () => {
            window.cancelAnimationFrame(raf1);
            if (raf2) window.cancelAnimationFrame(raf2);
        };
    }, []);

    const closeAndNavigate = useCallback(
        (navigate: () => void) => {
            setIsOpen(false);
            window.requestAnimationFrame(() => {
                navigate();
            });
        },
        [setIsOpen],
    );

    const handleOpenSiteBaseUrl = useCallback(() => {
        if (!card.base_url) return;
        window.open(card.base_url, '_blank', 'noopener,noreferrer');
    }, [card.base_url]);

    const resolvedAccount =
        card.accounts.find((account) => account.account_id === activeAccountId) ?? card.accounts[0] ?? null;

    const enableSiteAccount = useEnableSiteAccount();

    const setAccountTabRef = useCallback((accountId: number, node: HTMLButtonElement | null) => {
        if (node) {
            accountTabRefs.current.set(accountId, node);
            return;
        }
        accountTabRefs.current.delete(accountId);
    }, []);

    useEffect(() => {
        if (!jumpRequest) return;
        if (jumpRequest.target.siteId !== card.site_id) return;
        if (handledJumpRequestRef.current === jumpRequest.requestId) return;
        const target = jumpRequest.target;
        if (target.kind === 'site-channel-card') return;

        if (activeAccountId !== target.accountId) {
            const frameId = window.requestAnimationFrame(() => {
                setActiveAccountId(target.accountId);
            });
            return () => window.cancelAnimationFrame(frameId);
        }

        const node = accountTabRefs.current.get(target.accountId);
        const frameId = window.requestAnimationFrame(() => {
            if (node) {
                node.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' });
                setHighlightedAccountId(target.accountId);
                window.setTimeout(() => {
                    setHighlightedAccountId((current) => (current === target.accountId ? null : current));
                }, 1800);
            }

            if (target.kind === 'site-channel-account') {
                handledJumpRequestRef.current = jumpRequest.requestId;
                onJumpHandled(jumpRequest.requestId);
            }
        });

        return () => window.cancelAnimationFrame(frameId);
    }, [jumpRequest, card.site_id, activeAccountId, onJumpHandled]);

    return (
        <div className="flex h-[88vh] flex-col overflow-hidden">
            <header className="flex flex-none items-center gap-2 border-b border-border/70 px-5 py-3 text-left sm:px-6">
                <MorphingDialogDescription className="sr-only">站点渠道管理面板</MorphingDialogDescription>

                <MorphingDialogTitle className="flex min-w-0 flex-1 flex-wrap items-center gap-2 text-lg font-semibold sm:text-xl">
                    <span className="truncate">{card.site_name}</span>
                    <Badge variant="outline" className="h-6 px-2 text-[11px]">
                        {platformLabel(card.platform)}
                    </Badge>
                    <Badge
                        variant="outline"
                        className={cn(
                            'h-6 px-2 text-[11px]',
                            card.enabled
                                ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                                : 'border-destructive/30 bg-destructive/10 text-destructive',
                        )}
                    >
                        {card.enabled ? '站点启用' : '站点停用'}
                    </Badge>
                    {resolvedAccount && card.accounts.length <= 1 ? (
                        <>
                            <span className="text-sm font-normal text-muted-foreground">
                                · {resolvedAccount.account_name}
                            </span>
                            <button
                                type="button"
                                onClick={() =>
                                    enableSiteAccount.mutate({
                                        id: resolvedAccount.account_id,
                                        enabled: !resolvedAccount.enabled,
                                    })
                                }
                                disabled={enableSiteAccount.isPending}
                                className={cn(
                                    'inline-flex h-6 cursor-pointer items-center gap-1 rounded-full border px-2 text-[11px] font-medium transition hover:opacity-80',
                                    resolvedAccount.enabled
                                        ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                                        : 'border-destructive/30 bg-destructive/10 text-destructive',
                                )}
                            >
                                <Power className={cn('size-3', enableSiteAccount.isPending && 'animate-spin')} />
                                {resolvedAccount.enabled ? '账号启用' : '账号停用'}
                            </button>
                        </>
                    ) : null}
                </MorphingDialogTitle>

                <div className="flex flex-none items-center gap-1">
                    <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        className="size-8 rounded-xl"
                        onClick={handleOpenSiteBaseUrl}
                        disabled={!card.base_url}
                        aria-label="打开站点"
                        title="打开站点"
                    >
                        <ExternalLink className="size-4" />
                    </Button>
                    <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        className="size-8 rounded-xl"
                        onClick={() => closeAndNavigate(onNavigateToSite)}
                        aria-label="站点页"
                        title="站点页"
                    >
                        <Globe2 className="size-4" />
                    </Button>
                    {resolvedAccount ? (
                        <Button
                            type="button"
                            variant="outline"
                            size="icon"
                            className="size-8 rounded-xl"
                            onClick={() => closeAndNavigate(() => onNavigateToSiteAccount(resolvedAccount.account_id))}
                            aria-label="站点页账号"
                            title="站点页账号"
                        >
                            <Waypoints className="size-4" />
                        </Button>
                    ) : null}
                </div>
            </header>

            <div className="flex min-h-0 flex-1 flex-col overflow-hidden px-5 py-3 sm:px-6">
                {resolvedAccount ? (
                    panelReady ? (
                        <SiteAccountPanel
                            key={resolvedAccount.account_id}
                            siteId={card.site_id}
                            account={resolvedAccount}
                            accounts={card.accounts}
                            activeAccountId={activeAccountId}
                            onSelectAccount={setActiveAccountId}
                            highlightedAccountId={highlightedAccountId}
                            registerAccountTabRef={setAccountTabRef}
                            jumpRequest={jumpRequest}
                            onJumpHandled={onJumpHandled}
                            onNavigateToChannel={(channelId) => closeAndNavigate(() => onNavigateToChannel(channelId))}
                        />
                    ) : (
                        <div className="min-h-0 flex-1 overflow-y-auto">
                            <SiteAccountPanelSkeleton />
                        </div>
                    )
                ) : (
                    <div className="flex min-h-[16rem] flex-1 items-center justify-center rounded-3xl border border-dashed border-border/70 bg-muted/20 text-sm text-muted-foreground">
                        当前站点没有可管理的账号
                    </div>
                )}
            </div>
        </div>
    );
}

export function SiteAccountPanelSkeleton() {
    // Lightweight skeleton shown for one frame while the morph animation starts.
    // Keeps the dialog body roughly the same height so morph layout doesn't jump
    // when SiteAccountPanel mounts. Pure CSS, no framer-motion / recharts / dnd.
    return (
        <div className="space-y-4">
            <div className="flex flex-wrap gap-2">
                <div className="h-9 w-32 animate-pulse rounded-2xl bg-muted/50" />
                <div className="h-9 w-24 animate-pulse rounded-2xl bg-muted/50" />
                <div className="h-9 w-28 animate-pulse rounded-2xl bg-muted/50" />
                <div className="ml-auto h-9 w-44 animate-pulse rounded-2xl bg-muted/50" />
            </div>
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                {Array.from({ length: 4 }).map((_, idx) => (
                    <div key={idx} className="h-40 animate-pulse rounded-3xl border border-border/70 bg-muted/40" />
                ))}
            </div>
            <div className="h-72 animate-pulse rounded-3xl border border-border/70 bg-muted/40" />
        </div>
    );
}
