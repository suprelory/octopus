import { useState } from 'react';
import { CalendarCheck, Plus, Play, Settings2, RefreshCw } from 'lucide-react';
import { useTranslations } from 'use-intl';
import { useQueryClient } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    type CheckinAccount,
    type CheckinJob,
    type CheckinRun,
    type CheckinSite,
    type CheckinStats,
    useCheckinEvents,
    useCheckinMutation,
    useCheckinQuery,
} from '@/api/checkin';
import { SiteForm } from './SiteForm';
import { AccountForm } from './AccountForm';
import { SiteCard } from './SiteCard';
import { CheckinDialog, CheckinError } from './common';
import {
    CheckinFilters,
    type CheckinFilter,
    HistoryPanel,
    JobDetails,
    JobsPanel,
    RunDetails,
    SettingsPanel,
    StatsPanel,
} from './panels';

type Modal =
    | { kind: 'site'; site?: CheckinSite }
    | { kind: 'account'; site: CheckinSite; account?: CheckinAccount }
    | { kind: 'delete'; site: CheckinSite; account?: CheckinAccount }
    | { kind: 'run'; run: CheckinRun }
    | { kind: 'job'; id: string }
    | { kind: 'settings' }
    | null;

function DeleteDialog({
    site,
    account,
    onClose,
}: {
    site: CheckinSite;
    account?: CheckinAccount;
    onClose: () => void;
}) {
    const t = useTranslations('checkin');
    const remove = useCheckinMutation();
    return (
        <CheckinDialog
            title={t(account ? 'deleteAccount' : 'deleteSite')}
            description={account?.name || site.config.name}
            onClose={onClose}
        >
            <div className="space-y-5">
                <p className="text-sm text-muted-foreground">{t('deleteHint')}</p>
                <CheckinError error={remove.error} />
                <div className="flex justify-end gap-2">
                    <Button variant="outline" onClick={onClose}>
                        {t('cancel')}
                    </Button>
                    <Button
                        variant="destructive"
                        disabled={remove.isPending}
                        onClick={() =>
                            remove.mutate(
                                {
                                    path: account ? `/accounts/${account.id}` : `/sites/${site.id}`,
                                    method: 'DELETE',
                                },
                                { onSuccess: onClose },
                            )
                        }
                    >
                        {t('delete')}
                    </Button>
                </div>
            </div>
        </CheckinDialog>
    );
}

export function Checkin() {
    const t = useTranslations('checkin');
    const client = useQueryClient();
    const live = useCheckinEvents();
    const sites = useCheckinQuery<CheckinSite[]>('/sites');
    const stats = useCheckinQuery<CheckinStats>('/stats');
    const run = useCheckinMutation<CheckinJob>();
    const [tab, setTab] = useState<'sites' | 'history' | 'stats' | 'jobs'>('sites');
    const [search, setSearch] = useState('');
    const [selected, setSelected] = useState<string[]>([]);
    const [filter, setFilter] = useState<CheckinFilter>({ siteID: '', accountID: '' });
    const [modal, setModal] = useState<Modal>(null);
    const close = () => setModal(null);
    const showJob = (id: string) => setModal({ kind: 'job', id });
    const visible = (sites.data ?? []).filter((site) =>
        `${site.config.name} ${site.config.base_url}`
            .toLowerCase()
            .includes(search.toLowerCase().trim()),
    );
    const validSelected = selected.filter((id) => sites.data?.some((site) => site.id === id));

    return (
        <div className="h-full overflow-y-auto pb-28 md:pb-6">
            <div className="space-y-5 px-1">
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                    {[
                        [t('sites'), sites.data?.length ?? 0],
                        [t('todayCompleted'), stats.data?.today_completed ?? 0],
                        [t('pendingRecords'), stats.data?.pending ?? 0],
                        [t('failedRecords'), stats.data?.failed ?? 0],
                    ].map(([label, value]) => (
                        <div key={label} className="rounded-2xl border bg-card p-4">
                            <p className="text-xs text-muted-foreground">{label}</p>
                            <p className="mt-2 text-2xl font-semibold tabular-nums">{value}</p>
                        </div>
                    ))}
                </div>
                <div className="flex flex-wrap items-center justify-between gap-3">
                    <nav aria-label={t('sections')} className="flex flex-wrap gap-1">
                        {(['sites', 'history', 'stats', 'jobs'] as const).map((value) => (
                            <Button
                                key={value}
                                size="sm"
                                variant={tab === value ? 'default' : 'ghost'}
                                aria-pressed={tab === value}
                                onClick={() => setTab(value)}
                            >
                                {t(value)}
                            </Button>
                        ))}
                    </nav>
                    <div className="flex items-center gap-2">
                        <span
                            className={`hidden text-xs sm:inline ${live ? 'text-emerald-600 dark:text-emerald-400' : 'text-muted-foreground'}`}
                        >
                            {t(live ? 'live' : 'reconnecting')}
                        </span>
                        <Button
                            variant="ghost"
                            size="icon"
                            aria-label={t('refresh')}
                            onClick={() => {
                                void client.invalidateQueries({ queryKey: ['checkin'] });
                            }}
                        >
                            <RefreshCw className="size-4" />
                        </Button>
                        <Button
                            variant="ghost"
                            size="icon"
                            aria-label={t('settings')}
                            onClick={() => setModal({ kind: 'settings' })}
                        >
                            <Settings2 className="size-4" />
                        </Button>
                    </div>
                </div>
                <CheckinError error={sites.error || stats.error || run.error} />
                {tab === 'sites' && (
                    <>
                        <div className="flex flex-wrap items-center gap-2">
                            <Input
                                aria-label={t('search')}
                                placeholder={t('search')}
                                value={search}
                                onChange={(event) => setSearch(event.target.value)}
                                className="min-w-40 flex-1"
                            />
                            {validSelected.length > 0 && (
                                <Button
                                    variant="outline"
                                    disabled={run.isPending}
                                    onClick={() =>
                                        run.mutate(
                                            {
                                                path: '/jobs',
                                                body: { kind: 'sites', ids: validSelected },
                                            },
                                            { onSuccess: (job) => showJob(job.id) },
                                        )
                                    }
                                >
                                    {t('runSelected', { count: validSelected.length })}
                                </Button>
                            )}
                            <Button
                                variant="outline"
                                disabled={run.isPending || !sites.data?.length}
                                onClick={() =>
                                    run.mutate(
                                        { path: '/jobs', body: { kind: 'all', ids: [] } },
                                        { onSuccess: (job) => showJob(job.id) },
                                    )
                                }
                            >
                                <Play className="size-4" />
                                {t('runAll')}
                            </Button>
                            <Button onClick={() => setModal({ kind: 'site' })}>
                                <Plus className="size-4" />
                                {t('addSite')}
                            </Button>
                        </div>
                        <p className="text-xs text-muted-foreground">{t('overviewHint')}</p>
                        <div className="grid items-start gap-4 lg:grid-cols-2">
                            {visible.map((site) => (
                                <SiteCard
                                    key={site.id}
                                    site={site}
                                    selected={validSelected.includes(site.id)}
                                    onSelect={(value) =>
                                        setSelected(
                                            value
                                                ? [...new Set([...selected, site.id])]
                                                : selected.filter((id) => id !== site.id),
                                        )
                                    }
                                    onEdit={() => setModal({ kind: 'site', site })}
                                    onAccount={(account) =>
                                        setModal({ kind: 'account', site, account })
                                    }
                                    onDelete={(account) =>
                                        setModal({ kind: 'delete', site, account })
                                    }
                                    onHistory={(account) => {
                                        setFilter({
                                            siteID: site.id,
                                            accountID: account?.id ?? '',
                                        });
                                        setTab('history');
                                    }}
                                    onJob={showJob}
                                />
                            ))}
                        </div>
                        {!visible.length && (
                            <div className="flex flex-col items-center gap-3 rounded-3xl border border-dashed py-14 text-center">
                                <CalendarCheck className="size-10 text-muted-foreground" />
                                <p className="font-medium">
                                    {t(
                                        sites.isPending
                                            ? 'loading'
                                            : search
                                              ? 'noMatches'
                                              : 'noSites',
                                    )}
                                </p>
                                {!sites.isPending && !search && (
                                    <p className="max-w-sm px-5 text-sm text-muted-foreground">
                                        {t('siteDescription')}
                                    </p>
                                )}
                            </div>
                        )}
                    </>
                )}
                {(tab === 'history' || tab === 'stats') && (
                    <CheckinFilters sites={sites.data ?? []} value={filter} onChange={setFilter} />
                )}
                {tab === 'history' && (
                    <HistoryPanel
                        key={`${filter.siteID}:${filter.accountID}`}
                        filter={filter}
                        onRun={(run) => setModal({ kind: 'run', run })}
                    />
                )}
                {tab === 'stats' && <StatsPanel filter={filter} />}
                {tab === 'jobs' && <JobsPanel onJob={showJob} />}
            </div>
            {modal?.kind === 'site' && <SiteForm site={modal.site} onClose={close} />}
            {modal?.kind === 'account' && (
                <AccountForm site={modal.site} account={modal.account} onClose={close} />
            )}
            {modal?.kind === 'delete' && (
                <DeleteDialog site={modal.site} account={modal.account} onClose={close} />
            )}
            {modal?.kind === 'run' && <RunDetails run={modal.run} onClose={close} />}
            {modal?.kind === 'job' && <JobDetails id={modal.id} onClose={close} />}
            {modal?.kind === 'settings' && <SettingsPanel onClose={close} />}
        </div>
    );
}
