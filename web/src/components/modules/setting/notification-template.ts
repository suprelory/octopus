export const NOTIFICATION_VARIABLES = [
    'title', 'message', 'time', 'level', 'emoji', 'event', 'site', 'account',
    'source', 'detail', 'reward', 'balance', 'threshold', 'failure_count',
] as const;

export function renderNotificationTemplate(source: string, values: Record<string, string>): string | null {
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

export function validNotificationTemplate(template: { title?: string; body?: string }): boolean {
    const title = template.title ?? '';
    const body = template.body ?? '';
    const encoder = new TextEncoder();
    return encoder.encode(title).length <= 512 && encoder.encode(body).length <= 8192
        && !/[\r\n\0]/.test(title) && !body.includes('\0')
        && renderNotificationTemplate(title, {}) !== null && renderNotificationTemplate(body, {}) !== null;
}
