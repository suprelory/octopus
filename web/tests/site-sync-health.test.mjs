import assert from 'node:assert/strict';
import test from 'node:test';

import { siteSyncStatusHasFailure } from '../src/components/modules/site/sync-health.ts';

test('a failed sync marks the account as unhealthy', () => {
    assert.equal(siteSyncStatusHasFailure('failed'), true);
});

test('a partial sync also marks the account as unhealthy', () => {
    assert.equal(siteSyncStatusHasFailure('partial'), true);
});

test('idle and successful syncs remain healthy', () => {
    assert.equal(siteSyncStatusHasFailure('idle'), false);
    assert.equal(siteSyncStatusHasFailure('success'), false);
    assert.equal(siteSyncStatusHasFailure(undefined), false);
});
