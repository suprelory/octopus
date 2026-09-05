"use client";

import { SiteAccount, Site as SiteRecord } from "@/api/endpoints/site";
import { useSearchStore, useToolbarViewOptionsStore } from "@/components/modules/toolbar";
import { useEffect, useMemo, useState } from "react";
import { accountMatchesCheckinFilters, type CheckinFilterStatus } from "./checkin-status";
import { useSiteUIStore } from "./ui-store";

import { PLATFORM_LABELS, matchesSearch, normalizeSearchTerm } from "./site-display";
import { buildSiteSummary } from "./site-summary";
import { VisibleSite } from "./types";

export function useSiteView(sites: SiteRecord[] | undefined, forcedSiteId: number | null) {
  const [statusDayKey, setStatusDayKey] = useState(() => {
    const now = new Date();
    return `${now.getFullYear()}-${now.getMonth()}-${now.getDate()}`;
  });

  const searchTerm = useSearchStore((state) => state.getSearchTerm("site"));

  const setSearchTerm = useSearchStore((state) => state.setSearchTerm);

  const siteSortField = useToolbarViewOptionsStore((state) => state.getSortField("site"));

  const siteSortOrder = useToolbarViewOptionsStore((state) => state.getSortOrder("site"));

  const checkinFilterStatuses = useSiteUIStore((state) => state.checkinFilterStatuses);

  const setCheckinFilterStatuses = useSiteUIStore((state) => state.setCheckinFilterStatuses);

  const tagFilters = useSiteUIStore((state) => state.tagFilters);

  const setTagFilters = useSiteUIStore((state) => state.setTagFilters);

  const inventory = useMemo(() => {
    let totalBalance = 0;
    let totalBalanceUsed = 0;
    let enabledAccounts = 0;
    let totalAccounts = 0;

    for (const site of sites ?? []) {
      for (const account of site.accounts) {
        totalAccounts += 1;
        if (site.enabled && account.enabled) {
          enabledAccounts += 1;
        }
        totalBalance += typeof account.balance === "number" ? account.balance : 0;
        totalBalanceUsed += typeof account.balance_used === "number" ? account.balance_used : 0;
      }
    }

    return {
      totalBalance,
      totalBalanceUsed,
      enabledAccounts,
      totalAccounts,
    };
  }, [sites]);

  const normalizedQuery = useMemo(() => normalizeSearchTerm(searchTerm), [searchTerm]);

  const allTags = useMemo(() => {
    const counts = new Map<string, number>();
    for (const site of sites ?? []) {
      for (const tag of site.tags) {
        counts.set(tag, (counts.get(tag) ?? 0) + 1);
      }
    }
    return Array.from(counts, ([tag, count]) => ({ tag, count })).sort(
      (a, b) => b.count - a.count || a.tag.localeCompare(b.tag),
    );
  }, [sites]);

  const allTagNames = useMemo(() => allTags.map((item) => item.tag), [allTags]);

  const visibleSites = useMemo<VisibleSite[]>(() => {
    const hasSearch = normalizedQuery.length > 0;

    const list = (sites ?? []).flatMap((site) => {
      const summary = buildSiteSummary(site);
      const isForcedTarget = forcedSiteId === site.id;

      if (
        tagFilters.length > 0 &&
        !isForcedTarget &&
        !site.tags.some((tag) => tagFilters.includes(tag))
      ) {
        return [];
      }

      const hasCheckinFilters = checkinFilterStatuses.length > 0;

      const siteMatchesQuery =
        !hasSearch ||
        matchesSearch(site.name, normalizedQuery) ||
        matchesSearch(site.base_url, normalizedQuery) ||
        matchesSearch(PLATFORM_LABELS[site.platform], normalizedQuery);

      const accountMatchesQuery = (account: SiteAccount) =>
        matchesSearch(account.name, normalizedQuery);

      const matchedAccountsBySearch = hasSearch
        ? site.accounts.filter(accountMatchesQuery)
        : site.accounts;

      let visibleAccounts = site.accounts;
      let forceExpanded = hasCheckinFilters || isForcedTarget;

      if (hasCheckinFilters && !isForcedTarget) {
        visibleAccounts = visibleAccounts.filter((account) =>
          accountMatchesCheckinFilters(site, account, checkinFilterStatuses),
        );
      }

      if (hasSearch && !siteMatchesQuery && !isForcedTarget) {
        visibleAccounts = visibleAccounts.filter(accountMatchesQuery);
        forceExpanded = visibleAccounts.length > 0 || forceExpanded;
      }

      if (isForcedTarget) {
        visibleAccounts = site.accounts;
      }

      const visible = isForcedTarget
        ? true
        : hasCheckinFilters
          ? visibleAccounts.length > 0
          : !hasSearch || siteMatchesQuery || matchedAccountsBySearch.length > 0;

      if (!visible) {
        return [];
      }

      return [
        {
          site,
          summary,
          visibleAccounts,
          forceExpanded,
          hasFilteredAccounts: visibleAccounts.length !== site.accounts.length,
        },
      ];
    });

    if (siteSortField === "default") {
      return list;
    }

    return [...list].sort((a, b) => {
      if (a.site.is_pinned !== b.site.is_pinned) {
        return a.site.is_pinned ? -1 : 1;
      }

      let diff = 0;
      if (siteSortField === "balance") {
        diff = a.summary.balance - b.summary.balance;
      } else {
        diff = a.site.name.localeCompare(b.site.name);
      }

      if (diff !== 0) {
        return siteSortOrder === "asc" ? diff : -diff;
      }

      return a.site.sort_order - b.site.sort_order || a.site.id - b.site.id;
    });
  }, [
    sites,
    normalizedQuery,
    checkinFilterStatuses,
    tagFilters,
    forcedSiteId,
    siteSortField,
    siteSortOrder,
  ]);

  const hasActiveFilters =
    normalizedQuery.length > 0 || checkinFilterStatuses.length > 0 || tagFilters.length > 0;

  const visibleAccountCount = visibleSites.reduce(
    (sum, item) => sum + item.visibleAccounts.length,
    0,
  );

  function handleCheckinFilterChange(status: CheckinFilterStatus) {
    if (status === "all") {
      setCheckinFilterStatuses([]);
      return;
    }

    setCheckinFilterStatuses((current) =>
      current.includes(status) ? current.filter((item) => item !== status) : [...current, status],
    );
  }

  function handleTagFilterChange(tag: string) {
    setTagFilters((current) =>
      current.includes(tag) ? current.filter((item) => item !== tag) : [...current, tag],
    );
  }

  function clearFilters() {
    setSearchTerm("site", "");
    setCheckinFilterStatuses([]);
    setTagFilters([]);
  }

  useEffect(() => {
    const updateDayKey = () => {
      const now = new Date();
      setStatusDayKey(`${now.getFullYear()}-${now.getMonth()}-${now.getDate()}`);
    };

    updateDayKey();
    const timer = window.setInterval(updateDayKey, 60_000);
    return () => window.clearInterval(timer);
  }, []);

  return {
    searchTerm,
    checkinFilterStatuses,
    tagFilters,
    statusDayKey,
    inventory,
    allTags,
    allTagNames,
    visibleSites,
    hasActiveFilters,
    visibleAccountCount,
    handleCheckinFilterChange,
    handleTagFilterChange,
    clearFilters,
  };
}
