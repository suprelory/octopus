import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiRequest } from './client';
import type { ProxyMode } from './proxy-pool';

export type Match = { path: string; value: string | number | boolean };
export type CheckinRequest = {
    method: 'GET' | 'POST';
    path: string;
    body: string;
    headers: { name: string; value: string }[];
};
export type SiteConfig = {
    name: string;
    base_url: string;
    adapter: string;
    enabled: boolean;
    timezone: string;
    window_start: string;
    window_end: string;
    jitter_minutes: number;
    proxy_url: string;
    proxy_mode?: ProxyMode;
    proxy_config_id?: number | null;
    manual_url: string;
    user_agent?: string;
    submit: CheckinRequest;
    success?: Match;
    already?: Match;
    status?: { request: CheckinRequest; checked: Match; unchecked: Match; date_path: string };
    status_mode?: 'disabled';
    reward: { source: 'submit' | 'status'; path: string; unit: string; multiplier: string };
};
export type CheckinSite = {
    id: string;
    config: SiteConfig;
    revision: number;
    support: string;
    support_revision: number;
    verified_at?: string;
};
export type CredentialType = 'cookie' | 'bearer' | 'password';
export type CheckinBalance = {
    amount_micros?: string;
    used_micros?: string;
    amount_state: 'known' | 'unknown' | 'invalid';
    used_state: 'known' | 'unknown' | 'invalid';
    unit: string;
    updated_at: string;
};
export type CheckinInspection = {
    id: string;
    kind: 'balance' | 'detect';
    status: 'running' | 'success' | 'partial' | 'failed' | 'unsupported' | 'interrupted';
    reason: string;
    authentication: 'valid' | 'invalid' | 'unknown';
    user_id?: string;
    balance_updated: boolean;
    checkin_support: 'not_checked' | 'unknown' | 'supported' | 'unsupported' | 'disabled';
    checkin_status: 'unknown' | 'checked_in' | 'not_checked_in';
    checkin_date: string;
    timezone: string;
    started_at: string;
    finished_at?: string;
    steps: Omit<CheckinAttempt, 'id' | 'message'>[];
};
export type CheckinAccount = {
    id: string;
    site_id: string;
    name: string;
    enabled: boolean;
    auto_checkin: boolean;
    proxy_mode: ProxyMode;
    proxy_config_id?: number | null;
    proxy_url: string;
    credential_type: CredentialType;
    credential_version: number;
    has_refresh_token: boolean;
    token_expires_at?: string;
    token_refreshed_at?: string;
    refresh_pending?: boolean;
    refresh_error?: string;
    refresh_retry_at?: string;
    has_session?: boolean;
    session_saved_at?: string;
    balance?: CheckinBalance;
    inspection?: CheckinInspection;
    revision: number;
    next_run_at?: string;
    last_run_id: string;
    last_status: string;
    last_success_date: string;
    failure_count: number;
    user_id?: string;
};
export type AccountWrite = Partial<
    Pick<
        CheckinAccount,
        'name' | 'enabled' | 'auto_checkin' | 'proxy_mode' | 'proxy_url' | 'proxy_config_id'
    >
> & {
    revision: number;
    credential_version: number;
    credentials?: {
        type: CredentialType;
        cookie?: string;
        access_token?: string;
        refresh_token?: string;
        expires_at?: string;
        user_id: string;
        username?: string;
        password?: string;
    };
};
export type CheckinRun = {
    id: string;
    site_id: string;
    account_id: string;
    site_name: string;
    account_name: string;
    checkin_date: string;
    timezone: string;
    phase: string;
    status: string;
    reason: string;
    source: string;
    submit_intent_at?: string;
    next_confirm_at?: string;
    confirm_deadline?: string;
    reward_micros?: string;
    reward_unit: string;
    reward_state: string;
    created_at: string;
    updated_at: string;
};
export type CheckinAttempt = {
    id: string;
    stage: string;
    http_status: number;
    reason: string;
    message: string;
    duration_ms: number;
    created_at: string;
};
export type Scope = { kind: 'all' | 'sites' | 'accounts'; ids: string[]; only_auto?: boolean };
export type CheckinJob = {
    id: string;
    scope: Scope;
    source: string;
    status: string;
    total: number;
    completed: number;
    success: number;
    already: number;
    failed: number;
    pending: number;
    skipped: number;
    created_at: string;
};
export type JobItem = {
    id: string;
    site_id: string;
    account_id: string;
    site_name?: string;
    account_name?: string;
    checkin_date: string;
    state: string;
    outcome: string;
    run_id: string;
};
export type CheckinStats = {
    total: number;
    success: number;
    already: number;
    failed: number;
    pending: number;
    today_completed: number;
    unknown_reward: number;
    invalid_reward: number;
    rewards: {
        key: string;
        unit: string;
        site_name?: string;
        today: string;
        days_7: string;
        days_30: string;
        total: string;
    }[];
};
export type CheckinSettings = {
    concurrency: number;
    site_interval_seconds: number;
    http_timeout_seconds: number;
    confirm_attempts: number;
    confirm_interval_seconds: number;
    confirm_timeout_minutes: number;
};
export type Capability = {
    id: string;
    name: string;
    path: string;
    credential_types: CredentialType[];
    execution: 'request' | 'login';
    status_query: 'native' | 'logs' | 'configured';
    custom_request: boolean;
    custom_status: boolean;
    token_refresh: boolean;
    account_detection: boolean;
    balance_query: boolean;
};
export type RunPage = { items: CheckinRun[]; next_before: string };
export const notificationKinds = [
    'webhook',
    'telegram',
    'bark',
    'serverchan',
    'smtp',
    'feishu',
    'dingtalk',
    'wecom',
    'ntfy',
] as const;
export type NotificationKind = (typeof notificationKinds)[number];
export const notificationEvents = [
    'success',
    'already_checked_in',
    'failed',
    'attention',
    'low_balance',
] as const;
export type NotificationEventKind = (typeof notificationEvents)[number];
export type NotificationConfig = {
    name: string;
    kind: NotificationKind;
    enabled: boolean;
    events: NotificationEventKind[];
    include_manual: boolean;
    scope: 'all' | 'sites';
    site_ids: string[];
    cooldown_seconds: number;
    threshold_micros?: string;
    threshold_unit: string;
    language: string;
    title_template: string;
    body_template: string;
    proxy_mode: ProxyMode;
    proxy_config_id?: number | null;
};
export type NotificationTarget = {
    id: string;
    config: NotificationConfig;
    has_connection: boolean;
    connection_version: number;
    revision: number;
};
export type NotificationConnection = {
    url?: string;
    token?: string;
    chat_id?: string;
    send_key?: string;
    secret?: string;
    base_url?: string;
    topic?: string;
    priority?: number;
    headers?: { name: string; value: string }[];
    payload?: string;
    smtp?: {
        host: string;
        port: number;
        tls: 'starttls' | 'tls' | 'none';
        username: string;
        password: string;
        from: string;
        to: string[];
    };
};
export type NotificationDelivery = {
    id: string;
    target_id: string;
    target_name: string;
    target_kind: NotificationKind;
    target_revision: number;
    event: {
        kind: NotificationEventKind | 'test';
        identity: string;
        site_name: string;
        account_name: string;
        checkin_date: string;
        status: string;
        source: string;
    };
    title: string;
    message: string;
    status: 'queued' | 'sending' | 'sent' | 'failed' | 'uncertain' | 'skipped';
    reason: string;
    attempts: number;
    http_status: number;
    next_attempt_at?: string;
    created_at: string;
    updated_at: string;
};

export function notificationThreshold(value: string): string | undefined {
    if (!/^\d+(\.\d{1,6})?$/.test(value.trim())) return undefined;
    const [whole, fraction = ''] = value.trim().split('.');
    const amount = BigInt(whole) * 1_000_000n + BigInt(fraction.padEnd(6, '0'));
    return amount <= 9_223_372_036_854_775_807n ? amount.toString() : undefined;
}
export const checkinPath = '/api/v1/checkin';

export function useCheckinQuery<T>(path: string, enabled = true) {
    return useQuery({
        queryKey: ['checkin', path],
        queryFn: ({ signal }) => apiRequest<T>(`${checkinPath}${path}`, { signal }),
        enabled,
        refetchInterval: 30_000,
    });
}

export function useCheckinMutation<T = unknown>() {
    const client = useQueryClient();
    return useMutation({
        mutationFn: ({
            path,
            method = 'POST',
            body,
        }: {
            path: string;
            method?: 'POST' | 'PATCH' | 'PUT' | 'DELETE';
            body?: unknown;
        }) => apiRequest<T>(`${checkinPath}${path}`, { method, body }),
        onSuccess: () => {
            void client.invalidateQueries({ queryKey: ['checkin'] });
            void client.invalidateQueries({ queryKey: ['proxy-pool'] });
            void client.invalidateQueries({ queryKey: ['proxy-references'] });
        },
    });
}

export function useCheckinEvents() {
    const client = useQueryClient();
    const [connected, setConnected] = useState(false);
    useEffect(() => {
        let timer: ReturnType<typeof setTimeout> | undefined;
        const source = new EventSource(new URL(`.${checkinPath}/events`, document.baseURI), {
            withCredentials: true,
        });
        source.onopen = () => setConnected(true);
        source.onerror = () => setConnected(false);
        source.addEventListener('checkin', () => {
            if (timer) return;
            timer = setTimeout(() => {
                timer = undefined;
                void client.invalidateQueries({ queryKey: ['checkin'] });
            }, 250);
        });
        return () => {
            source.close();
            clearTimeout(timer);
        };
    }, [client]);
    return connected;
}

export function rewardText(micros?: string) {
    if (micros === undefined) return '—';
    const negative = micros.startsWith('-');
    const digits = (negative ? micros.slice(1) : micros).padStart(7, '0');
    const value = `${digits.slice(0, -6)}.${digits.slice(-6)}`.replace(/\.?0+$/, '') || '0';
    return negative && value !== '0' ? `-${value}` : value;
}
