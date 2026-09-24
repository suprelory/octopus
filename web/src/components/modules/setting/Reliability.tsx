'use client';

import { useTranslations } from 'next-intl';
import { CircleAlert, Hash, Route, ShieldCheck, Timer, TimerOff, type LucideIcon } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
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
	const affinityMode = useSettingField('channel_affinity_mode');
	const affinitySource = useSettingField('channel_affinity_source');
	const affinityHeader = useSettingField('channel_affinity_header');

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
				<div className="space-y-5">
					{[
						{field: affinityMode, name: 'mode', values: ['off', 'prefer', 'strict']},
						{field: affinitySource, name: 'source', values: ['auto', 'header', 'session_id', 'prompt_cache_key', 'api_key']},
					].map(({field, name, values}) => (
						<SettingRow key={name} label={t(`channelAffinity.${name}.label`)} tooltip={t(`channelAffinity.${name}.description`)}>
							<Select value={field.value} onValueChange={field.commit}>
								<SelectTrigger className={`${SETTING_CONTROL_WIDTH} rounded-xl`}><SelectValue /></SelectTrigger>
								<SelectContent>{values.map((value) => <SelectItem key={value} value={value}>{t(`channelAffinity.${name}.${value}`)}</SelectItem>)}</SelectContent>
							</Select>
						</SettingRow>
					))}
					{['auto', 'header'].includes(affinitySource.value) && <SettingRow label={t('channelAffinity.header')}>
						<Input value={affinityHeader.value} onChange={(e) => affinityHeader.setValue(e.target.value)} onBlur={affinityHeader.save} className={`${SETTING_CONTROL_WIDTH} rounded-xl`} />
					</SettingRow>}
                <NumberFieldRow
                    settingKey={SettingKey.ChannelAffinityTTLSeconds}
                    label={t('channelAffinity.ttl.label')}
                    placeholder={t('channelAffinity.ttl.placeholder')}
                    tooltip={t('channelAffinity.ttl.description')}
                    icon={Timer}
                    min={1}
                />
				</div>
            )}

            {/* HTTP/WS 故障转移预算 */}
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
			{(['images', 'compact'] as const).map((operation) => (
				<div key={operation} className="space-y-5">
					<SettingSection title={t(`relayBudget.${operation}`)} tooltip={t('relayBudget.inherit')} />
					{[
						['max_channel_attempts', 'maxChannels', 64],
						['max_total_attempts', 'maxAttempts', 256],
						['timeout_seconds', 'timeout', 3600],
					].map(([suffix, label, max]) => (
						<NumberFieldRow key={suffix} settingKey={`relay_${operation}_${suffix}`}
							label={t(`relayBudget.${label}.label`)} placeholder="0" min={0} max={Number(max)} />
					))}
				</div>
			))}
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
