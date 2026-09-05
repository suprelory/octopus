import { expect, type Page } from '@playwright/test';
import type { Site } from '../../src/api/endpoints/site';
import type { SiteChannelCard } from '../../src/api/endpoints/site-channel';

export function makeSite(id = 1, name = 'Alpha site'): Site {
    return {
        id, name, platform: 'new-api' as Site['platform'], base_url: `https://site-${id}.example`,
        enabled: true, proxy_mode: 'direct', checkin_timezone: 'Asia/Shanghai',
        checkin_window_start: '00:00', checkin_window_end: '23:59', is_pinned: false,
        sort_order: 0, global_weight: 1, custom_header: [], route_base_urls: [], tags: [],
        archived: false, accounts: [],
    };
}

export function makeSiteChannelCard(): SiteChannelCard {
    return {
        site_id: 1, site_name: 'Alpha site', base_url: 'https://site-1.example',
        platform: 'new-api' as Site['platform'], enabled: true, account_count: 1,
        accounts: [{
            site_id: 1, account_id: 11, account_name: 'Primary account', enabled: true,
            auto_sync: false, group_count: 1, model_count: 2,
            route_summaries: [{ route_type: 'openai_chat', count: 1 }, { route_type: 'anthropic', count: 1 }],
            groups: [{
                group_key: 'default', group_name: 'Default group', projection_disabled: false,
                projection_suspended: false, model_sync_status: 'synced', model_sync_authoritative: true,
                model_sync_model_count: 2, model_sync_failure_count: 0, key_count: 1,
                enabled_key_count: 1, masked_pending_key_count: 0, has_keys: true,
                has_projected_channel: true, projected_channel_ids: [101, 102],
                projected_channels: [{
                    channel_id: 101, channel_name: 'Chat route', route_type: 'openai_chat',
                    auto_group: 0, effective_auto_group: 0, global_override: false,
                    param_override: '{"temperature":0.2}',
                }, {
                    channel_id: 102, channel_name: 'Messages route', route_type: 'anthropic',
                    auto_group: 1, effective_auto_group: 1, global_override: false,
                    param_override: '{"max_tokens":64}',
                }],
                source_keys: [], projected_keys: [],
                models: [{
                    model_name: 'chat-model', source: 'sync', route_type: 'openai_chat',
                    route_source: 'sync_inferred', manual_override: false, disabled: false, projected_channel_id: 101,
                }, {
                    model_name: 'messages-model', source: 'sync', route_type: 'anthropic',
                    route_source: 'sync_inferred', manual_override: false, disabled: false, projected_channel_id: 102,
                }],
            }],
        }],
    };
}

export type Mutation = { method: string; path: string; body: unknown };
type MockResponse = { status?: number; data?: unknown; message?: string };

export async function mockApp(page: Page, nav: 'site' | 'channel', options: {
    sites?: Site[];
    siteChannels?: SiteChannelCard[];
    mutate?: (request: Mutation) => MockResponse | Promise<MockResponse>;
} = {}) {
    const state = {
        sites: options.sites ?? [makeSite()],
        siteChannels: options.siteChannels ?? [makeSiteChannelCard()],
        mutations: [] as Mutation[],
        unexpectedRequests: [] as string[],
        pageErrors: [] as string[],
    };
    page.on('pageerror', error => state.pageErrors.push(error.message));
    await page.addInitScript(activeItem => {
        localStorage.setItem('auth-storage', JSON.stringify({
            state: { token: 'browser-test-token', expireAt: '2099-01-01T00:00:00Z', isAPIKeyAuth: false }, version: 0,
        }));
        localStorage.setItem('nav-storage', JSON.stringify({ state: { activeItem, prevItem: null, direction: 0 }, version: 0 }));
        localStorage.setItem('octopus-settings', JSON.stringify({ state: { locale: 'zh_hans' }, version: 0 }));
    }, nav);
    await page.route('**/api/v1/**', async route => {
        const request = route.request();
        const path = new URL(request.url()).pathname;
        if (request.method() !== 'GET') {
            const mutation = { method: request.method(), path, body: request.postData() ? request.postDataJSON() as unknown : null };
            state.mutations.push(mutation);
            const response = await options.mutate?.(mutation);
            if (!response) state.unexpectedRequests.push(`${request.method()} ${path}`);
            await route.fulfill({
                status: response?.status ?? (response ? 200 : 500),
                json: { code: response?.status ?? (response ? 200 : 500), data: response?.data ?? null, message: response?.message ?? 'Unexpected mutation' },
            });
            return;
        }
        const data: Record<string, unknown> = {
            '/api/v1/user/status': null,
            '/api/v1/user/bootstrap': { required: false },
            '/api/v1/site/list': state.sites,
            '/api/v1/site-channel/list': state.siteChannels,
            '/api/v1/channel/list': [],
            '/api/v1/group/list': [],
            '/api/v1/model/channel': [],
            '/api/v1/model/list': [],
            '/api/v1/setting/list': [],
            '/api/v1/proxy-pool/list': [],
        };
        if (!(path in data)) state.unexpectedRequests.push(`GET ${path}`);
        await route.fulfill({ status: path in data ? 200 : 500, json: { code: 200, data: data[path] ?? null } });
    });
    return state;
}

export async function openAdvancedSettings(page: Page) {
    await page.getByText('Alpha site', { exact: true }).click();
    const groupPicker = page.getByRole('combobox').filter({ hasText: '分组' });
    await groupPicker.click();
    await page.getByRole('option', { name: /Default group/ }).click();
    await page.getByRole('button', { name: '高级', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: '站点渠道高级设置' });
    await expect(dialog).toBeVisible();
    return dialog;
}
