import { expect, test } from '@playwright/test';
import { makeSite, makeCheckinSite, mockApp } from './fixtures';

for (const width of [1440, 390]) {
    test(`subscription and checkin pages keep lists, forms and batch selection separate at ${width}px`, async ({ page }, testInfo) => {
        await page.setViewportSize({ width, height: 1000 });
        const state = await mockApp(page, 'site', {
            sites: [makeSite(1, 'Subscription'), { ...makeCheckinSite(2, 'Daily rewards'), linked_site_id: 1 }],
            mutate: request => {
                expect(request).toEqual({ method: 'POST', path: '/api/v1/site/batch', body: { ids: [2], action: 'checkin' } });
                return { data: { success_ids: [2], failed_items: [] } };
            },
        });
        await page.goto('/');
        await expect(page.getByRole('heading', { name: 'Subscription', exact: true })).toBeVisible();
        await expect(page.getByRole('heading', { name: 'Daily rewards', exact: true })).toHaveCount(0);
        await expect(page.getByRole('button', { name: '签到记录', exact: true })).toHaveCount(0);
        await expect(page.getByRole('region', { name: '签到收益', exact: true })).toHaveCount(0);

        await page.getByRole('button', { name: '更多站点操作', exact: true }).click();
        await page.getByRole('button', { name: '编辑站点', exact: true }).click();
        const relayEditor = page.getByRole('dialog');
        await expect(relayEditor.getByLabel('站点地址', { exact: true })).toBeVisible();
        await expect(relayEditor.getByLabel('自动签到时区', { exact: true })).toHaveCount(0);
        await expect(relayEditor.getByRole('button', { name: '高级设置', exact: true })).toBeVisible();
        await relayEditor.getByRole('button', { name: '取消', exact: true }).click();
        await page.getByRole('button', { name: '新增账号', exact: true }).click();
        await expect(page.getByRole('dialog').getByRole('switch', { name: '自动同步', exact: true })).toBeVisible();
        await expect(page.getByRole('dialog').getByRole('switch', { name: '自动签到', exact: true })).toHaveCount(0);
        await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click();
        await page.getByRole('button', { name: '选择站点', exact: true }).click();
        await expect(page.getByRole('button', { name: '批量签到', exact: true })).toHaveCount(0);

        await page.getByRole('navigation').getByRole('button', { name: '签到', exact: true }).click();
        await expect(page.getByRole('heading', { name: 'Daily rewards', exact: true })).toBeVisible();
        await expect(page.getByRole('heading', { name: 'Subscription', exact: true })).toHaveCount(0);
        await expect(page.getByText('关联订阅站：Subscription', { exact: true }).filter({ visible: true })).toBeVisible();
        await expect(page.getByRole('region', { name: '签到收益', exact: true })).toBeVisible();
        await expect(page.getByText('已选 1 个站点', { exact: true })).toHaveCount(0);
        await page.getByRole('button', { name: '选择站点', exact: true }).click();
        await page.getByRole('button', { name: '批量签到', exact: true }).click();
        await expect.poll(() => state.mutations.length).toBe(1);
        await page.getByRole('button', { name: '更多站点操作', exact: true }).click();
        await expect(page.getByRole('button', { name: '查看站点渠道', exact: true })).toHaveCount(0);
        await page.keyboard.press('Escape');
        const navBounds = await page.getByRole('navigation').boundingBox();
        expect(navBounds!.x).toBeGreaterThanOrEqual(0);
        expect(navBounds!.x + navBounds!.width).toBeLessThanOrEqual(width + 1);
        await page.screenshot({ path: testInfo.outputPath('independent-checkin-page.png'), fullPage: true, animations: 'disabled' });
        expect(state.unexpectedRequests).toEqual([]);
        expect(state.pageErrors).toEqual([]);
    });
}

test('creating a checkin site submits an independent URL and opens its account form', async ({ page }) => {
    const state = await mockApp(page, 'checkin', {
        sites: [makeSite(1, 'Subscription')],
        mutate: request => {
            expect(request.path).toBe('/api/v1/site/create');
            expect(request.body).toMatchObject({ kind: 'checkin', linked_site_id: 1, base_url: 'https://rewards.example', name: 'Rewards' });
            const created = { ...makeCheckinSite(2), ...request.body as object };
            state.sites.push(created);
            return { data: created };
        },
    });
    await page.goto('/');
    await page.getByRole('button', { name: '新增第一个签到站点', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('站点名称', { exact: true }).fill('Rewards');
    await dialog.getByLabel('签到站点地址', { exact: true }).fill('https://rewards.example');
    await dialog.getByRole('combobox').first().click();
    await page.getByRole('option', { name: 'New API', exact: true }).click();
    await dialog.getByRole('switch', { name: '启用自定义 HTTP 签到', exact: true }).check();
    await dialog.getByLabel('请求路径', { exact: true }).fill('/daily');
    await dialog.getByRole('combobox', { name: '关联订阅站', exact: true }).click();
    await page.getByRole('option', { name: 'Subscription', exact: true }).click();
    await dialog.getByRole('button', { name: '创建站点', exact: true }).click();
    await expect(page.getByRole('heading', { name: '新增站点账号', exact: true })).toBeVisible();
    await expect(page.getByRole('dialog').getByRole('switch', { name: '自动签到', exact: true })).toBeVisible();
    await expect(page.getByRole('dialog').getByRole('switch', { name: '自动同步', exact: true })).toHaveCount(0);
    await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Rewards', exact: true })).toBeVisible();
    expect(state.sites[0].base_url).toBe('https://site-1.example');
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});
