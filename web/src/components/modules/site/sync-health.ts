export function siteSyncStatusHasFailure(status?: string | null) {
  const normalized = status || "idle";
  return normalized === "failed";
}

export function siteAccountHasActiveSyncFailure(
  site: { enabled: boolean },
  account: {
    enabled: boolean;
    auto_sync: boolean;
    last_sync_status?: string | null;
  },
) {
  return (
    site.enabled &&
    account.enabled &&
    account.auto_sync &&
    siteSyncStatusHasFailure(account.last_sync_status)
  );
}
