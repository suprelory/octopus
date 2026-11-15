import { useState } from 'react';
import { CalendarCheck2, ChevronDown, History, MoreHorizontal, Pencil, Trash2 } from 'lucide-react';
import { useTranslations } from 'use-intl';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Switch } from '@/components/ui/switch';
import {
    type Capability,
    type CheckinAccount,
    type CheckinSite,
    useCheckinMutation,
} from '@/api/checkin';
import { CheckinError, CheckinStatus } from './common';
import { AccountInspection } from './AccountInspection';

type Props = {
    account: CheckinAccount;
    site: CheckinSite;
    capability?: Capability;
    checkinPending: boolean;
    onCheckin: () => void;
    onEdit: () => void;
    onDelete: () => void;
    onHistory: () => void;
};

export function CheckinAccountCard({
    account,
    site,
    capability,
    checkinPending,
    onCheckin,
    onEdit,
    onDelete,
    onHistory,
}: Props) {
    const t = useTranslations('checkin');
    const [actionsOpen, setActionsOpen] = useState(false);
    const mutation = useCheckinMutation();
    const sessionDetails = site.config.adapter === 'sub2api' && account.credential_type === 'bearer';
    const credentialLabel =
        account.credential_type === 'cookie'
            ? t('cookieCredential')
            : t(account.credential_type === 'password' ? 'usernamePassword' : 'accessToken');

    const menuAction = (action: () => void) => () => {
        setActionsOpen(false);
        action();
    };

    return (
        <article className="space-y-3 rounded-2xl border bg-card p-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
                <div className="min-w-0 flex-1">
                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <h4 className="max-w-full break-words text-sm font-semibold">{account.name}</h4>
                        <span className="inline-flex max-w-full items-center rounded-md border bg-muted/30 px-2 py-0.5 text-xs text-muted-foreground">
                            {credentialLabel}
                        </span>
                        <CheckinStatus value={account.last_status} />
                        {!account.enabled && (
                            <span className="inline-flex items-center rounded-md border bg-muted/30 px-2 py-0.5 text-xs text-muted-foreground">
                                {t('disabled')}
                            </span>
                        )}
                    </div>
                    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                        <span>
                            {account.next_run_at
                                ? `${t('nextRun')}: ${new Date(account.next_run_at).toLocaleString()}`
                                : t(account.auto_checkin ? 'waitingSchedule' : 'manualOnly')}
                        </span>
                        {account.last_success_date && !account.last_success_date.startsWith('0001-') && (
                            <span>
                                {t('lastSuccess')}: {account.last_success_date}
                            </span>
                        )}
                    </div>
                </div>

                <div className="flex shrink-0 items-center gap-2 sm:justify-end">
                    <Button
                        size="sm"
                        variant="outline"
                        disabled={!site.config.enabled || !account.enabled || checkinPending}
                        onClick={onCheckin}
                    >
                        <CalendarCheck2 className="size-3.5" />
                        {t('checkin')}
                    </Button>
                    <Popover open={actionsOpen} onOpenChange={setActionsOpen}>
                        <PopoverTrigger asChild>
                            <Button
                                type="button"
                                size="icon"
                                variant="ghost"
                                className="size-8"
                                aria-label={t('accountActions')}
                                title={t('accountActions')}
                            >
                                <MoreHorizontal className="size-4" />
                            </Button>
                        </PopoverTrigger>
                        <PopoverContent align="end" className="w-44 rounded-xl p-1.5">
                            <div className="grid gap-1">
                                <button
                                    type="button"
                                    className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm hover:bg-muted/60"
                                    onClick={menuAction(onHistory)}
                                >
                                    <History className="size-4" />
                                    {t('history')}
                                </button>
                                <button
                                    type="button"
                                    className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm hover:bg-muted/60"
                                    onClick={menuAction(onEdit)}
                                >
                                    <Pencil className="size-4" />
                                    {t('editAccount')}
                                </button>
                                <button
                                    type="button"
                                    className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm text-destructive hover:bg-destructive/10"
                                    onClick={menuAction(onDelete)}
                                >
                                    <Trash2 className="size-4" />
                                    {t('deleteAccount')}
                                </button>
                            </div>
                        </PopoverContent>
                    </Popover>
                </div>
            </div>

            <div className="grid gap-2 border-y py-3 text-xs sm:grid-cols-2">
                <div className="flex min-w-0 items-center justify-between gap-3">
                    <span className="text-muted-foreground">{t('autoCheckin')}</span>
                    <div className="flex shrink-0 items-center gap-2">
                        <span className="text-muted-foreground">
                            {t(account.auto_checkin ? 'enabled' : 'disabled')}
                        </span>
                        <Switch
                            aria-label={t('autoCheckin')}
                            checked={account.auto_checkin}
                            disabled={mutation.isPending}
                            onCheckedChange={(auto_checkin) =>
                                mutation.mutate({
                                    path: `/accounts/${account.id}`,
                                    method: 'PATCH',
                                    body: { auto_checkin, revision: account.revision },
                                })
                            }
                        />
                    </div>
                </div>
                {account.has_session && account.session_saved_at && (
                    <div className="flex min-w-0 items-center justify-between gap-3">
                        <span className="text-muted-foreground">{t('sessionSavedAt')}</span>
                        <time className="truncate text-right" dateTime={account.session_saved_at}>
                            {new Date(account.session_saved_at).toLocaleString()}
                        </time>
                    </div>
                )}
            </div>

            {sessionDetails && (
                <details className="group text-xs">
                    <summary className="flex cursor-pointer list-none items-center gap-2 text-muted-foreground hover:text-foreground [&::-webkit-details-marker]:hidden">
                        <span>{t('sessionInfo')}</span>
                        <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" />
                    </summary>
                    <div className="mt-2 space-y-1 pl-1 text-muted-foreground">
                        {site.config.adapter === 'sub2api' && account.credential_type === 'bearer' && (
                            <>
                                <p>
                                    {t(
                                        account.has_refresh_token
                                            ? 'sessionRefreshEnabled'
                                            : 'sessionRefreshDisabled',
                                    )}
                                </p>
                                {account.token_expires_at && (
                                    <p>
                                        {t('tokenExpiry')}: {new Date(account.token_expires_at).toLocaleString()}
                                    </p>
                                )}
                                {account.token_refreshed_at && (
                                    <p>
                                        {t('tokenRefreshedAt')}: {new Date(account.token_refreshed_at).toLocaleString()}
                                    </p>
                                )}
                                {(account.refresh_error || account.refresh_pending) && (
                                    <p className="text-amber-600 dark:text-amber-400">
                                        {account.refresh_error && t.has(`reasons.${account.refresh_error}`)
                                            ? t(`reasons.${account.refresh_error}`)
                                            : t('sessionRefreshPending')}
                                    </p>
                                )}
                            </>
                        )}
                    </div>
                </details>
            )}

            <AccountInspection account={account} site={site} capability={capability} />
            <CheckinError error={mutation.error} />
        </article>
    );
}
