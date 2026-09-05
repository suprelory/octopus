'use client';

import { useSiteChannelList } from '@/api/endpoints/site-channel';
import type { ToolbarSortField, ToolbarSortOrder } from '@/components/modules/toolbar/view-options-store';
import { cn } from '@/lib/utils';
import { isSiteChannelJumpTarget, useJumpStore } from '@/stores/jump';
import { useSettingStore } from '@/stores/setting';
import { useTranslations } from 'next-intl';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { translateSiteMessage } from '../site/site-message';
import { useCompletionStateSync } from './completion';
import { useCompletionStore } from './completion-store';
import { SiteChannelGrid } from './SiteChannelGrid';
import { SiteChannelPendingJump } from './types';
import { UnifiedCompletionDialog } from './UnifiedCompletionDialog';

export function SiteChannelSection({
    searchTerm,
    sortField,
    sortOrder,
    layout,
}: {
    searchTerm: string;
    sortField: ToolbarSortField;
    sortOrder: ToolbarSortOrder;
    layout: 'grid' | 'list';
}) {
    const t = useTranslations();
    const locale = useSettingStore((state) => state.locale);
    const { data, isLoading, error } = useSiteChannelList();
    const pendingJump = useJumpStore((state) => state.pending);
    const clearPending = useJumpStore((state) => state.clearPending);
    const requestJump = useJumpStore((state) => state.requestJump);
    const [highlightedSiteId, setHighlightedSiteId] = useState<number | null>(null);
    const siteCardRefs = useRef<Map<number, HTMLDivElement>>(new Map());

    // 同步补全状态到 store，并暴露对话框控制
    const { pendingCompletionSites, totalPendingCompletionCount } = useCompletionStateSync();
    const setPendingCount = useCompletionStore((s) => s.setPendingCount);
    const completionDialogOpen = useCompletionStore((s) => s.dialogOpen);
    const setCompletionDialogOpen = useCompletionStore((s) => s.setDialogOpen);

    useEffect(() => {
        setPendingCount(totalPendingCompletionCount);
        // 待补全清零时主动关闭对话框，避免残留的 open 状态在新任务到来时自动重开
        if (totalPendingCompletionCount === 0) {
            setCompletionDialogOpen(false);
        }
    }, [totalPendingCompletionCount, setPendingCount, setCompletionDialogOpen]);

    const pendingSiteChannelJump =
        pendingJump && isSiteChannelJumpTarget(pendingJump.target) ? (pendingJump as SiteChannelPendingJump) : null;
    const forcedSiteId = pendingSiteChannelJump?.target.siteId ?? null;

    const registerCardRef = useCallback((siteId: number, node: HTMLDivElement | null) => {
        if (node) {
            siteCardRefs.current.set(siteId, node);
            return;
        }
        siteCardRefs.current.delete(siteId);
    }, []);

    const cards = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        return (data ?? [])
            .filter((card) => card.account_count > 0)
            .filter((card) => {
                if (card.site_id === forcedSiteId) return true;
                if (!term) return true;

                const accountNames = card.accounts.map((account) => account.account_name.toLowerCase());
                return card.site_name.toLowerCase().includes(term) || accountNames.some((name) => name.includes(term));
            })
            .sort((a, b) => {
                // Pin the jump target to the top so the virtualized list keeps
                // it mounted in the initial overscan window. Without this, the
                // jump-to-card useEffect below would no-op when the target is
                // outside the rendered window (registerCardRef never fires for
                // off-screen items, so siteCardRefs.get() returns null).
                if (forcedSiteId !== null) {
                    if (a.site_id === forcedSiteId) return -1;
                    if (b.site_id === forcedSiteId) return 1;
                }
                const diff = sortField === 'name' ? a.site_name.localeCompare(b.site_name) : a.site_id - b.site_id;
                return sortOrder === 'asc' ? diff : -diff;
            });
    }, [data, searchTerm, sortField, sortOrder, forcedSiteId]);
    useEffect(() => {
        if (!pendingSiteChannelJump) return;
        const node = siteCardRefs.current.get(pendingSiteChannelJump.target.siteId);
        if (!node) return;

        const timer = window.setTimeout(() => {
            node.scrollIntoView({ behavior: 'smooth', block: 'center' });
            setHighlightedSiteId(pendingSiteChannelJump.target.siteId);
            window.setTimeout(() => {
                setHighlightedSiteId((current) => (current === pendingSiteChannelJump.target.siteId ? null : current));
            }, 1800);

            if (pendingSiteChannelJump.target.kind === 'site-channel-card') {
                clearPending(pendingSiteChannelJump.requestId);
            }
        }, 80);

        return () => window.clearTimeout(timer);
    }, [pendingSiteChannelJump, clearPending, cards.length]);

    if (isLoading) {
        return (
            <section className={cn('grid gap-4', layout === 'list' ? 'grid-cols-1' : 'md:grid-cols-2 xl:grid-cols-3')}>
                {Array.from({ length: layout === 'list' ? 2 : 3 }).map((_, index) => (
                    <div key={index} className="h-56 animate-pulse rounded-3xl border border-border/70 bg-muted/40" />
                ))}
            </section>
        );
    }

    if (error) {
        return (
            <section className="rounded-3xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                站点渠道加载失败：{translateSiteMessage(locale, error.message, t)}
            </section>
        );
    }

    return (
        <>
            {cards.length > 0 && (
                <SiteChannelGrid
                    cards={cards}
                    layout={layout}
                    pendingSiteChannelJump={pendingSiteChannelJump}
                    highlightedSiteId={highlightedSiteId}
                    registerCardRef={registerCardRef}
                    clearPending={clearPending}
                    requestJump={requestJump}
                />
            )}
            <UnifiedCompletionDialog
                open={completionDialogOpen && totalPendingCompletionCount > 0}
                onOpenChange={setCompletionDialogOpen}
                sites={pendingCompletionSites}
            />
        </>
    );
}

export { SiteChannelCompletionAction, useCompletionStateSync } from './completion';
