'use client';

import { useTranslations } from 'next-intl';
import type { RelayTrace } from '@/api/endpoints/log';

export function LogTraceTimeline({ trace }: { trace: RelayTrace }) {
    const t = useTranslations('log.trace');
    const timings = [trace.client, ...trace.attempts].flatMap(exchange => (exchange.timings ?? []).map(item => ({ ...item, attempt: exchange.attempt_id, channel: exchange.channel_name }))).sort((a, b) => a.elapsed_ms - b.elapsed_ms);
    if (!timings.length) return null;
    return <details className="shrink-0 text-xs"><summary className="cursor-pointer py-1">{t('timeline')}</summary>
        <ol className="max-h-36 space-y-1 overflow-auto rounded-lg bg-muted p-2">
            {timings.map((item, index) => <li key={index} className="flex gap-2"><span className="w-20 shrink-0 font-mono">+{item.elapsed_ms} ms</span><span className="break-words">{item.attempt ? `#${item.attempt} ${item.channel ?? ''}` : t('client')} · {t.has(`phases.${item.phase}`) ? t(`phases.${item.phase}`) : item.phase}{item.reused ? ` · ${t('connectionReused')}` : ''}</span></li>)}
        </ol>
    </details>;
}
