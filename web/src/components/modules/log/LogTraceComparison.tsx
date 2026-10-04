'use client';

import { useMemo } from 'react';
import { useQueries } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { getLogContent, type RelayTrace } from '@/api/endpoints/log';
import { compareJSON } from './trace-tools';

export function LogTraceComparison({ id, trace, attemptId, direction }: { id: number; trace: RelayTrace; attemptId: string; direction: 'request' | 'response' }) {
    const t = useTranslations('log.trace');
    const attempt = trace.attempts.find(item => item.attempt_id === attemptId);
    const summaries = [trace.client[direction], attempt?.[direction]];
    const queries = useQueries({ queries: summaries.map((summary, index) => ({
        queryKey: ['log-content', id, index ? attemptId : '', direction],
        queryFn: () => getLogContent(id, index ? attemptId : '', direction),
        enabled: !!summary && summary.captured_bytes > 0 && ['captured', 'partial', 'truncated'].includes(summary.state),
        gcTime: 0, staleTime: Infinity, retry: false,
    })) });
    const left = queries[0].data?.body, right = queries[1].data?.body;
    const diff = useMemo(() => compareJSON(left ?? '', right ?? ''), [left, right]);
    const available = left !== undefined && right !== undefined;
    return <div className="min-h-0 flex-1 overflow-auto rounded-lg border bg-background p-2 text-xs" aria-label={t('compare')}>
        {available && <p className="mb-2 text-muted-foreground">{diff.equal ? t('same') : t('different')}{diff.limited && ` · ${t('diffLimited')}`}</p>}
        {available && diff.comparable && diff.changes.length > 0 && <table className="mb-3 w-full table-fixed border-collapse text-left">
            <caption className="sr-only">{t('fieldChanges')}</caption>
            <thead><tr><th className="w-1/4 p-1">{t('field')}</th><th className="p-1">{t('client')}</th><th className="p-1">{t('upstream')}</th></tr></thead>
            <tbody>{diff.changes.map(change => <tr key={change.path} className="border-t"><td className="break-all p-1 font-mono">{change.path}</td><td className="whitespace-pre-wrap break-all bg-red-500/5 p-1 font-mono">{change.before}</td><td className="whitespace-pre-wrap break-all bg-green-500/5 p-1 font-mono">{change.after}</td></tr>)}</tbody>
        </table>}
        <div className="grid min-w-0 grid-cols-1 gap-2 md:grid-cols-2">
            {summaries.map((summary, index) => <div key={index} className="min-w-0">
                <p className="mb-1 font-medium">{index ? `${t('upstream')} #${attemptId}` : t('client')} · {summary ? t(`states.${queries[index].data?.state ?? summary.state}`) : t('noMessage')}</p>
                <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted/40 p-2">{queries[index].isError ? queries[index].error.message : queries[index].isFetching ? t('loading') : queries[index].data?.body ?? t('empty')}</pre>
            </div>)}
        </div>
    </div>;
}
