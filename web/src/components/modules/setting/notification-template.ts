export const NOTIFICATION_VARIABLES = [
    'title', 'message', 'time', 'level', 'emoji', 'event', 'site', 'account',
    'source', 'detail', 'reward', 'balance', 'threshold', 'failure_count',
] as const;

export function escapeNotificationMarkdown(value: string): string {
    let result = '';
    for (const character of value.replace(/\r\n/g, '\n')) {
        if (character === '\0') continue;
        const code = character.codePointAt(0)!;
        if ((code >= 33 && code <= 47) || (code >= 58 && code <= 64)
            || (code >= 91 && code <= 96) || (code >= 123 && code <= 126)) result += '\\';
        result += character;
    }
    return result;
}

export function renderNotificationTemplate(source: string, values: Record<string, string>, markdown = false): string | null {
    if (markdown) return renderMarkdownTemplate(source, values);
    let result = '';
    for (;;) {
        const start = source.indexOf('{{');
        if (start < 0) return result + source;
        result += source.slice(0, start);
        source = source.slice(start + 2);
        const end = source.indexOf('}}');
        if (end < 0) return null;
        const key = source.slice(0, end).trim();
        if (!(NOTIFICATION_VARIABLES as readonly string[]).includes(key)) return null;
        result += values[key] ?? '';
        source = source.slice(end + 2);
    }
}

function renderMarkdownTemplate(source: string, values: Record<string, string>): string | null {
    const escaped = Object.fromEntries(Object.entries(values).map(([key, value]) => [key, escapeNotificationMarkdown(value)]));
    let result = '', start = 0;
    for (let index = 0; index < source.length;) {
        const marker = source[index];
        if (marker !== '`' && marker !== '~') { index++; continue; }
        const count = delimiterRun(source, index, marker);
        const fenced = count >= 3 && fencePrefix(source, index);
        let backslashes = 0;
        for (let previous = index - 1; previous >= 0 && source[previous] === '\\'; previous--) backslashes++;
        if (!fenced && (marker === '~' || backslashes % 2 !== 0)) { index += count; continue; }
        let contentStart = index + count;
        if (fenced) {
            const newline = source.indexOf('\n', contentStart);
            if (newline < 0) { index += count; continue; }
            contentStart = newline + 1;
        }
        let closing = -1, closingCount = 0;
        for (let next = contentStart; next < source.length;) {
            next = source.indexOf(marker, next);
            if (next < 0) break;
            const run = delimiterRun(source, next, marker);
            if (fenced && run >= count && fencePrefix(source, next)) {
                const newline = source.indexOf('\n', next + run);
                if (!source.slice(next + run, newline < 0 ? source.length : newline).trim()) { closing = next; closingCount = run; break; }
            } else if (!fenced && run === count) { closing = next; closingCount = run; break; }
            next += run;
        }
        if (closing < 0) {
            if (!fenced) { index += count; continue; }
            closing = source.length;
        }
        const contentEnd = fenced && closing < source.length ? source.lastIndexOf('\n', closing - 1) + 1 : closing;
        let content = source.slice(contentStart, contentEnd);
        if (!fenced && content.startsWith(' ') && content.endsWith(' ') && content.trim()) content = content.slice(1, -1);
        let literal = renderNotificationTemplate(content, values);
        const normal = renderNotificationTemplate(source.slice(start, index), escaped);
        if (literal === null || normal === null) return null;
        literal = literal.replace(/\0/g, '').replace(/\r\n/g, '\n');
        if (!fenced) literal = literal.replace(/\n/g, ' ');
        let width = count;
        for (let offset = 0; offset < literal.length;) {
            if (literal[offset] === marker) {
                const run = delimiterRun(literal, offset, marker);
                width = Math.max(width, run + 1);
                offset += run;
            } else offset++;
        }
        const delimiter = marker.repeat(width);
        result += normal + delimiter;
        if (fenced) {
            const info = renderNotificationTemplate(source.slice(index + count, contentStart), escaped);
            if (info === null) return null;
            result += info + literal + (literal.endsWith('\n') ? '' : '\n') + source.slice(contentEnd, closing);
        } else result += literal.trim() ? ` ${literal} ` : literal;
        result += delimiter;
        index = closing + closingCount;
        start = index;
    }
    const normal = renderNotificationTemplate(source.slice(start), escaped);
    return normal === null ? null : result + normal;
}

function delimiterRun(source: string, index: number, marker: string): number {
    let end = index;
    while (source[end] === marker) end++;
    return end - index;
}

function fencePrefix(source: string, index: number): boolean {
    const prefix = source.slice(source.lastIndexOf('\n', index - 1) + 1, index);
    return prefix.length <= 3 && !prefix.replace(/ /g, '');
}

export function validNotificationTemplate(template: { title?: string; body?: string; format?: string }): boolean {
    const title = template.title ?? '';
    const body = template.body ?? '';
    const encoder = new TextEncoder();
    return ['', 'text', 'markdown'].includes(template.format ?? '')
        && encoder.encode(title).length <= 512 && encoder.encode(body).length <= 8192
        && !/[\r\n\0]/.test(title) && !body.includes('\0')
        && renderNotificationTemplate(title, {}) !== null && renderNotificationTemplate(body, {}) !== null;
}
