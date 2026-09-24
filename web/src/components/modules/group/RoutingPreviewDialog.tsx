'use client';

import { useState } from 'react';
import { Route } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { type Group, usePreviewGroupRouting } from '@/api/endpoints/group';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';

function sampleRequest(endpoint: string, model: string) {
    const content = endpoint === 'images' ? {prompt: 'Hello'} : ['chat', 'messages'].includes(endpoint)
        ? {messages: [{role: 'user', content: 'Hello'}], max_tokens: 100} : {input: 'Hello'};
    return JSON.stringify({model, ...content}, null, 2);
}

export function RoutingPreviewDialog({group}: {group: Group}) {
    const t = useTranslations('routing');
    const preview = usePreviewGroupRouting();
    const [endpoint, setEndpoint] = useState('responses');
    const [apiKeyID, setAPIKeyID] = useState('0');
    const [body, setBody] = useState(() => sampleRequest('responses', group.name));
    const [headers, setHeaders] = useState('{}');
    const [error, setError] = useState('');
    const result = preview.data;
    const selected = result?.candidates.find((candidate) => candidate.eligible);
    const reset = () => { preview.reset(); setError(''); };
    const run = async () => {
        try {
            setError('');
            preview.reset();
            const request: unknown = JSON.parse(body);
            const parsedHeaders: unknown = JSON.parse(headers);
            if (!request || typeof request !== 'object' || Array.isArray(request) || !parsedHeaders || typeof parsedHeaders !== 'object' || Array.isArray(parsedHeaders)) throw new Error(t('invalidJSON'));
            const id = Number(apiKeyID);
            if (!Number.isSafeInteger(id) || id < 0) throw new Error(t('invalidKeyID'));
            const headerValues: Record<string, string[]> = {};
            for (const [key, value] of Object.entries(parsedHeaders)) {
                if (typeof value !== 'string') throw new Error(t('invalidHeaders'));
                headerValues[key] = [value];
            }
            await preview.mutateAsync({group_id: group.id!, api_key_id: id, endpoint, request: request as Record<string, unknown>, headers: headerValues});
        } catch (err) { setError(err instanceof Error ? err.message : t('failed')); }
    };
    return <Dialog>
        <DialogTrigger asChild><button type="button" aria-label={t('title')} title={t('title')} disabled={!group.id} className="rounded-md p-2 text-muted-foreground hover:bg-muted hover:text-foreground"><Route className="size-4" /></button></DialogTrigger>
        <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-4xl">
            <DialogHeader><DialogTitle>{t('title')} · {group.name}</DialogTitle><DialogDescription>{t('description')}</DialogDescription></DialogHeader>
            <p className="text-sm text-muted-foreground">{t('rules')}</p>
            <div className="grid gap-4 sm:grid-cols-2">
                <label className="space-y-1 text-sm">{t('endpoint')}<select value={endpoint} disabled={preview.isPending} className="block h-9 w-full rounded-md border bg-background px-3" onChange={(e) => { setEndpoint(e.target.value); setBody(sampleRequest(e.target.value, group.name)); reset(); }}>
                    {['responses', 'chat', 'messages', 'embeddings', 'websocket', 'images', 'compact'].map((value) => <option key={value} value={value}>{value}</option>)}
                </select></label>
                <label className="space-y-1 text-sm">{t('apiKeyID')}<Input type="number" min={0} step={1} value={apiKeyID} disabled={preview.isPending} onChange={(e) => {setAPIKeyID(e.target.value); reset();}} /></label>
                <label className="space-y-1 text-sm">{t('request')}<textarea rows={7} value={body} disabled={preview.isPending} className="block w-full rounded-md border bg-background p-3 font-mono" onChange={(e) => {setBody(e.target.value); reset();}} /></label>
                <label className="space-y-1 text-sm">{t('headers')}<textarea rows={7} value={headers} disabled={preview.isPending} placeholder={'{"X-Session-Id":"session-1"}'} className="block w-full rounded-md border bg-background p-3 font-mono" onChange={(e) => {setHeaders(e.target.value); reset();}} /></label>
            </div>
            <Button onClick={run} disabled={preview.isPending || !group.id}>{preview.isPending ? t('running') : t('run')}</Button>
            {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
            {result && <div className="space-y-3">
                <p className="text-sm font-medium">{selected ? t('selected', {channel: selected.channel_name}) : t('noCandidate')}</p>
                <p className="text-sm text-muted-foreground">{t('previewBudget', {channels: result.budget.channel_limit, attempts: result.budget.attempt_limit, seconds: Math.ceil(result.budget.remaining_millis / 1000)})} · {t('affinity')}: {result.affinity.mode} / {result.affinity.source}</p>
                {result.warnings.map((warning, i) => <p key={i} className="text-sm text-amber-600">{warning}</p>)}
                <div className="overflow-x-auto"><table className="w-full text-left text-xs"><thead><tr className="border-b"><th className="p-2">#</th><th className="p-2">{t('channel')}</th><th className="p-2">{t('decision')}</th><th className="p-2">{t('health')}</th></tr></thead>
                    <tbody>{result.candidates.map((candidate, i) => <tr key={candidate.item.id ?? i} className="border-b align-top">
                        <td className="p-2">{candidate.order || '—'}</td>
                        <td className="p-2">{candidate.channel_name || candidate.item.channel_id}<div className="text-muted-foreground">{candidate.item.model_name}</div><div>{t('priority')}: {candidate.item.priority} · {t('weight')}: {candidate.item.weight}</div></td>
                        <td className="p-2"><span className={candidate.eligible ? 'text-green-600' : 'text-muted-foreground'}>{t(`reasons.${candidate.reason}`)}</span><div>{candidate.selection_reason && t(`selection.${candidate.selection_reason}`)} {candidate.strategy}</div>{candidate.capability_status && <div>{candidate.capability_status} · {t('quality')}: {candidate.quality_rank}</div>}<div>{candidate.conversion_path?.join(' → ')}</div>{candidate.capability_reasons?.map((reason, j) => <div key={j}>{reason}</div>)}</td>
                        <td className="p-2">{t('keys', {healthy: candidate.healthy_keys, blocked: candidate.blocked_keys})}{candidate.metrics && <div>{t('score')}: {candidate.metrics.composite_score.toFixed(1)} · {t('inFlight')}: {candidate.metrics.in_flight}</div>}{candidate.retry_at && <div>{t('retryAt')}: {new Date(candidate.retry_at).toLocaleString()}</div>}</td>
                    </tr>)}</tbody>
                </table></div>
            </div>}
        </DialogContent>
    </Dialog>;
}
