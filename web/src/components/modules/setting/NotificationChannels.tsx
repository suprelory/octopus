'use client';

import { useEffect, useRef, useState } from 'react';
import { Send } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    SettingKey, useSettingList, useSetSetting, useTestNotificationChannel,
    type NotificationChannel, type NotificationConfig,
} from '@/api/endpoints/setting';
import type { ApiError } from '@/api/types';
import { toast } from '@/components/common/Toast';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { SettingCard } from './shared';

const CHANNELS: NotificationChannel[] = ['webhook', 'bark', 'serverchan', 'telegram', 'smtp'];
const FIELDS = {
    webhook: ['webhook_url'],
    bark: ['bark_url'],
    serverchan: ['serverchan_key'],
    telegram: ['telegram_bot_token', 'telegram_chat_id'],
    smtp: ['smtp_host', 'smtp_port', 'smtp_user', 'smtp_password', 'smtp_from', 'smtp_to', 'smtp_tls'],
} as const;

function compact(config: NotificationConfig): NotificationConfig {
    return Object.fromEntries(Object.entries(config).filter(([, value]) => value !== '' && value !== 0 && value !== undefined));
}

function readConfig(raw: string, legacyWebhook: string): NotificationConfig {
    if (!raw.trim()) return legacyWebhook ? { webhook_url: legacyWebhook } : {};
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('Invalid configuration');
    const knownFields: readonly string[] = Object.values(FIELDS).flat();
    for (const [key, value] of Object.entries(parsed)) {
        if (!knownFields.includes(key) || (key === 'smtp_port' ? typeof value !== 'number' : typeof value !== 'string')) {
            throw new Error('Invalid configuration');
        }
    }
    return parsed as NotificationConfig;
}

function isConfigured(config: NotificationConfig, channel: NotificationChannel) {
    switch (channel) {
        case 'webhook': return !!config.webhook_url?.trim();
        case 'bark': return !!config.bark_url?.trim();
        case 'serverchan': return !!config.serverchan_key?.trim();
        case 'telegram': return !!config.telegram_bot_token?.trim() && !!config.telegram_chat_id?.trim();
        case 'smtp': return !!config.smtp_host?.trim() && !!config.smtp_from?.trim() && !!config.smtp_to?.trim();
    }
}

export function SettingNotificationChannels() {
    const t = useTranslations('notificationChannels');
    const settingT = useTranslations('setting');
    const { data: settings, isError } = useSettingList();
    const saveSetting = useSetSetting();
    const testChannel = useTestNotificationChannel();
    const [channel, setChannel] = useState<NotificationChannel>('webhook');
    const [draft, setDraft] = useState<NotificationConfig>({});
    const [saved, setSaved] = useState<string | null>(null);
    const [invalidConfig, setInvalidConfig] = useState(false);
    const initialized = useRef(false);

    useEffect(() => {
        if (!settings || initialized.current) return;
        initialized.current = true;
        const raw = settings.find(item => item.key === SettingKey.NotificationChannels)?.value ?? '';
        const legacy = settings.find(item => item.key === SettingKey.CheckinNotifyWebhookURL)?.value ?? '';
        let config: NotificationConfig = {};
        let invalid = false;
        try { config = readConfig(raw, legacy); } catch { invalid = true; }
        queueMicrotask(() => {
            setDraft(config);
            setSaved(JSON.stringify(compact(config)));
            setInvalidConfig(invalid);
        });
    }, [settings]);

    const busy = saveSetting.isPending || testChannel.isPending;
    const unavailable = saved === null || busy;
    const serialized = JSON.stringify(compact(draft));
    const configured = CHANNELS.filter(item => isConfigured(draft, item)).map(item => t(item));

    async function save() {
        try {
            await saveSetting.mutateAsync({ key: SettingKey.NotificationChannels, value: serialized });
            setSaved(serialized);
            setInvalidConfig(false);
            toast.success(settingT('saved'));
        } catch (error) {
            toast.error(settingT('saveFailed'), { description: (error as ApiError)?.message });
        }
    }

    async function test() {
        const config = Object.fromEntries(FIELDS[channel].map(key => [key, draft[key]]));
        try {
            await testChannel.mutateAsync({ channel, config: compact(config) });
            toast.success(t('testSuccess', { channel: t(channel) }));
        } catch (error) {
            toast.error(t('testFailed', { channel: t(channel) }), { description: (error as ApiError)?.message });
        }
    }

    function clearChannel() {
        setDraft(current => {
            const next = { ...current };
            for (const key of FIELDS[channel]) delete next[key];
            return next;
        });
    }

    function field(key: Exclude<keyof NotificationConfig, 'smtp_port' | 'smtp_tls'>, label: string, placeholder: string, type = 'text') {
        return <label className="grid min-w-0 gap-2 text-sm font-medium">
            {label}
            <Input type={type} value={draft[key] ?? ''} placeholder={placeholder} disabled={unavailable}
                autoComplete={type === 'password' ? 'new-password' : 'off'} spellCheck={false}
                onChange={event => setDraft(current => ({ ...current, [key]: event.target.value }))} />
        </label>;
    }

    return (
        <SettingCard icon={Send} title={t('title')}>
            <p className="text-sm text-muted-foreground">{t('description')}</p>
            {isError || invalidConfig ? <p role="alert" className="text-sm text-destructive">{t(isError ? 'loadFailed' : 'invalidConfig')}</p> : null}
            <label className="grid gap-2 text-sm font-medium">
                {t('channel')}
                <Select value={channel} onValueChange={value => setChannel(value as NotificationChannel)} disabled={unavailable}>
                    <SelectTrigger aria-label={t('channel')} className="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>{CHANNELS.map(item => <SelectItem key={item} value={item}>{t(item)}</SelectItem>)}</SelectContent>
                </Select>
            </label>
            <div className="space-y-4 rounded-xl border border-border/60 p-4">
                {channel === 'webhook' ? field('webhook_url', t('webhookURL'), 'https://example.com/webhook', 'url') : null}
                {channel === 'bark' ? <>
                    {field('bark_url', t('barkURL'), 'https://api.day.app/your-device-key', 'url')}
                    <p className="text-xs text-muted-foreground">{t('barkHint')}</p>
                </> : null}
                {channel === 'serverchan' ? field('serverchan_key', t('serverchanKey'), 'SCT…', 'password') : null}
                {channel === 'telegram' ? <>
                    {field('telegram_bot_token', t('telegramToken'), '123456:ABC…', 'password')}
                    {field('telegram_chat_id', t('telegramChatID'), '-1001234567890')}
                </> : null}
                {channel === 'smtp' ? <>
                    <div className="grid gap-4 sm:grid-cols-2">
                        {field('smtp_host', t('smtpHost'), 'smtp.example.com')}
                        <label className="grid gap-2 text-sm font-medium">
                            {t('smtpPort')}
                            <Input type="number" min="1" max="65535" step="1" placeholder="587" disabled={unavailable}
                                value={draft.smtp_port || ''} onChange={event => setDraft(current => ({ ...current, smtp_port: Number(event.target.value) }))} />
                        </label>
                    </div>
                    <label className="grid gap-2 text-sm font-medium">
                        {t('smtpSecurity')}
                        <Select value={draft.smtp_tls || 'auto'} disabled={unavailable}
                            onValueChange={value => setDraft(current => ({ ...current, smtp_tls: value === 'auto' ? '' : value as NotificationConfig['smtp_tls'] }))}>
                            <SelectTrigger aria-label={t('smtpSecurity')} className="w-full"><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value="auto">{t('smtpAuto')}</SelectItem>
                                <SelectItem value="starttls">STARTTLS</SelectItem>
                                <SelectItem value="tls">TLS</SelectItem>
                                <SelectItem value="none">{t('smtpNone')}</SelectItem>
                            </SelectContent>
                        </Select>
                    </label>
                    {field('smtp_user', t('smtpUser'), 'user@example.com')}
                    {field('smtp_password', t('smtpPassword'), '', 'password')}
                    {field('smtp_from', t('smtpFrom'), 'Octopus <alerts@example.com>')}
                    {field('smtp_to', t('smtpTo'), 'admin@example.com, team@example.com')}
                    <p className="text-xs text-muted-foreground">{t('smtpHint')}</p>
                </> : null}
                <div className="flex flex-wrap items-center gap-2">
                    <Button type="button" variant="outline" size="sm" disabled={unavailable || !isConfigured(draft, channel)} onClick={test}>
                        {testChannel.isPending ? t('testing') : t('test')}
                    </Button>
                    <Button type="button" variant="ghost" size="sm" disabled={unavailable} onClick={clearChannel}>{t('clear')}</Button>
                </div>
            </div>
            <p className="text-xs text-muted-foreground">{configured.length ? t('configured', { channels: configured.join(' / ') }) : t('noneConfigured')}</p>
            <div className="flex flex-wrap items-center gap-3">
                <Button type="button" disabled={unavailable || (!invalidConfig && serialized === saved)} onClick={save}>{t('save')}</Button>
                <p className="text-xs text-muted-foreground">{t('saveHint')}</p>
            </div>
        </SettingCard>
    );
}
