import { useState, type FormEvent } from 'react';
import { useLocale, useTranslations } from 'use-intl';
import {
    notificationEvents,
    notificationKinds,
    notificationThreshold,
    rewardText,
    type CheckinSite,
    type NotificationConfig,
    type NotificationConnection,
    type NotificationKind,
    type NotificationTarget,
    useCheckinMutation,
} from '@/api/checkin';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ProxySelector } from '../proxy-pool/ProxySelector';
import {
    CheckinDialog,
    CheckinError,
    CheckinField,
    CheckinSelect,
    CheckinTextarea,
    CheckinToggle,
} from './common';
import { NotificationConnectionFields } from './NotificationConnectionFields';

export function NotificationForm({
    target,
    sites,
    onClose,
}: {
    target?: NotificationTarget;
    sites: CheckinSite[];
    onClose: () => void;
}) {
    const t = useTranslations('checkin.notifications');
    const common = useTranslations('checkin');
    const locale = useLocale();
    const [config, setConfig] = useState<NotificationConfig>(
        target?.config ?? {
            name: '',
            kind: 'webhook',
            enabled: false,
            events: ['failed', 'attention'],
            include_manual: true,
            scope: 'all',
            site_ids: [],
            cooldown_seconds: 3600,
            threshold_unit: 'USD',
            language: locale,
            title_template: '',
            body_template: '',
            proxy_mode: 'direct',
        },
    );
    const [connection, setConnection] = useState<NotificationConnection>({});
    const [replaceConnection, setReplaceConnection] = useState(!target);
    const [headers, setHeaders] = useState('');
    const [threshold, setThreshold] = useState(
        target?.config.threshold_micros !== undefined
            ? rewardText(target.config.threshold_micros)
            : '5',
    );
    const [error, setError] = useState<unknown>();
    const save = useCheckinMutation();
    const preview = useCheckinMutation<{ title: string; message: string }>();
    function update<K extends keyof NotificationConfig>(key: K, value: NotificationConfig[K]) {
        setConfig((previous) => ({ ...previous, [key]: value }));
        if (key === 'title_template' || key === 'body_template' || key === 'language')
            preview.reset();
    }
    async function submit(event: FormEvent) {
        event.preventDefault();
        setError(undefined);
        try {
            if (!config.events.length) throw new Error(t('selectEvents'));
            if (config.scope === 'sites' && !config.site_ids.length)
                throw new Error(t('selectSites'));
            const amount = notificationThreshold(threshold);
            if (config.events.includes('low_balance') && amount === undefined)
                throw new Error(t('invalidThreshold'));
            let savedConnection: NotificationConnection | undefined;
            if (replaceConnection) {
                savedConnection = { ...connection };
                if (config.kind === 'webhook' && headers.trim()) {
                    let parsed: unknown;
                    try {
                        parsed = JSON.parse(headers);
                    } catch {
                        throw new Error(t('invalidHeaders'));
                    }
                    if (
                        !parsed ||
                        typeof parsed !== 'object' ||
                        Array.isArray(parsed) ||
                        Object.values(parsed).some((value) => typeof value !== 'string')
                    )
                        throw new Error(t('invalidHeaders'));
                    savedConnection.headers = Object.entries(parsed).map(([name, value]) => ({
                        name,
                        value: value as string,
                    }));
                }
                if (savedConnection.smtp)
                    savedConnection.smtp = {
                        ...savedConnection.smtp,
                        to: savedConnection.smtp.to
                            .map((address) => address.trim())
                            .filter(Boolean),
                    };
            }
            await save.mutateAsync({
                path: target ? `/notifications/${target.id}` : '/notifications',
                method: target ? 'PATCH' : 'POST',
                body: {
                    config: {
                        ...config,
                        site_ids: config.scope === 'all' ? [] : config.site_ids,
                        threshold_unit: config.events.includes('low_balance')
                            ? config.threshold_unit || 'USD'
                            : '',
                        threshold_micros: config.events.includes('low_balance')
                            ? amount
                            : undefined,
                    },
                    revision: target?.revision ?? 0,
                    connection: savedConnection,
                },
            });
            onClose();
        } catch (error) {
            setError(error);
        }
    }
    return (
        <CheckinDialog
            title={t(target ? 'edit' : 'add')}
            description={t('formHint')}
            onClose={onClose}
        >
            <form
                onSubmit={(event) => {
                    void submit(event);
                }}
                className="space-y-5"
            >
                <div className="grid gap-4 sm:grid-cols-2">
                    <CheckinField label={t('name')}>
                        <Input
                            required
                            maxLength={128}
                            value={config.name}
                            onChange={(event) => update('name', event.target.value)}
                        />
                    </CheckinField>
                    <CheckinField label={t('platform')}>
                        <CheckinSelect
                            value={config.kind}
                            onChange={(event) => {
                                const kind = event.target.value as NotificationKind;
                                setConfig((previous) => ({
                                    ...previous,
                                    kind,
                                    ...(kind === 'smtp'
                                        ? { proxy_mode: 'direct', proxy_config_id: null }
                                        : {}),
                                }));
                                setConnection({});
                                setHeaders('');
                                setReplaceConnection(true);
                            }}
                        >
                            {notificationKinds.map((kind) => (
                                <option key={kind} value={kind}>
                                    {t(`platforms.${kind}`)}
                                </option>
                            ))}
                        </CheckinSelect>
                    </CheckinField>
                </div>
                <CheckinToggle
                    label={t('enabled')}
                    checked={config.enabled}
                    onChange={(value) => update('enabled', value)}
                />
                <section className="space-y-4 rounded-2xl border p-4">
                    <h3 className="text-sm font-semibold">{t('connection')}</h3>
                    <p className="text-xs text-muted-foreground">
                        {t(target ? 'connectionSavedHint' : 'connectionHint')}
                    </p>
                    {target && config.kind === target.config.kind && (
                        <CheckinToggle
                            label={t('replaceConnection')}
                            checked={replaceConnection}
                            onChange={(value) => {
                                setReplaceConnection(value);
                                setConnection({});
                                setHeaders('');
                            }}
                        />
                    )}
                    {replaceConnection && (
                        <NotificationConnectionFields
                            kind={config.kind}
                            value={connection}
                            onChange={setConnection}
                            headers={headers}
                            onHeaders={setHeaders}
                        />
                    )}
                    {config.kind !== 'smtp' && (
                        <ProxySelector
                            allowCustom={false}
                            value={{
                                proxy_mode: config.proxy_mode,
                                proxy_config_id: config.proxy_config_id,
                            }}
                            onChange={(value) =>
                                setConfig((previous) => ({
                                    ...previous,
                                    proxy_mode: value.proxy_mode,
                                    proxy_config_id: value.proxy_config_id,
                                }))
                            }
                        />
                    )}
                </section>
                <fieldset className="space-y-3 rounded-2xl border p-4">
                    <legend className="px-1 text-sm font-semibold">{t('eventsTitle')}</legend>
                    <div className="grid gap-3 sm:grid-cols-2">
                        {notificationEvents.map((event) => (
                            <CheckinToggle
                                key={event}
                                label={t(`events.${event}`)}
                                checked={config.events.includes(event)}
                                onChange={(enabled) =>
                                    update(
                                        'events',
                                        enabled
                                            ? [...config.events, event]
                                            : config.events.filter((value) => value !== event),
                                    )
                                }
                            />
                        ))}
                    </div>
                    <CheckinToggle
                        label={t('includeManual')}
                        checked={config.include_manual}
                        onChange={(value) => update('include_manual', value)}
                    />
                    {config.events.includes('low_balance') && (
                        <>
                            <div className="grid gap-3 sm:grid-cols-2">
                                <CheckinField label={t('threshold')}>
                                    <Input
                                        required
                                        inputMode="decimal"
                                        maxLength={26}
                                        value={threshold}
                                        onChange={(event) => setThreshold(event.target.value)}
                                    />
                                </CheckinField>
                                <CheckinField label={t('unit')}>
                                    <CheckinSelect
                                        value={config.threshold_unit || 'USD'}
                                        onChange={(event) =>
                                            update('threshold_unit', event.target.value)
                                        }
                                    >
                                        <option value="USD">USD</option>
                                        <option value="quota">quota</option>
                                    </CheckinSelect>
                                </CheckinField>
                            </div>
                            <p className="text-xs text-muted-foreground">{t('balanceHint')}</p>
                        </>
                    )}
                    <CheckinField label={t('cooldown')} hint={t('cooldownHint')}>
                        <Input
                            required
                            type="number"
                            min={0}
                            max={604800}
                            step={1}
                            value={config.cooldown_seconds}
                            onChange={(event) =>
                                update('cooldown_seconds', Number(event.target.value))
                            }
                        />
                    </CheckinField>
                </fieldset>
                <div className="space-y-3">
                    <CheckinField label={t('scope')}>
                        <CheckinSelect
                            value={config.scope}
                            onChange={(event) =>
                                update('scope', event.target.value as 'all' | 'sites')
                            }
                        >
                            <option value="all">{t('allSites')}</option>
                            <option value="sites">{t('chosenSites')}</option>
                        </CheckinSelect>
                    </CheckinField>
                    {config.scope === 'sites' && (
                        <div className="max-h-48 space-y-2 overflow-y-auto rounded-xl border p-3">
                            {sites.map((site) => (
                                <CheckinToggle
                                    key={site.id}
                                    label={site.config.name}
                                    checked={config.site_ids.includes(site.id)}
                                    onChange={(enabled) =>
                                        update(
                                            'site_ids',
                                            enabled
                                                ? [...config.site_ids, site.id]
                                                : config.site_ids.filter((id) => id !== site.id),
                                        )
                                    }
                                />
                            ))}
                            {config.site_ids
                                .filter((id) => !sites.some((site) => site.id === id))
                                .map((id) => (
                                    <CheckinToggle
                                        key={id}
                                        label={t('unavailableSite', { id: id.slice(0, 8) })}
                                        checked
                                        onChange={() =>
                                            update(
                                                'site_ids',
                                                config.site_ids.filter((value) => value !== id),
                                            )
                                        }
                                    />
                                ))}
                            {!sites.length && (
                                <p className="text-xs text-muted-foreground">{t('noSites')}</p>
                            )}
                        </div>
                    )}
                </div>
                <section className="space-y-4 rounded-2xl border p-4">
                    <h3 className="text-sm font-semibold">{t('template')}</h3>
                    <CheckinField label={t('language')}>
                        <CheckinSelect
                            value={config.language}
                            onChange={(event) => update('language', event.target.value)}
                        >
                            <option value="en">English</option>
                            <option value="zh_hans">简体中文</option>
                            <option value="zh_hant">繁體中文</option>
                        </CheckinSelect>
                    </CheckinField>
                    <CheckinField label={t('titleTemplate')} hint={t('defaultTemplate')}>
                        <Input
                            value={config.title_template}
                            maxLength={512}
                            placeholder="Octopus · {{event}}"
                            onChange={(event) => update('title_template', event.target.value)}
                        />
                    </CheckinField>
                    <CheckinField label={t('bodyTemplate')}>
                        <CheckinTextarea
                            value={config.body_template}
                            maxLength={8192}
                            placeholder={
                                '{{site}} / {{account}}\n{{event}}\n{{balance}} {{balance_unit}}'
                            }
                            onChange={(event) => update('body_template', event.target.value)}
                        />
                    </CheckinField>
                    <details className="text-xs text-muted-foreground">
                        <summary className="cursor-pointer">{t('variables')}</summary>
                        <p className="mt-2 leading-6">
                            {[
                                'event',
                                'status',
                                'site',
                                'account',
                                'site_id',
                                'account_id',
                                'date',
                                'timezone',
                                'source',
                                'reward',
                                'reward_unit',
                                'balance',
                                'balance_unit',
                                'balance_time',
                                'time',
                            ].map((key) => (
                                <code key={key} className="mr-2 inline-block">{`{{${key}}}`}</code>
                            ))}
                        </p>
                    </details>
                    <p className="text-xs text-muted-foreground">{t('previewHint')}</p>
                    <Button
                        type="button"
                        variant="outline"
                        disabled={preview.isPending}
                        onClick={() =>
                            preview.mutate({
                                path: '/notification-preview',
                                body: {
                                    language: config.language,
                                    title_template: config.title_template,
                                    body_template: config.body_template,
                                },
                            })
                        }
                    >
                        {t('preview')}
                    </Button>
                    <CheckinError error={preview.error} />
                    {preview.data && (
                        <div aria-live="polite" className="rounded-xl bg-muted p-3 text-sm">
                            <p className="break-words font-medium">{preview.data.title}</p>
                            <pre className="mt-2 whitespace-pre-wrap break-words font-sans">
                                {preview.data.message}
                            </pre>
                        </div>
                    )}
                </section>
                <CheckinError error={error} />
                <div className="flex justify-end gap-2">
                    <Button type="button" variant="outline" onClick={onClose}>
                        {common('cancel')}
                    </Button>
                    <Button disabled={save.isPending}>{common('save')}</Button>
                </div>
            </form>
        </CheckinDialog>
    );
}
