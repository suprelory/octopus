import { expect, test } from '@playwright/test';
import { makeSite, mockApp } from './fixtures';

for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
    test.describe(`${viewport.width}px site forms`, () => {
        test.use({ viewport });

        test('site sections share a draft, validate the schedule and submit normalized headers', async ({ page }, testInfo) => {
            const state = await mockApp(page, 'site', {
                mutate: request => {
                    expect(request.method).toBe('POST');
                    expect(request.path).toBe('/api/v1/site/update');
                    state.sites[0] = { ...state.sites[0], ...request.body as object };
                    return { data: state.sites[0] };
                },
            });
            await page.goto('/');
            await page.getByRole('button', { name: '更多站点操作' }).click();
            await page.getByRole('button', { name: '编辑站点', exact: true }).click();
            const dialog = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '编辑站点' }) });
            await dialog.getByLabel('站点名称', { exact: true }).fill('  Updated site  ');
            await dialog.getByLabel('自动签到时区', { exact: true }).fill('UTC');
            await dialog.getByLabel('开始时间', { exact: true }).fill('09:00');
            await dialog.getByLabel('结束时间', { exact: true }).fill('08:00');
            await dialog.getByRole('button', { name: '保存修改', exact: true }).click();
            await expect(page.getByText('签到结束时间不能早于开始时间', { exact: true })).toBeVisible();
            expect(state.mutations).toEqual([]);

            await dialog.getByLabel('结束时间', { exact: true }).fill('18:00');
            await dialog.getByRole('button', { name: '高级设置', exact: true }).click();
            await dialog.getByPlaceholder('Header Key', { exact: true }).fill(' X-Project ');
            await dialog.getByPlaceholder('Header Value', { exact: true }).fill(' split-check ');
            await page.screenshot({ path: testInfo.outputPath('site-edit.png') });
            const bounds = await dialog.boundingBox();
            expect(bounds!.width).toBeLessThanOrEqual(viewport.width);
            await dialog.getByRole('button', { name: '保存修改', exact: true }).click();
            await expect(dialog).not.toBeVisible();
            expect(state.mutations).toEqual([{
                method: 'POST', path: '/api/v1/site/update', body: {
                    id: 1, name: 'Updated site', platform: 'new-api', base_url: 'https://site-1.example',
                    enabled: true, proxy_mode: 'direct', proxy_config_id: null,
                    external_checkin_url: null, checkin_timezone: 'UTC', checkin_window_start: '09:00',
                    checkin_window_end: '18:00', is_pinned: false, sort_order: 0, global_weight: 1,
                    custom_header: [{ header_key: 'X-Project', header_value: 'split-check' }],
                    route_base_urls: [], tags: [],
                },
            }]);
            expect(state.unexpectedRequests).toEqual([]);
            expect(state.pageErrors).toEqual([]);
        });

        test('account credential changes and scheduling fields produce one consistent payload', async ({ page }, testInfo) => {
            let finishSave!: () => void;
            const pendingSave = new Promise<void>(resolve => { finishSave = resolve; });
            const state = await mockApp(page, 'site', {
                sites: [makeSite()],
                mutate: async request => {
                    expect(request.method).toBe('POST');
                    expect(request.path).toBe('/api/v1/site/account/create');
                    await pendingSave;
                    return { data: { id: 11, ...request.body as object } };
                },
            });
            await page.goto('/');
            await page.getByRole('button', { name: '新增账号', exact: true }).click();
            const dialog = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '新增站点账号' }) });
            await dialog.getByLabel('账号名称', { exact: true }).fill('  Primary account  ');
            await dialog.getByPlaceholder('请输入 Access Token', { exact: true }).fill('old-token');
            await dialog.getByPlaceholder('例如 11494', { exact: true }).fill('42');
            await dialog.getByRole('combobox').first().click();
            await page.getByRole('option', { name: 'API Key', exact: true }).click();
            await dialog.getByPlaceholder('请输入 API Key', { exact: true }).fill('  new-api-key  ');
            await expect(dialog.getByPlaceholder('请输入 Access Token', { exact: true })).not.toBeVisible();
            await dialog.getByRole('switch', { name: '自动同步', exact: true }).uncheck();
            await dialog.getByRole('switch', { name: '随机签到', exact: true }).check();
            await dialog.getByRole('spinbutton', { name: /签到间隔/ }).fill('48');
            await dialog.getByRole('spinbutton', { name: /随机延迟窗口/ }).fill('30');
            await page.screenshot({ path: testInfo.outputPath('account-edit.png') });
            const bounds = await dialog.boundingBox();
            expect(bounds!.width).toBeLessThanOrEqual(viewport.width);
            await dialog.getByRole('button', { name: '创建账号', exact: true }).click();
            try {
                await expect(dialog.getByRole('button', { name: '保存中...' })).toBeDisabled();
                await expect.poll(() => state.mutations.length).toBe(1);
            } finally {
                finishSave();
            }
            await expect(dialog).not.toBeVisible();
            expect(state.mutations).toEqual([{
                method: 'POST', path: '/api/v1/site/account/create', body: {
                    site_id: 1, name: 'Primary account', credential_type: 'api_key',
                    username: '', password: '', access_token: '', api_key: 'new-api-key',
                    refresh_token: '', token_expires_at: 0, platform_user_id: null,
                    proxy_mode: 'inherit', proxy_config_id: null, enabled: true,
                    auto_sync: false, auto_checkin: true, random_checkin: true,
                    checkin_interval_hours: 48, checkin_random_window_minutes: 30,
                },
            }]);
            expect(state.unexpectedRequests).toEqual([]);
            expect(state.pageErrors).toEqual([]);
        });
    });
}
