'use client';

import { BellRing } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { SettingKey } from '@/api/endpoints/setting';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { SettingCard, SettingRow, useSettingField, useSettingToggle } from './shared';

export function SettingCheckinNotifications() {
    const t = useTranslations('checkinNotifications');
    const enabled = useSettingToggle(SettingKey.CheckinNotifyEnabled);
    const webhook = useSettingField(SettingKey.CheckinNotifyWebhookURL);
    const cooldown = useSettingField(SettingKey.CheckinNotifyCooldownSeconds);
    const threshold = useSettingField(SettingKey.CheckinLowBalanceThreshold);

    return (
        <SettingCard icon={BellRing} title={t('title')}>
            <p className="text-sm text-muted-foreground">{t('description')}</p>
            <SettingRow label={t('enabled')}>
                <Switch aria-label={t('enabled')} checked={enabled.enabled} onCheckedChange={enabled.toggle} />
            </SettingRow>
            <label className="grid gap-2 text-sm font-medium">
                {t('webhook')}
                <Input type="url" autoComplete="off" spellCheck={false} value={webhook.value}
                    placeholder="https://example.com/webhook" onChange={event => webhook.setValue(event.target.value)} onBlur={webhook.save} />
            </label>
            <SettingRow label={t('cooldown')} tooltip={t('cooldownHint')}>
                <Input aria-label={t('cooldown')} type="number" min="0" max="604800" step="1" className="w-32"
                    value={cooldown.value} onChange={event => cooldown.setValue(event.target.value)} onBlur={cooldown.save} />
            </SettingRow>
            <SettingRow label={t('threshold')} tooltip={t('thresholdHint')}>
                <Input aria-label={t('threshold')} type="number" min="0" step="any" className="w-32"
                    value={threshold.value} onChange={event => threshold.setValue(event.target.value)} onBlur={threshold.save} />
            </SettingRow>
            <p className="text-xs text-muted-foreground">{t('deliveryHint')}</p>
        </SettingCard>
    );
}
