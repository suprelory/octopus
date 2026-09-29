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
    const success = useSettingToggle(SettingKey.CheckinNotifySuccessEnabled);
    const manual = useSettingToggle(SettingKey.CheckinNotifyManualEnabled);
    const cooldown = useSettingField(SettingKey.CheckinNotifyCooldownSeconds);
    const threshold = useSettingField(SettingKey.CheckinLowBalanceThreshold);

    return (
        <SettingCard icon={BellRing} title={t('title')}>
            <p className="text-sm text-muted-foreground">{t('description')}</p>
            <SettingRow label={t('enabled')}>
                <Switch aria-label={t('enabled')} checked={enabled.enabled} onCheckedChange={enabled.toggle} />
            </SettingRow>
            <SettingRow label={t('success')} tooltip={t('successHint')}>
                <Switch aria-label={t('success')} checked={success.enabled} onCheckedChange={success.toggle} />
            </SettingRow>
            <SettingRow label={t('manual')} tooltip={t('manualHint')}>
                <Switch aria-label={t('manual')} checked={manual.enabled} onCheckedChange={manual.toggle} />
            </SettingRow>
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
