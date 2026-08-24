export function siteSyncStatusHasFailure(status?: string | null) {
  const normalized = status || "idle";
  return normalized === "failed";
}
