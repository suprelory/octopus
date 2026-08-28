type SyncHealthSite = { enabled: boolean };

type SyncHealthAccount = {
  enabled: boolean;
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
 * Disabled sites and accounts are inactive, but auto_sync only controls
 * scheduling: a manual sync failure remains actionable. Every surface that
 * reports sync health shares this gate so the site card, the account card and
 * the sync filter cannot disagree about the same account.
 */
export function siteAccountSyncIsActive(
  site: SyncHealthSite,
  account: SyncHealthAccount,
) {
  return site.enabled && account.enabled;
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
