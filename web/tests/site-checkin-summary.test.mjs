import assert from 'node:assert/strict';
import test from 'node:test';

import {
    createEmptyCheckinSummary,
    recordCheckinSummaryAccount,
} from '../src/components/modules/site/checkin-summary.ts';

test('every account contributes to the all-account total', () => {
    const summary = createEmptyCheckinSummary();

    recordCheckinSummaryAccount(summary, null, false);
    recordCheckinSummaryAccount(summary, 'idle', false);

    assert.equal(summary.total, 2);
    assert.equal(summary.idle, 1);
});

test('sync failure is an overlapping category and does not change total membership', () => {
    const summary = createEmptyCheckinSummary();

    recordCheckinSummaryAccount(summary, null, true);
    recordCheckinSummaryAccount(summary, 'failed', true);

    assert.deepEqual(summary, {
        total: 2,
        success: 0,
        failed: 1,
        sync_failed: 2,
        idle: 0,
        disabled: 0,
    });
});
