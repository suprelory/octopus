type SyncHealthSite = { enabled: boolean };

type SyncHealthAccount = {
  enabled: boolean;
  auto_sync: boolean;
  last_sync_status?: string | null;
};

export function siteSyncStatusHasFailure(status?: string | null) {
  const normalized = status || "idle";
  return normalized === "failed";
}

export function siteSyncStatusIsPartial(status?: string | null) {
  const normalized = status || "idle";
  return normalized === "partial";
}

/**
 * Whether an account's last sync status still describes something actionable.
 * A stale status on a disabled account, a disabled site, or an account that is
 * not synced automatically is history, not a live problem. Every surface that
 * reports sync health shares this gate so the site card, the account card and
 * the sync filter cannot disagree about the same account.
 */
export function siteAccountSyncIsActive(
  site: SyncHealthSite,
  account: SyncHealthAccount,
) {
  return site.enabled && account.enabled && account.auto_sync;
}

export function siteAccountHasActiveSyncFailure(
  site: SyncHealthSite,
  account: SyncHealthAccount,
) {
  return (
    siteAccountSyncIsActive(site, account) &&
    siteSyncStatusHasFailure(account.last_sync_status)
  );
}

export function siteAccountHasActivePartialSync(
  site: SyncHealthSite,
  account: SyncHealthAccount,
) {
  return (
    siteAccountSyncIsActive(site, account) &&
    siteSyncStatusIsPartial(account.last_sync_status)
  );
}
