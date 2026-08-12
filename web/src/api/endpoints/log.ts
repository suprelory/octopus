import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient, API_BASE_URL } from '../client';
import { logger } from '@/lib/logger';
import { useEffect, useMemo, useRef, useState } from 'react';

/**
 * 尝试状态
 */
export type AttemptStatus = 'success' | 'failed' | 'circuit_break' | 'skipped';

export type RelayLogWSMode = 'fresh' | 'continuation' | 'replay';

export type RelayLogWSExecMode = 'passthrough' | 'transform';

export type RelayLogWSRecovery = 'reconnect' | 'replay' | 'downgrade';

/**
 * 单次渠道尝试信息
 */
export interface ChannelAttempt {
    channel_id: number;
    channel_key_id?: number;
    channel_name: string;
    model_name: string;
    adapter_type?: string;
    attempt_num: number;    // 第几次尝试
    status: AttemptStatus;
    duration: number;       // 耗时(毫秒)
    sticky?: boolean;
    msg?: string;
}

/**
 * 日志数据
 */
export interface LogSiteActionTarget {
    site_id: number;
    site_name: string;
    account_id: number;
    account_name: string;
    group_key: string;
    group_name: string;
    model_name: string;
    model_disabled: boolean;
    can_disable_model: boolean;
    channel_id: number;
    channel_name: string;
}

export interface LogSiteActionTargets {
    attempt_targets: Array<LogSiteActionTarget | null>;
    legacy_error_target?: LogSiteActionTarget | null;
}

export interface RelayLog {
    id: number;
    time: number;                // 时间戳
    request_model_name: string;  // 请求模型名称
    request_api_key_id?: number;
    request_api_key_name?: string; // 请求使用的 API Key 名称
    client_ip?: string;
    endpoint_type?: string;
    channel: number;             // 实际使用的渠道ID
    channel_name: string;        // 渠道名称
    actual_model_name: string;   // 实际使用模型名称
    input_tokens: number;        // 输入Token
    transport_input_tokens?: number | null; // 实际发送到上游请求体的 Token 估算
    bill_input_tokens?: number | null; // 按常规输入价格计费的 Token
    cache_read_tokens?: number | null; // 从缓存读取的 Token
    cache_write_tokens?: number | null; // 写入缓存的 Token
    output_tokens: number;       // 输出Token
    semantic_cache_hit?: boolean;
    reasoning_effort?: string;
    reasoning_tokens?: number;
    reasoning_chars?: number;
    is_test?: boolean;
    ftut: number;                // 首字时间(毫秒)
    use_time: number;            // 总用时(毫秒)
    cost: number;                // 消耗费用
    request_content?: string;    // 列表接口省略，详情接口返回
    response_content?: string;   // 列表接口省略，详情接口返回
    error: string;               // 错误信息
    attempts?: ChannelAttempt[]; // 所有尝试记录
    total_attempts?: number;     // 总尝试次数
    used_ws?: boolean;           // 是否使用了上游WebSocket
    ws_mode?: RelayLogWSMode | null; // 上游 WebSocket 会话模式
    ws_exec_mode?: RelayLogWSExecMode | null; // 上游 WebSocket 事件处理方式
    ws_recovery?: RelayLogWSRecovery | null; // 本次请求触发的恢复动作
}

/** 完整日志详情；列表接口通常省略两个大字段。 */
export interface RelayLogDetail extends RelayLog {
    request_content: string;
    response_content: string;
}

export type LogStatusFilter = 'all' | 'success' | 'error';

/**
 * 日志列表查询参数
 */
export type LogKeywordScope = 'default' | 'content';
export type LogKeywordMode = 'default' | 'prefix' | 'exact' | 'contains';
export type LogPaginationMode = 'cursor' | 'page';

export interface LogCursor {
    time: number;
    id: number;
}

export interface LogListParams {
    page?: number;
    page_size?: number;
    limit?: number;
    before_time?: number;
    before_id?: number;
    start_time?: number;
    end_time?: number;
    channel_ids?: number[];
    status?: LogStatusFilter;
    keyword?: string;
    keyword_scope?: LogKeywordScope;
    keyword_mode?: LogKeywordMode;
    pagination?: LogPaginationMode;
    include_content?: boolean;
    with_total?: boolean;
    enabled?: boolean;
}

/** 列表筛选条件（不含分页参数），用于构建查询键与查询串。 */
export type LogFilterParams = Omit<LogListParams, 'page' | 'page_size'>;

const logFiltersKey = (filters?: LogFilterParams) => ({
    start_time: filters?.start_time ?? null,
    end_time: filters?.end_time ?? null,
    channel_ids: filters?.channel_ids?.filter((id) => id > 0).sort((a, b) => a - b) ?? [],
    status: filters?.status && filters.status !== 'all' ? filters.status : 'all',
    keyword: filters?.keyword?.trim() ?? '',
    keyword_scope: filters?.keyword_scope ?? 'default',
    keyword_mode: filters?.keyword_mode ?? 'default',
});

function appendLogListParams(params: URLSearchParams, filters?: LogFilterParams) {
    if (filters?.start_time) params.set('start_time', String(filters.start_time));
    if (filters?.end_time) params.set('end_time', String(filters.end_time));
    const channelIds = filters?.channel_ids?.filter((id) => id > 0) ?? [];
    if (channelIds.length > 0) params.set('channel_ids', channelIds.join(','));
    if (filters?.status && filters.status !== 'all') params.set('status', filters.status);
    const keyword = filters?.keyword?.trim();
    if (keyword) params.set('keyword', keyword);
    if (filters?.keyword_scope && filters.keyword_scope !== 'default') params.set('keyword_scope', filters.keyword_scope);
    if (filters?.keyword_mode && filters.keyword_mode !== 'default') params.set('keyword_mode', filters.keyword_mode);
}

export interface LogPageResponse {
    logs: RelayLog[];
    total: number;
    /** false 表示 total 是下界（后端做了有界计数），UI 应显示 "N+"。 */
    total_exact: boolean;
    has_more?: boolean;
    next_cursor?: LogCursor | null;
    search_mode?: string;
    warning?: string;
}

export const logPageQueryKey = (pageSize: number, page: number, filters?: LogFilterParams) =>
    ['logs', 'page', pageSize, page, logFiltersKey(filters)] as const;

export function useLogPage(params: LogListParams) {
    const page = params.page ?? 1;
    const pageSize = params.page_size ?? 20;

    return useQuery({
        queryKey: logPageQueryKey(pageSize, page, params),
        queryFn: async (): Promise<LogPageResponse> => {
            const search = new URLSearchParams();
            // 后端 page 模式只回 total，不回 has_more/next_cursor，页数必须由 total 推导。
            search.set('pagination', 'page');
            search.set('page', String(page));
            search.set('page_size', String(pageSize));
            search.set('include_content', String(params.include_content ?? false));
            search.set('with_total', String(params.with_total ?? true));
            appendLogListParams(search, params);
            const result = await apiClient.get<{ logs: RelayLog[] | null; total: number; total_exact?: boolean; has_more?: boolean; next_cursor?: LogCursor | null; warning?: string; search_mode?: string } | null>(
                `/api/v1/log/list?${search.toString()}`,
            );
            return {
                logs: result?.logs ?? [],
                total: result?.total ?? 0,
                // 老后端不回该字段，缺省按精确值处理。
                total_exact: result?.total_exact ?? true,
                has_more: result?.has_more ?? false,
                next_cursor: result?.next_cursor ?? null,
                warning: result?.warning,
                search_mode: result?.search_mode,
            };
        },
        placeholderData: keepPreviousData,
        staleTime: 0,
        refetchOnMount: 'always',
        refetchOnWindowFocus: false,
        enabled: params.enabled ?? true,
    });
}

/**
 * 订阅日志 SSE 推送。
 * 回调用 ref 持有，避免调用方每次渲染重建回调时都断开重连。
 */
export function useLogStream(onLog: (log: RelayLog) => void, enabled: boolean) {
    const [isConnected, setIsConnected] = useState(false);
    const onLogRef = useRef(onLog);

    useEffect(() => {
        onLogRef.current = onLog;
    }, [onLog]);

    useEffect(() => {
        // 关闭时无需复位 isConnected：返回值已与 enabled 相与，
        // 且上一次的 cleanup 会在 enabled 翻转时把状态清掉。
        if (!enabled) return;

        let cancelled = false;
        let eventSource: EventSource | null = null;
        let retryTimer: ReturnType<typeof setTimeout> | null = null;
        let retryAttempt = 0;

        const scheduleReconnect = () => {
            if (cancelled) return;
            const delay = Math.min(30000, 1000 * 2 ** retryAttempt);
            retryAttempt += 1;
            retryTimer = setTimeout(() => {
                retryTimer = null;
                void connect();
            }, delay);
        };

        const connect = async () => {
            try {
                const { token } = await apiClient.get<{ token: string }>('/api/v1/log/stream-token');
                if (cancelled) return;

                const source = new EventSource(`${API_BASE_URL}/api/v1/log/stream?token=${token}`);
                eventSource = source;

                source.onopen = () => {
                    retryAttempt = 0;
                    setIsConnected(true);
                };

                source.onmessage = (event) => {
                    try {
                        onLogRef.current(JSON.parse(event.data) as RelayLog);
                    } catch (e) {
                        logger.error('解析日志数据失败:', e);
                    }
                };

                source.onerror = () => {
                    setIsConnected(false);
                    source.close();
                    if (eventSource === source) eventSource = null;
                    scheduleReconnect();
                };
            } catch (e) {
                if (cancelled) return;
                logger.error('获取 stream token 失败:', e);
                scheduleReconnect();
            }
        };

        void connect();

        return () => {
            cancelled = true;
            if (retryTimer) clearTimeout(retryTimer);
            eventSource?.close();
            eventSource = null;
            setIsConnected(false);
        };
    }, [enabled]);

    return isConnected && enabled;
}

/**
 * 清空日志 Hook
 * 
 * @example
 * const clearLogs = useClearLogs();
 * 
 * clearLogs.mutate();
 */
export async function getLogDetail(id: number): Promise<RelayLogDetail> {
    return apiClient.get<RelayLogDetail>(`/api/v1/log/${id}`);
}

export function useLogSiteActionTargets(ids: number[], enabled = true) {
    const stableIds = useMemo(() => Array.from(new Set(ids.filter((id) => id > 0))).sort((a, b) => a - b), [ids]);
    return useQuery({
        queryKey: ['logs', 'site-action-targets', stableIds],
        queryFn: async () => {
            if (stableIds.length === 0) return {} as Record<number, LogSiteActionTargets>;
            const chunkSize = 100;
            const chunks: number[][] = [];
            for (let i = 0; i < stableIds.length; i += chunkSize) {
                chunks.push(stableIds.slice(i, i + chunkSize));
            }
            const results = await Promise.all(
                chunks.map((chunk) =>
                    apiClient.get<Record<number, LogSiteActionTargets>>(
                        `/api/v1/log/site-action-targets?ids=${chunk.join(',')}`,
                    ),
                ),
            );
            return Object.assign({}, ...results) as Record<number, LogSiteActionTargets>;
        },
        enabled: enabled && stableIds.length > 0,
        staleTime: 30000,
        refetchOnWindowFocus: false,
    });
}

export function useClearLogs() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async () => {
            return apiClient.delete<null>('/api/v1/log/clear');
        },
        onSuccess: () => {
            logger.log('日志清空成功');
            queryClient.invalidateQueries({ queryKey: ['logs'] });
        },
        onError: (error) => {
            logger.error('日志清空失败:', error);
        },
    });
}