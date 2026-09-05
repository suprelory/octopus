'use client';

import { useMemo } from 'react';
import { Clock, Cpu, Gauge, Percent, Globe, Zap, ArrowDownToLine, ArrowUpFromLine, DollarSign, ArrowRight, Pin, KeyRound, TestTube2, Brain, Type, Sigma } from 'lucide-react';
import { useTranslations } from 'next-intl';
import type { RelayLog } from '@/api/endpoints/log';
import { getModelIcon } from '@/lib/model-icons';
import { Badge } from '@/components/ui/badge';
import type { resolveLogDisplayFields } from './display';
import { formatTime, formatDurationCompact, formatTPS, formatCacheHitRate, sanitizeErrorMessage, formatCompactTokenCount, type TokenUsageDisplay } from './log-format';
import { RetryBadgeWithTooltip, WSModeBadge } from './log-status';
import { useLogFieldVisibility } from './ui-store';

export function LogSummary({ log, displayFields, tokenUsage, endpointLabel, channelNameById }: {
    log: RelayLog;
    displayFields: ReturnType<typeof resolveLogDisplayFields>;
    tokenUsage: TokenUsageDisplay;
    endpointLabel: string;
    channelNameById?: ReadonlyMap<number, string>;
}) {
    const t = useTranslations('log.card');
    const visibility = useLogFieldVisibility();
    const hasError = !!log.error;
    const hasMultipleAttempts = (log.attempts?.length ?? 0) > 1;
    const displayActualModelName = displayFields.actualModelName;
    const displayRequestModelName = displayFields.requestModelName;
    const displayChannelName = displayFields.channelName || '-';
    const requestAPIKeyName = displayFields.requestAPIKeyName;
    const clientIP = displayFields.clientIP;
    const { cacheReadTokens, cacheWriteTokens, totalInputTokens, totalTokens } = tokenUsage;
    const { Avatar: ModelAvatar, color: brandColor } = useMemo(
        () => getModelIcon(displayActualModelName),
        [displayActualModelName],
    );

    return (
        <div className="grid grid-cols-[auto_1fr] items-center gap-2.5 p-2.5 sm:gap-4 sm:p-4">
            <div className="sm:hidden"><ModelAvatar size={36} /></div>
            <div className="hidden sm:block"><ModelAvatar size={40} /></div>
            <div className="min-w-0 flex flex-col gap-2">
                <div className="flex min-w-0 flex-wrap items-center gap-2 text-sm md:flex-nowrap">
                    <span className="min-w-0 max-w-full font-semibold text-card-foreground truncate md:max-w-[32%]" title={displayRequestModelName}>
                        {displayRequestModelName}
                    </span>
                    {log.is_test ? (
                        <Badge variant="outline" className="shrink-0 border-blue-400/50 px-1.5 py-0 text-xs text-blue-500 dark:text-blue-400">
                            <TestTube2 className="mr-1 size-3" />{t('testLog')}
                        </Badge>
                    ) : null}
                    <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/50" />
                    {visibility.endpointType && endpointLabel ? (
                        <Badge variant="secondary" className="max-w-full shrink-0 px-1.5 py-0 text-xs" style={{ backgroundColor: `${brandColor}15`, color: brandColor }}>
                            <span className="max-w-[10rem] truncate">{endpointLabel}</span>
                        </Badge>
                    ) : null}
                    {visibility.channelName ? (
                        <>
                            <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/50" />
                            {hasMultipleAttempts ? (
                                <RetryBadgeWithTooltip channelName={displayChannelName} brandColor={brandColor} attempts={log.attempts!} channelNameById={channelNameById} />
                            ) : (
                                <Badge variant="secondary" className="max-w-full shrink-0 px-1.5 py-0 text-xs" style={{ backgroundColor: `${brandColor}15`, color: brandColor }}>
                                    <span className="max-w-[18rem] truncate">{displayChannelName}</span>
                                </Badge>
                            )}
                        </>
                    ) : null}
                    {visibility.actualModel ? (
                        <span className="min-w-0 truncate text-muted-foreground md:flex-1" title={displayActualModelName}>
                            {displayActualModelName}
                        </span>
                    ) : null}
                    {log.attempts?.some((attempt) => attempt.sticky) ? <Pin className="size-3.5 shrink-0 text-amber-500" /> : null}
                    <WSModeBadge log={log} />
                </div>
                <div className="grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs tabular-nums text-muted-foreground md:grid-cols-7">
                    <div className="flex items-center gap-1.5">
                        <Clock className="size-3.5 shrink-0" style={{ color: brandColor }} />
                        <span>{formatTime(log.time)}</span>
                    </div>
                    {visibility.apiKeyName && requestAPIKeyName ? (
                        <div className="flex items-center gap-1.5">
                            <KeyRound className="size-3.5 shrink-0 text-orange-500" />
                            <span className="truncate" title={requestAPIKeyName}>
                                {requestAPIKeyName}
                            </span>
                        </div>
                    ) : null}
                    {visibility.clientIP && clientIP ? <div className="flex items-center gap-1.5"><Globe className="size-3.5 shrink-0 text-sky-500" /><span className="truncate" title={clientIP}>{clientIP}</span></div> : null}
                    <div className="flex items-center gap-1.5">
                        <Zap className="size-3.5 shrink-0 text-amber-500" />
                        <span>{t('firstToken')} {formatDurationCompact(log.ftut)}</span>
                    </div>
                    <div className="flex items-center gap-1.5"><Cpu className="size-3.5 shrink-0 text-blue-500" /><span>{t('totalTime')} {formatDurationCompact(log.use_time)}</span></div>
                    {visibility.tps ? <div className="flex items-center gap-1.5"><Gauge className="size-3.5 shrink-0 text-lime-500" /><span>{t('tps')} {formatTPS(log.output_tokens, log.use_time)}</span></div> : null}
                    {visibility.cacheHitRate && cacheReadTokens > 0 ? <div className="flex items-center gap-1.5"><Percent className="size-3.5 shrink-0 text-teal-500" /><span>{t('cacheHitRate')} {formatCacheHitRate(cacheReadTokens, totalInputTokens)}</span></div> : null}
                    <div className="flex items-center gap-1.5"><ArrowDownToLine className="size-3.5 shrink-0 text-green-500" /><span>{t(cacheReadTokens > 0 || cacheWriteTokens > 0 ? 'realInput' : 'input')} {tokenUsage.nonCachedInputTokens.toLocaleString()}</span></div>
                    {displayFields.semanticCacheHit ? <div className="flex items-center gap-1.5"><ArrowDownToLine className="size-3.5 shrink-0 text-cyan-500" /><span>{t('semanticCacheHit')}</span></div> : null}
                    {cacheReadTokens > 0 ? <div className="flex items-center gap-1.5"><ArrowDownToLine className="size-3.5 shrink-0 text-teal-500" /><span>{t('cacheHit')} {formatCompactTokenCount(cacheReadTokens)}</span></div> : null}
                    {cacheWriteTokens > 0 ? <div className="flex items-center gap-1.5"><ArrowDownToLine className="size-3.5 shrink-0 text-sky-500" /><span>{t('cacheWrite')} {formatCompactTokenCount(cacheWriteTokens)}</span></div> : null}
                    <div className="flex items-center gap-1.5">
                        <ArrowUpFromLine className="size-3.5 shrink-0 text-purple-500" />
                        <span>{t('output')} {log.output_tokens.toLocaleString()}</span>
                    </div>
                    <div className="flex items-center gap-1.5"><Sigma className="size-3.5 shrink-0 text-rose-500" /><span className="font-medium text-rose-600 dark:text-rose-400">{t('totalTokens')} {totalTokens.toLocaleString()}</span></div>
                    {visibility.cost ? <div className="flex items-center gap-1.5">
                        <DollarSign className="size-3.5 shrink-0 text-emerald-500" />
                        <span className="font-medium text-emerald-600 dark:text-emerald-400">
                            {t('cost')} {Number(log.cost).toFixed(6)}
                        </span>
                    </div> : null}
                    {visibility.reasoningEffort && log.reasoning_effort ? <div className="flex items-center gap-1.5"><Brain className="size-3.5 shrink-0 text-violet-500" /><span>{t('reasoningEffort')} {log.reasoning_effort}</span></div> : null}
                    {visibility.reasoningTokens && (log.reasoning_tokens ?? 0) > 0 ? <div className="flex items-center gap-1.5"><Brain className="size-3.5 shrink-0 text-indigo-500" /><span>{t('reasoningTokens')} {formatCompactTokenCount(log.reasoning_tokens ?? 0)}t</span></div> : null}
                    {visibility.reasoningTokens && (log.reasoning_tokens ?? 0) <= 0 && (log.reasoning_chars ?? 0) > 0 ? <div className="flex items-center gap-1.5"><Type className="size-3.5 shrink-0 text-indigo-500" /><span>{t('reasoningChars')} {formatCompactTokenCount(log.reasoning_chars ?? 0)}{t('reasoningCharsUnit')}</span></div> : null}
                </div>
                {hasError ? (
                    <div className="p-2.5 rounded-xl bg-destructive/10 border border-destructive/20 overflow-hidden">
                        <p className="text-xs text-destructive line-clamp-2">{sanitizeErrorMessage(log.error)}</p>
                    </div>
                ) : null}
            </div>
        </div>
    );
}
