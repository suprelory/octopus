import type { RelayMessage } from '@/api/endpoints/log';

export type CurlShell = 'powershell' | 'bash';
const credentialName = /authorization|cookie|token|secret|api[-_]?key|password|credential|signature/i;
const transportHeader = /^(host|content-length|connection|transfer-encoding|accept-encoding|proxy-connection|upgrade|te|trailer|keep-alive)$/i;

export function messageFilename(traceId: string, attemptId: string, direction: string) {
    return `${traceId}-${attemptId || 'client'}-${direction}`.replace(/[^a-zA-Z0-9._-]/g, '_');
}

export function canReproduce(message: RelayMessage, transport: string) {
    return transport === 'http' && message.state === 'captured' && !message.representation &&
        !!message.method && /^[A-Z]+$/.test(message.method) && !!message.url &&
        (message.captured_bytes === 0 || message.body !== undefined) && message.body_encoding !== 'base64';
}

// Payloads stay in a separate file; shell metacharacters in URLs/header values
// are quoted as literal arguments. Credentials are omitted, never reconstructed.
export function buildCurl(message: RelayMessage, filename: string, shell: CurlShell, origin: string): string | null {
    if (!canReproduce(message, 'http')) return null;
    let url: URL;
    try { url = new URL(message.url!, origin); } catch { return null; }
    if (!['https:', 'http:'].includes(url.protocol)) return null;
    url.username = ''; url.password = '';
    for (const [name, value] of [...url.searchParams]) {
        if (credentialName.test(name) || value.includes('[REDACTED]')) url.searchParams.delete(name);
    }
    const quote = (s: string) => shell === 'powershell' ? `'${s.replace(/'/g, "''")}'` : `'${s.replace(/'/g, "'\\''")}'`;
    const args = [shell === 'powershell' ? 'curl.exe' : 'curl', '--request', quote(message.method!), '--url', quote(url.href)];
    for (const [name, values] of Object.entries(message.headers ?? {})) {
        if (credentialName.test(name) || transportHeader.test(name) || !/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(name)) continue;
        for (const value of values) {
            if (value.includes('[REDACTED]') || /[\r\n\0]/.test(value)) continue;
            args.push('--header', quote(`${name}: ${value}`));
        }
    }
    if (message.captured_bytes > 0) args.push('--data-binary', quote(`@${filename}.txt`));
    return args.join(' ');
}

export function downloadTraceFile(filename: string, body: string | Uint8Array<ArrayBuffer>, type = 'application/octet-stream') {
    const url = URL.createObjectURL(new Blob([body], { type }));
    const anchor = document.createElement('a');
    anchor.href = url; anchor.download = filename; anchor.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export interface JSONChange { path: string; before: string; after: string }
export function compareJSON(left: string, right: string) {
    const changes: JSONChange[] = [];
    const result = { changes, comparable: false, limited: false, equal: left === right };
    if (left.length > 1 << 20 || right.length > 1 << 20) return result;
    let a: unknown, b: unknown;
    try { a = JSON.parse(left); b = JSON.parse(right); } catch { return result; }
    result.comparable = true;
    let visited = 0;
    const preview = (value: unknown) => value === undefined ? '—' : (JSON.stringify(value) ?? '').slice(0, 500);
    const walk = (before: unknown, after: unknown, path: string, depth: number) => {
        if (before === after) return;
        if (changes.length >= 100 || ++visited > 4096) { result.limited = true; return; }
        if (before && after && typeof before === 'object' && typeof after === 'object' && Array.isArray(before) === Array.isArray(after) && depth < 16) {
            const one = before as Record<string, unknown>, two = after as Record<string, unknown>;
            for (const key of new Set([...Object.keys(one), ...Object.keys(two)])) {
                walk(Object.hasOwn(one, key) ? one[key] : undefined, Object.hasOwn(two, key) ? two[key] : undefined, `${path}/${key.replace(/~/g, '~0').replace(/\//g, '~1')}`, depth + 1);
                if (result.limited) break;
            }
        } else changes.push({ path: path || '/', before: preview(before), after: preview(after) });
    };
    walk(a, b, '', 0);
    result.equal = changes.length === 0 && !result.limited;
    return result;
}
