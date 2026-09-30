import { expect, test, type Page } from '@playwright/test';
import { mockApp } from './fixtures';
import type { NotificationConfig, Setting } from '../../src/api/endpoints/setting';

function initialSettings(): Setting[] {
    return [
        { key: 'notification_channels', value: '' },
        { key: 'checkin_notify_webhook_url', value: 'https://legacy.example/hook' },
        { key: 'checkin_notify_enabled', value: 'false' },
        { key: 'checkin_notify_success_enabled', value: 'false' },
        { key: 'checkin_notify_manual_enabled', value: 'false' },
        { key: 'checkin_notify_cooldown_seconds', value: '3600' },
        { key: 'checkin_low_balance_threshold', value: '0' },
    ];
}

async function openChannels(page: Page) {
    await page.goto('/');
    await page.getByRole('group', { name: '设置分类' }).getByRole('button', { name: '连接与任务' }).click();
    const card = page.locator('.page-card').filter({ has: page.getByRole('heading', { name: '统一通知渠道', exact: true }) });
    await card.scrollIntoViewIfNeeded();
    await expect(card.getByRole('combobox', { name: '通知渠道', exact: true })).toBeEnabled();
    return card;
}

for (const width of [1440, 390]) {
	test(`notification templates stay isolated and persist at ${width}px`, async ({ page }) => {
		await page.setViewportSize({ width, height: 1000 });
		const settings = initialSettings();
		const state = await mockApp(page, 'setting', { settings, mutate: request => {
			if (request.path === '/api/v1/setting/set') {
				const body = request.body as Setting;
				settings.find(item => item.key === body.key)!.value = body.value;
				return { data: body };
			}
			if (request.path === '/api/v1/setting/notification/test') return { data: { channel: 'bark', success: true } };
			return { status: 500, message: 'Unexpected mutation' };
		} });
		let card = await openChannels(page);
		await card.getByLabel('标题模板', { exact: true }).fill('[Webhook] {{site}}');
		await card.getByLabel('正文模板', { exact: true }).fill('{{account}} / {{detail}}');
		await expect(card.getByLabel('预览 · 示例数据')).toContainText('[Webhook] 示例站点');
		await card.getByRole('combobox', { name: '通知渠道', exact: true }).click();
		await page.getByRole('option', { name: 'Bark', exact: true }).click();
		await expect(card.getByLabel('标题模板', { exact: true })).toHaveValue('');
		await card.getByLabel('Bark 推送地址').fill('https://bark.example/device');
		await card.getByLabel('正文模板', { exact: true }).fill('Bark {{message}}');
		await card.getByRole('button', { name: '测试此渠道', exact: true }).click();
		await expect(page.getByText('Bark 测试通知已发送', { exact: true })).toBeVisible();
		const probe = state.mutations.find(item => item.path === '/api/v1/setting/notification/test')!.body as { config: NotificationConfig };
		expect(probe.config.templates).toEqual({ bark: { body: 'Bark {{message}}' } });
		expect(probe.config.webhook_url).toBeUndefined();
		expect(settings[0].value).toBe('');
		await card.getByRole('button', { name: '保存通知渠道', exact: true }).click();
		await expect.poll(() => settings[0].value).not.toBe('');
		expect(JSON.parse(settings[0].value).templates).toEqual({ webhook: { title: '[Webhook] {{site}}', body: '{{account}} / {{detail}}' }, bark: { body: 'Bark {{message}}' } });
		card = await openChannels(page);
		await expect(card.getByLabel('标题模板', { exact: true })).toHaveValue('[Webhook] {{site}}');
		await card.getByLabel('正文模板', { exact: true }).fill('{{unknown}}');
		await expect(card.getByRole('alert')).toBeVisible();
		await expect(card.getByRole('button', { name: '保存通知渠道', exact: true })).toBeDisabled();
		await expect(card.getByRole('button', { name: '测试此渠道', exact: true })).toBeDisabled();
		await card.getByRole('button', { name: '恢复默认', exact: true }).click();
		await expect(card.getByLabel('正文模板', { exact: true })).toHaveValue('');
		await card.getByRole('button', { name: '保存通知渠道', exact: true }).click();
		await expect.poll(() => JSON.parse(settings[0].value).templates.webhook).toBeUndefined();
		expect(JSON.parse(settings[0].value).templates.bark.body).toBe('Bark {{message}}');
		const bounds = await card.boundingBox();
		expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width + 1);
		expect(state.pageErrors).toEqual([]);
		expect(state.unexpectedRequests).toEqual([]);
	});

    test(`configure and test all notification channels at ${width}px`, async ({ page }, testInfo) => {
        await page.setViewportSize({ width, height: 1000 });
        const settings = initialSettings();
        const state = await mockApp(page, 'setting', { settings, mutate: request => {
            if (request.path === '/api/v1/setting/set') {
                const body = request.body as Setting;
                settings.find(item => item.key === body.key)!.value = body.value;
                return { data: body };
            }
            if (request.path === '/api/v1/setting/notification/test') {
                return { data: { channel: 'smtp', success: true } };
            }
            return { status: 500, message: 'Unexpected mutation' };
        } });
        const card = await openChannels(page);
        await expect(card.getByLabel('Webhook 地址', { exact: true })).toHaveValue('https://legacy.example/hook');

        async function selectChannel(name: string) {
            await card.getByRole('combobox', { name: '通知渠道', exact: true }).click();
            await page.getByRole('option', { name, exact: true }).click();
        }
        await selectChannel('Bark');
        await card.getByLabel('Bark 推送地址').fill('https://bark.example/device-key');
        // An unrelated autosave refetches the settings list while this draft is dirty.
        await page.getByRole('switch', { name: '启用签到结果通知' }).click();
        await expect.poll(() => settings[2].value).toBe('true');
        await expect(card.getByLabel('Bark 推送地址')).toHaveValue('https://bark.example/device-key');
        await selectChannel('Server酱');
        await card.getByLabel('Server酱 SendKey').fill('SCTtest');
        await selectChannel('Telegram');
        await card.getByLabel('Telegram Bot Token').fill('123:test-token');
        await expect(card.getByLabel('Telegram Bot Token')).toHaveAttribute('type', 'password');
        await card.getByLabel('Telegram Chat ID').fill('-100123');
        await selectChannel('SMTP 邮件');
        await card.getByLabel('SMTP 服务器').fill('smtp.example.com');
        await card.getByLabel('SMTP 端口').fill('465');
        await card.getByRole('combobox', { name: 'SMTP 安全连接' }).click();
        await page.getByRole('option', { name: 'TLS', exact: true }).click();
        await card.getByLabel('SMTP 用户名').fill('sender@example.com');
        await card.getByLabel('SMTP 密码或授权码').fill('smtp-test-password');
        await card.getByLabel('发件人地址').fill('Octopus <sender@example.com>');
        await card.getByLabel('收件人地址').fill('a@example.com, b@example.com');
        await card.getByRole('button', { name: '测试此渠道', exact: true }).click();
        await expect(page.getByText('SMTP 邮件 测试通知已发送', { exact: true })).toBeVisible();
        const probe = state.mutations.find(item => item.path === '/api/v1/setting/notification/test')!.body as { channel: string; config: NotificationConfig };
        expect(probe.channel).toBe('smtp');
        expect(probe.config.smtp_host).toBe('smtp.example.com');
        expect(probe.config.webhook_url).toBeUndefined();
        expect(settings[0].value).toBe('');
        await card.getByRole('button', { name: '保存通知渠道', exact: true }).click();
        await expect.poll(() => settings[0].value).not.toBe('');
        expect(JSON.parse(settings[0].value)).toEqual({
            webhook_url: 'https://legacy.example/hook', bark_url: 'https://bark.example/device-key',
            serverchan_key: 'SCTtest', telegram_bot_token: '123:test-token', telegram_chat_id: '-100123',
            smtp_host: 'smtp.example.com', smtp_port: 465, smtp_tls: 'tls', smtp_user: 'sender@example.com',
            smtp_password: 'smtp-test-password', smtp_from: 'Octopus <sender@example.com>', smtp_to: 'a@example.com, b@example.com',
        });
        await expect(card.getByRole('button', { name: '保存通知渠道' })).toBeDisabled();
        const bounds = await card.boundingBox();
        expect(bounds!.x).toBeGreaterThanOrEqual(0);
        expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width + 1);
        await card.screenshot({ path: testInfo.outputPath('notification-channels.png') });

        await selectChannel('Webhook');
        await card.getByRole('button', { name: '清空此渠道' }).click();
        await card.getByRole('button', { name: '保存通知渠道' }).click();
        await expect.poll(() => (JSON.parse(settings[0].value) as NotificationConfig).webhook_url).toBeUndefined();
        const reloaded = await openChannels(page);
        await expect(reloaded.getByLabel('Webhook 地址', { exact: true })).toHaveValue('');
        await reloaded.getByRole('combobox', { name: '通知渠道', exact: true }).click();
        await page.getByRole('option', { name: 'Telegram', exact: true }).click();
        await expect(reloaded.getByLabel('Telegram Chat ID')).toHaveValue('-100123');
        expect(state.unexpectedRequests).toEqual([]);
        expect(state.pageErrors).toEqual([]);
    });
}

test('notification configuration errors preserve the draft and explicit empty config disables legacy fallback', async ({ page }) => {
    const settings = initialSettings();
    settings[0].value = '{}';
    const state = await mockApp(page, 'setting', { settings, mutate: request => {
        if (request.path === '/api/v1/setting/set') return { status: 400, message: 'Invalid webhook URL' };
        if (request.path === '/api/v1/setting/notification/test') return { status: 502, message: 'Notification request failed' };
        return { status: 500, message: 'Unexpected mutation' };
    } });
    const card = await openChannels(page);
    const url = card.getByLabel('Webhook 地址', { exact: true });
    await expect(url).toHaveValue('');
    await url.fill('not-a-url');
    await card.getByRole('button', { name: '保存通知渠道' }).click();
    await expect(page.getByText('Invalid webhook URL', { exact: true })).toBeVisible();
    await expect(url).toHaveValue('not-a-url');
    expect(settings[0].value).toBe('{}');
    await url.fill('https://draft.example/hook');
    await card.getByRole('button', { name: '测试此渠道' }).click();
    await expect(page.getByText('Webhook 测试发送失败', { exact: true })).toBeVisible();
    await expect(url).toHaveValue('https://draft.example/hook');
    expect(settings[0].value).toBe('{}');
    expect(state.unexpectedRequests).toEqual([]);
    expect(state.pageErrors).toEqual([]);
});
