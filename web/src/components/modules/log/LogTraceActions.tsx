'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import type { RelayExchange, RelayMessage, RelayTrace } from '@/api/endpoints/log';
import { Button } from '@/components/ui/button';
import { CopyIconButton } from '@/components/common/CopyButton';
import { buildCurl, canReproduce, downloadTraceFile, messageFilename, type CurlShell } from './trace-tools';

export function LogTraceActions({ id, trace, attemptId, direction, upstream, exchange, message, pending, comparing, onCompare }: {
    id: number; trace: RelayTrace; attemptId: string; direction: 'request' | 'response'; upstream: boolean;
    exchange?: RelayExchange; message: RelayMessage; pending: boolean; comparing: boolean; onCompare: () => void;
}) {
    const t = useTranslations('log.trace');
    const [showCurl, setShowCurl] = useState(false);
    const [shell, setShell] = useState<CurlShell>('powershell');
    const filename = messageFilename(trace.id, upstream ? attemptId : '', direction);
    const eligible = direction === 'request' && !pending && canReproduce(message, exchange?.transport ?? '');
    const curl = eligible && typeof window !== 'undefined' ? buildCurl(message, filename, shell, window.location.origin) : null;
    const exportMessage = () => downloadTraceFile(`${filename}.json`, JSON.stringify({
        schema_version: 1, log_id: id, trace_id: trace.id, serving_attempt_id: trace.serving_attempt_id,
        perspective: upstream ? 'upstream' : 'client', direction, attempt_id: upstream ? attemptId : undefined,
        transport: exchange?.transport, timings: exchange?.timings, message,
    }, null, 2), 'application/json');
    return <>
        <div className="flex flex-wrap gap-1">
            <Button size="sm" variant={comparing ? 'secondary' : 'ghost'} disabled={!attemptId} aria-pressed={comparing} onClick={onCompare}>{t('compare')}</Button>
            <Button size="sm" variant="ghost" disabled={pending} onClick={exportMessage}>{t('exportMessage')}</Button>
            {direction === 'request' && <Button size="sm" variant="ghost" disabled={!curl} aria-pressed={showCurl && !!curl} title={!curl ? t('curlUnavailable') : undefined} onClick={() => setShowCurl(value => !value)}>cURL</Button>}
        </div>
        {showCurl && curl && <div className="shrink-0 rounded-md border p-2 text-xs">
            <p className="text-muted-foreground">{t('curlHint', { filename: `${filename}.txt` })}</p>
            <div className="flex items-center gap-2"><select aria-label={t('shell')} className="rounded border bg-background p-1" value={shell} onChange={e => setShell(e.target.value as CurlShell)}><option value="powershell">PowerShell</option><option value="bash">Bash</option></select><CopyIconButton text={curl} /></div>
            <pre className="max-h-24 overflow-auto whitespace-pre-wrap break-all">{curl}</pre>
        </div>}
    </>;
}
