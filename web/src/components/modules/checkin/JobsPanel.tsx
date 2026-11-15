import { useState } from 'react';
import { useTranslations } from 'use-intl';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { type JobPage, useCheckinQuery } from '@/api/checkin';
import { CheckinError, CheckinField, CheckinSelect, CheckinStatus, CheckinToggle } from './common';
import { CleanupDialog, LastCleanup } from './Cleanup';

export function JobsPanel({ onJob }: { onJob: (id: string) => void }) {
    const t = useTranslations('checkin');
    const [filters, setFilters] = useState({
        status: '',
        source: '',
        from: '',
        until: '',
        limit: '50',
    });
    const [cursors, setCursors] = useState(['']);
    const [selected, setSelected] = useState<string[]>([]);
    const [cleanup, setCleanup] = useState<{ jobIDs?: string[] } | null>(null);
    const params = new URLSearchParams({ ...filters, before: cursors[cursors.length - 1] });
    const query = useCheckinQuery<JobPage>(`/jobs?${params}`);
    const page = query.data ?? { items: [], next_before: '' };
    const jobs = page.items;
    const selectable = jobs
        .filter(
            (job) =>
                ['completed', 'completed_with_errors', 'canceled', 'interrupted'].includes(
                    job.status,
                ) && job.completed === job.total,
        )
        .map((job) => job.id);
    const selectedIDs = selected.filter((id) => selectable.includes(id));
    function setFilter(key: keyof typeof filters, value: string) {
        setFilters({ ...filters, [key]: value });
        setCursors(['']);
        setSelected([]);
    }
    return (
        <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
                <CheckinField label={t('result')}>
                    <CheckinSelect
                        value={filters.status}
                        onChange={(event) => setFilter('status', event.target.value)}
                    >
                        <option value="">{t('allResults')}</option>
                        {[
                            'queued',
                            'running',
                            'completed',
                            'completed_with_errors',
                            'canceled',
                            'interrupted',
                        ].map((state) => (
                            <option key={state} value={state}>
                                {t(`states.${state}`)}
                            </option>
                        ))}
                    </CheckinSelect>
                </CheckinField>
                <CheckinField label={t('cleanup.source')}>
                    <CheckinSelect
                        value={filters.source}
                        onChange={(event) => setFilter('source', event.target.value)}
                    >
                        <option value="">{t('cleanup.allSources')}</option>
                        {['manual', 'scheduled', 'confirmation', 'confirmation_auto'].map(
                            (source) => (
                                <option key={source} value={source}>
                                    {t(`sources.${source}`)}
                                </option>
                            ),
                        )}
                    </CheckinSelect>
                </CheckinField>
                <CheckinField label={`${t('from')} (UTC)`}>
                    <Input
                        type="date"
                        value={filters.from}
                        max={filters.until || undefined}
                        onChange={(event) => setFilter('from', event.target.value)}
                    />
                </CheckinField>
                <CheckinField label={`${t('until')} (UTC)`}>
                    <Input
                        type="date"
                        value={filters.until}
                        min={filters.from || undefined}
                        onChange={(event) => setFilter('until', event.target.value)}
                    />
                </CheckinField>
                <CheckinField label={t('cleanup.pageSize')}>
                    <CheckinSelect
                        value={filters.limit}
                        onChange={(event) => setFilter('limit', event.target.value)}
                    >
                        {[20, 50, 100].map((limit) => (
                            <option key={limit} value={limit}>
                                {limit}
                            </option>
                        ))}
                    </CheckinSelect>
                </CheckinField>
            </div>
            <div className="flex flex-wrap items-center gap-3">
                <CheckinToggle
                    label={t('cleanup.selectPage')}
                    checked={selectable.length > 0 && selectedIDs.length === selectable.length}
                    onChange={(checked) => setSelected(checked ? selectable : [])}
                />
                <Button
                    variant="outline"
                    disabled={!selectedIDs.length}
                    onClick={() => setCleanup({ jobIDs: selectedIDs })}
                >
                    {t('cleanup.selectedButton', { count: selectedIDs.length })}
                </Button>
                <Button variant="outline" onClick={() => setCleanup({})}>
                    {t('cleanup.olderButton')}
                </Button>
            </div>
            <CheckinError error={query.error} />
            <div className="space-y-3">
                {jobs.map((job) => (
                    <div
                        key={job.id}
                        className="flex items-start gap-3 rounded-3xl border bg-card p-4"
                    >
                        <input
                            type="checkbox"
                            className="mt-1 size-4 shrink-0 accent-[var(--primary)]"
                            aria-label={t('cleanup.selectJob', {
                                time: new Date(job.created_at).toLocaleString(),
                            })}
                            disabled={!selectable.includes(job.id)}
                            checked={selectedIDs.includes(job.id)}
                            onChange={(event) =>
                                setSelected(
                                    event.target.checked
                                        ? [...selectedIDs, job.id]
                                        : selectedIDs.filter((id) => id !== job.id),
                                )
                            }
                        />
                        <button
                            type="button"
                            onClick={() => onJob(job.id)}
                            className="min-w-0 flex-1 space-y-3 rounded-lg text-left hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
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
                    </div>
                ))}
                {!jobs.length && !query.error && (
                    <p className="p-8 text-center text-sm text-muted-foreground">
                        {t(query.isPending ? 'loading' : 'noJobs')}
                    </p>
                )}
            </div>
            <div className="flex justify-end gap-2">
                <Button
                    variant="outline"
                    disabled={cursors.length <= 1 || query.isFetching}
                    onClick={() => {
                        setCursors(cursors.slice(0, -1));
                        setSelected([]);
                    }}
                >
                    {t('previous')}
                </Button>
                <Button
                    variant="outline"
                    disabled={!page.next_before || query.isFetching}
                    onClick={() => {
                        if (!page.next_before) return;
                        setCursors([...cursors, page.next_before]);
                        setSelected([]);
                    }}
                >
                    {t('next')}
                </Button>
            </div>
            <LastCleanup />
            {cleanup && (
                <CleanupDialog
                    jobIDs={cleanup.jobIDs}
                    onClose={() => setCleanup(null)}
                    onCleaned={() => {
                        setSelected([]);
                        setCursors(['']);
                    }}
                />
            )}
        </div>
    );
}
