// The calendar and time input both use the browser's local time zone.
export function toExpireAt(date: Date, time: string): number {
    const [hours, minutes] = time.split(':').map(Number);
    return Math.floor(new Date(date.getFullYear(), date.getMonth(), date.getDate(), hours, minutes, 0).getTime() / 1000);
}

export function parseExpireDate(expireAt?: number): Date | undefined {
    if (!expireAt) return undefined;
    const date = new Date(expireAt * 1000);
    return Number.isNaN(date.getTime()) ? undefined : date;
}

export function formatExpireTime(date: Date): string {
    return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
}

export function updateExpireTime(expireAt: number, time: string): number {
    const date = new Date(expireAt * 1000);
    // Preserve seconds and the selected offset in a repeated DST hour when the
    // user simply focuses and blurs an unchanged minute-resolution input.
    return time === formatExpireTime(date) ? expireAt : toExpireAt(date, time);
}
