'use client';

import { type SiteChannelCard } from '@/api/endpoints/site-channel';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { type JumpTarget } from '@/stores/jump';
import { useCallback } from 'react';
import { SiteCard } from './SiteCard';
import { SiteChannelPendingJump } from './types';

export function SiteChannelGrid({
    cards,
    layout,
    pendingSiteChannelJump,
    highlightedSiteId,
    registerCardRef,
    clearPending,
    requestJump,
}: {
    cards: SiteChannelCard[];
    layout: 'grid' | 'list';
    pendingSiteChannelJump: SiteChannelPendingJump | null;
    highlightedSiteId: number | null;
    registerCardRef: (siteId: number, node: HTMLDivElement | null) => void;
    clearPending: (requestId?: number) => void;
    requestJump: (target: JumpTarget) => void;
}) {
    const columnCompute = useCallback(
        (width: number) => {
            if (layout === 'list') return 1;
            const MIN_CARD_WIDTH = 320;
            const GUTTER = 16;
            const cols = Math.floor((width + GUTTER) / (MIN_CARD_WIDTH + GUTTER));
            return Math.max(1, Math.min(6, cols));
        },
        [layout],
    );

    const renderCard = useCallback(
        (card: SiteChannelCard) => (
            <SiteCard
                key={card.site_id}
                card={card}
                layout={layout}
                jumpRequest={pendingSiteChannelJump?.target.siteId === card.site_id ? pendingSiteChannelJump : null}
                highlighted={highlightedSiteId === card.site_id}
                registerCardRef={registerCardRef}
                onJumpHandled={clearPending}
                requestJump={requestJump}
            />
        ),
        [layout, pendingSiteChannelJump, highlightedSiteId, registerCardRef, clearPending, requestJump],
    );

    return (
        <VirtualizedGrid
            items={cards}
            layout={layout}
            columns={columnCompute}
            estimateItemHeight={240}
            getItemKey={(card) => `site-channel-${card.site_id}`}
            renderItem={renderCard}
        />
    );
}
