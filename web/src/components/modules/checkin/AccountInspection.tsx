import { useState } from 'react';
import { Loader2, RefreshCw, ShieldCheck } from 'lucide-react';
import { useTranslations } from 'use-intl';
import {
    type Capability,
    type CheckinAccount,
    type CheckinSite,
    rewardText,
    useCheckinMutation,
} from '@/api/checkin';
import { Button } from '@/components/ui/button';
import { CheckinDialog, CheckinError, CheckinStatus } from './common';

export function AccountInspection({
    account,
    site,
    capability,
}: {
    account: CheckinAccount;
    site: CheckinSite;
    capability?: Capability;
}) {
    const t = useTranslations('checkin');
    const mutation = useCheckinMutation<CheckinAccount>();
    const [open, setOpen] = useState(false);
    const balance = account.balance;
    const inspection = account.inspection;
    const busy = mutation.isPending || inspection?.status === 'running';
    const disabled = !account.enabled || !site.config.enabled || busy;
    const stale = balance && (!inspection?.balance_updated || busy);
    const amount = (value: string | undefined, state: string | undefined) =>
        state === 'known' && value !== undefined
            ? `${rewardText(value)} ${balance?.unit ?? ''}`
            : t(state === 'invalid' ? 'inspection.invalidAmount' : 'states.unknown');

    if (!capability?.account_detection && !capability?.balance_query && !inspection && !balance) {
        return capability ? (
            <p className="text-xs text-muted-foreground">{t('inspection.unsupported')}</p>
        ) : null;
    }

    return (
        <div className="space-y-2 rounded-xl border bg-background/50 p-3">
            <dl className="grid gap-2 text-xs sm:grid-cols-2">
                <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                    <dt className="text-muted-foreground">{t('inspection.balance')}</dt>
                    <dd className="break-all font-medium tabular-nums">
                        {amount(balance?.amount_micros, balance?.amount_state)}
                    </dd>
                </div>
                <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                    <dt className="text-muted-foreground">{t('inspection.used')}</dt>
                    <dd className="break-all tabular-nums">
                        {amount(balance?.used_micros, balance?.used_state)}
                    </dd>
                </div>
            </dl>
            {balance && (
                <p className="text-xs text-muted-foreground">
                    {t(stale ? 'inspection.lastKnown' : 'inspection.updatedAt')}:{' '}
                    <time dateTime={balance.updated_at}>
                        {new Date(balance.updated_at).toLocaleString()}
                    </time>
                </p>
            )}
            <div className="flex flex-wrap items-center gap-2">
                {capability?.balance_query && (
                    <Button
                        size="sm"
                        variant="outline"
                        disabled={disabled}
                        onClick={() => mutation.mutate({ path: `/accounts/${account.id}/balance` })}
                    >
                        <RefreshCw className="size-3.5" />
                        {t('inspection.refreshBalance')}
                    </Button>
                )}
                {capability?.account_detection && (
                    <Button
                        size="sm"
                        variant="outline"
                        disabled={disabled}
                        onClick={() => {
                            setOpen(true);
                            mutation.mutate({ path: `/accounts/${account.id}/detect` });
                        }}
                    >
                        <ShieldCheck className="size-3.5" />
                        {t('inspection.detect')}
                    </Button>
                )}
                {busy && <Loader2 className="size-4 animate-spin" aria-label={t('loading')} />}
                {inspection && (
                    <Button size="sm" variant="ghost" onClick={() => setOpen(true)}>
                        {t('inspection.details')}
                        <CheckinStatus value={inspection.status} />
                    </Button>
                )}
            </div>
            <CheckinError error={mutation.error} />
            {open && (
                <CheckinDialog
                    title={t('inspection.title', { name: account.name })}
                    description={t('inspection.hint')}
                    onClose={() => setOpen(false)}
                >
                    <div className="space-y-4">
                        {busy && <p role="status" className="text-sm">{t('inspection.running')}</p>}
                        <CheckinError error={mutation.error} />
                        {inspection && (!mutation.isPending || inspection.status === 'running') && (
                            <>
                                <div className="flex flex-wrap items-center gap-2 text-sm">
                                    <span>{t(`inspection.kinds.${inspection.kind}`)}</span>
                                    <CheckinStatus value={inspection.status} />
                                </div>
                                <p className="text-sm text-muted-foreground">
                                    {t.has(`reasons.${inspection.reason}`)
                                        ? t(`reasons.${inspection.reason}`)
                                        : t('inspection.running')}
                                </p>
                                <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                                    <dt className="text-muted-foreground">{t('inspection.authentication')}</dt>
                                    <dd>{t(`inspection.auth.${inspection.authentication}`)}</dd>
                                    {inspection.user_id && (
                                        <>
                                            <dt className="text-muted-foreground">{t('inspection.verifiedUser')}</dt>
                                            <dd>{inspection.user_id}</dd>
                                        </>
                                    )}
                                    {inspection.kind === 'detect' && (
                                        <>
                                            <dt className="text-muted-foreground">{t('inspection.checkinSupport')}</dt>
                                            <dd>{t(`inspection.support.${inspection.checkin_support}`)}</dd>
                                            <dt className="text-muted-foreground">{t('inspection.checkinStatus')}</dt>
                                            <dd>
                                                {t(`states.${inspection.checkin_status}`)}
                                                <span className="mt-1 block text-xs text-muted-foreground">
                                                    {inspection.checkin_date} · {inspection.timezone}
                                                </span>
                                            </dd>
                                        </>
                                    )}
                                    <dt className="text-muted-foreground">{t('inspection.checkedAt')}</dt>
                                    <dd>{new Date(inspection.finished_at || inspection.started_at).toLocaleString()}</dd>
                                </dl>
                                <div className="space-y-2">
                                    <p className="text-sm font-medium">{t('inspection.steps')}</p>
                                    {inspection.steps.length ? (
                                        <ol className="space-y-2">
                                            {inspection.steps.map((step, index) => (
                                                <li key={index} className="rounded-xl border p-3 text-xs">
                                                    <div className="flex flex-wrap justify-between gap-2">
                                                        <span>{index + 1}. {t(`stages.${step.stage}`)}</span>
                                                        <span className="text-muted-foreground">
                                                            HTTP {step.http_status || '—'} · {step.duration_ms} ms
                                                        </span>
                                                    </div>
                                                    <p className="mt-1 text-muted-foreground">{t(`reasons.${step.reason}`)}</p>
                                                </li>
                                            ))}
                                        </ol>
                                    ) : (
                                        <p className="text-xs text-muted-foreground">{t('inspection.noRequests')}</p>
                                    )}
                                </div>
                            </>
                        )}
                    </div>
                </CheckinDialog>
            )}
        </div>
    );
}
