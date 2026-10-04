import { expect, test } from '@playwright/test';
import type { RelayLogDetail, RelayMessage } from '../../src/api/endpoints/log';
import { mockApp } from './fixtures';

const message = (body: string, status?: number): RelayMessage => ({ state: 'captured', bytes: body.length, captured_bytes: body.length, status_code: status, content_type: 'application/json', headers: { Authorization: ['[REDACTED]'] } });
const bodies: Record<string, string> = { 'client/request': '{"origin":"client"}', 'client/response': '{"result":"delivered"}', '1/request': '{"model":"first"}', '1/response': '{"error":"rate limited"}', '2/request': '{"model":"second"}', '2/response': '{"result":"upstream"}' };
const detail: RelayLogDetail = {
    id: 1, time: 1790300000, request_model_name: 'trace-model', actual_model_name: 'second', channel: 2, channel_name: 'Fallback',
    input_tokens: 20, output_tokens: 10, ftut: 100, use_time: 300, cost: 0, error: '', request_content: '', response_content: '',
    trace: { id: 'req_browser_test', client: { transport: 'http', request: message(bodies['client/request']), response: message(bodies['client/response'], 200) }, attempts: [
        { attempt_id: '1', channel_name: 'Primary', transport: 'http', request: message(bodies['1/request']), response: message(bodies['1/response'], 429) },
        { attempt_id: '2', channel_name: 'Fallback', transport: 'http', request: message(bodies['2/request']), response: message(bodies['2/response'], 200) },
    ] },
};

for (const width of [1440, 390]) {
    test(`request bodies load on demand and stay scoped to their attempt at ${width}px`, async ({ page }, info) => {
        await page.setViewportSize({ width, height: 1000 });
        await page.addInitScript(() => localStorage.setItem('log-ui-storage', JSON.stringify({ state: { liveEnabled: false, pageSize: 20 }, version: 0 })));
        const state = await mockApp(page, 'log', { logs: [detail] });
        const loaded: string[] = [];
        await page.route('**/api/v1/log/1', route => route.fulfill({ json: { code: 200, data: detail } }));
        await page.route('**/api/v1/log/1/content?**', route => {
            const params = new URL(route.request().url()).searchParams;
            const key = `${params.get('attempt_id') || 'client'}/${params.get('direction')}`;
            loaded.push(key);
            return route.fulfill({ json: { code: 200, data: { ...message(bodies[key]), body: bodies[key], body_encoding: 'utf-8' } } });
        });
        await page.goto('/');
        await expect(page.getByRole('heading', { name: '请求记录' })).toBeVisible();
        expect(loaded).toEqual([]);
        await page.getByRole('button', { name: /trace-model/ }).first().click();
        const dialog = page.getByRole('dialog');
        await expect(dialog.getByText(bodies['client/request'], { exact: true })).toBeVisible();
        expect(loaded).toEqual(['client/request']);
        await dialog.getByRole('tab', { name: '上游响应', exact: true }).click();
        await expect(dialog.getByText(bodies['2/response'], { exact: true })).toBeVisible();
        await dialog.getByRole('combobox', { name: '上游尝试' }).selectOption('1');
        await expect(dialog.getByText(bodies['1/response'], { exact: true })).toBeVisible();
        await dialog.getByRole('tab', { name: '返回客户端', exact: true }).click();
        await expect(dialog.getByText(bodies['client/response'], { exact: true })).toBeVisible();
        await dialog.getByRole('textbox', { name: '搜索正文' }).fill('delivered');
        await expect(dialog.getByRole('status')).toContainText('1 处匹配');
        await page.screenshot({ path: info.outputPath('request-content.png'), fullPage: true });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        expect(state.unexpectedRequests).toEqual([]);
        expect(state.pageErrors).toEqual([]);
    });
}

test('stream event selection uses byte offsets across Unicode messages', async ({ page }) => {
    const first = 'data: {"delta":"你好"}\r\n\r\n';
    const last = 'data: {"delta":"结束"}\n\n';
    const body = first + last;
    const firstBytes = new TextEncoder().encode(first).length;
    const response: RelayMessage = { ...message(body), bytes: new TextEncoder().encode(body).length, captured_bytes: new TextEncoder().encode(body).length, content_type: 'text/event-stream', events: [
        { sequence: 1, offset: 0, bytes: firstBytes, elapsed_ms: 10, type: 'sse', complete: true },
        { sequence: 2, offset: firstBytes, bytes: new TextEncoder().encode(last).length, elapsed_ms: 30, type: 'sse', complete: true },
    ] };
    const streamDetail = { ...detail, trace: { ...detail.trace!, client: { ...detail.trace!.client, response } } };
    await page.addInitScript(() => localStorage.setItem('log-ui-storage', JSON.stringify({ state: { liveEnabled: false, pageSize: 20 }, version: 0 })));
    const state = await mockApp(page, 'log', { logs: [streamDetail] });
    await page.route('**/api/v1/log/1', route => route.fulfill({ json: { code: 200, data: streamDetail } }));
    await page.route('**/api/v1/log/1/content?**', route => {
        const isResponse = new URL(route.request().url()).searchParams.get('direction') === 'response';
        return route.fulfill({ json: { code: 200, data: isResponse ? { ...response, body } : { ...message(bodies['client/request']), body: bodies['client/request'] } } });
    });
    await page.goto('/');
    await page.getByRole('button', { name: /trace-model/ }).first().click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('tab', { name: '返回客户端', exact: true }).click();
    await dialog.getByRole('combobox', { name: '事件序列' }).selectOption('2');
    await expect(dialog.locator('pre').last()).toHaveText(last);
    await dialog.getByRole('combobox', { name: '事件序列' }).selectOption('1');
    await expect(dialog.locator('pre').last()).toHaveText(first);
    expect(state.pageErrors).toEqual([]);
});
