'use client';

import type { ChannelAttempt, LogSiteActionTarget as ApiLogSiteActionTarget, LogSiteActionTargets as ApiLogSiteActionTargets } from '@/api/endpoints/log';
import type { useTranslations } from 'next-intl';

export type LogSiteActionTarget = ApiLogSiteActionTarget;
export type LogSiteActionTargets = ApiLogSiteActionTargets;

export function formatEndpointLabel(t: ReturnType<typeof useTranslations<'log.card'>>, value: string) {
    const key = value.trim().toLowerCase();
    const labels: Record<string, string> = {
        chat: 'adapterLabels.chat',
        response: 'adapterLabels.response',
        responses: 'adapterLabels.response',
        anthropic: 'adapterLabels.anthropic',
        messages: 'adapterLabels.anthropic',
        gemini: 'adapterLabels.gemini',
        unsupported: 'adapterLabels.unsupported',
        volcengine: 'adapterLabels.unsupported',
        ark: 'adapterLabels.unsupported',
        embedding: 'adapterLabels.embedding',
        embeddings: 'adapterLabels.embedding',
        images: 'adapterLabels.images',
        mimo: 'adapterLabels.mimo',
        cloudflare: 'adapterLabels.cloudflare',
        passthrough: 'adapterLabels.passthrough',
        codex: 'adapterLabels.codex',
    };
    const labelKey = labels[key];
    if (!labelKey) return value;
    const translated = t(labelKey as never);
    return translated.includes('adapterLabels.') ? value : translated;
}

export function formatAttemptAdapterLabel(t: ReturnType<typeof useTranslations<'log.card'>>, value: string | undefined) {
    if (!value?.trim()) return '';
    return formatEndpointLabel(t, value);
}

export function formatRequestTypeLabel(t: ReturnType<typeof useTranslations<'log.card'>>, value: string) {
    if (!value) return '';
    const translated = t(`requestTypeLabels.${value}` as never);
    return translated.includes('requestTypeLabels.') ? value : translated;
}

export function formatTime(timestamp: number): string {
    const date = new Date(timestamp * 1000);
    return date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    });
}

export function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
}

export function formatDurationCompact(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    const s = ms / 1000;
    if (s < 10) return `${s.toFixed(2)}s`;
    if (s < 100) return `${s.toFixed(1)}s`;
    return `${Math.round(s)}s`;
}

export function formatTPS(tokens: number, timeMs: number): string {
    if (tokens <= 0 || timeMs <= 0) return '- tk/s';
    const tps = tokens / (timeMs / 1000);
    if (tps >= 100) return `${tps.toFixed(0)} tk/s`;
    if (tps >= 10) return `${tps.toFixed(1)} tk/s`;
    return `${tps.toFixed(2)} tk/s`;
}

export function formatCacheHitRate(cacheRead: number, totalInput: number): string {
    if (cacheRead <= 0 || totalInput <= 0) return '-';
    const rate = (cacheRead / totalInput) * 100;
    return rate >= 10 ? `${rate.toFixed(1)}%` : `${rate.toFixed(2)}%`;
}

export function sanitizeErrorMessage(raw: string | undefined | null): string {
    if (!raw) return '';
    let text = raw.replace(/^upstream error:\s*(\d+):\s*/i, (_m, code) => `[HTTP ${code}] `);
    if (/<\/?(html|body|head|title|div|p|h[1-6]|br|script|style)[\s>]/i.test(text)) {
        const titleMatch = text.match(/<title[^>]*>([\s\S]*?)<\/title>/i);
        const h1Match = text.match(/<h1[^>]*>([\s\S]*?)<\/h1>/i);
        const summarySource = titleMatch?.[1] || h1Match?.[1] || '';
        const summary = summarySource
            ? summarySource.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim()
            : '(HTML response)';
        const stripped = text
            .replace(/<script[\s\S]*?<\/script>/gi, ' ')
            .replace(/<style[\s\S]*?<\/style>/gi, ' ')
            .replace(/<[^>]+>/g, ' ')
            .replace(/&nbsp;/gi, ' ')
            .replace(/&amp;/gi, '&')
            .replace(/&lt;/gi, '<')
            .replace(/&gt;/gi, '>')
            .replace(/&quot;/gi, '"')
            .replace(/\s+/g, ' ')
            .trim();
        const detail = stripped.length > 500 ? `${stripped.slice(0, 500)}…` : stripped;
        text = summary && detail && detail !== summary ? `${summary} — ${detail}` : (summary || detail || '(HTML response)');
    }
    return text;
}

export interface MergedAttempt extends ChannelAttempt {
    originalIndex: number;
    repeat: number;
    lastAttemptNum: number;
    totalDuration: number;
}

export function mergeAdjacentAttempts(attempts: ChannelAttempt[]): MergedAttempt[] {
    const out: MergedAttempt[] = [];
    for (const [originalIndex, a] of attempts.entries()) {
        const last = out[out.length - 1];
        if (
            last
            && last.channel_id === a.channel_id
            && last.channel_key_id === a.channel_key_id
            && last.model_name === a.model_name
            && last.status === a.status
            && (last.msg ?? '') === (a.msg ?? '')
        ) {
            last.repeat += 1;
            last.lastAttemptNum = a.attempt_num;
            last.totalDuration += a.duration;
            continue;
        }
        out.push({
            ...a,
            originalIndex,
            repeat: 1,
            lastAttemptNum: a.attempt_num,
            totalDuration: a.duration,
        });
    }
    return out;
}

export function makeDisableTargetKey(target: LogSiteActionTarget | null | undefined) {
    if (!target) return '';
    return `${target.site_id}\u0000${target.account_id}\u0000${target.group_key}\u0000${target.model_name}`;
}

export function formatCompactTokenCount(value: number): string {
    if (value < 1000) return value.toLocaleString();
    // 截断到指定小数位（非四舍五入）：先放大取 floor 再缩回，1e-9 修正浮点下溢。
    const trunc = (n: number, decimals: number) => {
        const factor = 10 ** decimals;
        return (Math.floor(n * factor + 1e-9) / factor).toFixed(decimals);
    };
    if (value < 10000) return `${trunc(value / 1000, 2)}K`;
    if (value < 1000000) return `${trunc(value / 1000, 1)}K`;
    return `${trunc(value / 1000000, 2)}M`;
}

// 投影渠道命名 "站点/账号/分组-端点后缀"，Anthropic 端点后缀为 -Anthropic。
// 仅 Anthropic 端点的 input_tokens 不含 cache_read（Anthropic 原生语义），不应做减法；
// OpenAI/Gemini 等的 input_tokens 已含 cache_read。见 SiteModelRouteType 后缀映射。
export function usesAnthropicCacheSemantics(adapterType: string, channelName: string): boolean {
    if (adapterType.trim().toLowerCase() === 'anthropic') return true;
    return /-Anthropic$/i.test(channelName);
}

export interface TokenUsageDisplay {
    nonCachedInputTokens: number;
    cacheReadTokens: number;
    cacheWriteTokens: number;
    totalInputTokens: number;
    totalTokens: number;
}

export function resolveTokenUsageDisplay({
    inputTokens,
    outputTokens,
    billInputTokens,
    cacheReadTokens,
    cacheWriteTokens,
    adapterType,
    channelName,
}: {
    inputTokens: number;
    outputTokens: number;
    billInputTokens: number | null;
    cacheReadTokens: number;
    cacheWriteTokens: number;
    adapterType: string;
    channelName: string;
}): TokenUsageDisplay {
    const safeInput = Math.max(0, inputTokens);
    const safeOutput = Math.max(0, outputTokens);
    const safeCacheRead = Math.max(0, cacheReadTokens);
    const safeCacheWrite = Math.max(0, cacheWriteTokens);

    // 新日志直接使用后端统一后的非缓存输入口径。旧日志没有 bill_input_tokens 时，
    // Anthropic 的 input 本身不含缓存；OpenAI/Gemini 的 input 通常包含 cache read。
    const legacyInputExcludesCache = usesAnthropicCacheSemantics(adapterType, channelName)
        || safeCacheWrite > 0
        || safeInput < safeCacheRead;
    const nonCachedInput = billInputTokens != null
        ? Math.max(0, billInputTokens)
        : legacyInputExcludesCache
            ? safeInput
            : Math.max(0, safeInput - safeCacheRead);
    const totalInput = nonCachedInput + safeCacheRead + safeCacheWrite;

    return {
        nonCachedInputTokens: nonCachedInput,
        cacheReadTokens: safeCacheRead,
        cacheWriteTokens: safeCacheWrite,
        totalInputTokens: totalInput,
        totalTokens: totalInput + safeOutput,
    };
}
