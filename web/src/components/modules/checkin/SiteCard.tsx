import { useState } from 'react';
import { ChevronDown, ExternalLink, History, Pencil, Play, Plus, Trash2 } from 'lucide-react';
import { useTranslations } from 'use-intl';
import { Button } from '@/components/ui/button';
import {
    type CheckinAccount,
    type CheckinJob,
    type CheckinSite,
    useCheckinMutation,
    useCheckinQuery,
} from '@/api/checkin';
import { CheckinError, CheckinStatus, CheckinToggle } from './common';

type Props = {
    site: CheckinSite;
    selected: boolean;
    onSelect: (selected: boolean) => void;
    onEdit: () => void;
    onAccount: (account?: CheckinAccount) => void;
    onDelete: (account?: CheckinAccount) => void;
    onHistory: (account?: CheckinAccount) => void;
    onJob: (id: string) => void;
};

export function SiteCard({
    site,
    selected,
    onSelect,
    onEdit,
    onAccount,
    onDelete,
    onHistory,
    onJob,
}: Props) {
    const t = useTranslations('checkin');
    const [expanded, setExpanded] = useState(false);
    const accounts = useCheckinQuery<CheckinAccount[]>(`/sites/${site.id}/accounts`, expanded);
    const mutate = useCheckinMutation();
    const run = useCheckinMutation<CheckinJob>();
    return (
        <article className="overflow-hidden rounded-3xl border bg-card">
            <div className="space-y-3 p-5">
                <div className="flex items-start gap-3">
                    <input
                        type="checkbox"
                        aria-label={t('selectSite', { name: site.config.name })}
                        checked={selected}
                        onChange={(event) => onSelect(event.target.checked)}
                        className="mt-1 size-4 accent-[var(--primary)]"
                    />
                    <button
                        type="button"
                        aria-expanded={expanded}
                        onClick={() => setExpanded(!expanded)}
                        className="min-w-0 flex-1 text-left"
                    >
                        <h3 className="truncate font-semibold">{site.config.name}</h3>
                        <p className="mt-1 truncate text-xs text-muted-foreground">
                            {site.config.base_url}
                        </p>
                    </button>
                    <Button variant="ghost" size="icon" aria-label={t('editSite')} onClick={onEdit}>
                        <Pencil className="size-4" />
                    </Button>
                    <Button
                        variant="ghost"
                        size="icon"
                        aria-label={t('deleteSite')}
                        onClick={() => onDelete()}
                    >
                        <Trash2 className="size-4" />
                    </Button>
                </div>
                <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                    <span>{site.config.adapter}</span>
                    <span>
                        {site.config.timezone} · {site.config.window_start}–{site.config.window_end}
                    </span>
                    {site.support &&
                        site.support !== 'unknown' &&
                        site.support_revision === site.revision && (
                            <CheckinStatus value={site.support} />
                        )}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                    <CheckinToggle
                        label={t('enabled')}
                        checked={site.config.enabled}
                        onChange={(enabled) =>
                            mutate.mutate({
                                path: `/sites/${site.id}`,
                                method: 'PATCH',
                                body: {
                                    config: { ...site.config, enabled },
                                    revision: site.revision,
                                },
                            })
                        }
                    />
                    <div className="ml-auto flex flex-wrap gap-1">
                        <Button
                            size="sm"
                            variant="outline"
                            disabled={!site.config.enabled || run.isPending}
                            onClick={() =>
                                run.mutate(
                                    { path: '/jobs', body: { kind: 'sites', ids: [site.id] } },
                                    { onSuccess: (job) => onJob(job.id) },
                                )
                            }
                        >
                            <Play className="size-3.5" />
                            {t('checkin')}
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => onAccount()}>
                            <Plus className="size-3.5" />
                            {t('addAccount')}
                        </Button>
                        <Button
                            size="icon"
                            variant="ghost"
                            aria-label={t('history')}
                            onClick={() => onHistory()}
                        >
                            <History className="size-4" />
                        </Button>
                        <Button
                            size="icon"
                            variant="ghost"
                            aria-label={t('accounts')}
                            aria-expanded={expanded}
                            onClick={() => setExpanded(!expanded)}
                        >
                            <ChevronDown
                                className={`size-4 transition-transform ${expanded ? 'rotate-180' : ''}`}
                            />
                        </Button>
                    </div>
                </div>
                <CheckinError error={mutate.error || run.error} />
            </div>
            {expanded && (
                <div className="border-t bg-muted/20 px-5">
                    <CheckinError error={accounts.error} />
                    {accounts.data?.map((account) => (
                        <div key={account.id} className="space-y-2 border-b py-4 last:border-b-0">
                            <div className="flex flex-wrap items-center gap-2">
                                <span className="font-medium text-sm">{account.name}</span>
                                <span className="text-xs text-muted-foreground">
                                    {account.credential_type === 'cookie'
                                        ? 'Cookie'
                                        : t('accessToken')}
                                </span>
                                <CheckinStatus value={account.last_status} />
                                {!account.enabled && (
                                    <span className="text-xs text-muted-foreground">
                                        {t('disabled')}
                                    </span>
                                )}
                            </div>
                            <p className="text-xs text-muted-foreground">
                                {account.next_run_at
                                    ? `${t('nextRun')}: ${new Date(account.next_run_at).toLocaleString()}`
                                    : t(account.auto_checkin ? 'waitingSchedule' : 'manualOnly')}
                            </p>
                            {site.config.adapter === 'sub2api' && account.credential_type === 'bearer' && (
                                <div className="space-y-1 text-xs text-muted-foreground">
                                    <p>{t(account.has_refresh_token ? 'sessionRefreshEnabled' : 'sessionRefreshDisabled')}</p>
                                    {account.token_expires_at && (
                                        <p>{t('tokenExpiry')}: {new Date(account.token_expires_at).toLocaleString()}</p>
                                    )}
                                    {account.token_refreshed_at && (
                                        <p>{t('tokenRefreshedAt')}: {new Date(account.token_refreshed_at).toLocaleString()}</p>
                                    )}
                                    {(account.refresh_error || account.refresh_pending) && (
                                        <p className="text-amber-600 dark:text-amber-400">
                                            {account.refresh_error && t.has(`reasons.${account.refresh_error}`)
                                                ? t(`reasons.${account.refresh_error}`)
                                                : t('sessionRefreshPending')}
                                        </p>
                                    )}
                                </div>
                            )}
                            <div className="flex flex-wrap items-center gap-2">
                                <CheckinToggle
                                    label={t('autoCheckin')}
                                    checked={account.auto_checkin}
                                    onChange={(auto_checkin) =>
                                        mutate.mutate({
                                            path: `/accounts/${account.id}`,
                                            method: 'PATCH',
                                            body: { auto_checkin, revision: account.revision },
                                        })
                                    }
                                />
                                <div className="ml-auto flex gap-1">
                                    <Button
                                        size="sm"
                                        variant="outline"
                                        disabled={
                                            !site.config.enabled ||
                                            !account.enabled ||
                                            run.isPending
                                        }
                                        onClick={() =>
                                            run.mutate(
                                                { path: `/accounts/${account.id}/checkin` },
                                                { onSuccess: (job) => onJob(job.id) },
                                            )
                                        }
                                    >
                                        {t('checkin')}
                                    </Button>
                                    <Button
                                        size="icon"
                                        variant="ghost"
                                        aria-label={t('editAccount')}
                                        onClick={() => onAccount(account)}
                                    >
                                        <Pencil className="size-3.5" />
                                    </Button>
                                    <Button
                                        size="icon"
                                        variant="ghost"
                                        aria-label={t('history')}
                                        onClick={() => onHistory(account)}
                                    >
                                        <History className="size-3.5" />
                                    </Button>
                                    <Button
                                        size="icon"
                                        variant="ghost"
                                        aria-label={t('deleteAccount')}
                                        onClick={() => onDelete(account)}
                                    >
                                        <Trash2 className="size-3.5" />
                                    </Button>
                                </div>
                            </div>
                        </div>
                    ))}
                    {!accounts.data?.length && (
                        <p className="py-5 text-center text-sm text-muted-foreground">
                            {t(accounts.isPending ? 'loading' : 'noAccounts')}
                        </p>
                    )}
                    <a
                        href={site.config.manual_url || site.config.base_url}
                        target="_blank"
                        rel="noreferrer"
                        className="mb-4 inline-flex items-center gap-1 text-xs text-muted-foreground underline underline-offset-4"
                    >
                        {t('openSite')}
                        <ExternalLink className="size-3" />
                    </a>
                </div>
            )}
        </article>
    );
}
