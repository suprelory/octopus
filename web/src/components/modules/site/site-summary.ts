import { SiteAccount, Site as SiteRecord } from "@/api/endpoints/site";
import { accountHasCheckinEnabled, deriveCheckinStatus } from "./checkin-status";
import { normalizedStatus } from "./site-display";
import { siteAccountHasActivePartialSync, siteAccountHasActiveSyncFailure } from "./sync-health";
import { SiteSummary, VisibleSite } from "./types";

export function accountHasCheckinFailure(site: SiteRecord, account: SiteAccount) {
  return deriveCheckinStatus(site, account) === "failed";
}

// Shares siteAccountHasActiveSyncFailure's gate so the card badge and the
// "同步失败" filter describe the same set of accounts.
export function accountHasHealthFailure(site: SiteRecord, account: SiteAccount) {
  return siteAccountHasActiveSyncFailure(site, account) || accountHasCheckinFailure(site, account);
}

export function buildSiteSummary(site: SiteRecord): SiteSummary {
  let keyCount = 0;
  let modelCount = 0;
  let groupCount = 0;
  let balance = 0;
  let todayIncome = 0;
  let failedAccountCount = 0;
  let partialAccountCount = 0;
  let disabledAccountCount = 0;
  let enabledAccountCount = 0;

  for (const account of site.accounts) {
    keyCount += account.tokens.length;
    modelCount += account.models.length;
    groupCount += account.user_groups.length;
    balance += account.balance;
    todayIncome += typeof account.today_income === "number" ? account.today_income : 0;

    if (account.enabled) enabledAccountCount += 1;
    else disabledAccountCount += 1;

    if (accountHasHealthFailure(site, account)) {
      failedAccountCount += 1;
    } else if (siteAccountHasActivePartialSync(site, account)) {
      partialAccountCount += 1;
    }
  }

  if (!site.enabled) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: "站点停用",
      healthTone: "muted",
    };
  }

  if (failedAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: `${failedAccountCount} 异常`,
      healthTone: "danger",
    };
  }

  // A partial sync is actionable, so it outranks the purely informational
  // "some accounts are disabled" label; otherwise one disabled account hides
  // the warning for every partially synced account on the site.
  if (partialAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: `${partialAccountCount} 部分同步`,
      healthTone: "warning",
    };
  }

  if (disabledAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: `${disabledAccountCount} 已停用`,
      healthTone: "muted",
    };
  }

  if (site.accounts.length === 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: "待配置",
      healthTone: "warning",
    };
  }

  const allIdle = site.accounts.every(
    (account) =>
      account.enabled &&
      normalizedStatus(account.last_sync_status) === "idle" &&
      (!accountHasCheckinEnabled(account, site.platform) ||
        deriveCheckinStatus(site, account) === "idle"),
  );

  return {
    accountCount: site.accounts.length,
    keyCount,
    modelCount,
    groupCount,
    balance,
    todayIncome,
    failedAccountCount,
    partialAccountCount,
    disabledAccountCount,
    enabledAccountCount,
    healthLabel: allIdle ? "未执行" : "正常",
    healthTone: allIdle ? "warning" : "default",
  };
}

export function estimateVisibleSiteCardHeight(item: VisibleSite, expanded: boolean) {
  const tagRow = item.site.tags.length > 0 ? 30 : 0;
  if (item.forceExpanded || expanded) {
    return 360 + tagRow + item.visibleAccounts.length * 190;
  }
  if (item.site.accounts.length === 0) {
    return 280 + tagRow;
  }
  return 310 + tagRow;
}
