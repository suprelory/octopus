import assert from 'node:assert/strict';
import test from 'node:test';

import {
    makeDisableTargetKey,
    mergeAdjacentAttempts,
    resolveTokenUsageDisplay,
} from '../src/components/modules/log/log-format.ts';

function attempt(attemptNum, overrides = {}) {
    return {
        channel_id: 1,
        channel_key_id: 11,
        channel_name: 'Primary',
        model_name: 'gpt-test',
        attempt_num: attemptNum,
        status: 'failed',
        duration: 100,
        msg: 'upstream unavailable',
        ...overrides,
    };
}

test('merged retries preserve original target indexes and leave source logs intact', () => {
    const attempts = [
        attempt(1),
        attempt(2, { duration: 250 }),
        attempt(3, { channel_id: 2, channel_key_id: 22 }),
        attempt(4),
    ];
    const original = structuredClone(attempts);
    const merged = mergeAdjacentAttempts(attempts);

    assert.deepEqual(merged.map(item => item.originalIndex), [0, 2, 3]);
    assert.deepEqual(merged.map(item => item.repeat), [2, 1, 1]);
    assert.equal(merged[0].totalDuration, 350);
    assert.equal(merged[0].lastAttemptNum, 2);
    assert.deepEqual(attempts, original);
});

test('retries with different keys, models or outcomes remain separate', () => {
    for (const change of [
        { channel_id: 2 },
        { channel_key_id: 22 },
        { model_name: 'another-model' },
        { status: 'success' },
        { msg: 'different failure' },
    ]) {
        assert.equal(mergeAdjacentAttempts([attempt(1), attempt(2, change)]).length, 2);
    }
    assert.deepEqual(mergeAdjacentAttempts([]), []);
});

test('token totals preserve modern billing and legacy cache accounting', () => {
    const base = {
        inputTokens: 100,
        outputTokens: 20,
        billInputTokens: null,
        cacheReadTokens: 40,
        cacheWriteTokens: 0,
        adapterType: 'chat',
        channelName: 'Primary',
    };
    const cases = [
        { name: 'legacy OpenAI', changes: {}, nonCached: 60, input: 100, total: 120 },
        { name: 'legacy Anthropic', changes: { adapterType: 'anthropic' }, nonCached: 100, input: 140, total: 160 },
        { name: 'historical channel name', changes: { channelName: 'Site/Account/Group-Anthropic' }, nonCached: 100, input: 140, total: 160 },
        { name: 'cache write', changes: { cacheWriteTokens: 10 }, nonCached: 100, input: 150, total: 170 },
        { name: 'normalized billing', changes: { billInputTokens: 7 }, nonCached: 7, input: 47, total: 67 },
        { name: 'zero is a known billed count', changes: { billInputTokens: 0 }, nonCached: 0, input: 40, total: 60 },
        { name: 'input excludes a larger cache read', changes: { inputTokens: 10 }, nonCached: 10, input: 50, total: 70 },
        { name: 'negative counts', changes: { inputTokens: -5, outputTokens: -2, cacheReadTokens: -3, cacheWriteTokens: -4 }, nonCached: 0, input: 0, total: 0 },
    ];
    for (const { name, changes, nonCached, input, total } of cases) {
        const usage = resolveTokenUsageDisplay({ ...base, ...changes });
        assert.equal(usage.nonCachedInputTokens, nonCached, name);
        assert.equal(usage.totalInputTokens, input, name);
        assert.equal(usage.totalTokens, total, name);
    }
});

test('disable pending state distinguishes accounts and groups using the same model', () => {
    const target = { site_id: 1, account_id: 11, group_key: 'default', model_name: 'gpt-test' };
    const keys = [
        target,
        { ...target, site_id: 2 },
        { ...target, account_id: 12 },
        { ...target, group_key: 'other' },
        { ...target, model_name: 'other-model' },
    ].map(makeDisableTargetKey);
    assert.equal(new Set(keys).size, keys.length);
});
