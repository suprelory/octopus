import { useState } from 'react';
import { Bell, Plus, RefreshCw, Send, Pencil, Trash2 } from 'lucide-react';
import { useTranslations } from 'use-intl';
import {
    type CheckinSite,
    type NotificationTarget,
    type NotificationDelivery,
    useCheckinQuery,
    useCheckinMutation,
} from '@/api/checkin';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { CheckinDialog, CheckinError, CheckinSelect } from './common';
import { NotificationForm } from './NotificationForm';

export function NotificationsPanel({
    sites,
    onClose,
}: {
    sites: CheckinSite[];
    onClose: () => void;
}) {
    const t = useTranslations('checkin.notifications');
    const common = useTranslations('checkin');
    const targets = useCheckinQuery<NotificationTarget[]>('/notifications');
    const [tab, setTab] = useState<'targets' | 'deliveries'>('targets');
    const [filter, setFilter] = useState('');
    const [cursors, setCursors] = useState<string[]>([]);
    const before = cursors.at(-1) ?? '';
    const deliveries = useCheckinQuery<NotificationDelivery[]>(
        `/notification-deliveries?limit=50&target_id=${filter}&before=${before}`,
        tab === 'deliveries',
    );
    const [edit, setEdit] = useState<NotificationTarget | 'new' | null>(null);
    const [remove, setRemove] = useState<NotificationTarget | null>(null);
    const [retry, setRetry] = useState<NotificationDelivery | null>(null);
    const [notice, setNotice] = useState('');
    const action = useCheckinMutation();
    if (edit)
        return (
            <NotificationForm
                target={edit === 'new' ? undefined : edit}
                sites={sites}
                onClose={() => setEdit(null)}
            />
        );
    const retryTarget = targets.data?.find((target) => target.id === retry?.target_id);
    if (remove || retry) {
        const cancel = () => {
            setRemove(null);
            setRetry(null);
            action.reset();
        };
        return (
            <CheckinDialog title={remove ? common('delete') : t('retry')} onClose={cancel}>
                <div className="space-y-4">
                    <p className="break-words text-sm">
                        {remove ? t('deleteHint', { name: remove.config.name }) : t('retryHint')}
                    </p>
                    {retry && (
                        <p className="break-words text-xs text-muted-foreground">
                            {retry.target_name} · {retry.title}
                        </p>
                    )}
                    <CheckinError error={action.error} />
                    <div className="flex justify-end gap-2">
                        <Button variant="outline" onClick={cancel}>
                            {common('cancel')}
                        </Button>
                        <Button
                            variant={remove ? 'destructive' : 'default'}
                            disabled={action.isPending || (!!retry && !retryTarget)}
                            onClick={() => {
                                if (remove)
                                    action.mutate(
                                        {
                                            path: `/notifications/${remove.id}`,
                                            method: 'DELETE',
                                            body: { revision: remove.revision },
                                        },
                                        {
                                            onSuccess: () => {
                                                setRemove(null);
                                                setNotice('');
                                            },
                                        },
                                    );
                                else if (retry)
                                    action.mutate(
                                        {
                                            path: `/notification-deliveries/${retry.id}/retry`,
                                            body: {
                                                attempts: retry.attempts,
                                                target_revision: retryTarget?.revision,
                                            },
                                        },
                                        {
                                            onSuccess: () => {
                                                setRetry(null);
                                                setNotice(t('retryQueued'));
                                            },
                                        },
                                    );
                            }}
                        >
                            {remove ? common('delete') : t('confirmRetry')}
                        </Button>
                    </div>
                </div>
            </CheckinDialog>
        );
    }
    return (
        <CheckinDialog title={t('title')} description={t('description')} onClose={onClose}>
            <div className="space-y-5">
                <div className="flex flex-wrap items-center justify-between gap-2">
                    <nav className="flex gap-1" aria-label={t('sections')}>
                        {(['targets', 'deliveries'] as const).map((value) => (
                            <Button
                                key={value}
                                size="sm"
                                variant={tab === value ? 'default' : 'ghost'}
                                aria-pressed={tab === value}
                                onClick={() => {
                                    setTab(value);
                                    setRetry(null);
                                    setRemove(null);
                                }}
                            >
                                {t(value)}
                            </Button>
                        ))}
                    </nav>
                    <div className="flex gap-1">
                        <Button
                            variant="ghost"
                            size="icon"
                            aria-label={common('refresh')}
                            onClick={() => {
                                void targets.refetch();
                                if (tab === 'deliveries') void deliveries.refetch();
                            }}
                        >
                            <RefreshCw className="size-4" />
                        </Button>
                        {tab === 'targets' && (
                            <Button
                                size="sm"
                                onClick={() => setEdit('new')}
                                disabled={targets.isPending || (targets.data?.length ?? 0) >= 20}
                            >
                                <Plus className="size-4" />
                                {t('add')}
                            </Button>
                        )}
                    </div>
                </div>
                <CheckinError
                    error={
                        targets.error || (tab === 'deliveries' && deliveries.error) || action.error
                    }
                />
                {notice && (
                    <p role="status" className="rounded-xl bg-muted p-3 text-sm">
                        {notice}
                    </p>
                )}
                {tab === 'targets' && (
                    <div className="space-y-3">
                        {targets.isPending && (
                            <p className="text-sm text-muted-foreground">{common('loading')}</p>
                        )}
                        {targets.data?.map((target) => (
                            <article key={target.id} className="space-y-3 rounded-2xl border p-4">
                                <div className="flex flex-wrap items-center gap-2">
                                    <h3 className="min-w-0 break-words font-medium">
                                        {target.config.name}
                                    </h3>
                                    <span className="rounded-full bg-muted px-2 py-0.5 text-xs">
                                        {t(`platforms.${target.config.kind}`)}
                                    </span>
                                    <span
                                        className={cn(
                                            'text-xs',
                                            target.config.enabled
                                                ? 'text-emerald-600 dark:text-emerald-400'
                                                : 'text-muted-foreground',
                                        )}
                                    >
                                        {t(target.config.enabled ? 'active' : 'disabled')}
                                    </span>
                                </div>
                                <p className="break-words text-xs text-muted-foreground">
                                    {target.config.events
                                        .map((event) => t(`events.${event}`))
                                        .join(' · ')}
                                </p>
                                <p className="text-xs text-muted-foreground">
                                    {target.config.scope === 'all'
                                        ? t('allSites')
                                        : t('siteCount', {
                                              count: target.config.site_ids.length,
                                          })}{' '}
                                    ·{' '}
                                    {t(
                                        target.has_connection
                                            ? 'connectionSaved'
                                            : 'connectionMissing',
                                    )}
                                </p>
                                <div className="flex flex-wrap gap-2">
                                    <Button
                                        size="sm"
                                        variant="outline"
                                        onClick={() => setEdit(target)}
                                    >
                                        <Pencil className="size-3.5" />
                                        {t('edit')}
                                    </Button>
                                    <Button
                                        size="sm"
                                        variant="outline"
                                        disabled={action.isPending}
                                        onClick={() =>
                                            action.mutate(
                                                {
                                                    path: `/notifications/${target.id}/test`,
                                                    body: { revision: target.revision },
                                                },
                                                {
                                                    onSuccess: () => {
                                                        setNotice(t('testQueued'));
                                                        setFilter(target.id);
                                                        setCursors([]);
                                                        setTab('deliveries');
                                                    },
                                                },
                                            )
                                        }
                                    >
                                        <Send className="size-3.5" />
                                        {t('test')}
                                    </Button>
                                    <Button
                                        size="sm"
                                        variant="ghost"
                                        onClick={() => {
                                            setRemove(target);
                                            setNotice('');
                                        }}
                                    >
                                        <Trash2 className="size-3.5" />
                                        {common('delete')}
                                    </Button>
                                </div>
                            </article>
                        ))}
                        {!targets.isPending && !targets.data?.length && (
                            <div className="flex flex-col items-center gap-3 rounded-2xl border border-dashed py-10 text-center">
                                <Bell className="size-8 text-muted-foreground" />
                                <p className="text-sm">{t('empty')}</p>
                                <p className="max-w-sm px-4 text-xs text-muted-foreground">
                                    {t('emptyHint')}
                                </p>
                            </div>
                        )}
                    </div>
                )}
                {tab === 'deliveries' && (
                    <div className="space-y-3">
                        <CheckinSelect
                            aria-label={t('filterTarget')}
                            className="w-full"
                            value={filter}
                            onChange={(event) => {
                                setFilter(event.target.value);
                                setCursors([]);
                                setRetry(null);
                            }}
                        >
                            <option value="">{t('allTargets')}</option>
                            {filter && !targets.data?.some((target) => target.id === filter) && (
                                <option value={filter}>{t('deletedTarget')}</option>
                            )}
                            {targets.data?.map((target) => (
                                <option key={target.id} value={target.id}>
                                    {target.config.name}
                                </option>
                            ))}
                        </CheckinSelect>
                        {deliveries.isPending && (
                            <p className="text-sm text-muted-foreground">{common('loading')}</p>
                        )}
                        {deliveries.data?.map((delivery) => {
                            const target = targets.data?.find(
                                (target) => target.id === delivery.target_id,
                            );
                            const canRetry =
                                ['failed', 'uncertain', 'skipped'].includes(delivery.status) &&
                                target &&
                                (target.config.enabled || delivery.event.kind === 'test') &&
                                delivery.attempts < 100;
                            return (
                                <article
                                    key={delivery.id}
                                    className="space-y-3 rounded-2xl border p-4"
                                >
                                    <div className="flex flex-wrap items-center gap-2">
                                        <h3 className="min-w-0 break-words text-sm font-medium">
                                            {delivery.target_name}
                                        </h3>
                                        <span
                                            className={cn(
                                                'rounded-full px-2 py-0.5 text-xs',
                                                delivery.status === 'sent'
                                                    ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400'
                                                    : delivery.status === 'failed'
                                                      ? 'bg-destructive/10 text-destructive'
                                                      : 'bg-muted text-muted-foreground',
                                            )}
                                        >
                                            {t(`states.${delivery.status}`)}
                                        </span>
                                    </div>
                                    <p className="break-words text-sm">
                                        {t(`events.${delivery.event.kind}`)} ·{' '}
                                        {delivery.event.site_name} / {delivery.event.account_name}
                                    </p>
                                    <p className="text-xs text-muted-foreground">
                                        {new Date(delivery.created_at).toLocaleString()} ·{' '}
                                        {t('attempts', { count: delivery.attempts })}
                                        {delivery.http_status
                                            ? ` · HTTP ${delivery.http_status}`
                                            : ''}
                                    </p>
                                    <p className="text-xs text-muted-foreground">
                                        {t.has(`reasons.${delivery.reason}`)
                                            ? t(`reasons.${delivery.reason}`)
                                            : delivery.reason}
                                    </p>
                                    {delivery.next_attempt_at && (
                                        <p className="text-xs text-muted-foreground">
                                            {t('nextAttempt', {
                                                time: new Date(
                                                    delivery.next_attempt_at,
                                                ).toLocaleString(),
                                            })}
                                        </p>
                                    )}
                                    <details className="text-xs">
                                        <summary className="cursor-pointer text-muted-foreground">
                                            {t('viewMessage')}
                                        </summary>
                                        <p className="mt-3 break-words font-medium">
                                            {delivery.title}
                                        </p>
                                        <pre className="mt-2 max-h-52 overflow-y-auto whitespace-pre-wrap break-words font-sans">
                                            {delivery.message}
                                        </pre>
                                    </details>
                                    {canRetry && (
                                        <Button
                                            size="sm"
                                            variant="outline"
                                            disabled={action.isPending}
                                            onClick={() => {
                                                setRetry(delivery);
                                                setNotice('');
                                            }}
                                        >
                                            {t('retry')}
                                        </Button>
                                    )}
                                </article>
                            );
                        })}
                        {!deliveries.isPending && !deliveries.data?.length && (
                            <p className="py-6 text-center text-sm text-muted-foreground">
                                {t('noDeliveries')}
                            </p>
                        )}
                        <div className="flex justify-between gap-2">
                            <Button
                                variant="outline"
                                size="sm"
                                disabled={!cursors.length || deliveries.isPending}
                                onClick={() => setCursors((previous) => previous.slice(0, -1))}
                            >
                                {t('newer')}
                            </Button>
                            <Button
                                variant="outline"
                                size="sm"
                                disabled={deliveries.isPending || deliveries.data?.length !== 50}
                                onClick={() => {
                                    const last = deliveries.data?.at(-1);
                                    if (last) setCursors((previous) => [...previous, last.id]);
                                }}
                            >
                                {t('older')}
                            </Button>
                        </div>
                    </div>
                )}
            </div>
        </CheckinDialog>
    );
}
