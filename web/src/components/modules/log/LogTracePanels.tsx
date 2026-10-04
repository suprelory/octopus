'use client';

import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { Download } from 'lucide-react';
import { getLogContent, type RelayTrace } from '@/api/endpoints/log';
import { CopyIconButton } from '@/components/common/CopyButton';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';

const stages = ['clientRequest', 'upstreamRequest', 'upstreamResponse', 'clientResponse'] as const;
type Stage = typeof stages[number];

export function LogTracePanels({ id, trace }: { id: number; trace: RelayTrace }) {
    const t = useTranslations('log.trace');
    const [attemptId, setAttemptId] = useState(trace.attempts.at(-1)?.attempt_id ?? '');
    const [stage, setStage] = useState<Stage>('clientRequest');
    const [formatted, setFormatted] = useState(false);
    const [search, setSearch] = useState('');
    const [eventSelection, setEventSelection] = useState<{ key: string; sequence: number }>();
    const upstream = stage.startsWith('upstream');
    const direction = stage.endsWith('Request') ? 'request' : 'response';
    const exchange = upstream ? trace.attempts.find(attempt => attempt.attempt_id === attemptId) : trace.client;
    const summary = exchange?.[direction];
    const canFetch = !!summary && summary.captured_bytes > 0 && ['captured', 'partial', 'truncated'].includes(summary.state);
    const query = useQuery({
        queryKey: ['log-content', id, upstream ? attemptId : '', direction],
        queryFn: () => getLogContent(id, upstream ? attemptId : '', direction),
        enabled: canFetch,
        staleTime: 0,
        gcTime: 0,
        retry: false,
    });
    const message = query.data ?? summary;
    const body = query.data?.body ?? '';
    const selectionKey = `${stage}:${attemptId}`;
    const event = eventSelection?.key === selectionKey ? message?.events?.find(item => item.sequence === eventSelection.sequence) : undefined;
    const selectedBody = useMemo(() => {
        if (!event) return body;
        const bytes = message?.body_encoding === 'base64' ? Uint8Array.from(atob(body), char => char.charCodeAt(0)) : new TextEncoder().encode(body);
        const selected = bytes.slice(event.offset, event.offset + event.bytes);
        if (event.type === 'ws_binary') {
            let binary = '';
            for (const byte of selected) binary += String.fromCharCode(byte);
            return btoa(binary);
        }
        return new TextDecoder().decode(selected);
    }, [body, event, message?.body_encoding]);
    const text = useMemo(() => {
        if (!formatted || !selectedBody) return selectedBody;
        try { return JSON.stringify(JSON.parse(selectedBody), null, 2); } catch { return selectedBody; }
    }, [selectedBody, formatted]);
    const matches = useMemo(() => search ? text.toLocaleLowerCase().split(search.toLocaleLowerCase()).length - 1 : 0, [search, text]);
    const download = () => {
        const data = message?.body_encoding === 'base64' ? Uint8Array.from(atob(body), char => char.charCodeAt(0)) : body;
        const url = URL.createObjectURL(new Blob([data], { type: 'application/octet-stream' }));
        const anchor = document.createElement('a');
        anchor.href = url;
        anchor.download = `${trace.id}-${upstream ? attemptId : 'client'}-${direction}.txt`;
        anchor.click();
        setTimeout(() => URL.revokeObjectURL(url), 1000);
    };

    return (
        <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-hidden rounded-2xl border bg-muted/20 p-3">
            <div className="flex flex-wrap items-center gap-2 text-xs">
                <span className="truncate font-mono" title={trace.id}>{trace.id}</span>
                <CopyIconButton text={trace.id} />
                {trace.attempts.length > 0 && <select aria-label={t('attempt')} className="ml-auto max-w-full rounded-md border bg-background p-1.5" value={attemptId} onChange={event => setAttemptId(event.target.value)}>
                    {trace.attempts.map(attempt => <option key={attempt.attempt_id} value={attempt.attempt_id}>#{attempt.attempt_id} · {attempt.channel_name} · {attempt.model} · {attempt.response?.status_code ?? attempt.transport}</option>)}
                </select>}
            </div>
            <div className="flex flex-wrap gap-1" role="tablist" aria-label={t('messages')}>
                {stages.map(value => <Button key={value} size="sm" role="tab" aria-selected={stage === value} variant={stage === value ? 'secondary' : 'ghost'} onClick={() => setStage(value)}>{t(value)}</Button>)}
            </div>
            {message ? <>
                <div className="flex flex-wrap items-center gap-2 text-xs">
                    {message.method && <Badge variant="outline">{message.method}</Badge>}
                    {message.status_code != null && <Badge variant="outline">HTTP {message.status_code}</Badge>}
                    <Badge variant={message.state === 'captured' ? 'secondary' : 'outline'}>{t(`states.${message.state}`)}</Badge>
                    <span>{message.captured_bytes.toLocaleString()} / {message.bytes.toLocaleString()} B</span>
                    {message.body_encoding === 'base64' && <Badge variant="outline">Base64</Badge>}
                    {exchange?.completion_status && <Badge variant="outline" title={exchange.finish_cause}>{t('completion')}: {exchange.completion_status}</Badge>}
                    {message.content_type && <span className="break-all text-muted-foreground">{message.content_type}</span>}
                </div>
                {message.representation === 'multipart_metadata' && <p className="text-xs text-muted-foreground">{t('multipartMetadata')}</p>}
                {message.url && <div className="max-h-16 overflow-auto break-all font-mono text-xs">{message.url}</div>}
                {exchange?.upstream_request_id && <div className="text-xs">{t('upstreamId')}: <span className="font-mono">{exchange.upstream_request_id}</span></div>}
                {upstream && exchange?.error && <div className="max-h-20 overflow-auto break-words text-xs text-destructive">{exchange.error}</div>}
                <details className="shrink-0 text-xs"><summary className="cursor-pointer py-1">{t('headers')}</summary><pre className="max-h-36 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-muted p-2">{JSON.stringify(message.headers ?? {}, null, 2)}</pre></details>
                <div className="flex flex-wrap items-center gap-2">
                    {!!message.events?.length && <select aria-label={t('events')} className="max-w-full rounded-md border bg-background p-1.5 text-xs" value={event?.sequence ?? 0} onChange={e => setEventSelection({ key: selectionKey, sequence: Number(e.target.value) })}>
                        <option value={0}>{t('allEvents')}</option>
                        {message.events.map(item => <option key={item.sequence} value={item.sequence}>#{item.sequence} · +{item.elapsed_ms} ms · {item.type} · {item.bytes} B{item.complete ? '' : ` · ${t('states.partial')}`}</option>)}
                    </select>}
                    <input aria-label={t('search')} placeholder={t('search')} className="min-w-0 flex-1 rounded-md border bg-background px-2 py-1 text-xs" value={search} onChange={event => setSearch(event.target.value)} />
                    {search && <span className="text-xs" role="status">{t('matches', { count: matches })}</span>}
                    <Button size="sm" variant="ghost" aria-pressed={formatted} onClick={() => setFormatted(value => !value)}>{formatted ? t('raw') : t('format')}</Button>
                    <CopyIconButton text={text} />
                    <Button size="icon" variant="ghost" aria-label={t('download')} disabled={!query.data || !body} onClick={download}><Download className="size-4" /></Button>
                </div>
                {message.events_truncated && <p className="text-xs text-muted-foreground">{t('eventsTruncated')}</p>}
                {canFetch && query.isPending ? <p className="text-sm text-muted-foreground">{t('loading')}</p> : query.isError ? <div className="text-sm text-destructive">{query.error.message}<Button size="sm" variant="ghost" onClick={() => query.refetch()}>{t('retry')}</Button></div> : <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-background p-3 font-mono text-xs">{text || t('empty')}</pre>}
            </> : <p className="text-sm text-muted-foreground">{t('noMessage')}</p>}
        </div>
    );
}
