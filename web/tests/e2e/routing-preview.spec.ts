import { expect, test } from '@playwright/test';
import { mockApp } from './fixtures';

test('routing preview validates drafts and submits only the read-only simulation', async ({page}, testInfo) => {
    const state = await mockApp(page, 'group', {
        groups: [{id: 77, name: 'routing-demo', mode: 3, match_regex: '', items: [{id: 1, channel_id: 101, model_name: 'upstream-model', priority: 1, weight: 2}]}],
        mutate: (request) => {
            expect(request.path).toBe('/api/v1/group/preview');
            expect(request.method).toBe('POST');
            return {data: {
                group_id: 77, model: 'routing-demo', affinity: {mode: 'prefer', source: 'header'}, warnings: [],
                budget: {attempt_limit: 12, channel_limit: 4, attempts_used: 0, channels_used: 0, remaining_millis: 300000, committed: false},
                candidates: [
                    {item: {id: 1, channel_id: 101, model_name: 'upstream-model', priority: 1, weight: 2}, channel_name: 'Primary route', order: 1, eligible: true, reason: 'eligible', selection_reason: 'channel_affinity', strategy: 'failover', quality_rank: 0, capability_status: 'supported', healthy_keys: 1, blocked_keys: 1, conversion_path: ['responses', 'responses'], metrics: {composite_score: 125.5, in_flight: 1}},
                    {item: {id: 2, channel_id: 102, model_name: 'upstream-model', priority: 2, weight: 1}, channel_name: 'Disabled route', order: 0, eligible: false, reason: 'channel_disabled', quality_rank: 0, healthy_keys: 0, blocked_keys: 0},
                ],
            }};
        },
    });
    await page.goto('/');
    await page.getByText('routing-demo', {exact: true}).click();
    await page.getByRole('button', {name: '路由预览', exact: true}).click();
    const dialog = page.getByRole('dialog', {name: '路由预览 · routing-demo'});
    await expect(dialog).toBeVisible();
    await dialog.getByRole('textbox', {name: '会话 Header（JSON 字符串值）'}).fill('[]');
    await dialog.getByRole('button', {name: '预览路由', exact: true}).click();
    await expect(dialog.getByRole('alert')).toBeVisible();
    expect(state.mutations).toEqual([]);
    await dialog.getByRole('textbox', {name: '会话 Header（JSON 字符串值）'}).fill('{"X-Session-Id":"session-7"}');
    await dialog.getByRole('spinbutton').fill('7');
    await dialog.getByRole('button', {name: '预览路由', exact: true}).click();
    await expect(dialog.getByText('首个可用渠道：Primary route')).toBeVisible();
    await expect(dialog.getByText('渠道已禁用', {exact: true})).toBeVisible();
    expect(state.mutations).toEqual([{method: 'POST', path: '/api/v1/group/preview', body: {group_id: 77, api_key_id: 7, endpoint: 'responses', headers: {'X-Session-Id': ['session-7']}, request: {model: 'routing-demo', input: 'Hello'}}}]);
    await page.screenshot({path: testInfo.outputPath('routing-preview.png'), fullPage: true});
    await page.setViewportSize({width: 390, height: 844});
    await expect(dialog).toBeVisible();
    expect(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
    await dialog.getByRole('cell', {name: /^Disabled route/}).scrollIntoViewIfNeeded();
    await expect(dialog.getByRole('cell', {name: /^Disabled route/})).toBeInViewport();
    await page.screenshot({path: testInfo.outputPath('routing-preview-mobile.png'), fullPage: true});
    await dialog.getByRole('textbox', {name: '请求 JSON'}).fill('{"model":"routing-demo","input":"changed"}');
    await expect(dialog.getByText('首个可用渠道：Primary route')).not.toBeVisible();
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});
