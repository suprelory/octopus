import { useState, type FormEvent } from 'react';
import { useTranslations } from 'use-intl';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    type CheckinSettings,
    type CleanupCounts,
    type CleanupFilter,
    type CleanupReport,
    useCheckinMutation,
    useCheckinQuery,
} from '@/api/checkin';
import { CheckinDialog, CheckinError, CheckinField, CheckinToggle } from './common';

function RetentionDays({
    value,
    onChange,
    disabled,
}: {
    value: number;
    onChange: (value: number) => void;
    disabled?: boolean;
}) {
    const t = useTranslations('checkin.cleanup');
    return (
        <div className="space-y-2">
            <CheckinField label={t('days')}>
                <Input
                    required
                    type="number"
                    min={1}
                    max={3650}
                    step={1}
                    disabled={disabled}
                    value={value || ''}
                    onChange={(event) => onChange(Number(event.target.value))}
                />
            </CheckinField>
            <div className="flex flex-wrap gap-2">
                {[30, 90, 180, 365].map((days) => (
                    <Button
                        key={days}
                        type="button"
                        size="sm"
                        variant={value === days ? 'secondary' : 'ghost'}
                        disabled={disabled}
                        aria-pressed={value === days}
                        onClick={() => onChange(days)}
                    >
                        {t('preset', { days })}
                    </Button>
                ))}
            </div>
        </div>
    );
}

function CleanupSummary({ counts }: { counts: CleanupCounts }) {
    const t = useTranslations('checkin.cleanup');
    return (
        <div className="space-y-2 text-sm">
            <p>
                {t('counts', { jobs: counts.jobs, items: counts.items, attempts: counts.attempts })}
            </p>
            <p className="text-xs text-muted-foreground">
                {t('matched', { count: counts.matched })}
            </p>
            <ul className="space-y-1 text-xs text-muted-foreground">
                {(['active', 'unconfirmed', 'leased', 'changed', 'missing'] as const).map(
                    (reason) =>
                        counts.skipped[reason] ? (
                            <li key={reason}>
                                {t(`skips.${reason}`, { count: counts.skipped[reason] })}
                            </li>
                        ) : null,
                )}
            </ul>
        </div>
    );
}

export function CleanupResult({ report }: { report: CleanupReport }) {
    const t = useTranslations('checkin.cleanup');
    return (
        <div role="status" className="space-y-2 rounded-xl border bg-muted/30 p-3">
            <p className="text-sm font-medium">{t(`results.${report.status}`)}</p>
            <p className="text-xs text-muted-foreground">
                {t(`sources.${report.source}`)} ·{' '}
                {new Date(report.finished_at || report.started_at).toLocaleString()}
            </p>
            {report.status !== 'running' && report.status !== 'interrupted' && (
                <CleanupSummary counts={report.counts} />
            )}
            {['partial', 'failed', 'interrupted'].includes(report.status) && (
                <p className="text-xs text-muted-foreground">{t('retryHint')}</p>
            )}
        </div>
    );
}

export function LastCleanup() {
    const t = useTranslations('checkin.cleanup');
    const query = useCheckinQuery<CleanupReport | null>('/jobs/cleanup/status');
    return (
        <div className="space-y-2">
            <p className="text-sm font-medium">{t('lastCleanup')}</p>
            <CheckinError error={query.error} />
            {query.data ? (
                <CleanupResult report={query.data} />
            ) : (
                !query.isPending &&
                !query.error && <p className="text-xs text-muted-foreground">{t('never')}</p>
            )}
        </div>
    );
}

export function CleanupDialog({
    jobIDs,
    onClose,
    onCleaned,
}: {
    jobIDs?: string[];
    onClose: () => void;
    onCleaned: () => void;
}) {
    const t = useTranslations('checkin');
    const [days, setDays] = useState(90);
    const [snapshot, setSnapshot] = useState<{ filter: CleanupFilter; counts: CleanupCounts }>();
    const preview = useCheckinMutation<CleanupCounts>();
    const cleanup = useCheckinMutation<CleanupReport>();
    const busy = preview.isPending || cleanup.isPending;
    function loadPreview(event: FormEvent) {
        event.preventDefault();
        const filter: CleanupFilter = jobIDs
            ? { job_ids: jobIDs }
            : { before: new Date(Date.now() - days * 86_400_000).toISOString() };
        setSnapshot(undefined);
        preview.mutate(
            { path: '/jobs/cleanup/preview', body: filter },
            { onSuccess: (counts) => setSnapshot({ filter, counts }) },
        );
    }
    return (
        <CheckinDialog
            title={t(jobIDs ? 'cleanup.selectedTitle' : 'cleanup.olderTitle')}
            description={t('cleanup.scopeHint')}
            onClose={() => {
                if (!busy) onClose();
            }}
        >
            <div className="space-y-4">
                {cleanup.data ? (
                    <CleanupResult report={cleanup.data} />
                ) : (
                    <>
                        <form onSubmit={loadPreview} className="space-y-4">
                            {jobIDs ? (
                                <p className="text-sm">
                                    {t('cleanup.selectedCount', { count: jobIDs.length })}
                                </p>
                            ) : (
                                <>
                                    <RetentionDays
                                        value={days}
                                        disabled={busy}
                                        onChange={(value) => {
                                            setDays(value);
                                            setSnapshot(undefined);
                                        }}
                                    />
                                    <p className="text-xs text-muted-foreground">
                                        {t('cleanup.ageHint')}
                                    </p>
                                </>
                            )}
                            <Button type="submit" variant="outline" disabled={busy}>
                                {t(preview.isPending ? 'loading' : 'cleanup.preview')}
                            </Button>
                        </form>
                        {snapshot && (
                            <div className="space-y-3 rounded-xl border p-4">
                                {'before' in snapshot.filter && (
                                    <p className="text-xs text-muted-foreground">
                                        {t('cleanup.cutoff', {
                                            time: new Date(snapshot.filter.before).toLocaleString(),
                                        })}
                                    </p>
                                )}
                                <CleanupSummary counts={snapshot.counts} />
                                <p className="text-xs text-muted-foreground">
                                    {t('cleanup.protectedHint')}
                                </p>
                                <p className="text-sm">{t('cleanup.confirmHint')}</p>
                                <Button
                                    variant="destructive"
                                    disabled={busy || snapshot.counts.jobs === 0}
                                    onClick={() =>
                                        cleanup.mutate(
                                            { path: '/jobs/cleanup', body: snapshot.filter },
                                            { onSuccess: onCleaned },
                                        )
                                    }
                                >
                                    {t(cleanup.isPending ? 'cleanup.cleaning' : 'cleanup.confirm')}
                                </Button>
                            </div>
                        )}
                    </>
                )}
                <CheckinError error={preview.error || cleanup.error} />
                <div className="flex justify-end">
                    <Button variant="outline" disabled={busy} onClick={onClose}>
                        {t('close')}
                    </Button>
                </div>
            </div>
        </CheckinDialog>
    );
}

export function RetentionSettings({
    settings,
    onClose,
}: {
    settings: CheckinSettings;
    onClose: () => void;
}) {
    const t = useTranslations('checkin');
    const [enabled, setEnabled] = useState(settings.auto_cleanup);
    const [days, setDays] = useState(settings.retention_days);
    const save = useCheckinMutation();
    return (
        <form
            className="space-y-4"
            onSubmit={(event) => {
                event.preventDefault();
                save.mutate(
                    {
                        path: '/settings',
                        method: 'PATCH',
                        body: { auto_cleanup: enabled, retention_days: days },
                    },
                    { onSuccess: onClose },
                );
            }}
        >
            <CheckinToggle label={t('cleanup.enabled')} checked={enabled} onChange={setEnabled} />
            <RetentionDays value={days} onChange={setDays} disabled={save.isPending} />
            <p className="text-xs text-muted-foreground">{t('cleanup.autoHint')}</p>
            <p className="text-xs text-muted-foreground">{t('cleanup.scopeHint')}</p>
            <p className="text-xs text-muted-foreground">{t('cleanup.protectedHint')}</p>
            <LastCleanup />
            <CheckinError error={save.error} />
            <div className="flex justify-end">
                <Button type="submit" disabled={save.isPending}>
                    {t('save')}
                </Button>
            </div>
        </form>
    );
}
