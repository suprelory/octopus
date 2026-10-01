import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

for (const zone of ['Asia/Shanghai', 'America/Los_Angeles', 'UTC']) {
    test(`expiration round trips and edits use local time in ${zone}`, () => {
        const moduleURL = new URL('../src/lib/api-key-expiration.ts', import.meta.url).href;
        const result = spawnSync(process.execPath, ['--input-type=module', '-e', `
            import assert from 'node:assert/strict';
            import { toExpireAt, parseExpireDate, formatExpireTime, updateExpireTime } from ${JSON.stringify(moduleURL)};
            for (const iso of ['2026-09-30T20:00:37Z', '2026-11-01T08:30:00Z', '2026-11-01T09:30:00Z', '2026-03-08T10:30:00Z']) {
                const stamp = Date.parse(iso) / 1000;
                const date = parseExpireDate(stamp);
                assert.equal(updateExpireTime(stamp, formatExpireTime(date)), stamp);
                assert.equal(formatExpireTime(date), String(date.getHours()).padStart(2, '0') + ':' + String(date.getMinutes()).padStart(2, '0'));
            }
            const date = new Date(2026, 9, 1);
            const stamp = toExpireAt(date, '18:45');
            assert.equal(stamp, new Date(2026, 9, 1, 18, 45).getTime() / 1000);
            assert.equal(updateExpireTime(stamp, '19:15'), new Date(2026, 9, 1, 19, 15).getTime() / 1000);
            assert.equal(parseExpireDate(undefined), undefined);
        `], { env: { ...process.env, TZ: zone }, encoding: 'utf8' });
        assert.equal(result.status, 0, result.stderr);
    });
}
