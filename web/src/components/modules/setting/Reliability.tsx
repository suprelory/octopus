'use client';

import { useTranslations } from 'next-intl';
import { CircleAlert, Hash, HeartPulse, Route, ShieldCheck, Timer, TimerOff, type LucideIcon } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { SettingKey } from '@/api/endpoints/setting';
import { SettingCard, SettingRow, SettingSection, useSettingField, useSettingToggle } from './shared';

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
                className="w-48 rounded-xl"
            />
        </SettingRow>
    );
}

export function SettingReliability() {
    const t = useTranslations('setting');
    const groupHealth = useSettingToggle(SettingKey.GroupHealthEnabled);
    const channelAffinity = useSettingToggle(SettingKey.ChannelAffinityEnabled);
    const emptyResponseDetection = useSettingToggle(SettingKey.EmptyResponseDetectionEnabled);

    return (
        <SettingCard icon={ShieldCheck} title={t('reliability.title')}>
            {/* 分组健康检查 */}
            <SettingRow icon={HeartPulse} label={t('groupHealth.label')} tooltip={t('groupHealth.description')}>
                <Switch checked={groupHealth.enabled} onCheckedChange={groupHealth.toggle} />
            </SettingRow>

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
