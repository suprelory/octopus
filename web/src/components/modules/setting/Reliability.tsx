'use client';

import { useTranslations } from 'next-intl';
import { CircleAlert, Hash, Route, ShieldCheck, Timer, TimerOff, type LucideIcon } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { SettingKey } from '@/api/endpoints/setting';
import { SETTING_CONTROL_WIDTH, SettingCard, SettingRow, SettingSection, useSettingField, useSettingToggle } from './shared';

function NumberFieldRow({ settingKey, label, placeholder, tooltip, icon, min, max }: {
    settingKey: string;
    label: string;
    placeholder: string;
    tooltip?: React.ReactNode;
    icon?: LucideIcon;
    min?: number;
    max?: number;
}) {
    const field = useSettingField(settingKey);
    return (
        <SettingRow icon={icon} label={label} tooltip={tooltip}>
            <Input
                type="number"
                step={1}
                min={min}
                max={max}
                value={field.value}
                onChange={(e) => field.setValue(e.target.value)}
                onBlur={field.save}
                placeholder={placeholder}
                className={`${SETTING_CONTROL_WIDTH} rounded-xl`}
            />
        </SettingRow>
    );
}

export function SettingReliability() {
    const t = useTranslations('setting');
    const channelAffinity = useSettingToggle(SettingKey.ChannelAffinityEnabled);
    const emptyResponseDetection = useSettingToggle(SettingKey.EmptyResponseDetectionEnabled);

    return (
        <SettingCard icon={ShieldCheck} title={t('reliability.title')}>
            {/* 空回检测 */}
            <SettingRow icon={CircleAlert} label={t('emptyResponseDetection.label')} tooltip={t('emptyResponseDetection.description')}>
                <Switch checked={emptyResponseDetection.enabled} onCheckedChange={emptyResponseDetection.toggle} />
            </SettingRow>

            {/* 渠道亲和 */}
            <SettingSection title={t('channelAffinity.title')} tooltip={t('channelAffinity.description')} />
            <SettingRow icon={Route} label={t('channelAffinity.enabled.label')} tooltip={t('channelAffinity.enabled.description')}>
                <Switch checked={channelAffinity.enabled} onCheckedChange={channelAffinity.toggle} />
            </SettingRow>
            {channelAffinity.enabled && (
                <NumberFieldRow
                    settingKey={SettingKey.ChannelAffinityTTLSeconds}
                    label={t('channelAffinity.ttl.label')}
                    placeholder={t('channelAffinity.ttl.placeholder')}
                    tooltip={t('channelAffinity.ttl.description')}
                    icon={Timer}
                    min={1}
                />
            )}

            {/* HTTP 故障转移预算 */}
            <SettingSection title={t('relayBudget.title')} tooltip={t('relayBudget.description')} />
            <NumberFieldRow
                settingKey={SettingKey.RelayMaxChannelAttempts}
                label={t('relayBudget.maxChannels.label')}
                placeholder={t('relayBudget.maxChannels.placeholder')}
                tooltip={t('relayBudget.maxChannels.description')}
                icon={Route}
                min={1}
                max={64}
            />
            <NumberFieldRow
                settingKey={SettingKey.RelayMaxTotalAttempts}
                label={t('relayBudget.maxAttempts.label')}
                placeholder={t('relayBudget.maxAttempts.placeholder')}
                tooltip={t('relayBudget.maxAttempts.description')}
                icon={Hash}
                min={1}
                max={256}
            />
            <NumberFieldRow
                settingKey={SettingKey.RelayFailoverTimeoutSeconds}
                label={t('relayBudget.timeout.label')}
                placeholder={t('relayBudget.timeout.placeholder')}
                tooltip={t('relayBudget.timeout.description')}
                icon={Timer}
                min={1}
                max={3600}
            />

            {/* 熔断器 */}
            <SettingSection title={t('circuitBreaker.title')} tooltip={t('circuitBreaker.hint')} />
            <NumberFieldRow
                settingKey={SettingKey.CircuitBreakerThreshold}
                label={t('circuitBreaker.threshold.label')}
                placeholder={t('circuitBreaker.threshold.placeholder')}
                icon={Hash}
            />
            <NumberFieldRow
                settingKey={SettingKey.CircuitBreakerCooldown}
                label={t('circuitBreaker.cooldown.label')}
                placeholder={t('circuitBreaker.cooldown.placeholder')}
                icon={Timer}
            />
            <NumberFieldRow
                settingKey={SettingKey.CircuitBreakerMaxCooldown}
                label={t('circuitBreaker.maxCooldown.label')}
                placeholder={t('circuitBreaker.maxCooldown.placeholder')}
                icon={TimerOff}
            />
        </SettingCard>
    );
}
