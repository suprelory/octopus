import { expect, test } from '@playwright/test';
import { makeSite, makeCheckinSite, mockApp } from './fixtures';

const activeBatch = {
    id: '900001', status: 'running', trigger: 'manual', total: 2, attempted: 1,
    success: 1, partial: 0, failed: 0, skipped: 0, warnings: 0, canceled: false,
    current_site_id: 1, current_site_name: 'Alpha site',
    current_account_id: 11, current_account_name: 'Primary account',
    duration_ms: 1200, started_at: '2026-09-27T01:00:00Z', updated_at: '2026-09-27T01:00:01Z',
};

test('checkin page polls the active batch and opens logs scoped to that batch on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    const state = await mockApp(page, 'checkin', { sites: [makeCheckinSite(), makeSite(2, 'Subscription')] });
    const logQueries: URL[] = [];
    await page.route('**/api/v1/site/checkin-batches/latest', route =>
        route.fulfill({ json: { code: 200, data: activeBatch } }));
    await page.route('**/api/v1/site/checkin-logs**', async route => {
        logQueries.push(new URL(route.request().url()));
        await route.fulfill({ json: { code: 200, data: { items: [] } } });
    });

    await page.goto('/');
    const status = page.getByTestId('site-checkin-batch-status');
    await expect(status).toContainText('执行中');
    await expect(status).toContainText('1/2');
    await expect(status).toContainText('Alpha site / Primary account');
    const bounds = await status.boundingBox();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(391);

    await status.getByRole('button', { name: '查看本批日志' }).click();
    const dialog = page.getByRole('dialog', { name: '签到记录' });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText('没有符合条件的签到记录。')).toBeVisible();
    await expect.poll(() => logQueries.at(-1)?.searchParams.get('batch_id')).toBe(activeBatch.id);
    await dialog.getByRole('combobox', { name: '站点', exact: true }).click();
    await expect(page.getByRole('option', { name: 'Alpha site', exact: true })).toBeVisible();
    await expect(page.getByRole('option', { name: 'Subscription', exact: true })).toHaveCount(0);
    await page.keyboard.press('Escape');
    await dialog.getByRole('button', { name: '关闭', exact: true }).click();

    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});

test('settings task panel shows the latest full check-in batch', async ({ page }) => {
    const state = await mockApp(page, 'setting');
    await page.route('**/api/v1/site/checkin-batches/latest', route =>
        route.fulfill({ json: { code: 200, data: activeBatch } }));

    await page.goto('/');
    await page.getByRole('group', { name: '设置分类' }).getByRole('button', { name: '连接与任务' }).click();
    await expect(page.getByRole('heading', { name: '网络与服务', exact: true })).toBeVisible();
    const status = page.getByTestId('site-checkin-batch-status');
    await expect(status).toContainText('执行中');
    await expect(status).toContainText('1/2');
    await expect(status).toContainText('Alpha site / Primary account');
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});
