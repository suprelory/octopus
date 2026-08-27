import assert from 'node:assert/strict';
import test from 'node:test';

import {
    siteAccountHasActivePartialSync,
    siteAccountHasActiveSyncFailure,
    siteAccountSyncIsActive,
    siteSyncStatusHasFailure,
    siteSyncStatusIsPartial,
} from '../src/components/modules/site/sync-health.ts';

test('a failed sync marks the account as unhealthy', () => {
    assert.equal(siteSyncStatusHasFailure('failed'), true);
});

test('only a partial status counts as a partial sync', () => {
    assert.equal(siteSyncStatusIsPartial('partial'), true);
    assert.equal(siteSyncStatusIsPartial('failed'), false);
    assert.equal(siteSyncStatusIsPartial('success'), false);
    assert.equal(siteSyncStatusIsPartial(undefined), false);
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

test('the partial sync signal is gated the same way as the failure signal', () => {
    const enabledSite = { enabled: true };
    const partialAccount = {
        enabled: true,
        auto_sync: true,
        last_sync_status: 'partial',
    };

    assert.equal(siteAccountHasActivePartialSync(enabledSite, partialAccount), true);
    assert.equal(
        siteAccountHasActivePartialSync(enabledSite, { ...partialAccount, auto_sync: false }),
        false,
    );
    assert.equal(
        siteAccountHasActivePartialSync(enabledSite, { ...partialAccount, enabled: false }),
        false,
    );
    assert.equal(
        siteAccountHasActivePartialSync({ enabled: false }, partialAccount),
        false,
    );
});

// The card badge and the sync filter must agree, so both read the same gate.
test('a stale status on an inactive account is not reported as a live problem', () => {
    const failedAccount = {
        enabled: true,
        auto_sync: true,
        last_sync_status: 'failed',
    };

    assert.equal(siteAccountSyncIsActive({ enabled: true }, failedAccount), true);
    assert.equal(siteAccountSyncIsActive({ enabled: false }, failedAccount), false);
    assert.equal(
        siteAccountSyncIsActive({ enabled: true }, { ...failedAccount, enabled: false }),
        false,
    );
    assert.equal(
        siteAccountSyncIsActive({ enabled: true }, { ...failedAccount, auto_sync: false }),
        false,
    );

    for (const account of [
        { ...failedAccount, enabled: false },
        { ...failedAccount, auto_sync: false },
    ]) {
        assert.equal(
            siteAccountHasActiveSyncFailure({ enabled: true }, account),
            siteAccountSyncIsActive({ enabled: true }, account),
        );
    }
});
