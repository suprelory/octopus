'use client';

import { useTranslations } from 'next-intl';

export function ChannelListSummary({ total, enabled, visible }: { total: number; enabled: number; visible: number }) {
    const t = useTranslations('channel.list');
    return (
        <div className="mb-4 flex flex-wrap items-center justify-between gap-x-4 gap-y-3 rounded-2xl border border-border/60 bg-card/60 px-4 py-3">
            <div>
                <p className="text-sm font-medium">{t('title')}</p>
                <p className="mt-0.5 text-xs text-muted-foreground">{t('description')}</p>
            </div>
            <dl className="flex items-center gap-4 text-xs sm:gap-5">
                {[
                    { label: t('total'), value: total, tone: 'text-foreground' },
                    { label: t('enabled'), value: enabled, tone: 'text-primary' },
                    { label: t('disabled'), value: total - enabled, tone: 'text-muted-foreground' },
                ].map(item => <div key={item.label} className="flex items-baseline gap-1.5"><dt className="text-muted-foreground">{item.label}</dt><dd className={`text-base font-semibold tabular-nums ${item.tone}`}>{item.value}</dd></div>)}
                {visible !== total && <div className="border-l border-border pl-4 text-muted-foreground"><dt className="sr-only">{t('results')}</dt><dd>{t('visible', { count: visible })}</dd></div>}
            </dl>
        </div>
    );
}
