'use client';

import type { RelayLog, RelayLogWSMode, RelayLogWSExecMode, RelayLogWSRecovery, AttemptStatus, ChannelAttempt } from '@/api/endpoints/log';
import { Badge } from '@/components/ui/badge';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import { cn } from '@/lib/utils';
import { ArrowDown, Link, RotateCw } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useMemo } from 'react';
import { formatAttemptAdapterLabel, formatDuration, mergeAdjacentAttempts } from './log-format';

interface RetryBadgeWithTooltipProps {
    channelName: string;
    brandColor: string;
    attempts: ChannelAttempt[];
    channelNameById?: ReadonlyMap<number, string>;
}

function getWSBadgeMeta(mode: RelayLogWSMode | null | undefined, usedWS: boolean | undefined, t: ReturnType<typeof useTranslations<'log.card'>>) {
    if (!usedWS && !mode) return null;

    switch (mode) {
        case 'continuation':
            return {
                label: t('wsContinuation'),
                className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
                description: t('wsContinuationHint'),
            };
        case 'replay':
            return {
                label: t('wsReplay'),
                className: 'bg-amber-500/10 text-amber-700 dark:text-amber-300',
                description: t('wsReplayHint'),
            };
        case 'fresh':
        default:
            return {
                label: t('ws'),
                className: 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400',
                description: t('wsFreshHint'),
            };
    }
}

function getWSExecBadgeMeta(mode: RelayLogWSExecMode | null | undefined, t: ReturnType<typeof useTranslations<'log.card'>>) {
    switch (mode) {
        case 'passthrough':
            return {
                label: t('wsPassthrough'),
                className: 'bg-violet-500/10 text-violet-700 dark:text-violet-300',
                description: t('wsPassthroughHint'),
            };
        case 'transform':
            return {
                label: t('wsTransform'),
                className: 'bg-indigo-500/10 text-indigo-700 dark:text-indigo-300',
                description: t('wsTransformHint'),
            };
        default:
            return null;
    }
}

function getWSRecoveryBadgeMeta(recovery: RelayLogWSRecovery | null | undefined, t: ReturnType<typeof useTranslations<'log.card'>>) {
    switch (recovery) {
        case 'reconnect':
            return {
                label: t('wsReconnect'),
                className: 'bg-sky-500/10 text-sky-700 dark:text-sky-300',
                description: t('wsReconnectHint'),
            };
        case 'replay':
            return {
                label: t('wsReplayRecovery'),
                className: 'bg-amber-500/10 text-amber-700 dark:text-amber-300',
                description: t('wsReplayRecoveryHint'),
            };
        case 'downgrade':
            return {
                label: t('wsDowngrade'),
                className: 'bg-slate-500/10 text-slate-700 dark:text-slate-300',
                description: t('wsDowngradeHint'),
            };
        default:
            return null;
    }
}

export function getAttemptStatusMeta(status: AttemptStatus, t: ReturnType<typeof useTranslations<'log.card'>>) {
    switch (status) {
        case 'success':
            return {
                label: t('success'),
                badgeClassName: 'bg-primary/15 text-primary',
                containerClassName: 'bg-primary/5 border-primary/20 hover:bg-primary/10',
                messageClassName: 'text-primary/90 border-primary/30',
            };
        case 'skipped':
            return {
                label: t('skipped'),
                badgeClassName: 'bg-muted text-muted-foreground',
                containerClassName: 'bg-muted/40 border-border/60 hover:bg-muted/60',
                messageClassName: 'text-muted-foreground border-border/50',
            };
        case 'circuit_break':
            return {
                label: t('circuitBreak'),
                badgeClassName: 'bg-amber-500/15 text-amber-700 dark:text-amber-300',
                containerClassName: 'bg-amber-500/5 border-amber-500/20 hover:bg-amber-500/10',
                messageClassName: 'text-amber-700 dark:text-amber-300 border-amber-500/30',
            };
        case 'failed':
        default:
            return {
                label: t('failed'),
                badgeClassName: 'bg-destructive/15 text-destructive',
                containerClassName: 'bg-destructive/5 border-destructive/20 hover:bg-destructive/10',
                messageClassName: 'text-destructive/90 border-destructive/30',
            };
    }
}

export function RetryBadgeWithTooltip({ channelName, brandColor, attempts, channelNameById }: RetryBadgeWithTooltipProps) {
    const t = useTranslations('log.card');
    const merged = useMemo(() => mergeAdjacentAttempts(attempts), [attempts]);

    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Badge
                    variant="secondary"
                    className="shrink-0 text-xs px-1.5 py-0 cursor-help"
                    style={{ backgroundColor: `${brandColor}15`, color: brandColor }}
                >
                    <RotateCw className="size-3 mr-1 opacity-80" />
                    {channelName}
                </Badge>
            </TooltipTrigger>
            <TooltipContent className="border bg-card p-2 min-w-[280px] shadow-sm rounded-3xl flex flex-col gap-1">
                {merged.map((attempt, idx) => {
                    const statusMeta = getAttemptStatusMeta(attempt.status, t);

                    return (
                        <div key={idx} className="flex flex-col w-full">
                            <div className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted/50 transition-colors">
                                <Badge
                                    className={cn(
                                        'h-5 shrink-0 px-1.5 text-[10px] font-bold uppercase shadow-none border-0',
                                        statusMeta.badgeClassName,
                                    )}
                                >
                                    {statusMeta.label}
                                </Badge>
                                <div className="flex min-w-0 flex-col flex-1">
                                    <span className="truncate text-xs font-semibold text-foreground">
                                        {attempt.channel_name?.trim() || channelNameById?.get(attempt.channel_id) || `Channel #${attempt.channel_id}`}
                                    </span>
                                    <span className="text-[10px] text-muted-foreground">
                                        {attempt.model_name}{attempt.adapter_type ? ` • ${formatAttemptAdapterLabel(t, attempt.adapter_type)}` : ''} • {formatDuration(attempt.totalDuration)}
                                    </span>
                                </div>
                                {attempt.repeat > 1 ? (
                                    <Badge variant="outline" className="shrink-0 h-5 px-1.5 text-[10px] font-semibold tabular-nums">
                                        ×{attempt.repeat}
                                    </Badge>
                                ) : null}
                            </div>
                            {idx < merged.length - 1 ? (
                                <div className="flex justify-center py-0.5">
                                    <ArrowDown className="size-3 text-muted-foreground/30" />
                                </div>
                            ) : null}
                        </div>
                    );
                })}
            </TooltipContent>
        </Tooltip>
    );
}

export function WSModeBadge({ log }: { log: RelayLog }) {
    const t = useTranslations('log.card');
    const modeMeta = getWSBadgeMeta(log.ws_mode, log.used_ws, t);
    const execMeta = getWSExecBadgeMeta(log.ws_exec_mode, t);
    const recoveryMeta = getWSRecoveryBadgeMeta(log.ws_recovery, t);

    if (!modeMeta && !execMeta && !recoveryMeta) return null;

    return (
        <div className="flex items-center gap-1.5 shrink-0">
            {modeMeta ? (
                <Tooltip>
                    <TooltipTrigger asChild>
                        <Badge
                            variant="secondary"
                            className={cn('shrink-0 gap-1 px-1.5 py-0 text-xs', modeMeta.className)}
                        >
                            <Link className="size-3.5 shrink-0" />
                            {modeMeta.label}
                        </Badge>
                    </TooltipTrigger>
                    <TooltipContent>{modeMeta.description}</TooltipContent>
                </Tooltip>
            ) : null}
            {execMeta ? (
                <Tooltip>
                    <TooltipTrigger asChild>
                        <Badge
                            variant="secondary"
                            className={cn('shrink-0 gap-1 px-1.5 py-0 text-xs', execMeta.className)}
                        >
                            <Link className="size-3.5 shrink-0" />
                            {execMeta.label}
                        </Badge>
                    </TooltipTrigger>
                    <TooltipContent>{execMeta.description}</TooltipContent>
                </Tooltip>
            ) : null}
            {recoveryMeta ? (
                <Tooltip>
                    <TooltipTrigger asChild>
                        <Badge
                            variant="secondary"
                            className={cn('shrink-0 gap-1 px-1.5 py-0 text-xs', recoveryMeta.className)}
                        >
                            <RotateCw className="size-3.5 shrink-0" />
                            {recoveryMeta.label}
                        </Badge>
                    </TooltipTrigger>
                    <TooltipContent>{recoveryMeta.description}</TooltipContent>
                </Tooltip>
            ) : null}
        </div>
    );
}
