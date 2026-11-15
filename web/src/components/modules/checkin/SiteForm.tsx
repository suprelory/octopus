import { useState, type FormEvent } from 'react';
import { useTranslations } from 'use-intl';
import { FlaskConical, LoaderCircle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ProxySelector } from '../proxy-pool/ProxySelector';
import { siteProxySelection } from '@/api/proxy-pool';
import {
    type Capability,
    type CheckinRequest,
    type CheckinSite,
    type Match,
    type SiteConfig,
    useCheckinMutation,
    useCheckinQuery,
} from '@/api/checkin';
import {
    CheckinDialog,
    CheckinError,
    CheckinField,
    CheckinSelect,
    CheckinTextarea,
    CheckinToggle,
} from './common';

function defaultConfig(): SiteConfig {
    return {
        name: '',
        base_url: '',
        adapter: 'new-api',
        enabled: true,
        timezone: 'Asia/Shanghai',
        window_start: '00:00',
        window_end: '24:00',
        jitter_minutes: 0,
        proxy_url: '',
        manual_url: '',
        submit: { method: 'POST', path: '/api/user/checkin', body: '{}', headers: [] },
        reward: { source: 'status', path: 'data.quota_awarded', unit: 'quota', multiplier: '1' },
    };
}

function defaultStatus(): NonNullable<SiteConfig['status']> {
    return {
        request: {
            method: 'GET',
            path: '/api/checkin/status?date={{checkin_date}}',
            body: '',
            headers: [],
        },
        checked: { path: 'data.checked_in', value: true },
        unchecked: { path: 'data.checked_in', value: false },
        date_path: 'data.date',
    };
}

function scalar(value: string): Match['value'] {
    const parsed: unknown = JSON.parse(value);
    if (typeof parsed !== 'boolean' && typeof parsed !== 'string' && typeof parsed !== 'number')
        throw new Error('Expected a JSON boolean, string or number');
    return parsed;
}

function headers(value: string): CheckinRequest['headers'] {
    const parsed: unknown = JSON.parse(value || '[]');
    if (
        !Array.isArray(parsed) ||
        !parsed.every(
            (item: unknown) =>
                typeof item === 'object' &&
                item !== null &&
                'name' in item &&
                typeof item.name === 'string' &&
                'value' in item &&
                typeof item.value === 'string',
        )
    )
        throw new Error('Headers must be a JSON array of {name, value}');
    return parsed as CheckinRequest['headers'];
}

function RequestFields({
    value,
    onChange,
    headerText,
    onHeaders,
}: {
    value: CheckinRequest;
    onChange: (value: CheckinRequest) => void;
    headerText: string;
    onHeaders: (value: string) => void;
}) {
    const t = useTranslations('checkin');
    return (
        <div className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-[120px_1fr]">
                <CheckinField label={t('method')}>
                    <CheckinSelect
                        value={value.method}
                        onChange={(event) =>
                            onChange({ ...value, method: event.target.value as 'GET' | 'POST' })
                        }
                    >
                        <option>GET</option>
                        <option>POST</option>
                    </CheckinSelect>
                </CheckinField>
                <CheckinField label={t('path')}>
                    <Input
                        required
                        value={value.path}
                        onChange={(event) => onChange({ ...value, path: event.target.value })}
                        placeholder="/api/checkin"
                    />
                </CheckinField>
            </div>
            <CheckinField label={t('body')} hint={t('templateHint')}>
                <CheckinTextarea
                    value={value.body}
                    onChange={(event) => onChange({ ...value, body: event.target.value })}
                />
            </CheckinField>
            <CheckinField label={t('headers')} hint={t('headersHint')}>
                <CheckinTextarea
                    value={headerText}
                    onChange={(event) => onHeaders(event.target.value)}
                    placeholder='[{"name":"Accept","value":"application/json"}]'
                />
            </CheckinField>
        </div>
    );
}

export function SiteForm({ site, onClose }: { site?: CheckinSite; onClose: () => void }) {
    const t = useTranslations('checkin');
    const [config, setConfig] = useState<SiteConfig>(() =>
        structuredClone(site?.config ?? defaultConfig()),
    );
    const [submitHeaders, setSubmitHeaders] = useState(() =>
        JSON.stringify(site?.config.submit.headers ?? [], null, 2),
    );
    const [statusHeaders, setStatusHeaders] = useState(() =>
        JSON.stringify(site?.config.status?.request.headers ?? [], null, 2),
    );
    const [successPath, setSuccessPath] = useState(site?.config.success?.path ?? '');
    const [successValue, setSuccessValue] = useState(
        JSON.stringify(site?.config.success?.value ?? true),
    );
    const [alreadyPath, setAlreadyPath] = useState(site?.config.already?.path ?? '');
    const [alreadyValue, setAlreadyValue] = useState(
        JSON.stringify(site?.config.already?.value ?? true),
    );
    const [checkedValue, setCheckedValue] = useState(
        JSON.stringify(site?.config.status?.checked.value ?? true),
    );
    const [uncheckedValue, setUncheckedValue] = useState(
        JSON.stringify(site?.config.status?.unchecked.value ?? false),
    );
    const [sample, setSample] = useState('{"success":true,"data":{"quota_awarded":0}}');
    const [sampleDate, setSampleDate] = useState(() => new Date().toISOString().slice(0, 10));
    const [sampleStatus, setSampleStatus] = useState(200);
    const [error, setError] = useState<unknown>(null);
    const [preview, setPreview] = useState<{
        state: string;
        reward: string | null;
        reward_state: string;
    } | null>(null);
    const save = useCheckinMutation();
    const test = useCheckinMutation<NonNullable<typeof preview>>();
    const capabilities = useCheckinQuery<Capability[]>('/capabilities');
    const platform = capabilities.data?.find((item) => item.id === config.adapter);
    const nativeStatusAvailable =
        platform?.status_query === 'native' &&
        config.submit.method === 'POST' &&
        config.submit.path === platform.path;
    const nativeStatus =
        nativeStatusAvailable && !config.status && config.status_mode !== 'disabled';

    function buildConfig(): SiteConfig {
        return {
            ...config,
            submit: { ...config.submit, headers: headers(submitHeaders) },
            success: successPath ? { path: successPath, value: scalar(successValue) } : undefined,
            already: alreadyPath ? { path: alreadyPath, value: scalar(alreadyValue) } : undefined,
            status: config.status
                ? {
                      ...config.status,
                      request: { ...config.status.request, headers: headers(statusHeaders) },
                      checked: { ...config.status.checked, value: scalar(checkedValue) },
                      unchecked: { ...config.status.unchecked, value: scalar(uncheckedValue) },
                  }
                : undefined,
        };
    }

    async function submit(event: FormEvent) {
        event.preventDefault();
        setError(null);
        try {
            await save.mutateAsync({
                path: site ? `/sites/${site.id}` : '/sites',
                method: site ? 'PATCH' : 'POST',
                body: { config: buildConfig(), revision: site?.revision ?? 0 },
            });
            onClose();
        } catch (error) {
            setError(error);
        }
    }

    async function previewRules() {
        setError(null);
        try {
            const response: unknown = JSON.parse(sample);
            const result = await test.mutateAsync({
                path: '/rules/test',
                body: {
                    config: buildConfig(),
                    response,
                    date: sampleDate,
                    http_status: sampleStatus,
                },
            });
            setPreview(result);
        } catch (error) {
            setError(error);
        }
    }

    return (
        <CheckinDialog
            title={t(site ? 'editSite' : 'addSite')}
            description={t('siteDescription')}
            onClose={onClose}
        >
            <form
                onSubmit={(event) => {
                    void submit(event);
                }}
                className="space-y-6"
            >
                <fieldset disabled={save.isPending} className="space-y-5">
                    <div className="grid gap-4 sm:grid-cols-2">
                        <CheckinField label={t('name')}>
                            <Input
                                required
                                value={config.name}
                                onChange={(event) =>
                                    setConfig({ ...config, name: event.target.value })
                                }
                            />
                        </CheckinField>
                        <CheckinField label={t('adapter')}>
                            <CheckinSelect
                                value={config.adapter}
                                onChange={(event) => {
                                    const adapter = capabilities.data?.find(
                                        (item) => item.id === event.target.value,
                                    );
                                    const id = event.target.value;
                                    setSuccessPath(id === 'sub2api' ? 'code' : '');
                                    setSuccessValue(id === 'sub2api' ? '0' : 'true');
                                    setAlreadyPath('');
                                    setAlreadyValue('true');
                                    setCheckedValue('true');
                                    setUncheckedValue('false');
                                    setSubmitHeaders('[]');
                                    setStatusHeaders('[]');
                                    setPreview(null);
                                    setSample(
                                        id === 'agentrouter'
                                            ? '{"success":true,"data":{"id":1,"checked_in":true}}'
                                            : id === 'new-api'
                                              ? '{"success":true,"data":{"quota_awarded":0}}'
                                              : id === 'sub2api'
                                                ? '{"code":0,"data":{"reward":0}}'
                                                : '{"success":true,"data":{"reward":0}}',
                                    );
                                    setConfig({
                                        ...config,
                                        adapter: id,
                                        submit: {
                                            method: 'POST',
                                            path: adapter?.path || '',
                                            body: '{}',
                                            headers: [],
                                        },
                                        status: undefined,
                                        status_mode: undefined,
                                        success: undefined,
                                        already: undefined,
                                        reward: {
                                            source: id === 'new-api' ? 'status' : 'submit',
                                            path:
                                                id === 'new-api'
                                                    ? 'data.quota_awarded'
                                                    : 'data.reward',
                                            unit: id === 'new-api' ? 'quota' : 'points',
                                            multiplier: '1',
                                        },
                                    });
                                }}
                            >
                                {(
                                    capabilities.data ?? [
                                        { id: config.adapter, name: config.adapter },
                                    ]
                                ).map((item) => (
                                    <option key={item.id} value={item.id}>
                                        {item.name}
                                    </option>
                                ))}
                            </CheckinSelect>
                        </CheckinField>
                    </div>
                    <CheckinField label={t('baseURL')} hint={t('baseURLHint')}>
                        <Input
                            type="url"
                            required
                            value={config.base_url}
                            onChange={(event) =>
                                setConfig({ ...config, base_url: event.target.value })
                            }
                            placeholder="https://example.com"
                        />
                    </CheckinField>
                    {t.has(`platformHints.${config.adapter}`) && (
                        <p className="text-xs text-muted-foreground">
                            {t(`platformHints.${config.adapter}`)}
                        </p>
                    )}
                    {config.adapter === 'sub2api' && (
                        <p className="text-xs text-muted-foreground">{t('sub2apiHint')}</p>
                    )}
                    <CheckinField label="User-Agent" hint={t('requestUserAgentHint')}>
                        <Input
                            value={config.user_agent ?? ''}
                            maxLength={512}
                            onChange={(event) =>
                                setConfig({ ...config, user_agent: event.target.value })
                            }
                            placeholder="Octopus-Checkin/1.0"
                        />
                    </CheckinField>
                    <CheckinToggle
                        label={t('siteEnabled')}
                        checked={config.enabled}
                        onChange={(enabled) => setConfig({ ...config, enabled })}
                    />
                    <fieldset className="space-y-3 rounded-2xl border p-4">
                        <legend className="px-1 text-sm font-semibold">{t('schedule')}</legend>
                        <div className="grid gap-3 sm:grid-cols-2">
                            <CheckinField label={t('timezone')}>
                                <Input
                                    required
                                    value={config.timezone}
                                    onChange={(event) =>
                                        setConfig({ ...config, timezone: event.target.value })
                                    }
                                />
                            </CheckinField>
                            <CheckinField label={t('jitter')}>
                                <Input
                                    type="number"
                                    min={0}
                                    max={1440}
                                    required
                                    value={config.jitter_minutes}
                                    onChange={(event) =>
                                        setConfig({
                                            ...config,
                                            jitter_minutes: Number(event.target.value),
                                        })
                                    }
                                />
                            </CheckinField>
                            <CheckinField label={t('windowStart')}>
                                <Input
                                    required
                                    placeholder="00:00"
                                    value={config.window_start}
                                    onChange={(event) =>
                                        setConfig({ ...config, window_start: event.target.value })
                                    }
                                />
                            </CheckinField>
                            <CheckinField label={t('windowEnd')} hint={t('windowHint')}>
                                <Input
                                    required
                                    placeholder="24:00"
                                    value={config.window_end}
                                    onChange={(event) =>
                                        setConfig({ ...config, window_end: event.target.value })
                                    }
                                />
                            </CheckinField>
                        </div>
                    </fieldset>
                    {platform?.custom_request !== false && (
                        <>
                            <fieldset className="space-y-3 rounded-2xl border p-4">
                                <legend className="px-1 text-sm font-semibold">
                                    {t('submitRequest')}
                                </legend>
                                <RequestFields
                                    value={config.submit}
                                    onChange={(submit) =>
                                        setConfig({
                                            ...config,
                                            submit,
                                            reward:
                                                !config.status &&
                                                (submit.path !== platform?.path ||
                                                    submit.method !== 'POST')
                                                    ? { ...config.reward, source: 'submit' }
                                                    : config.reward,
                                        })
                                    }
                                    headerText={submitHeaders}
                                    onHeaders={setSubmitHeaders}
                                />
                            </fieldset>
                            <details className="rounded-2xl border p-4">
                                <summary className="cursor-pointer text-sm font-semibold">
                                    {t('responseRules')}
                                </summary>
                                <div className="mt-4 grid gap-3 sm:grid-cols-2">
                                    <CheckinField
                                        label={t('successPath')}
                                        hint={t(
                                            config.adapter === 'sub2api'
                                                ? 'sub2apiSuccessHint'
                                                : 'successHint',
                                        )}
                                    >
                                        <Input
                                            value={successPath}
                                            onChange={(event) => setSuccessPath(event.target.value)}
                                            placeholder="success"
                                        />
                                    </CheckinField>
                                    <CheckinField label={t('expectedValue')}>
                                        <Input
                                            value={successValue}
                                            onChange={(event) =>
                                                setSuccessValue(event.target.value)
                                            }
                                        />
                                    </CheckinField>
                                    <CheckinField label={t('alreadyPath')}>
                                        <Input
                                            value={alreadyPath}
                                            onChange={(event) => setAlreadyPath(event.target.value)}
                                        />
                                    </CheckinField>
                                    <CheckinField label={t('expectedValue')}>
                                        <Input
                                            value={alreadyValue}
                                            onChange={(event) =>
                                                setAlreadyValue(event.target.value)
                                            }
                                        />
                                    </CheckinField>
                                </div>
                            </details>
                        </>
                    )}
                    {platform?.custom_status !== false && (
                        <div className="space-y-3">
                            {nativeStatusAvailable ? (
                                <CheckinField label={t('statusMode')} hint={t('nativeStatusHint')}>
                                    <CheckinSelect
                                        value={
                                            config.status
                                                ? 'custom'
                                                : config.status_mode === 'disabled'
                                                  ? 'disabled'
                                                  : 'native'
                                        }
                                        onChange={(event) => {
                                            const mode = event.target.value;
                                            setConfig({
                                                ...config,
                                                status:
                                                    mode === 'custom' ? defaultStatus() : undefined,
                                                status_mode:
                                                    mode === 'disabled' ? 'disabled' : undefined,
                                                reward: {
                                                    ...config.reward,
                                                    source:
                                                        mode === 'disabled'
                                                            ? 'submit'
                                                            : config.reward.source,
                                                },
                                            });
                                        }}
                                    >
                                        <option value="native">{t('nativeStatus')}</option>
                                        <option value="custom">{t('customStatus')}</option>
                                        <option value="disabled">{t('noStatus')}</option>
                                    </CheckinSelect>
                                </CheckinField>
                            ) : (
                                <CheckinToggle
                                    label={t('useStatus')}
                                    checked={!!config.status}
                                    onChange={(enabled) =>
                                        setConfig({
                                            ...config,
                                            status: enabled ? defaultStatus() : undefined,
                                            status_mode: undefined,
                                            reward: {
                                                ...config.reward,
                                                source: enabled ? config.reward.source : 'submit',
                                            },
                                        })
                                    }
                                />
                            )}
                            <p className="text-xs text-muted-foreground">{t('statusHint')}</p>
                            {config.status && (
                                <fieldset className="space-y-4 rounded-2xl border p-4">
                                    <legend className="px-1 text-sm font-semibold">
                                        {t('statusRequest')}
                                    </legend>
                                    <RequestFields
                                        value={config.status.request}
                                        onChange={(request) =>
                                            setConfig({
                                                ...config,
                                                status: { ...config.status!, request },
                                            })
                                        }
                                        headerText={statusHeaders}
                                        onHeaders={setStatusHeaders}
                                    />
                                    <div className="grid gap-3 sm:grid-cols-2">
                                        <CheckinField label={t('checkedPath')}>
                                            <Input
                                                required
                                                value={config.status.checked.path}
                                                onChange={(event) =>
                                                    setConfig({
                                                        ...config,
                                                        status: {
                                                            ...config.status!,
                                                            checked: {
                                                                ...config.status!.checked,
                                                                path: event.target.value,
                                                            },
                                                        },
                                                    })
                                                }
                                            />
                                        </CheckinField>
                                        <CheckinField label={t('checkedValue')}>
                                            <Input
                                                required
                                                value={checkedValue}
                                                onChange={(event) =>
                                                    setCheckedValue(event.target.value)
                                                }
                                            />
                                        </CheckinField>
                                        <CheckinField label={t('uncheckedPath')}>
                                            <Input
                                                required
                                                value={config.status.unchecked.path}
                                                onChange={(event) =>
                                                    setConfig({
                                                        ...config,
                                                        status: {
                                                            ...config.status!,
                                                            unchecked: {
                                                                ...config.status!.unchecked,
                                                                path: event.target.value,
                                                            },
                                                        },
                                                    })
                                                }
                                            />
                                        </CheckinField>
                                        <CheckinField label={t('uncheckedValue')}>
                                            <Input
                                                required
                                                value={uncheckedValue}
                                                onChange={(event) =>
                                                    setUncheckedValue(event.target.value)
                                                }
                                            />
                                        </CheckinField>
                                    </div>
                                    <CheckinField label={t('datePath')} hint={t('datePathHint')}>
                                        <Input
                                            value={config.status.date_path}
                                            onChange={(event) =>
                                                setConfig({
                                                    ...config,
                                                    status: {
                                                        ...config.status!,
                                                        date_path: event.target.value,
                                                    },
                                                })
                                            }
                                        />
                                    </CheckinField>
                                </fieldset>
                            )}
                        </div>
                    )}
                    {platform?.execution !== 'login' && (
                        <fieldset className="space-y-3 rounded-2xl border p-4">
                            <legend className="px-1 text-sm font-semibold">
                                {t('rewardRule')}
                            </legend>
                            <div className="grid gap-3 sm:grid-cols-2">
                                <CheckinField label={t('rewardSource')}>
                                    <CheckinSelect
                                        value={config.reward.source}
                                        onChange={(event) =>
                                            setConfig({
                                                ...config,
                                                reward: {
                                                    ...config.reward,
                                                    source: event.target.value as
                                                        | 'submit'
                                                        | 'status',
                                                },
                                            })
                                        }
                                    >
                                        <option value="submit">{t('submitResponse')}</option>
                                        {(config.status || nativeStatus) && (
                                            <option value="status">{t('statusResponse')}</option>
                                        )}
                                    </CheckinSelect>
                                </CheckinField>
                                <CheckinField label={t('rewardPath')}>
                                    <Input
                                        value={config.reward.path}
                                        onChange={(event) =>
                                            setConfig({
                                                ...config,
                                                reward: {
                                                    ...config.reward,
                                                    path: event.target.value,
                                                },
                                            })
                                        }
                                    />
                                </CheckinField>
                                <CheckinField label={t('rewardUnit')} hint={t('unitHint')}>
                                    <Input
                                        required
                                        value={config.reward.unit}
                                        onChange={(event) =>
                                            setConfig({
                                                ...config,
                                                reward: {
                                                    ...config.reward,
                                                    unit: event.target.value,
                                                },
                                            })
                                        }
                                    />
                                </CheckinField>
                                <CheckinField label={t('multiplier')}>
                                    <Input
                                        required
                                        value={config.reward.multiplier}
                                        onChange={(event) =>
                                            setConfig({
                                                ...config,
                                                reward: {
                                                    ...config.reward,
                                                    multiplier: event.target.value,
                                                },
                                            })
                                        }
                                    />
                                </CheckinField>
                            </div>
                        </fieldset>
                    )}
                    <details className="rounded-2xl border p-4">
                        <summary className="cursor-pointer text-sm font-semibold">
                            {t('advanced')}
                        </summary>
                        <div className="mt-4 space-y-3">
                            <ProxySelector value={siteProxySelection(config)} onChange={(value) => setConfig({ ...config, ...value, proxy_url: value.proxy_url ?? '' })} />
                            <CheckinField label={t('manualURL')}>
                                <Input
                                    type="url"
                                    value={config.manual_url}
                                    onChange={(event) =>
                                        setConfig({ ...config, manual_url: event.target.value })
                                    }
                                />
                            </CheckinField>
                        </div>
                    </details>
                    <details className="rounded-2xl border p-4">
                        <summary className="cursor-pointer text-sm font-semibold">
                            {t('testRules')}
                        </summary>
                        <div className="mt-4 space-y-3">
                            <p className="text-xs text-muted-foreground">{t('testHint')}</p>
                            <CheckinField label={t('sampleResponse')}>
                                <CheckinTextarea
                                    value={sample}
                                    onChange={(event) => setSample(event.target.value)}
                                />
                            </CheckinField>
                            <div className="grid grid-cols-2 gap-3">
                                <CheckinField label={t('httpStatus')}>
                                    <Input
                                        type="number"
                                        min={100}
                                        max={599}
                                        value={sampleStatus}
                                        onChange={(event) =>
                                            setSampleStatus(Number(event.target.value))
                                        }
                                    />
                                </CheckinField>
                                <CheckinField label={t('targetDate')}>
                                    <Input
                                        type="date"
                                        value={sampleDate}
                                        onChange={(event) => setSampleDate(event.target.value)}
                                    />
                                </CheckinField>
                            </div>
                            <Button
                                type="button"
                                variant="outline"
                                onClick={() => {
                                    void previewRules();
                                }}
                                disabled={test.isPending}
                            >
                                <FlaskConical className="size-4" />
                                {t('testRules')}
                            </Button>
                            {preview && (
                                <div role="status" className="rounded-xl bg-muted p-3 text-sm">
                                    {t('result')}:{' '}
                                    {t.has(`states.${preview.state}`)
                                        ? t(`states.${preview.state}`)
                                        : preview.state}{' '}
                                    · {t('reward')}: {preview.reward ?? '—'}
                                </div>
                            )}
                        </div>
                    </details>
                </fieldset>
                <CheckinError error={error} />
                <CheckinError error={capabilities.error} />
                <div className="flex justify-end gap-2 border-t pt-4">
                    <Button type="button" variant="outline" onClick={onClose}>
                        {t('cancel')}
                    </Button>
                    <Button type="submit" disabled={save.isPending || capabilities.isPending}>
                        {save.isPending && <LoaderCircle className="size-4 animate-spin" />}
                        {t('save')}
                    </Button>
                </div>
            </form>
        </CheckinDialog>
    );
}
