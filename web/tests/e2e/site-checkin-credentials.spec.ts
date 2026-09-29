import { expect, test } from '@playwright/test';
import { SiteCredentialType, type SiteAccount } from '../../src/api/endpoints/site';
import { makeCheckinSite, makeSite, mockApp } from './fixtures';

test('platform checkin selects a subscription account without copying credentials', async ({ page }) => {
    const source: SiteAccount = {
        id: 11, site_id: 1, name: 'Subscription account', credential_type: SiteCredentialType.AccessToken,
        username: '', password: '', access_token: 'source-token', api_key: '', refresh_token: '',
        token_expires_at: 0, platform_user_id: 42, proxy_mode: 'inherit', enabled: true, auto_sync: true,
        auto_checkin: false, random_checkin: false, checkin_interval_hours: 24, checkin_random_window_minutes: 0,
        checkin_failure_count: 0, last_sync_status: '', last_checkin_status: '', last_sync_message: '', last_checkin_message: '',
        balance: 0, balance_used: 0, today_income: 0, tokens: [], user_groups: [], models: [], channel_bindings: [],
    };
    const state = await mockApp(page, 'checkin', {
        sites: [{ ...makeSite(1), accounts: [source] }, { ...makeCheckinSite(2), linked_site_id: 1, base_url: 'https://site-1.example' }],
        mutate: request => {
            expect(request.path).toBe('/api/v1/site/account/create');
            expect(request.body).toMatchObject({ credential_type: 'linked_account', linked_account_id: 11, access_token: '', password: '', cookie: '' });
            return { data: { id: 22, ...request.body as object } };
        },
    });
    await page.goto('/');
    await page.getByRole('button', { name: '新增账号', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('账号名称', { exact: true }).fill('Daily rewards');
    await expect(dialog.getByPlaceholder('请输入 Access Token')).toHaveCount(0);
    await expect(dialog.getByLabel('签到站 Cookie', { exact: true })).toHaveCount(0);
    await dialog.getByRole('button', { name: '创建账号', exact: true }).click();
    expect(state.mutations).toHaveLength(0);
    await dialog.getByRole('combobox', { name: '订阅站账号', exact: true }).click();
    await page.getByRole('option', { name: 'Subscription account', exact: true }).click();
    await dialog.getByRole('button', { name: '创建账号', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(state.mutations).toHaveLength(1);
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});
