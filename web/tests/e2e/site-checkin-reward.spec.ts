import { expect, test } from '@playwright/test';
import { makeCheckinSite, mockApp } from './fixtures';

test('reward templates preview sample JSON and persist or clear extraction code', async ({ page }) => {
    const state = await mockApp(page, 'checkin', {
        sites: [{ ...makeCheckinSite(), checkin_http_enabled: true, checkin_http_path: '/daily' }],
        mutate: request => {
            if (request.path === '/api/v1/site/checkin-reward/test') {
                expect(request.body).toEqual({
                    code: 'const quota = response.data?.quota_awarded;\nreturn quota == null ? null : Number(quota) / 500000;',
                    response: { success: true, data: { quota_awarded: 50000 } },
                });
                return { data: { reward: '0.1', found: true } };
            }
            expect(request.path).toBe('/api/v1/site/update');
            state.sites[0] = { ...state.sites[0], ...request.body as object };
            return { data: state.sites[0] };
        },
    });
    await page.goto('/');
    async function openEditor() {
        await page.getByRole('button', { name: '更多站点操作', exact: true }).click();
        await page.getByRole('button', { name: '编辑站点', exact: true }).click();
    }
    await openEditor();
    const dialog = page.getByRole('dialog');
    const code = dialog.getByLabel('奖励提取代码（可选 JavaScript）', { exact: true });
    await dialog.getByRole('button', { name: '通用模板', exact: true }).click();
    await expect(code).toHaveValue('return response.data?.reward ?? response.reward ?? null;');
    await dialog.getByRole('button', { name: 'New API 模板', exact: true }).click();
    const sample = dialog.getByLabel('签到响应样例（JSON）', { exact: true });
    const validSample = await sample.inputValue();
    await sample.fill('{invalid');
    await dialog.getByRole('button', { name: '测试提取', exact: true }).click();
    await expect(dialog.getByRole('status')).toHaveText('请输入有效的 JSON 响应样例');
    expect(state.mutations).toHaveLength(0);
    await sample.fill(validSample);
    await dialog.getByRole('button', { name: '测试提取', exact: true }).click();
    await expect(dialog.getByRole('status')).toHaveText('提取结果：$0.1');
    expect(state.mutations).toHaveLength(1);
    const savedCode = await code.inputValue();
    await dialog.getByRole('button', { name: '保存修改', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(state.sites[0].checkin_reward_extractor).toBe(savedCode);
    await openEditor();
    await expect(code).toHaveValue(savedCode);
    await code.fill('');
    await expect(dialog.getByRole('button', { name: '测试提取', exact: true })).toHaveCount(0);
    await dialog.getByRole('button', { name: '保存修改', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(state.sites[0].checkin_reward_extractor).toBe('');
    expect(state.mutations).toHaveLength(3);
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});
