'use client';

import { useTranslations } from 'next-intl';
import type { ChannelAttempt } from '@/api/endpoints/log';

export function RoutingDetails({attempt}: {attempt: ChannelAttempt}) {
    const t = useTranslations('routing');
    const summary = attempt.routing;
    if (!attempt.selection_reason && !attempt.failure_class && !summary) return null;
    return <div className="space-y-1 border-t border-border/40 pt-2 text-[11px] text-muted-foreground">
        {attempt.selection_reason && <p>{t('decision')}: {t(`selection.${attempt.selection_reason}`)} · {attempt.selection_strategy} · {t('quality')}: {attempt.quality_rank} / {attempt.capability_status}</p>}
        {attempt.selection_metrics && <p>{t('score')}: {attempt.selection_metrics.composite_score.toFixed(1)} · {t('priority')}: {attempt.selection_metrics.priority} · {t('inFlight')}: {attempt.selection_metrics.in_flight} · {t('failureRate')}: {(attempt.selection_metrics.failure_rate * 100).toFixed(1)}%</p>}
        {attempt.conversion_path?.length ? <p>{attempt.conversion_path.join(' → ')}</p> : null}
        {attempt.failure_class && <p>{t('failure')}: {attempt.failure_class}{attempt.failure_scope && <> · {t('scope')}: {t(`scopeLabels.${attempt.failure_scope}`)}</>}</p>}
        {attempt.retry_at && <p>{t('retryAt')}: {new Date(attempt.retry_at).toLocaleString()}</p>}
        {summary && <><p>{t('stop')}: {summary.stop_reason && t(`stops.${summary.stop_reason}`)}</p><p>{t('usage', {attempts: summary.attempts_used, attemptLimit: summary.attempt_limit, channels: summary.channels_used, channelLimit: summary.channel_limit})} · {t('affinity')}: {summary.affinity_mode} / {summary.affinity_source}</p></>}
    </div>;
}
