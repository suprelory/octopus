"use client";

import {
  SiteAccount,
  Site as SiteRecord,
  useCheckinAllSites,
  useSiteList,
  useSyncAllSites,
} from "@/api/endpoints/site";
import { PageWrapper } from "@/components/common/PageWrapper";
import { toast } from "@/components/common/Toast";
import { Button } from "@/components/ui/button";
import { isSiteJumpTarget, useJumpStore } from "@/stores/jump";
import { useSettingStore } from "@/stores/setting";
import { CircleAlert, FilterX, Plus } from "lucide-react";
import { useTranslations } from "next-intl";
import { useEffect, useMemo, useState } from "react";
import { AccountEditDialog } from "./AccountEditDialog";
import { BatchEditDialog } from "./BatchEditDialog";
import { CheckinPanel } from "./CheckinPanel";
import { ManualSyncDialog } from "./ManualSyncDialog";
import { SiteEditDialog } from "./SiteEditDialog";
import { useSiteUIStore } from "./ui-store";

import { getSiteErrorMessage } from "./site-display";

import { SitePendingJump } from "./types";

import { ArchivedSitesDialog } from "./ArchivedSitesDialog";
import { SiteBatchBar } from "./SiteBatchBar";
import { SiteCard } from "./SiteCard";
import { SiteDeleteDialog } from "./SiteDeleteDialog";
import { SiteImportDialog } from "./SiteImportDialog";
import { useSiteActions } from "./useSiteActions";
import { useSiteLayout } from "./useSiteLayout";
import { useSiteView } from "./useSiteView";

export function Site() {
  const t = useTranslations();
  const locale = useSettingStore((state) => state.locale);
  const { data: sites, isLoading, error } = useSiteList();
  const syncAllSites = useSyncAllSites();
  const checkinAllSites = useCheckinAllSites();

  const [siteDialogOpen, setSiteDialogOpen] = useState(false);
  const [importDialogOpen, setImportDialogOpen] = useState(false);
  const [archivedDialogOpen, setArchivedDialogOpen] = useState(false);
  const [editingSite, setEditingSite] = useState<SiteRecord | null>(null);

  const [accountDialogOpen, setAccountDialogOpen] = useState(false);
  const [accountSite, setAccountSite] = useState<SiteRecord | null>(null);
  const [editingAccount, setEditingAccount] = useState<SiteAccount | null>(null);
  const [manualSyncDialogOpen, setManualSyncDialogOpen] = useState(false);
  const [manualSyncSite, setManualSyncSite] = useState<SiteRecord | null>(null);
  const [manualSyncAccount, setManualSyncAccount] = useState<SiteAccount | null>(null);
  const [batchEditOpen, setBatchEditOpen] = useState(false);
  const setSiteHandlers = useSiteUIStore((state) => state.setHandlers);
  const resetSiteHandlers = useSiteUIStore((state) => state.resetHandlers);
  const pendingJump = useJumpStore((state) => state.pending);

  const pendingSiteJump =
    pendingJump && isSiteJumpTarget(pendingJump.target) ? (pendingJump as SitePendingJump) : null;
  const forcedSiteId = pendingSiteJump?.target.siteId ?? null;

  const view = useSiteView(sites, forcedSiteId);
  const {
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
  } = view;
  const layout = useSiteLayout(visibleSites, pendingSiteJump);
  const { getSiteCardMeasureRef, masonryColumns } = layout;
  const actions = useSiteActions(layout.setExpandedSiteIds);
  const { selectedSiteIds } = actions;

  const selectedSiteTags = useMemo(() => {
    const tags = new Set<string>();
    for (const site of sites ?? []) {
      if (!selectedSiteIds.includes(site.id)) continue;
      for (const tag of site.tags) {
        tags.add(tag);
      }
    }
    return Array.from(tags);
  }, [sites, selectedSiteIds]);

  function openCreateSiteDialog() {
    setEditingSite(null);
    setSiteDialogOpen(true);
  }

  function openEditSiteDialog(site: SiteRecord) {
    setEditingSite(site);
    setSiteDialogOpen(true);
  }

  function closeSiteDialog(open: boolean) {
    setSiteDialogOpen(open);
    if (!open) {
      setEditingSite(null);
    }
  }

  function openCreateAccountDialog(site: SiteRecord) {
    setAccountSite(site);
    setEditingAccount(null);
    setAccountDialogOpen(true);
  }

  function openEditAccountDialog(site: SiteRecord, account: SiteAccount) {
    setAccountSite(site);
    setEditingAccount(account);
    setAccountDialogOpen(true);
  }

  function closeAccountDialog(open: boolean) {
    setAccountDialogOpen(open);
    if (!open) {
      setAccountSite(null);
      setEditingAccount(null);
    }
  }

  function openManualSyncDialog(site: SiteRecord, account: SiteAccount) {
    setManualSyncSite(site);
    setManualSyncAccount(account);
    setManualSyncDialogOpen(true);
  }

  function closeManualSyncDialog(open: boolean) {
    setManualSyncDialogOpen(open);
    if (!open) {
      setManualSyncSite(null);
      setManualSyncAccount(null);
    }
  }

  useEffect(() => {
    setSiteHandlers({
      openCreateDialog: () => {
        setEditingSite(null);
        setSiteDialogOpen(true);
      },
      openImportDialog: () => setImportDialogOpen(true),
      openArchivedDialog: () => setArchivedDialogOpen(true),
      syncAll: () => {
        syncAllSites.mutate(undefined, {
          onSuccess: () => toast.success("已触发后台全量同步，页面会自动刷新"),
          onError: (error) => toast.error(getSiteErrorMessage(locale, error, t)),
        });
      },
      checkinAll: () => {
        checkinAllSites.mutate(undefined, {
          onSuccess: () => toast.success("已触发后台全量签到，页面会自动刷新"),
          onError: (error) => toast.error(getSiteErrorMessage(locale, error, t)),
        });
      },
    });

    return () => {
      resetSiteHandlers();
    };
  }, [setSiteHandlers, resetSiteHandlers, syncAllSites, checkinAllSites, locale, t]);

  const editors = {
    openEditSiteDialog,
    openCreateAccountDialog,
    openEditAccountDialog,
    openManualSyncDialog,
  };

  return (
    <div className="page-scroll-area">
      <PageWrapper className="space-y-4" animateChildren={false}>
        <CheckinPanel
          sites={sites}
          inventory={inventory}
          statusDayKey={statusDayKey}
          visibleSiteCount={visibleSites.length}
          visibleAccountCount={visibleAccountCount}
          searchTerm={searchTerm.trim()}
          hasActiveFilters={hasActiveFilters}
          onClearFilters={clearFilters}
          activeFilterStatuses={checkinFilterStatuses}
          onFilterChange={handleCheckinFilterChange}
          allTags={allTags}
          activeTags={tagFilters}
          onTagFilterChange={handleTagFilterChange}
        />

        <SiteBatchBar
          actions={actions}
          visibleSites={visibleSites}
          onEdit={() => setBatchEditOpen(true)}
        />

        {error ? (
          <section className="page-card border-destructive/30 bg-destructive/5 p-6 text-sm text-destructive">
            站点列表加载失败：{getSiteErrorMessage(locale, error, t)}
          </section>
        ) : null}

        {isLoading ? (
          <section className="page-card p-6 text-sm text-muted-foreground">
            正在加载站点信息...
          </section>
        ) : null}

        {!isLoading && !error && (!sites || sites.length === 0) ? (
          <section className="page-empty-state p-10 text-foreground">
            <CircleAlert className="mx-auto size-8 text-muted-foreground" />
            <div className="mt-4 text-lg font-semibold">还没有站点</div>
            <p className="mt-2 text-sm text-muted-foreground">
              先新增一个站点，再为它配置账号，后续即可自动同步分组、模型和托管渠道。
            </p>
            <Button onClick={openCreateSiteDialog} className="mt-5 rounded-xl">
              <Plus className="size-4" />
              新增第一个站点
            </Button>
          </section>
        ) : null}

        {!isLoading && !error && sites && sites.length > 0 && visibleSites.length === 0 ? (
          <section className="page-empty-state p-10 text-foreground">
            <CircleAlert className="mx-auto size-8 text-muted-foreground" />
            <div className="mt-4 text-lg font-semibold">没有匹配的站点</div>
            <p className="mt-2 text-sm text-muted-foreground">
              当前搜索和筛选条件没有命中任何站点或账号。
            </p>
            <Button
              type="button"
              variant="outline"
              className="mt-5 rounded-xl"
              onClick={clearFilters}
            >
              <FilterX className="size-4" />
              清空筛选
            </Button>
          </section>
        ) : null}

        {visibleSites.length > 0 ? (
          <>
            <div className="space-y-4 md:hidden">
              {visibleSites.map((item) => (
                <div key={item.site.id} ref={getSiteCardMeasureRef(item.site.id)}>
                  <SiteCard
                    item={item}
                    actions={actions}
                    editors={editors}
                    layout={layout}
                    tagFilters={tagFilters}
                    handleTagFilterChange={handleTagFilterChange}
                  />
                </div>
              ))}
            </div>
            <div className="hidden items-start gap-4 md:grid md:grid-cols-2">
              <div className="space-y-4">
                {masonryColumns[0].map((item) => (
                  <div key={item.site.id} ref={getSiteCardMeasureRef(item.site.id)}>
                    <SiteCard
                      item={item}
                      actions={actions}
                      editors={editors}
                      layout={layout}
                      tagFilters={tagFilters}
                      handleTagFilterChange={handleTagFilterChange}
                    />
                  </div>
                ))}
              </div>
              <div className="space-y-4">
                {masonryColumns[1].map((item) => (
                  <div key={item.site.id} ref={getSiteCardMeasureRef(item.site.id)}>
                    <SiteCard
                      item={item}
                      actions={actions}
                      editors={editors}
                      layout={layout}
                      tagFilters={tagFilters}
                      handleTagFilterChange={handleTagFilterChange}
                    />
                  </div>
                ))}
              </div>
            </div>
          </>
        ) : null}
      </PageWrapper>

      <SiteEditDialog
        key={editingSite ? `edit-site-${editingSite.id}` : "create-site"}
        open={siteDialogOpen}
        onOpenChange={closeSiteDialog}
        site={editingSite}
        onCreated={(createdSite) => openCreateAccountDialog(createdSite)}
        allTags={allTagNames}
      />

      <BatchEditDialog
        open={batchEditOpen}
        onOpenChange={setBatchEditOpen}
        selectedSiteIds={selectedSiteIds}
        allTagNames={allTagNames}
        selectedSiteTags={selectedSiteTags}
      />

      <AccountEditDialog
        key={
          editingAccount
            ? `edit-site-account-${editingAccount.id}`
            : accountSite
              ? `create-site-account-${accountSite.id}`
              : "site-account"
        }
        open={accountDialogOpen}
        onOpenChange={closeAccountDialog}
        site={accountSite}
        account={editingAccount}
      />

      <ManualSyncDialog
        key={manualSyncAccount ? `manual-sync-${manualSyncAccount.id}` : "manual-sync"}
        open={manualSyncDialogOpen}
        onOpenChange={closeManualSyncDialog}
        site={manualSyncSite}
        account={manualSyncAccount}
      />

      <SiteImportDialog open={importDialogOpen} onOpenChange={setImportDialogOpen} />

      <ArchivedSitesDialog open={archivedDialogOpen} onOpenChange={setArchivedDialogOpen} />

      <SiteDeleteDialog actions={actions} />
    </div>
  );
}
