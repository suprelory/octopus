import { useState, type FormEvent } from 'react';
import { useTranslations } from 'use-intl';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    type CheckinAccount,
    type CheckinAttempt,
    type CheckinJob,
    type CheckinRun,
    type CheckinSettings,
    type CheckinSite,
    type CheckinStats,
    type JobItem,
    type RunPage,
    rewardText,
    useCheckinMutation,
    useCheckinQuery,
} from '@/api/checkin';
import { CheckinDialog, CheckinError, CheckinField, CheckinSelect, CheckinStatus } from './common';
import { TurnstileSettingsForm } from './TurnstileSettings';

export type CheckinFilter = { siteID: string; accountID: string };

function filterQuery(filter: CheckinFilter) {
    const params = new URLSearchParams();
    if (filter.siteID) params.set('site_id', filter.siteID);
    if (filter.accountID) params.set('account_id', filter.accountID);
    return params;
}

export function CheckinFilters({
    sites,
    value,
    onChange,
}: {
    sites: CheckinSite[];
    value: CheckinFilter;
    onChange: (filter: CheckinFilter) => void;
}) {
    const t = useTranslations('checkin');
    const accounts = useCheckinQuery<CheckinAccount[]>(
        `/sites/${value.siteID}/accounts`,
        !!value.siteID,
    );
    return (
        <div className="flex flex-wrap gap-3">
            <CheckinField label={t('site')}>
                <CheckinSelect
                    value={value.siteID}
                    onChange={(event) => onChange({ siteID: event.target.value, accountID: '' })}
                >
                    <option value="">{t('allSites')}</option>
                    {sites.map((site) => (
                        <option key={site.id} value={site.id}>
                            {site.config.name}
                        </option>
                    ))}
                </CheckinSelect>
            </CheckinField>
            {value.siteID && (
                <CheckinField label={t('account')}>
                    <CheckinSelect
                        value={value.accountID}
                        onChange={(event) => onChange({ ...value, accountID: event.target.value })}
                    >
                        <option value="">{t('allAccounts')}</option>
                        {accounts.data?.map((account) => (
                            <option key={account.id} value={account.id}>
                                {account.name}
                            </option>
                        ))}
                    </CheckinSelect>
                </CheckinField>
            )}
        </div>
    );
}

export function StatsPanel({ filter }: { filter: CheckinFilter }) {
    const t = useTranslations('checkin');
    const query = useCheckinQuery<CheckinStats>(`/stats?${filterQuery(filter)}`);
    return (
        <div className="space-y-4">
            <p className="text-sm text-muted-foreground">{t('statsHint')}</p>
            <CheckinError error={query.error} />
            {query.isPending && <p className="text-sm text-muted-foreground">{t('loading')}</p>}
            <div className="grid gap-4 sm:grid-cols-2">
                {query.data?.rewards.map((reward) => (
                    <article key={reward.key} className="rounded-3xl border bg-card p-5">
                        <div className="mb-4 flex items-baseline justify-between gap-2">
                            <h3 className="font-semibold">{reward.site_name || t('allSites')}</h3>
                            <span className="text-sm text-muted-foreground">{reward.unit}</span>
                        </div>
                        <dl className="grid grid-cols-2 gap-4">
                            {(['today', 'days_7', 'days_30', 'total'] as const).map((key) => (
                                <div key={key}>
                                    <dt className="text-xs text-muted-foreground">
                                        {t(`periods.${key}`)}
                                    </dt>
                                    <dd className="mt-1 break-all text-xl font-semibold tabular-nums">
                                        {reward[key]}
                                    </dd>
                                </div>
                            ))}
                        </dl>
                    </article>
                ))}
            </div>
            {!query.isPending && !query.data?.rewards.length && (
                <p className="rounded-3xl border border-dashed p-8 text-center text-sm text-muted-foreground">
                    {t('noRewards')}
                </p>
            )}
            {query.data && (
                <p className="text-xs text-muted-foreground">
                    {t('rewardMissing', {
                        unknown: query.data.unknown_reward,
                        invalid: query.data.invalid_reward,
                    })}
                </p>
            )}
        </div>
    );
}

export function HistoryPanel({
    filter,
    onRun,
}: {
    filter: CheckinFilter;
    onRun: (run: CheckinRun) => void;
}) {
    'use no memo';
    // React Compiler 1.0.0 hoists data.items past the optional-chain guard in
    // this component. Keep its initial loading render safe until that is fixed.
    const t = useTranslations('checkin');
    const [cursors, setCursors] = useState<string[]>(['']);
    const [status, setStatus] = useState('');
    const [from, setFrom] = useState('');
    const [until, setUntil] = useState('');
    const params = filterQuery(filter);
    params.set('limit', '30');
    if (cursors.at(-1)) params.set('before', cursors.at(-1)!);
    if (status) params.set('status', status);
    if (from) params.set('from', from);
    if (until) params.set('until', until);
    const query = useCheckinQuery<RunPage>(`/runs?${params}`);
    const records = query.data?.items ?? [];
    return (
        <div className="space-y-4">
            <div className="flex flex-wrap gap-3">
                <CheckinField label={t('result')}>
                    <CheckinSelect
                        value={status}
                        onChange={(event) => {
                            setStatus(event.target.value);
                            setCursors(['']);
                        }}
                    >
                        <option value="">{t('allResults')}</option>
                        {[
                            'success',
                            'already_checked_in',
                            'pending',
                            'manual_required',
                            'failed',
                            'skipped',
                        ].map((state) => (
                            <option key={state} value={state}>
                                {t(`states.${state}`)}
                            </option>
                        ))}
                    </CheckinSelect>
                </CheckinField>
                <CheckinField label={t('from')}>
                    <Input
                        type="date"
                        value={from}
                        onChange={(event) => {
                            setFrom(event.target.value);
                            setCursors(['']);
                        }}
                    />
                </CheckinField>
                <CheckinField label={t('until')}>
                    <Input
                        type="date"
                        value={until}
                        onChange={(event) => {
                            setUntil(event.target.value);
                            setCursors(['']);
                        }}
                    />
                </CheckinField>
            </div>
            <CheckinError error={query.error} />
            <div className="divide-y overflow-hidden rounded-3xl border bg-card">
                {records.map((run) => (
                    <button
                        key={run.id}
                        type="button"
                        onClick={() => onRun(run)}
                        className="flex w-full flex-wrap items-center gap-3 p-4 text-left transition-colors hover:bg-muted/50"
                    >
                        <div className="min-w-0 flex-1">
                            <div className="truncate text-sm font-medium">
                                {run.site_name} · {run.account_name}
                            </div>
                            <div className="mt-1 text-xs text-muted-foreground">
                                {run.checkin_date} · {run.timezone}
                            </div>
                        </div>
                        <CheckinStatus value={run.status} />
                        <span className="min-w-16 text-right text-sm tabular-nums">
                            {rewardText(run.reward_micros)}{' '}
                            {run.reward_micros !== undefined && run.reward_unit}
                        </span>
                    </button>
                ))}
                {query.isPending && !query.data && (
                    <p className="p-8 text-center text-sm text-muted-foreground">{t('loading')}</p>
                )}
                {!query.isPending && records.length === 0 && !query.error && (
                    <p className="p-8 text-center text-sm text-muted-foreground">
                        {t('noHistory')}
                    </p>
                )}
            </div>
            <div className="flex justify-end gap-2">
                <Button
                    variant="outline"
                    disabled={cursors.length <= 1 || query.isFetching}
                    onClick={() => setCursors(cursors.slice(0, -1))}
                >
                    {t('previous')}
                </Button>
                <Button
                    variant="outline"
                    disabled={!query.data?.next_before || query.isFetching}
                    onClick={() => setCursors([...cursors, query.data!.next_before])}
                >
                    {t('next')}
                </Button>
            </div>
        </div>
    );
}

export function RunDetails({ run: initialRun, onClose }: { run: CheckinRun; onClose: () => void }) {
    const t = useTranslations('checkin');
    const record = useCheckinQuery<CheckinRun>(`/runs/${initialRun.id}`);
    const run = record.data ?? initialRun;
    const query = useCheckinQuery<CheckinAttempt[]>(`/runs/${run.id}/attempts`);
    const site = useCheckinQuery<CheckinSite>(`/sites/${run.site_id}`);
    const confirm = useCheckinMutation();
    const [requested, setRequested] = useState(false);
    return (
        <CheckinDialog
            title={`${run.site_name} · ${run.account_name}`}
            description={`${run.checkin_date} · ${run.timezone}`}
            onClose={onClose}
        >
            <div className="space-y-4">
                <div className="flex flex-wrap items-center gap-3">
                    <CheckinStatus value={run.status} />
                    <span className="text-sm">
                        {t('reward')}: {rewardText(run.reward_micros)} {run.reward_unit}
                    </span>
                </div>
                <p className="text-sm">
                    {t.has(`reasons.${run.reason}`) ? t(`reasons.${run.reason}`) : run.reason}
                </p>
                {run.submit_intent_at && (
                    <p className="text-xs text-muted-foreground">{t('submittedHint')}</p>
                )}
                {site.data?.config.status &&
                    !['success', 'already_checked_in'].includes(run.status) && (
                        <Button
                            disabled={confirm.isPending || requested}
                            onClick={() => {
                                confirm.mutate(
                                    { path: `/runs/${run.id}/confirm` },
                                    { onSuccess: () => setRequested(true) },
                                );
                            }}
                        >
                            {t(requested ? 'queuedHint' : 'confirmOnly')}
                        </Button>
                    )}
                {site.data && (
                    <a
                        href={site.data.config.manual_url || site.data.config.base_url}
                        target="_blank"
                        rel="noreferrer"
                        className="ml-3 text-sm underline underline-offset-4"
                    >
                        {t('openSite')}
                    </a>
                )}
                <CheckinError error={confirm.error || query.error} />
                <ol className="space-y-3">
                    {query.data?.map((attempt) => (
                        <li key={attempt.id} className="rounded-xl border p-3 text-sm">
                            <div className="flex flex-wrap justify-between gap-2">
                                <strong>{t(`stages.${attempt.stage}`)}</strong>
                                <time className="text-xs text-muted-foreground">
                                    {new Date(attempt.created_at).toLocaleString()}
                                </time>
                            </div>
                            <p className="mt-1 text-xs text-muted-foreground">
                                HTTP {attempt.http_status || '—'} · {attempt.duration_ms} ms ·{' '}
                                {t.has(`reasons.${attempt.reason}`)
                                    ? t(`reasons.${attempt.reason}`)
                                    : attempt.reason}
                            </p>
                        </li>
                    ))}
                </ol>
            </div>
        </CheckinDialog>
    );
}

export function JobsPanel({ onJob }: { onJob: (id: string) => void }) {
    const t = useTranslations('checkin');
    const query = useCheckinQuery<CheckinJob[]>('/jobs');
    return (
        <div className="space-y-3">
            <CheckinError error={query.error} />
            {query.data?.map((job) => (
                <button
                    key={job.id}
                    type="button"
                    onClick={() => onJob(job.id)}
                    className="block w-full space-y-3 rounded-3xl border bg-card p-5 text-left hover:bg-muted/40"
                >
                    <div className="flex flex-wrap items-center justify-between gap-2">
                        <span className="text-sm font-medium">
                            {t.has(`sources.${job.source}`)
                                ? t(`sources.${job.source}`)
                                : job.source}{' '}
                            · {new Date(job.created_at).toLocaleString()}
                        </span>
                        <CheckinStatus value={job.status} />
                    </div>
                    <progress
                        className="h-2 w-full accent-[var(--primary)]"
                        value={job.completed}
                        max={job.total || 1}
                        aria-label={t('progress')}
                    />
                    <p className="text-xs text-muted-foreground">
                        {t('jobSummary', {
                            completed: job.completed,
                            total: job.total,
                            success: job.success + job.already,
                            failed: job.failed,
                            pending: job.pending,
                            skipped: job.skipped,
                        })}
                    </p>
                </button>
            ))}
            {!query.data?.length && (
                <p className="p-8 text-center text-sm text-muted-foreground">
                    {t(query.isPending ? 'loading' : 'noJobs')}
                </p>
            )}
        </div>
    );
}

export function JobDetails({ id, onClose }: { id: string; onClose: () => void }) {
    const t = useTranslations('checkin');
    const query = useCheckinQuery<{ job: CheckinJob; items: JobItem[] }>(`/jobs/${id}`);
    const cancel = useCheckinMutation();
    return (
        <CheckinDialog title={t('jobDetails')} description={t('jobHint')} onClose={onClose}>
            <div className="space-y-4">
                <CheckinError error={query.error || cancel.error} />
                {query.data && (
                    <>
                        <div className="flex items-center justify-between">
                            <CheckinStatus value={query.data.job.status} />
                            <span className="text-sm tabular-nums">
                                {query.data.job.completed} / {query.data.job.total}
                            </span>
                        </div>
                        {['queued', 'running'].includes(query.data.job.status) && (
                            <Button
                                variant="outline"
                                disabled={cancel.isPending}
                                onClick={() => cancel.mutate({ path: `/jobs/${id}/cancel` })}
                            >
                                {t('cancelJob')}
                            </Button>
                        )}
                        <div className="divide-y">
                            {query.data.items.map((item) => (
                                <div
                                    key={item.id}
                                    className="flex flex-wrap items-center justify-between gap-2 py-3 text-sm"
                                >
                                    <span>
                                        {item.site_name || t('site')} ·{' '}
                                        {item.account_name || t('account')}
                                        <span className="ml-2 text-xs text-muted-foreground">
                                            {item.checkin_date}
                                        </span>
                                    </span>
                                    <CheckinStatus value={item.outcome || item.state} />
                                </div>
                            ))}
                        </div>
                    </>
                )}
            </div>
        </CheckinDialog>
    );
}

function SettingsForm({ settings, onClose }: { settings: CheckinSettings; onClose: () => void }) {
    const t = useTranslations('checkin');
    const [value, setValue] = useState(settings);
    const save = useCheckinMutation();
    function submit(event: FormEvent) {
        event.preventDefault();
        save.mutate({ path: '/settings', method: 'PATCH', body: value }, { onSuccess: onClose });
    }
    return (
        <form onSubmit={submit} className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
                {(Object.keys(value) as (keyof CheckinSettings)[]).map((key) => (
                    <CheckinField key={key} label={t(`settingsFields.${key}`)}>
                        <Input
                            required
                            type="number"
                            min={key === 'site_interval_seconds' ? 0 : 1}
                            value={value[key]}
                            onChange={(event) =>
                                setValue({ ...value, [key]: Number(event.target.value) })
                            }
                        />
                    </CheckinField>
                ))}
            </div>
            <p className="text-xs text-muted-foreground">{t('backupHint')}</p>
            <CheckinError error={save.error} />
            <div className="flex justify-end">
                <Button type="submit" disabled={save.isPending}>
                    {t('save')}
                </Button>
            </div>
        </form>
    );
}

export function SettingsPanel({ onClose }: { onClose: () => void }) {
    const t = useTranslations('checkin');
    const [tab, setTab] = useState<'execution' | 'turnstile'>('execution');
    const query = useCheckinQuery<CheckinSettings>('/settings');
    return (
        <CheckinDialog title={t('settings')} onClose={onClose}>
            <div className="mb-5 flex flex-wrap gap-2">
                {(['execution', 'turnstile'] as const).map((key) => (
                    <Button
                        key={key}
                        size="sm"
                        variant={tab === key ? 'default' : 'ghost'}
                        aria-pressed={tab === key}
                        onClick={() => setTab(key)}
                    >
                        {t(key === 'execution' ? 'turnstile.execution' : 'turnstile.title')}
                    </Button>
                ))}
            </div>
            {tab === 'turnstile' ? (
                <TurnstileSettingsForm onClose={onClose} />
            ) : (
                <>
                    <CheckinError error={query.error} />
                    {query.data ? (
                        <SettingsForm settings={query.data} onClose={onClose} />
                    ) : (
                        <p>{t('loading')}</p>
                    )}
                </>
            )}
        </CheckinDialog>
    );
}
