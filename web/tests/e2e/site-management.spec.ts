import { expect, test } from '@playwright/test';
import { makeSite, mockApp } from './fixtures';

test('canceling deletion sends no request; confirming deletes only the selected site once', async ({ page }) => {
    let finishDelete!: () => void;
    const pendingDelete = new Promise<void>(resolve => { finishDelete = resolve; });
    const state = await mockApp(page, 'site', {
        sites: [makeSite(), makeSite(2, 'Beta site')],
        mutate: async request => {
            expect(request).toEqual({ method: 'DELETE', path: '/api/v1/site/delete/1', body: null });
            await pendingDelete;
            state.sites = state.sites.filter(site => site.id !== 1);
            return { data: null };
        },
    });
    await page.goto('/');
    const site = page.locator('section.page-card:visible').filter({ hasText: 'Alpha site' });
    await site.getByRole('button', { name: '更多站点操作' }).click();
    await page.getByRole('button', { name: '删除站点', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: '确认删除' });
    await expect(dialog).toContainText('Alpha site');
    await dialog.getByRole('button', { name: '取消', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(state.mutations).toEqual([]);

    await site.getByRole('button', { name: '更多站点操作' }).click();
    await page.getByRole('button', { name: '删除站点', exact: true }).click();
    await dialog.getByRole('button', { name: '确认删除', exact: true }).click();
    try {
        await expect(dialog.getByRole('button', { name: '删除中...' })).toBeDisabled();
        await expect.poll(() => state.mutations.length).toBe(1);
    } finally {
        finishDelete();
    }
    await expect(dialog).not.toBeVisible();
    await expect(site).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Beta site', exact: true })).toBeVisible();
    expect(state.mutations).toHaveLength(1);
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});

test('failed deletion reports the error and keeps the site available for retry', async ({ page }) => {
    let failures = 1;
    const state = await mockApp(page, 'site', {
        mutate: request => {
            expect(request.path).toBe('/api/v1/site/delete/1');
            if (failures-- > 0) return { status: 503, message: 'Delete temporarily unavailable' };
            state.sites = [];
            return { data: null };
        },
    });
    await page.goto('/');
    const site = page.locator('section.page-card:visible').filter({ hasText: 'Alpha site' });
    for (let attempt = 0; attempt < 2; attempt++) {
        await site.getByRole('button', { name: '更多站点操作' }).click();
        await page.getByRole('button', { name: '删除站点', exact: true }).click();
        const dialog = page.getByRole('dialog', { name: '确认删除' });
        await dialog.getByRole('button', { name: '确认删除', exact: true }).click();
        await expect(dialog).not.toBeVisible();
        if (attempt === 0) {
            await expect(page.getByText('Delete temporarily unavailable', { exact: true })).toBeVisible();
            await expect(site).toBeVisible();
        }
    }
    await expect(site).toHaveCount(0);
    expect(state.mutations).toHaveLength(2);
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});
