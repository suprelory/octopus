import { expect, test } from '@playwright/test';
import type { RelayLog } from '../../src/api/endpoints/log';
import { mockApp } from './fixtures';

const failedLog: RelayLog = {
    id: 1, time: 1790300000, request_model_name: 'empty-reply', actual_model_name: 'model',
    channel: 1, channel_name: 'Primary', input_tokens: 0, output_tokens: 0,
    ftut: 0, use_time: 100, cost: 0, success: false, error: '',
};
const recoveredLog: RelayLog = {
    ...failedLog, id: 2, request_model_name: 'recovered-reply', channel: 2, channel_name: 'Fallback', success: true,
    total_attempts: 2,
    attempts: [
        { channel_id: 1, channel_name: 'Primary', model_name: 'model', attempt_num: 1, status: 'failed', duration: 50, msg: 'empty upstream response' },
        { channel_id: 2, channel_name: 'Fallback', model_name: 'model', attempt_num: 2, status: 'success', duration: 50 },
    ],
};

for (const width of [1440, 390]) {
    test(`request logs mark failures without error text at ${width}px`, async ({ page }) => {
        await page.setViewportSize({ width, height: 1000 });
        await page.addInitScript(() => localStorage.setItem('log-ui-storage', JSON.stringify({ state: { liveEnabled: false, pageSize: 20 }, version: 0 })));
        const state = await mockApp(page, 'log', { logs: [failedLog, recoveredLog] });
        await page.goto('/');

        const failedCard = page.getByRole('button', { name: /empty-reply/ }).first();
        await expect(failedCard).toHaveClass(/border-destructive\/40/);
        await expect(failedCard.getByText('失败', { exact: true })).toBeVisible();
        const recoveredCard = page.getByRole('button', { name: /recovered-reply/ }).first();
        await expect(recoveredCard).not.toHaveClass(/border-destructive\/40/);

        await failedCard.click();
        const dialog = page.getByRole('dialog');
        await dialog.getByText('错误信息', { exact: true }).click();
        await expect(dialog.getByText('失败', { exact: true })).toBeVisible();
        expect(state.pageErrors).toEqual([]);
        expect(state.unexpectedRequests).toEqual([]);
    });
}
