import { SiteAccount, Site as SiteRecord } from "@/api/endpoints/site";
import { type PendingJump, type SiteJumpTarget } from "@/stores/jump";

export type HealthTone = "default" | "danger" | "muted" | "warning";

export type SiteSummary = {
  accountCount: number;
  keyCount: number;
  modelCount: number;
  groupCount: number;
  balance: number;
  todayIncome: number;
  failedAccountCount: number;
  partialAccountCount: number;
  disabledAccountCount: number;
  enabledAccountCount: number;
  healthLabel: string;
  healthTone: HealthTone;
};

export type VisibleSite = {
  site: SiteRecord;
  summary: SiteSummary;
  visibleAccounts: SiteAccount[];
  forceExpanded: boolean;
  hasFilteredAccounts: boolean;
};

export type SitePendingJump = PendingJump & { target: SiteJumpTarget };

export type ImportSource = "all-api-hub" | "metapi";

export type SiteImportResult = {
  created_sites: number;
  reused_sites: number;
  created_accounts: number;
  updated_accounts: number;
  skipped_accounts: number;
  scheduled_sync_accounts?: number;
  warnings: string[];
  imported_tokens?: number;
  imported_groups?: number;
  imported_models?: number;
  disabled_models?: number;
};

export type SiteEditorActions = {
  openEditSiteDialog: (site: SiteRecord) => void;
  openCreateAccountDialog: (site: SiteRecord) => void;
  openEditAccountDialog: (site: SiteRecord, account: SiteAccount) => void;
  openManualSyncDialog: (site: SiteRecord, account: SiteAccount) => void;
};
