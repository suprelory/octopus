import assert from 'node:assert/strict';
import test from 'node:test';

import {
    siteAccountHasActiveSyncFailure,
    siteSyncStatusHasFailure,
} from '../src/components/modules/site/sync-health.ts';

test('a failed sync marks the account as unhealthy', () => {
    assert.equal(siteSyncStatusHasFailure('failed'), true);
});

test('a partial sync does not mark the account as unhealthy', () => {
    assert.equal(siteSyncStatusHasFailure('partial'), false);
});

test('idle and successful syncs remain healthy', () => {
    assert.equal(siteSyncStatusHasFailure('idle'), false);
    assert.equal(siteSyncStatusHasFailure('success'), false);
    assert.equal(siteSyncStatusHasFailure(undefined), false);
});

test('the sync failure filter only matches enabled automatic sync accounts', () => {
    const enabledSite = { enabled: true };
    const failedAccount = {
        enabled: true,
        auto_sync: true,
        last_sync_status: 'failed',
    };

    assert.equal(siteAccountHasActiveSyncFailure(enabledSite, failedAccount), true);
    assert.equal(
        siteAccountHasActiveSyncFailure(enabledSite, { ...failedAccount, auto_sync: false }),
        false,
    );
    assert.equal(
        siteAccountHasActiveSyncFailure(enabledSite, { ...failedAccount, enabled: false }),
        false,
    );
    assert.equal(
        siteAccountHasActiveSyncFailure({ enabled: false }, failedAccount),
        false,
    );
    assert.equal(
        siteAccountHasActiveSyncFailure(enabledSite, {
            ...failedAccount,
            last_sync_status: 'partial',
        }),
        false,
    );
});
