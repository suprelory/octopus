import { expect, test } from '@playwright/test';
import { mockApp, openAdvancedSettings } from './fixtures';

test('channel drafts survive switching and cancel restores the saved values', async ({ page }) => {
    const state = await mockApp(page, 'channel');
    await page.goto('/');
    const dialog = await openAdvancedSettings(page);
    const input = dialog.getByRole('textbox', { name: '参数覆盖 JSON' });
    await expect(input).toHaveValue('{"temperature":0.2}');
    await input.fill('{"temperature":0.8}');
    await dialog.getByRole('button', { name: /#102/ }).click();
    await expect(input).toHaveValue('{"max_tokens":64}');
    await input.fill('{"max_tokens":128}');
    await dialog.getByRole('button', { name: /#101/ }).click();
    await expect(input).toHaveValue('{"temperature":0.8}');
    await dialog.getByRole('button', { name: '取消', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(state.mutations).toEqual([]);
    await page.getByRole('button', { name: '高级', exact: true }).click();
    await expect(input).toHaveValue('{"temperature":0.2}');
    await dialog.getByRole('button', { name: /#102/ }).click();
    await expect(input).toHaveValue('{"max_tokens":64}');
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});

test('invalid JSON blocks submission; operation arrays and cleared overrides save once', async ({ page }) => {
    let finishSave!: () => void;
    const pendingSave = new Promise<void>(resolve => { finishSave = resolve; });
    const state = await mockApp(page, 'channel', {
        mutate: async request => {
            expect(request.method).toBe('PUT');
            expect(request.path).toBe('/api/v1/site-channel/1/account/11/projected-channel-settings');
            await pendingSave;
            const account = state.siteChannels[0].accounts[0];
            account.groups[0].projected_channels[0].param_override = '[{"op":"remove","path":"/temperature"}]';
            account.groups[0].projected_channels[1].param_override = '';
            return { data: account };
        },
    });
    await page.goto('/');
    const dialog = await openAdvancedSettings(page);
    const input = dialog.getByRole('textbox', { name: '参数覆盖 JSON' });
    await input.fill('{"temperature":');
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    await expect(page.getByText('参数覆盖必须是合法的 JSON 对象或操作数组', { exact: true })).toBeVisible();
    expect(state.mutations).toEqual([]);

    await input.fill('  [{"op":"remove","path":"/temperature"}]  ');
    await dialog.getByRole('button', { name: /#102/ }).click();
    await input.fill('   ');
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    try {
        await expect(dialog.getByRole('button', { name: '保存中...' })).toBeDisabled();
        await expect(dialog.getByRole('button', { name: '取消', exact: true })).toBeDisabled();
        await page.keyboard.press('Escape');
        await expect(dialog).toBeVisible();
        await expect.poll(() => state.mutations).toEqual([{
            method: 'PUT', path: '/api/v1/site-channel/1/account/11/projected-channel-settings',
            body: [
                { channel_id: 101, auto_group: 0, param_override: '[{"op":"remove","path":"/temperature"}]' },
                { channel_id: 102, auto_group: 1, param_override: '' },
            ],
        }]);
    } finally {
        finishSave();
    }
    await expect(dialog).not.toBeVisible();
    await expect(page.getByText('高级设置已保存', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: '高级', exact: true }).click();
    await expect(input).toHaveValue('[{"op":"remove","path":"/temperature"}]');
    expect(state.mutations).toHaveLength(1);
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});

test('failed settings save retains the draft and can be retried', async ({ page }) => {
    let failures = 1;
    const state = await mockApp(page, 'channel', {
        mutate: () => {
            if (failures-- > 0) return { status: 503, message: 'Settings temporarily unavailable' };
            const account = state.siteChannels[0].accounts[0];
            account.groups[0].projected_channels[0].param_override = '{"temperature":0.6}';
            return { data: account };
        },
    });
    await page.goto('/');
    const dialog = await openAdvancedSettings(page);
    const input = dialog.getByRole('textbox', { name: '参数覆盖 JSON' });
    await input.fill('{"temperature":0.6}');
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    await expect(page.getByText('Settings temporarily unavailable', { exact: true })).toBeVisible();
    await expect(dialog).toBeVisible();
    await expect(input).toHaveValue('{"temperature":0.6}');
    await expect(dialog.getByRole('button', { name: '保存', exact: true })).toBeEnabled();
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(state.mutations).toHaveLength(2);
    expect(state.mutations[1]).toEqual(state.mutations[0]);
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});
