import type { ChannelAttempt, RelayLog, RelayLogDetail } from '@/api/endpoints/log';

function firstNonEmpty(...values: Array<string | null | undefined>) {
    for (const value of values) {
        const trimmed = value?.trim();
        if (trimmed) return trimmed;
    }
    return '';
}

function firstNonZero(...values: Array<number | null | undefined>) {
    for (const value of values) {
        if (typeof value === 'number' && value > 0) return value;
    }
    return 0;
}

function lastAttemptValue(
    attempts: ChannelAttempt[] | undefined,
    pick: (attempt: ChannelAttempt) => string | undefined,
) {
    if (!attempts?.length) return '';
    for (let index = attempts.length - 1; index >= 0; index -= 1) {
        const value = pick(attempts[index])?.trim();
        if (value) return value;
    }
    return '';
}

function lastAttemptChannelId(attempts: ChannelAttempt[] | undefined) {
    if (!attempts?.length) return 0;
    for (let index = attempts.length - 1; index >= 0; index -= 1) {
        const channelID = attempts[index]?.channel_id;
        if (typeof channelID === 'number' && channelID > 0) return channelID;
    }
    return 0;
}

function isStreamRequest(content?: string | null) {
    if (!content) return false;
    try {
        return (JSON.parse(content) as { stream?: unknown }).stream === true;
    } catch {
        return false;
    }
}

function normalizeEndpointType(value: string) {
    const normalized = value.trim().toLowerCase();
    if (normalized === 'response') return 'responses';
    if (normalized === 'anthropic') return 'messages';
    if (normalized === 'embedding' || normalized === 'openai_embedding') return 'embeddings';
    if (normalized === 'image') return 'images';
    return normalized;
}

function inferEndpointType(modelNames: string[]) {
    const names = modelNames.map((name) => name.trim().toLowerCase()).filter(Boolean);
    if (names.some((name) => /(^|[-_/])(?:text-|bge|gte|e5).*embedding|embedding/.test(name))) return 'embeddings';
    if (names.some((name) => name.includes('rerank') || name.includes('re-rank'))) return 'rerank';
    if (names.some((name) => name.includes('moderation'))) return 'moderations';
    if (names.some((name) => name.includes('claude'))) return 'messages';
    if (names.some((name) => name.includes('gemini') || name.includes('gemma'))) return 'gemini';
    if (names.some((name) => name.includes('doubao') || name.includes('volcengine') || name.includes('ark-'))) return 'volcengine';
    if (names.some((name) => name.includes('deepseek'))) return 'deepseek';
    if (names.some((name) => name.includes('mimo'))) return 'mimo';
    return names.length > 0 ? 'chat' : '';
}

function resolveOutboundAdapterType(attempts: ChannelAttempt[] | undefined) {
    if (!attempts?.length) return '';
    // A successful attempt is the protocol that actually served the request.
    for (let index = attempts.length - 1; index >= 0; index -= 1) {
        const attempt = attempts[index];
        if (attempt.status === 'success' && attempt.adapter_type?.trim()) return attempt.adapter_type.trim();
    }
    return lastAttemptValue(attempts, (attempt) => attempt.adapter_type);
}

function inferRequestTypeKey(endpointType: string, modelNames: string[], requestContent?: string | null) {
    const endpoint = normalizeEndpointType(endpointType);
    const names = modelNames.map((name) => name.toLowerCase());
    const streaming = isStreamRequest(requestContent);

    if (endpoint === 'embeddings') return 'embedding';
    if (endpoint === 'images') return 'images';
    if (endpoint === 'rerank') return 'rerank';
    if (endpoint === 'moderations') return 'moderations';
    if (endpoint === 'responses') return 'responses';
    if (endpoint === 'messages') return 'anthropicMessages';
    if (endpoint === 'gemini') return 'gemini';
    if (endpoint === 'volcengine') return 'volcengine';
    if (endpoint === 'mimo' || names.some((name) => name.includes('mimo'))) return 'mimoChat';
    if (names.some((name) => name.includes('claude'))) return 'anthropicMessages';
    if (names.some((name) => name.includes('gemini'))) return 'gemini';
    if (names.some((name) => name.includes('doubao') || name.includes('volcengine'))) return 'volcengine';
    return streaming ? 'streamingChat' : 'chat';
}

export function resolveLogDisplayFields(
    log: RelayLog,
    detail?: RelayLogDetail | null,
    channelNameById?: ReadonlyMap<number, string>,
) {
    const attempts = detail?.attempts?.length ? detail.attempts : log.attempts;
    const requestModelName = firstNonEmpty(detail?.request_model_name, log.request_model_name);
    const actualModelName = firstNonEmpty(
        detail?.actual_model_name,
        log.actual_model_name,
        lastAttemptValue(attempts, (attempt) => attempt.model_name),
        requestModelName,
    );
    const endpointType = normalizeEndpointType(firstNonEmpty(
        detail?.endpoint_type,
        log.endpoint_type,
        inferEndpointType([actualModelName, requestModelName, ...(attempts ?? []).map((attempt) => attempt.model_name)]),
    ));
    const channelId = firstNonZero(detail?.channel, log.channel, lastAttemptChannelId(attempts));
    const channelName = firstNonEmpty(
        detail?.channel_name,
        log.channel_name,
        lastAttemptValue(attempts, (attempt) => attempt.channel_name),
        channelId > 0 ? channelNameById?.get(channelId) : '',
        channelId > 0 ? `Channel #${channelId}` : '',
    );
    const requestContent = firstNonEmpty(detail?.request_content, '');
    const requestTypeKey = inferRequestTypeKey(endpointType, [requestModelName, actualModelName], requestContent);
    const outboundAdapterType = resolveOutboundAdapterType(attempts);

    return {
        requestAPIKeyName: firstNonEmpty(detail?.request_api_key_name, log.request_api_key_name),
        clientIP: firstNonEmpty(detail?.client_ip, log.client_ip),
        requestModelName,
        actualModelName,
        endpointType,
        adapterType: outboundAdapterType,
        outboundAdapterType,
        requestType: requestTypeKey,
        requestTypeKey,
        channelId,
        channelName,
        inputTokens: detail?.input_tokens ?? log.input_tokens ?? 0,
        outputTokens: detail?.output_tokens ?? log.output_tokens ?? 0,
        billInputTokens: detail?.bill_input_tokens ?? log.bill_input_tokens ?? null,
        cacheReadTokens: detail?.cache_read_tokens ?? log.cache_read_tokens ?? 0,
        cacheWriteTokens: detail?.cache_write_tokens ?? log.cache_write_tokens ?? 0,
        semanticCacheHit: detail?.semantic_cache_hit ?? log.semantic_cache_hit ?? false,
    };
}

export function formatJsonForCopy(content: string | undefined | null) {
    if (!content) return '';
    try {
        return JSON.stringify(JSON.parse(content), null, 2);
    } catch {
        return content;
    }
}
