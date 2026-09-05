import { type SiteChannelCard, type SiteModelRouteType } from '@/api/endpoints/site-channel';

export function collectSiteSummary(card: SiteChannelCard) {
    let groupCount = 0;
    let modelCount = 0;
    let totalKeys = 0;
    let enabledKeys = 0;
    const routeCounts = new Map<SiteModelRouteType, number>();

    for (const account of card.accounts) {
        groupCount += account.group_count;
        modelCount += account.model_count;

        for (const group of account.groups) {
            totalKeys += group.key_count;
            enabledKeys += group.enabled_key_count;
        }

        for (const route of account.route_summaries) {
            routeCounts.set(route.route_type, (routeCounts.get(route.route_type) ?? 0) + route.count);
        }
    }

    return { groupCount, modelCount, totalKeys, enabledKeys, routeCounts };
}

export function collectSiteRuntimeSummary(card: SiteChannelCard) {
    let successCount = 0;
    let failureCount = 0;
    let totalCost = 0;
    let lastRequestAt: number | null = null;
    let maskedPendingKeys = 0;

    for (const account of card.accounts) {
        for (const group of account.groups) {
            maskedPendingKeys += group.masked_pending_key_count;
            for (const key of group.projected_keys) {
                totalCost += key.total_cost;
                if (key.last_use_time_stamp > 0) {
                    lastRequestAt = Math.max(lastRequestAt ?? 0, key.last_use_time_stamp);
                }
            }
            for (const m of group.models) {
                const h = m.history;
                if (!h) continue;
                successCount += h.success_count;
                failureCount += h.failure_count;
                if (typeof h.last_request_at === 'number' && h.last_request_at > 0) {
                    lastRequestAt = Math.max(lastRequestAt ?? 0, h.last_request_at);
                }
            }
        }
    }

    return {
        totalRequests: successCount + failureCount,
        successCount,
        failureCount,
        totalCost,
        lastRequestAt,
        maskedPendingKeys,
    };
}
