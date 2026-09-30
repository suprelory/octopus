'use client';

import { useRef } from 'react';
import { useTranslations } from 'next-intl';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { NotificationChannel, NotificationTemplate } from '@/api/endpoints/setting';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { NOTIFICATION_VARIABLES, escapeNotificationMarkdown, renderNotificationTemplate, validNotificationTemplate } from './notification-template';

export function NotificationTemplateEditor({ channel, template, disabled, onChange }: {
    channel: NotificationChannel;
    template: NotificationTemplate;
    disabled: boolean;
    onChange: (template: NotificationTemplate) => void;
}) {
    const t = useTranslations('notificationChannels');
    const bodyRef = useRef<HTMLTextAreaElement>(null);
    const sample: Record<string, string> = {
        title: `Octopus ${t('sampleTitle')}`, event: t('sampleTitle'), message: t('sampleMessage'),
        detail: t('sampleDetail'), site: t('sampleSite'), account: t('sampleAccount'),
        source: t('sampleSource'), emoji: '✅', level: 'info', time: '2026-01-01T12:00:00Z',
        reward: '0.50 USD', balance: '12.5000', threshold: '5.0000', failure_count: '0',
    };
    const valid = validNotificationTemplate(template);
    const markdown = template.format === 'markdown';
    const title = (renderNotificationTemplate(template.title ?? '', sample) ?? '').replace(/[\r\n]/g, ' ').trim() || sample.title;
    const body = renderNotificationTemplate(template.body ?? '', sample, markdown) ?? '';
    const previewBody = body.trim() ? body : markdown ? escapeNotificationMarkdown(sample.message) : sample.message;

    function insertVariable(variable: string) {
        const input = bodyRef.current;
        const current = template.body ?? '';
        const start = input?.selectionStart ?? current.length;
        const end = input?.selectionEnd ?? start;
        const token = `{{${variable}}}`;
        onChange({ ...template, body: current.slice(0, start) + token + current.slice(end) });
        requestAnimationFrame(() => { input?.focus(); input?.setSelectionRange(start + token.length, start + token.length); });
    }

    return <div className="space-y-3 border-t border-border/60 pt-4">
        <div className="flex items-center justify-between gap-2">
            <h3 className="text-sm font-medium">{t('templateTitle')}</h3>
            <Button type="button" variant="ghost" size="sm" disabled={disabled} onClick={() => onChange({})}>{t('templateReset')}</Button>
        </div>
        <p className="text-xs text-muted-foreground">{t('templateHint')}</p>
        <label className="grid gap-2 text-sm font-medium">
            {t('templateFormat')}
            <Select value={template.format || 'text'} disabled={disabled}
                onValueChange={value => onChange({ ...template, format: value === 'markdown' ? 'markdown' : undefined })}>
                <SelectTrigger aria-label={t('templateFormat')} className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                    <SelectItem value="text">{t('formatText')}</SelectItem>
                    <SelectItem value="markdown">Markdown</SelectItem>
                </SelectContent>
            </Select>
        </label>
        {markdown ? <p className="text-xs text-muted-foreground">{t(`formatHints.${channel}`)}</p> : null}
        <label className="grid gap-2 text-sm font-medium">
            {t('templateSubject')}
            <Input aria-label={t('templateSubject')} value={template.title ?? ''} placeholder="{{title}}" disabled={disabled} maxLength={512}
                spellCheck={false} aria-invalid={!valid} onChange={event => onChange({ ...template, title: event.target.value })} />
        </label>
        <label className="grid gap-2 text-sm font-medium">
            {t('templateBody')}
            <textarea aria-label={t('templateBody')} ref={bodyRef} value={template.body ?? ''} placeholder="{{message}}" disabled={disabled} rows={5}
                className="w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/50 disabled:opacity-50" maxLength={8192} spellCheck={false} aria-invalid={!valid}
                onChange={event => onChange({ ...template, body: event.target.value })} />
        </label>
        <details className="text-xs">
            <summary className="cursor-pointer text-muted-foreground">{t('templateVariables')}</summary>
            <div className="mt-2 grid gap-1 sm:grid-cols-2">
                {NOTIFICATION_VARIABLES.map(variable => <button key={variable} type="button" disabled={disabled}
                    className="flex items-start gap-2 rounded-md px-2 py-1 text-left hover:bg-muted disabled:opacity-50"
                    onClick={() => insertVariable(variable)}>
                    <code className="shrink-0">{`{{${variable}}}`}</code><span className="text-muted-foreground">{t(`variables.${variable}`)}</span>
                </button>)}
            </div>
        </details>
        {!valid ? <p role="alert" className="text-xs text-destructive">{t('templateInvalid')}</p> : null}
        <div className="space-y-2 rounded-lg bg-muted/50 p-3" aria-label={t('templatePreview')}>
            <p className="text-xs text-muted-foreground">{t('templatePreview')}</p>
            {valid ? <><p className="break-words text-sm font-medium">{title}</p>
                {markdown ? <div className="min-w-0 space-y-2 break-words text-xs [&_p]:whitespace-pre-wrap [&_a]:text-primary [&_a]:underline [&_h1]:text-base [&_h2]:text-sm [&_h3]:font-semibold [&_ul]:list-disc [&_ul]:pl-4 [&_ol]:list-decimal [&_ol]:pl-4 [&_blockquote]:border-l-2 [&_blockquote]:pl-3 [&_pre]:overflow-x-auto [&_pre]:rounded [&_pre]:bg-muted [&_pre]:p-2 [&_th]:border [&_th]:px-2 [&_th]:py-1 [&_td]:border [&_td]:px-2 [&_td]:py-1">
                    <Markdown remarkPlugins={[remarkGfm]} skipHtml components={{
                        a: ({ href, children }) => <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>,
                        img: ({ alt }) => <span>{alt}</span>,
                        table: ({ children }) => <div className="max-w-full overflow-x-auto"><table>{children}</table></div>,
                    }}>{previewBody}</Markdown>
                </div> : <p className="whitespace-pre-wrap break-words text-xs">{previewBody}</p>}
            </> : null}
        </div>
    </div>;
}
