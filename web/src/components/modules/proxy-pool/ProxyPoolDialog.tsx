import { useState, type FormEvent } from 'react';
import { Dialog } from 'radix-ui';
import { Globe, X } from 'lucide-react';
import { useTranslations } from 'use-intl';
import { type ProxyNode, type ProxyWrite, type ProxyTestResult, useProxyPool, useProxyMutation, useProxyReferences, testProxy } from '@/api/proxy-pool';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';

function ErrorMessage({ error }: { error: unknown }) {
    return error ? <p role="alert" className="rounded-lg bg-destructive/10 p-3 text-sm text-destructive">{error instanceof Error ? error.message : String(error)}</p> : null;
}

function ProxyEditor({ node, onClose }: { node?: ProxyNode; onClose: () => void }) {
    const t = useTranslations('proxyPool');
    const save = useProxyMutation();
    const [showURL, setShowURL] = useState(false);
    const [value, setValue] = useState<ProxyWrite>({ name: node?.name ?? '', url: node?.url ?? '', enabled: node?.enabled ?? true, remark: node?.remark ?? '', revision: node?.revision ?? 0 });
    async function submit(event: FormEvent) {
        event.preventDefault();
        event.stopPropagation();
        try { await save.mutateAsync({ id: node?.id, value }); onClose(); } catch { /* rendered below */ }
    }
    return <form onSubmit={submit} className="space-y-4">
        <p className="font-medium">{t(node ? 'edit' : 'add')}</p>
        <ErrorMessage error={save.error} />
        <fieldset disabled={save.isPending} className="space-y-4">
            <label className="block space-y-1 text-sm"><span>{t('name')}</span><Input required maxLength={128} value={value.name} onChange={(event) => setValue({ ...value, name: event.target.value })} /></label>
            <label className="block space-y-1 text-sm"><span>{t('url')}</span><Input required type={showURL ? 'text' : 'password'} autoComplete="new-password" maxLength={4096} value={value.url} placeholder="http://user:password@127.0.0.1:7890" onChange={(event) => setValue({ ...value, url: event.target.value })} /></label>
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={showURL} onChange={(event) => setShowURL(event.target.checked)} />{t('showURL')}</label>
            <p className="text-xs text-muted-foreground">{t('urlHint')}</p>
            {node?.has_auth && <><p className="text-xs text-muted-foreground">{t('keepAuth')}</p><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={!!value.clear_auth} onChange={(event) => setValue({ ...value, clear_auth: event.target.checked })} />{t('clearAuth')}</label></>}
            <label className="block space-y-1 text-sm"><span>{t('remark')}</span><Input maxLength={1024} value={value.remark} onChange={(event) => setValue({ ...value, remark: event.target.value })} /></label>
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={value.enabled} onChange={(event) => setValue({ ...value, enabled: event.target.checked })} />{t('enabled')}</label>
            <p className="text-xs text-muted-foreground">{t('changeHint')}</p>
            <div className="flex justify-end gap-2"><Button type="button" variant="outline" onClick={onClose}>{t('cancel')}</Button><Button type="submit">{save.isPending ? t('saving') : t('save')}</Button></div>
        </fieldset>
    </form>;
}

export function ProxyPoolDialog({ onClose }: { onClose: () => void }) {
    const t = useTranslations('proxyPool');
    const pool = useProxyPool();
    const mutation = useProxyMutation();
    const [search, setSearch] = useState('');
    const [editing, setEditing] = useState<ProxyNode | 'new' | null>(null);
    const [referenceID, setReferenceID] = useState<number | null>(null);
    const references = useProxyReferences(referenceID);
    const [deleting, setDeleting] = useState<ProxyNode | null>(null);
    const [target, setTarget] = useState('https://api.openai.com/v1/models');
    const [testing, setTesting] = useState<number | null>(null);
    const [results, setResults] = useState<Record<number, ProxyTestResult & { revision: number }>>({});
    const [testError, setTestError] = useState<unknown>(null);
    const nodes = pool.data?.filter((node) => `${node.name} ${node.url} ${node.remark}`.toLowerCase().includes(search.toLowerCase())) ?? [];
    async function probe(node: ProxyNode) {
        setTesting(node.id); setTestError(null);
        try { const result = await testProxy({ id: node.id, target_url: target }); setResults((current) => ({ ...current, [node.id]: { ...result, revision: node.revision } })); }
        catch (error) { setTestError(error); }
        finally { setTesting(null); }
    }
    async function remove() {
        if (!deleting) return;
        try { await mutation.mutateAsync({ id: deleting.id, remove: true }); setDeleting(null); setReferenceID(null); } catch { /* rendered below */ }
    }
    return <Dialog.Root open onOpenChange={(open) => { if (!open) onClose(); }}>
        <Dialog.Portal>
            <Dialog.Overlay className="fixed inset-0 z-[90] bg-black/45 backdrop-blur-sm" />
            <Dialog.Content
                onEscapeKeyDown={(event) => event.stopPropagation()}
                className="fixed left-1/2 top-1/2 z-[100] flex max-h-[90dvh] w-[calc(100%_-_1.5rem)] max-w-3xl -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-3xl border bg-card shadow-xl"
            >
                <div className="flex items-start gap-4 border-b p-5"><div className="flex-1"><Dialog.Title className="text-lg font-semibold">{t('title')}</Dialog.Title><Dialog.Description className="mt-1 text-sm text-muted-foreground">{t('description')}</Dialog.Description></div><Dialog.Close asChild><Button type="button" variant="ghost" size="icon" aria-label={t('close')}><X className="size-4" /></Button></Dialog.Close></div>
                <div className="space-y-4 overflow-y-auto p-5">
                    {editing ? <ProxyEditor key={editing === 'new' ? 'new' : editing.id} node={editing === 'new' ? undefined : editing} onClose={() => setEditing(null)} /> : <>
                        <ErrorMessage error={pool.error || mutation.error || testError} />
                        <div className="flex gap-2"><Input aria-label={t('search')} placeholder={t('search')} value={search} onChange={(event) => setSearch(event.target.value)} /><Button type="button" onClick={() => setEditing('new')}>{t('add')}</Button></div>
                        <label className="block space-y-1 text-sm"><span>{t('target')}</span><Input type="url" value={target} onChange={(event) => setTarget(event.target.value)} /></label>
                        <p className="text-xs text-muted-foreground">{t('testHint')}</p>
                        {pool.isPending ? <p>{t('loading')}</p> : nodes.length === 0 && <p className="py-6 text-center text-muted-foreground">{t('empty')}</p>}
                        {nodes.map((node) => <article key={node.id} className="space-y-3 rounded-xl border p-4">
                            <div className="flex flex-wrap items-center gap-2"><span className="font-medium">{node.name}</span><span className="rounded-full border px-2 py-0.5 text-xs">{t(node.enabled ? 'enabled' : 'disabled')}</span>{node.has_auth && <span className="text-xs text-muted-foreground">{t('authenticated')}</span>}</div>
                            <p className="break-all font-mono text-xs text-muted-foreground">{node.url}</p>{node.remark && <p className="text-sm">{node.remark}</p>}
                            <div className="flex flex-wrap gap-2">
                                <Button type="button" size="sm" variant="outline" onClick={() => setEditing(node)}>{t('edit')}</Button>
                                <Button type="button" size="sm" variant="outline" disabled={mutation.isPending} onClick={() => mutation.mutate({ id: node.id, value: { name: node.name, url: node.url, remark: node.remark, revision: node.revision, enabled: !node.enabled } })}>{t(node.enabled ? 'disable' : 'enable')}</Button>
                                <Button type="button" size="sm" variant="outline" disabled={testing !== null} onClick={() => void probe(node)}>{testing === node.id ? t('testing') : t('test')}</Button>
                                <Button type="button" size="sm" variant="ghost" onClick={() => setReferenceID(referenceID === node.id ? null : node.id)}>{t('references', { count: node.reference_count })}</Button>
                                <Button type="button" size="sm" variant="ghost" disabled={node.reference_count > 0 || mutation.isPending} onClick={() => setDeleting(node)}>{t('delete')}</Button>
                            </div>
                            {results[node.id]?.revision === node.revision && <p role="status" className="text-sm">{t(results[node.id].connected ? 'connected' : 'failed')} · HTTP {results[node.id].http_status || '—'} · {results[node.id].duration_ms} ms</p>}
                            {referenceID === node.id && <div className="space-y-1 rounded-lg bg-muted/50 p-3 text-sm"><ErrorMessage error={references.error} />{references.isPending ? t('loading') : references.data?.length ? references.data.map((ref) => <p key={`${ref.kind}-${ref.id}`}>{t(`kinds.${ref.kind}`)} · {ref.site_name ? `${ref.site_name} / ` : ''}{ref.name}{ref.inherited ? ` (${t('modes.inherit')})` : ''}</p>) : t('noReferences')}</div>}
                        </article>)}
                        {deleting && <div role="alert" className="space-y-3 rounded-xl border border-destructive/40 p-4"><p>{t('confirmDelete', { name: deleting.name })}</p><div className="flex justify-end gap-2"><Button type="button" variant="outline" disabled={mutation.isPending} onClick={() => setDeleting(null)}>{t('cancel')}</Button><Button type="button" variant="destructive" disabled={mutation.isPending} onClick={() => void remove()}>{t('delete')}</Button></div></div>}
                    </>}
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
