import { expect, test } from '@playwright/test';
import { buildCurl, canReproduce, compareJSON } from '../../src/components/modules/log/trace-tools';
import type { RelayMessage } from '../../src/api/endpoints/log';

test('cURL quotes shell arguments, strips credentials, and references the downloaded body', () => {
    const message: RelayMessage = {
        state: 'captured', method: 'POST', url: 'https://user:password@example.com/v1?api_key=secret&model=alias',
        headers: { Authorization: ['Bearer secret'], Cookie: ['secret'], 'Content-Length': ['2'], 'X-Comment': ["hello'; $(echo unsafe)"], 'X-Custom': ['[REDACTED]'] },
        bytes: 2, captured_bytes: 2, body: '{}', body_encoding: 'utf-8',
    };
    const ps = buildCurl(message, 'request', 'powershell', 'https://localhost')!;
    expect(ps).toContain("'X-Comment: hello''; $(echo unsafe)'");
    const bash = buildCurl(message, 'request', 'bash', 'https://localhost')!;
    expect(bash).toContain("'X-Comment: hello'\\''; $(echo unsafe)'");
    for (const value of [ps, bash]) {
        expect(value).not.toMatch(/secret|password|Authorization|Cookie|Content-Length|REDACTED/);
        expect(value).toContain("--data-binary '@request.txt'");
        expect(value).toContain('model=alias');
    }
    expect(canReproduce({ ...message, state: 'truncated' }, 'http')).toBe(false);
    expect(canReproduce({ ...message, representation: 'multipart_metadata' }, 'http')).toBe(false);
    expect(canReproduce(message, 'ws')).toBe(false);
    expect(buildCurl({ ...message, url: 'file:///etc/passwd' }, 'request', 'bash', 'https://localhost')).toBeNull();
});

test('JSON comparison preserves null/missing distinctions and limits large diffs', () => {
    expect(compareJSON('{"a":1,"b":2}', '{"b":2,"a":1}').equal).toBe(true);
    const diff = compareJSON('{"model":"alias","cache":null}', '{"model":"provider"}');
    expect(diff.changes).toContainEqual({ path: '/model', before: '"alias"', after: '"provider"' });
    expect(diff.changes).toContainEqual({ path: '/cache', before: 'null', after: '—' });
    expect(compareJSON('data: stream\n\n', '{}').comparable).toBe(false);
    const large = compareJSON(JSON.stringify(Array(200).fill(1)), JSON.stringify(Array(200).fill(2)));
    expect(large.limited).toBe(true);
    expect(large.changes).toHaveLength(100);
});
