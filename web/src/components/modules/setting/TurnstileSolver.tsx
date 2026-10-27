'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Eye, EyeOff, ShieldCheck } from 'lucide-react';
import { SettingKey } from '@/api/endpoints/setting';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { SETTING_CONTROL_WIDTH, SettingCard, SettingRow, useSettingField, useSettingToggle } from './shared';

// Listed in fallback order; the backend tries Cloudflare Solver first.
const PLATFORMS = [
    { name: 'Cloudflare Solver', enabledKey: SettingKey.TurnstileCloudflareSolverEnabled, apiKeyKey: SettingKey.TurnstileCloudflareSolverAPIKey },
    { name: 'SolverQ', enabledKey: SettingKey.TurnstileSolverQEnabled, apiKeyKey: SettingKey.TurnstileSolverQAPIKey },
] as const;

export function SettingTurnstileSolver() {
    const t = useTranslations('turnstileSolver');
    const enabled = useSettingToggle(SettingKey.TurnstileSolverEnabled);

    return (
        <SettingCard icon={ShieldCheck} title={t('title')}>
            <p className="text-sm text-muted-foreground">{t('description')}</p>
            <SettingRow label={t('enabled')} tooltip={t('enabledHint')}>
                <Switch aria-label={t('enabled')} checked={enabled.enabled} onCheckedChange={enabled.toggle} />
            </SettingRow>
            {PLATFORMS.map(platform => <SolverPlatform key={platform.name} {...platform} />)}
            <p className="text-xs text-muted-foreground">{t('orderHint')}</p>
        </SettingCard>
    );
}

function SolverPlatform({ name, enabledKey, apiKeyKey }: { name: string; enabledKey: string; apiKeyKey: string }) {
    const t = useTranslations('turnstileSolver');
    const enabled = useSettingToggle(enabledKey);
    const apiKey = useSettingField(apiKeyKey);
    const [visible, setVisible] = useState(false);
    const enabledLabel = t('platformEnabled', { name });
    const apiKeyLabel = t('platformAPIKey', { name });

    return (
        <>
            <SettingRow label={enabledLabel}>
                <Switch aria-label={enabledLabel} checked={enabled.enabled} onCheckedChange={enabled.toggle} />
            </SettingRow>
            <SettingRow label={apiKeyLabel}>
                <div className={`relative ${SETTING_CONTROL_WIDTH}`}>
                    <Input aria-label={apiKeyLabel} type={visible ? 'text' : 'password'} autoComplete="off" spellCheck={false}
                        className="rounded-xl pr-10" value={apiKey.value}
                        onChange={event => apiKey.setValue(event.target.value)} onBlur={apiKey.save} />
                    <button type="button" aria-label={visible ? t('hideAPIKey') : t('showAPIKey')} onClick={() => setVisible(value => !value)}
                        className="absolute right-2 top-1/2 -translate-y-1/2 rounded text-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring">
                        {visible ? <EyeOff aria-hidden className="size-4" /> : <Eye aria-hidden className="size-4" />}
                    </button>
                </div>
            </SettingRow>
        </>
    );
}
