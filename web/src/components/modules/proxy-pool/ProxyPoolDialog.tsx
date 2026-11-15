import { useRef, useState, type FormEvent } from 'react';
import { Dialog } from 'radix-ui';
import { Globe, Plus, X } from 'lucide-react';
import { useTranslations } from 'use-intl';
import { type ProxyNode, type ProxyWrite, type ProxyTestResult, useProxyPool, useProxyMutation, useProxyReferences, testProxy } from '@/api/proxy-pool';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

type ProxyEditorState = { node?: ProxyNode; value: ProxyWrite };

function createEditor(node?: ProxyNode): ProxyEditorState {
    return {
        node,
        value: { name: node?.name ?? '', url: node?.url ?? '', enabled: node?.enabled ?? true, remark: node?.remark ?? '', revision: node?.revision ?? 0 },
    };
}

function ErrorMessage({ error }: { error: unknown }) {
    return error ? <p role="alert" className="rounded-lg bg-destructive/10 p-3 text-sm text-destructive">{error instanceof Error ? error.message : String(error)}</p> : null;
}

function ProxyEditor({ editor, onChange, onSave, onReset, pending }: {
    editor: ProxyEditorState;
    onChange: (value: ProxyWrite) => void;
    onSave: (value: ProxyWrite) => Promise<void>;
    onReset: () => void;
    pending: boolean;
}) {
    const t = useTranslations('proxyPool');
    const { node, value } = editor;
    async function submit(event: FormEvent) {
        event.preventDefault();
        event.stopPropagation();
        try { await onSave(value); } catch { /* rendered in the dialog */ }
    }
    return <form onSubmit={submit} className="space-y-4">
        <div className="flex items-start justify-between gap-3">
            <div className="min-w-0"><h3 className="text-lg font-semibold">{t(node ? 'edit' : 'add')}</h3>{node && <p className="mt-1 break-all text-sm text-muted-foreground">{node.name}</p>}</div>
            <Button type="button" size="sm" variant="outline" disabled={pending} onClick={onReset}><Plus className="size-4" />{t('new')}</Button>
        </div>
        <fieldset disabled={pending} className="space-y-4">
            <label className="block space-y-1 text-sm"><span>{t('name')}</span><Input required maxLength={128} value={value.name} onChange={(event) => onChange({ ...value, name: event.target.value })} /></label>
            <label className="block space-y-1 text-sm"><span>{t('url')}</span><Input required type="text" autoComplete="off" autoCapitalize="none" spellCheck={false} maxLength={4096} value={value.url} placeholder="http://user:password@127.0.0.1:7890" onChange={(event) => onChange({ ...value, url: event.target.value })} /></label>
            <p className="text-xs text-muted-foreground">{t('urlHint')}</p>
            {node?.has_auth && <><p className="text-xs text-muted-foreground">{t('keepAuth')}</p><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={!!value.clear_auth} onChange={(event) => onChange({ ...value, clear_auth: event.target.checked })} />{t('clearAuth')}</label></>}
            <label className="block space-y-1 text-sm"><span>{t('remark')}</span><Input maxLength={1024} value={value.remark} onChange={(event) => onChange({ ...value, remark: event.target.value })} /></label>
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={value.enabled} onChange={(event) => onChange({ ...value, enabled: event.target.checked })} />{t('enabled')}</label>
            <p className="text-xs text-muted-foreground">{t('changeHint')}</p>
            <div className="flex gap-2"><Button type="button" variant="outline" onClick={onReset}>{t('cancel')}</Button><Button type="submit" className="flex-1">{pending ? t('saving') : t('save')}</Button></div>
        </fieldset>
    </form>;
}

export function ProxyPoolDialog({ onClose }: { onClose: () => void }) {
    const t = useTranslations('proxyPool');
    const pool = useProxyPool();
    const mutation = useProxyMutation();
    const [search, setSearch] = useState('');
    const [editor, setEditor] = useState<ProxyEditorState>(() => createEditor());
    const editorPanel = useRef<HTMLElement>(null);
    const [referenceID, setReferenceID] = useState<number | null>(null);
    const references = useProxyReferences(referenceID);
    const [deleting, setDeleting] = useState<ProxyNode | null>(null);
    const [target, setTarget] = useState('https://api.openai.com/v1/models');
    const [testing, setTesting] = useState<number | null>(null);
    const [results, setResults] = useState<Record<number, ProxyTestResult & { revision: number }>>({});
    const [testError, setTestError] = useState<unknown>(null);
    const nodes = pool.data?.filter((node) => `${node.name} ${node.display_url ?? node.url} ${node.remark}`.toLowerCase().includes(search.toLowerCase())) ?? [];
    const error = pool.error || mutation.error || testError;
    function edit(node?: ProxyNode) {
        mutation.reset();
        setEditor(createEditor(node));
        setDeleting(null);
        editorPanel.current?.scrollTo({ top: 0 });
        if (window.matchMedia('(max-width: 767px)').matches) {
            editorPanel.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
        }
    }
    async function save(value: ProxyWrite) {
        await mutation.mutateAsync({ id: editor.node?.id, value });
        setEditor(createEditor());
        editorPanel.current?.scrollTo({ top: 0 });
    }
    async function toggle(node: ProxyNode) {
        try {
            const updated = await mutation.mutateAsync({ id: node.id, value: { name: node.name, url: node.url, remark: node.remark, revision: node.revision, enabled: !node.enabled } });
            setEditor((current) => current.node?.id === node.id && current.value.revision === node.revision
                ? { node: updated, value: { ...current.value, enabled: updated.enabled, revision: updated.revision } }
                : current);
        } catch { /* rendered in the dialog */ }
    }
    async function probe(node: ProxyNode) {
        setTesting(node.id); setTestError(null);
        try { const result = await testProxy({ id: node.id, target_url: target }); setResults((current) => ({ ...current, [node.id]: { ...result, revision: node.revision } })); }
        catch (error) { setTestError(error); }
        finally { setTesting(null); }
    }
    async function remove() {
        if (!deleting) return;
        try {
            await mutation.mutateAsync({ id: deleting.id, remove: true });
            setEditor((current) => current.node?.id === deleting.id ? createEditor() : current);
            setDeleting(null);
            setReferenceID(null);
        } catch { /* rendered in the dialog */ }
    }
    return <Dialog.Root open onOpenChange={(open) => { if (!open) onClose(); }}>
        <Dialog.Portal>
            <Dialog.Overlay className="fixed inset-0 z-[90] bg-black/45 backdrop-blur-sm" />
            <Dialog.Content
                onEscapeKeyDown={(event) => event.stopPropagation()}
                className="fixed left-1/2 top-1/2 z-[100] flex h-[min(800px,90dvh)] w-[calc(100%_-_1.5rem)] max-w-5xl -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-3xl border bg-card shadow-xl"
            >
                <div className="shrink-0 space-y-4 border-b p-5">
                    <div className="flex items-start gap-4"><div className="min-w-0 flex-1"><Dialog.Title className="text-lg font-semibold">{t('title')}</Dialog.Title><Dialog.Description className="mt-1 text-sm text-muted-foreground">{t('description')}</Dialog.Description></div><Dialog.Close asChild><Button type="button" variant="ghost" size="icon" aria-label={t('close')}><X className="size-4" /></Button></Dialog.Close></div>
                    <ErrorMessage error={error} />
                </div>
                <div className="min-h-0 flex-1 overflow-y-auto md:grid md:grid-cols-[1.1fr_0.9fr] md:overflow-hidden">
                    <section aria-label={t('node')} className="flex min-h-0 min-w-0 flex-col border-b md:border-r md:border-b-0">
                        <div className="shrink-0 p-5"><Input aria-label={t('search')} placeholder={t('search')} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
                        <div className="max-h-[35dvh] min-h-0 flex-1 space-y-3 overflow-y-auto p-5 pt-0 md:max-h-none">
                            {pool.isPending ? <p>{t('loading')}</p> : nodes.length === 0 && <p className="py-6 text-center text-muted-foreground">{t('empty')}</p>}
                            {nodes.map((node) => <article key={node.id} className={cn('space-y-3 rounded-xl border p-4 transition-colors', editor.node?.id === node.id && 'border-primary/50 bg-primary/5')}>
                                <div className="flex flex-wrap items-center gap-2"><span className="min-w-0 break-all font-medium">{node.name}</span><span className="rounded-full border px-2 py-0.5 text-xs">{t(node.enabled ? 'enabled' : 'disabled')}</span>{node.has_auth && <span className="text-xs text-muted-foreground">{t('authenticated')}</span>}</div>
                                <p className="break-all font-mono text-xs text-muted-foreground">{node.display_url ?? node.url}</p>{node.remark && <p className="break-words text-sm">{node.remark}</p>}
                                <div className="flex flex-wrap gap-2">
                                    <Button type="button" size="sm" variant="outline" aria-pressed={editor.node?.id === node.id} disabled={mutation.isPending} onClick={() => edit(node)}>{t('edit')}</Button>
                                    <Button type="button" size="sm" variant="outline" disabled={mutation.isPending} onClick={() => void toggle(node)}>{t(node.enabled ? 'disable' : 'enable')}</Button>
                                    <Button type="button" size="sm" variant="outline" disabled={testing !== null || mutation.isPending} onClick={() => void probe(node)}>{testing === node.id ? t('testing') : t('test')}</Button>
                                    <Button type="button" size="sm" variant="ghost" onClick={() => setReferenceID(referenceID === node.id ? null : node.id)}>{t('references', { count: node.reference_count })}</Button>
                                    <Button type="button" size="sm" variant="ghost" disabled={node.reference_count > 0 || mutation.isPending} onClick={() => { mutation.reset(); setDeleting(node); }}>{t('delete')}</Button>
                                </div>
                                {results[node.id]?.revision === node.revision && <p role="status" className="text-sm">{t(results[node.id].connected ? 'connected' : 'failed')} · HTTP {results[node.id].http_status || '—'} · {results[node.id].duration_ms} ms</p>}
                                {referenceID === node.id && <div className="space-y-1 break-words rounded-lg bg-muted/50 p-3 text-sm"><ErrorMessage error={references.error} />{references.isPending ? t('loading') : references.data?.length ? references.data.map((ref) => <p key={`${ref.kind}-${ref.id}`}>{t(`kinds.${ref.kind}`)} · {ref.site_name ? `${ref.site_name} / ` : ''}{ref.name}{ref.inherited ? ` (${t('modes.inherit')})` : ''}</p>) : t('noReferences')}</div>}
                                {deleting?.id === node.id && <div role="alert" className="space-y-3 break-words rounded-xl border border-destructive/40 p-4"><p>{t('confirmDelete', { name: deleting.name })}</p><div className="flex justify-end gap-2"><Button type="button" variant="outline" disabled={mutation.isPending} onClick={() => setDeleting(null)}>{t('cancel')}</Button><Button type="button" variant="destructive" disabled={mutation.isPending} onClick={() => void remove()}>{t('delete')}</Button></div></div>}
                            </article>)}
                        </div>
                    </section>
                    <section ref={editorPanel} aria-label={t('edit')} className="min-h-0 min-w-0 space-y-5 p-5 md:overflow-y-auto">
                        <ProxyEditor editor={editor} onChange={(value) => setEditor((current) => ({ ...current, value }))} onSave={save} onReset={() => edit()} pending={mutation.isPending} />
                        <div className="space-y-2 rounded-xl border bg-muted/20 p-4">
                            <label className="block space-y-1 text-sm"><span>{t('target')}</span><Input type="url" value={target} onChange={(event) => setTarget(event.target.value)} /></label>
                            <p className="text-xs text-muted-foreground">{t('testHint')}</p>
                        </div>
                    </section>
                </div>
            </Dialog.Content>
        </Dialog.Portal>
    </Dialog.Root>;
}

export function SettingProxyPool() {
    const t = useTranslations('proxyPool');
    const [open, setOpen] = useState(false);
    return <section className="space-y-4 rounded-3xl border bg-card p-6"><div className="flex items-center gap-3"><Globe className="size-5" /><h2 className="font-semibold">{t('title')}</h2></div><p className="text-sm text-muted-foreground">{t('description')}</p><Button type="button" variant="outline" onClick={() => setOpen(true)}>{t('manage')}</Button>{open && <ProxyPoolDialog onClose={() => setOpen(false)} />}</section>;
}
