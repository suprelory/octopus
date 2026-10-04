import type { Site } from '@/api/endpoints/site';
import { siteAccountHasActiveSyncFailure } from './sync-health';

export type SiteActiveFilterStatus = 'normal' | 'abnormal' | 'disabled';
export type SiteFilterStatus = 'all' | SiteActiveFilterStatus;
export type SiteStatusSummary = Record<SiteFilterStatus, number>;

export function deriveSiteStatus(site: Site): SiteActiveFilterStatus {
    if (!site.enabled) return 'disabled';
    return site.accounts.some(account => siteAccountHasActiveSyncFailure(site, account))
        ? 'abnormal'
        : 'normal';
}

export function buildSiteStatusSummary(sites: Site[] | undefined): SiteStatusSummary {
    const summary: SiteStatusSummary = { all: 0, normal: 0, abnormal: 0, disabled: 0 };
    for (const site of sites ?? []) {
        summary.all += 1;
        summary[deriveSiteStatus(site)] += 1;
    }
    return summary;
}
