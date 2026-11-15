import { useId, useState } from 'react';
import { useTranslations } from 'use-intl';
import { type ProxyMode, type ProxySelection, useProxyPool } from '@/api/proxy-pool';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { ProxyPoolDialog } from './ProxyPoolDialog';

export function ProxySelector({ value, onChange, allowInherit = false, parent }: {
    value: ProxySelection; onChange: (value: ProxySelection) => void;
    allowInherit?: boolean; parent?: ProxySelection;
}) {
    const t = useTranslations('proxyPool');
    const id = useId();
    const pool = useProxyPool();
    const [manage, setManage] = useState(false);
    const mode = value.proxy_mode;
    const effective = mode === 'inherit' ? parent : value;
    const selected = pool.data?.find((node) => node.id === effective?.proxy_config_id);
    const unavailable = effective?.proxy_mode === 'pool' && !!effective.proxy_config_id && !pool.isPending && (!selected || !selected.enabled);
    const summary = effective?.proxy_mode === 'pool'
        ? selected?.name ?? t('missing')
        : effective?.proxy_mode === 'custom' ? effective.proxy_url : effective ? t(`modes.${effective.proxy_mode}`) : '';
    return (
        <div className="space-y-2">
            <div className="flex items-center justify-between gap-3">
                <label htmlFor={id} className="text-sm font-medium">{t('proxy')}</label>
                <Button type="button" variant="ghost" size="sm" onClick={() => setManage(true)}>{t('manage')}</Button>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
                <select id={id} className="h-9 rounded-md border bg-background px-3 text-sm" value={mode}
                    onChange={(event) => onChange({ ...value, proxy_mode: event.target.value as ProxyMode, proxy_config_id: null })}>
                    {(allowInherit ? ['inherit', 'direct', 'system', 'pool', 'custom'] : ['direct', 'system', 'pool', 'custom']).map((choice) => <option key={choice} value={choice}>{t(`modes.${choice}`)}</option>)}
                </select>
                {mode === 'pool' && <select required aria-label={t('node')} className="h-9 rounded-md border bg-background px-3 text-sm" value={value.proxy_config_id ?? ''}
                    onChange={(event) => onChange({ ...value, proxy_config_id: event.target.value ? Number(event.target.value) : null })}>
                    <option value="">{pool.isPending ? t('loading') : t('choose')}</option>
                    {value.proxy_config_id && !pool.data?.some((node) => node.id === value.proxy_config_id) && <option value={value.proxy_config_id} disabled>{t('missing')}</option>}
                    {pool.data?.filter((node) => node.enabled || node.id === value.proxy_config_id).map((node) => <option key={node.id} value={node.id} disabled={!node.enabled}>{node.name}{!node.enabled ? ` (${t('disabled')})` : ''}</option>)}
                </select>}
                {mode === 'custom' && <Input required aria-label={t('url')} value={value.proxy_url ?? ''} placeholder="http://127.0.0.1:7890" onChange={(event) => onChange({ ...value, proxy_url: event.target.value })} />}
            </div>
            {mode === 'inherit' && <p className="break-all text-xs text-muted-foreground">{t('effective', { value: summary ?? '' })}</p>}
            {mode === 'system' && <p className="text-xs text-muted-foreground">{t('systemHint')}</p>}
            {unavailable && <p role="alert" className="text-xs text-destructive">{t('unavailable')}</p>}
            {pool.error && <p role="alert" className="text-xs text-destructive">{t('loadFailed')}</p>}
            {manage && <ProxyPoolDialog onClose={() => setManage(false)} />}
        </div>
    );
}
