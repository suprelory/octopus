import { expect, test, type Locator } from '@playwright/test';
import { mockApp } from './fixtures';
import { SettingKey } from '../../src/api/endpoints/setting';

async function expectFocusInside(dialog: Locator) {
    await expect.poll(() => dialog.evaluate(node => node.contains(document.activeElement))).toBe(true);
}

for (const timezoneId of ['Asia/Shanghai', 'America/Los_Angeles', 'UTC']) {
    test.describe(timezoneId, () => {
        test.use({ timezoneId });
        test('key editor preserves expiration on blur and traps and restores keyboard focus', async ({ page }) => {
            const expireAt = Date.parse('2026-09-30T20:00:37Z') / 1000;
            const state = await mockApp(page, 'setting', {
                apiKeys: [{ id: 1, name: 'Development', api_key: 'test-only', enabled: true, expire_at: expireAt }],
                mutate: request => {
                    expect(request.path).toBe('/api/v1/apikey/update');
                    expect(request.body).toMatchObject({ id: 1, expire_at: expireAt });
                    return { data: request.body };
                },
            });
            await page.goto('/');
            await page.getByRole('button', { name: '访问密钥', exact: true }).click();
            const edit = page.getByRole('button', { name: 'Edit', exact: true });
            await edit.focus();
            await page.keyboard.press('Enter');
            const dialog = page.getByRole('dialog', { name: 'API 密钥 · Development', exact: true });
            await expect(dialog).toBeVisible();
            await expect(dialog.getByRole('textbox', { name: '名称', exact: true })).toBeFocused();
            await page.keyboard.press('Shift+Tab');
            await expect(dialog.getByRole('button', { name: '保存', exact: true })).toBeFocused();
            await page.keyboard.press('Tab');
            await expect(dialog.getByRole('textbox', { name: '名称', exact: true })).toBeFocused();
            const time = dialog.getByPlaceholder('HH:mm');
            const expected = await page.evaluate(value => {
                const date = new Date(value * 1000);
                return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
            }, expireAt);
            await expect(time).toHaveValue(expected);
            await time.focus();
            await page.keyboard.press('Tab');
            await dialog.getByRole('button').filter({ has: page.locator('svg.lucide-calendar-days') }).click();
            await expect(page.locator('[data-slot="popover-content"]')).toBeVisible();
            await page.keyboard.press('Escape');
            await expect(page.locator('[data-slot="popover-content"]')).toHaveCount(0);
            await expect(dialog).toBeVisible();
            await dialog.getByRole('button', { name: '保存', exact: true }).click();
            await expect(dialog).toHaveCount(0);
            await expect(edit).toBeFocused();
            expect(state.mutations).toHaveLength(1);
            expect(state.pageErrors).toEqual([]);
        });
    });
}

test('statistics and export overlays trap focus and restore their openers', async ({ page }) => {
    const state = await mockApp(page, 'setting', {
        apiKeys: [{ id: 1, name: 'Development', api_key: 'test-only', enabled: true }],
        settings: [{ key: SettingKey.ApiBaseUrl, value: 'https://api.example.test' }],
    });
    await page.goto('/');
    await page.getByRole('button', { name: '访问密钥', exact: true }).click();
    for (const [opener, name] of [['Stats', 'API 密钥 · Development'], ['Export', '导出到客户端 · Development']]) {
        const trigger = page.getByRole('button', { name: opener, exact: true });
        await trigger.focus();
        await page.keyboard.press('Enter');
        const dialog = page.getByRole('dialog', { name, exact: true });
        await expect(dialog).toBeVisible();
        await expectFocusInside(dialog);
        for (let i = 0; i < 20; i++) {
            await page.keyboard.press(i < 10 ? 'Tab' : 'Shift+Tab');
            await expectFocusInside(dialog);
        }
        await page.keyboard.press('Escape');
        await expect(dialog).toHaveCount(0);
        await expect(trigger).toBeFocused();
    }
    expect(state.pageErrors).toEqual([]);
});

test('an editor inside the expanded key panel closes only its own layer', async ({ page }) => {
    await mockApp(page, 'setting', { apiKeys: [{ id: 1, name: 'Development', api_key: 'test-only', enabled: true }] });
    await page.goto('/');
    await page.getByRole('button', { name: '访问密钥', exact: true }).click();
    await page.getByRole('button').filter({ has: page.locator('svg.lucide-maximize2') }).click();
    const panel = page.locator('[data-slot="morphing-dialog-content"]');
    const edit = panel.getByRole('button', { name: 'Edit', exact: true });
    await edit.focus();
    await page.keyboard.press('Enter');
    const dialog = page.getByRole('dialog', { name: 'API 密钥 · Development', exact: true });
    await expect(dialog).toBeVisible();
    await page.keyboard.press('Shift+Tab');
    await expectFocusInside(dialog);
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
    await expect(panel).toBeVisible();
    await expect(edit).toBeFocused();
});
