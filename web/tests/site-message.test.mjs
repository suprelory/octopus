import assert from 'node:assert/strict';
import test from 'node:test';

import { isCloudflareProtectionMessage } from '../src/components/modules/site/site-message.ts';

test('upstream failures keep their failure status when served by Cloudflare', () => {
    for (const message of [
        'http 502: Bad Gateway',
        'http 502: Cloudflare Tunnel error (Error 1033)',
        'http 503: Service Unavailable | Cloudflare',
        'http 403: Forbidden (Cloudflare Ray ID: abc123)',
        'Please wait just a moment',
        '',
        null,
        undefined,
    ]) {
        assert.equal(isCloudflareProtectionMessage(message), false, String(message));
    }
});

test('explicit Cloudflare challenges keep their protection status', () => {
    for (const message of [
        'http 403: 站点触发 Cloudflare 保护，请稍后重试',
        '站點觸發 Cloudflare 保護',
        'The site triggered Cloudflare protection.',
        'http 403: Cloudflare challenge',
        'http 503: Just a moment...',
        'Attention Required! | Cloudflare',
        'Sorry, you have been blocked | Cloudflare',
    ]) {
        assert.equal(isCloudflareProtectionMessage(message), true, message);
    }
});
