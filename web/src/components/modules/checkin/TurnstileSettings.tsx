import { useState, type FormEvent } from 'react';
import { useTranslations } from 'use-intl';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { type TurnstileSettings, useCheckinMutation, useCheckinQuery } from '@/api/checkin';
import { CheckinError, CheckinField, CheckinToggle } from './common';

function TurnstileForm({
    settings,
    onClose,
}: {
    settings: TurnstileSettings;
    onClose: () => void;
}) {
    const t = useTranslations('checkin');
    const [config, setConfig] = useState(settings.config);
    const [keys, setKeys] = useState({ cloudflare_solver: '', solverq: '' });
    const [clear, setClear] = useState({ cloudflare_solver: false, solverq: false });
    const save = useCheckinMutation();
    const providers = [
        { id: 'cloudflare_solver', name: 'Cloudflare Solver', hasKey: settings.has_cloudflare_key },
        { id: 'solverq', name: 'SolverQ', hasKey: settings.has_solverq_key },
    ] as const;

    function submit(event: FormEvent) {
        event.preventDefault();
        save.mutate(
            {
                path: '/turnstile',
                method: 'PATCH',
                body: {
                    config,
                    revision: settings.revision,
                    cloudflare_key: clear.cloudflare_solver
                        ? ''
                        : keys.cloudflare_solver.trim() || undefined,
                    solverq_key: clear.solverq ? '' : keys.solverq.trim() || undefined,
                },
            },
            { onSuccess: onClose },
        );
    }

    return (
        <form onSubmit={submit} className="space-y-5">
            <p className="text-sm text-muted-foreground">{t('turnstile.description')}</p>
            <fieldset disabled={save.isPending} className="space-y-5">
                <CheckinToggle
                    label={t('turnstile.enabled')}
                    checked={config.enabled}
                    onChange={(enabled) => setConfig({ ...config, enabled })}
                />
                <p className="text-xs text-muted-foreground">{t('turnstile.order')}</p>
                {providers.map(({ id, name, hasKey }) => {
                    const provider = config[id];
                    const endpointChanged =
                        provider.base_url.trim().replace(/\/+$/, '') !==
                        settings.config[id].base_url;
                    return (
                        <fieldset key={id} className="space-y-3 rounded-2xl border p-4">
                            <legend className="px-1 text-sm font-semibold">{name}</legend>
                            <CheckinToggle
                                label={t('turnstile.providerEnabled', { name })}
                                checked={provider.enabled}
                                onChange={(enabled) =>
                                    setConfig({ ...config, [id]: { ...provider, enabled } })
                                }
                            />
                            <CheckinField
                                label={t('turnstile.endpoint', { name })}
                                hint={t('turnstile.endpointHint')}
                            >
                                <Input
                                    type="url"
                                    value={provider.base_url}
                                    maxLength={2048}
                                    onChange={(event) =>
                                        setConfig({
                                            ...config,
                                            [id]: { ...provider, base_url: event.target.value },
                                        })
                                    }
                                />
                            </CheckinField>
                            <CheckinField
                                label={t('turnstile.apiKey', { name })}
                                hint={t(hasKey ? 'turnstile.keySaved' : 'turnstile.keyHint')}
                            >
                                <Input
                                    type="password"
                                    autoComplete="new-password"
                                    spellCheck={false}
                                    value={keys[id]}
                                    disabled={clear[id]}
                                    maxLength={4096}
                                    required={
                                        !clear[id] &&
                                        ((config.enabled && provider.enabled && !hasKey) ||
                                            (hasKey && endpointChanged))
                                    }
                                    onChange={(event) =>
                                        setKeys({ ...keys, [id]: event.target.value })
                                    }
                                />
                            </CheckinField>
                            {hasKey && (
                                <CheckinToggle
                                    label={t('turnstile.clearKey', { name })}
                                    checked={clear[id]}
                                    onChange={(value) => setClear({ ...clear, [id]: value })}
                                />
                            )}
                        </fieldset>
                    );
                })}
            </fieldset>
            <p className="text-xs text-muted-foreground">{t('turnstile.scopeHint')}</p>
            <CheckinError error={save.error} />
            <div className="flex justify-end">
                <Button type="submit" disabled={save.isPending}>
                    {t('save')}
                </Button>
            </div>
        </form>
    );
}

export function TurnstileSettingsForm({ onClose }: { onClose: () => void }) {
    const t = useTranslations('checkin');
    const query = useCheckinQuery<TurnstileSettings>('/turnstile');
    return (
        <>
            <CheckinError error={query.error} />
            {query.data ? (
                <TurnstileForm key={query.data.revision} settings={query.data} onClose={onClose} />
            ) : (
                <p>{t('loading')}</p>
            )}
        </>
    );
}
