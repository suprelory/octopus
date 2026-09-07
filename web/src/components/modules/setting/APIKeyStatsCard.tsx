'use client';

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { X } from 'lucide-react';
import { type APIKey } from '@/api/endpoints/apikey';
import { useStatsAPIKey } from '@/api/endpoints/stats';
import { OverlayPortal } from './OverlayPortal';
import { OVERLAY_ENTRANCE } from '@/lib/animations/css-entrances';
import { cn } from '@/lib/utils';

export function APIKeyStatsCard({
    apiKey,
    onClose,
}: {
    apiKey: APIKey;
    onClose: () => void;
}) {
    const t = useTranslations('setting');
    const { data: statsList = [] } = useStatsAPIKey();
    const stats = useMemo(() => statsList.find((s) => s.api_key_id === apiKey.id), [statsList, apiKey.id]);

    return (
        <OverlayPortal onClose={onClose}>
            <div
                role="dialog"
                aria-modal="true"
                data-slot="dialog-content"
                className={cn(
                    'fixed left-1/2 top-1/2 z-50 w-[min(320px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 flex flex-col bg-card p-5 rounded-3xl border border-border max-h-[80vh] overflow-auto',
                    OVERLAY_ENTRANCE,
                )}
            >
                <div className="flex items-center justify-between gap-2 mb-3">
                    <h3 className="text-sm font-semibold text-card-foreground line-clamp-1">
                        {apiKey.name}
                    </h3>
                    <button
                        type="button"
                        onClick={onClose}
                        className="size-8 flex items-center justify-center rounded-lg bg-muted text-muted-foreground transition-colors hover:bg-muted/80"
                    >
                        <X className="size-4" />
                    </button>
                </div>

                {!stats ? (
                    <div className="text-sm text-muted-foreground">{t('apiKey.stats.noData')}</div>
                ) : (
                    <div className="grid grid-cols-2 gap-3 text-sm">
                        <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.inputToken')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.input_token.formatted.value}
                                {stats.input_token.formatted.unit}
                            </div>
                        </div>
                        <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.outputToken')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.output_token.formatted.value}
                                {stats.output_token.formatted.unit}
                            </div>
                        </div>
                        <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.inputCost')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.input_cost.formatted.value}
                                {stats.input_cost.formatted.unit}
                            </div>
                        </div>
                        <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.outputCost')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.output_cost.formatted.value}
                                {stats.output_cost.formatted.unit}
                            </div>
                        </div>
                        <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.requestSuccess')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.request_success.formatted.value}
                                {stats.request_success.formatted.unit}
                            </div>
                        </div>
                        <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.requestFailed')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.request_failed.formatted.value}
                                {stats.request_failed.formatted.unit}
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </OverlayPortal>
    );
}
