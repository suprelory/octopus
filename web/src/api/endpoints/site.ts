import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../client";
import { logger } from "@/lib/logger";
import type { Site, SiteServer } from './site-types';
import { normalizeSiteServerList, invalidateSiteQueries } from './site-query';

export function useSiteList() {
  return useQuery({
    queryKey: ["sites", "list"],
    queryFn: async () => apiClient.get<SiteServer[]>("/api/v1/site/list"),
    select: normalizeSiteServerList,
    refetchInterval: 30000,
  });
}

export function useArchivedSiteList(enabled = false) {
  return useQuery({
    queryKey: ["sites", "archived"],
    queryFn: async () => apiClient.get<SiteServer[]>("/api/v1/site/archived"),
    select: normalizeSiteServerList,
    enabled,
  });
}

export function useCreateSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      data: Omit<Site, "id" | "accounts" | "archived" | "archived_at">,
    ) => apiClient.post<Site>("/api/v1/site/create", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点创建失败:", error),
  });
}

export function useUpdateSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      data: Partial<Omit<Site, "accounts">> & { id: number },
    ) => apiClient.post<Site>("/api/v1/site/update", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点更新失败:", error),
  });
}

export function useEnableSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: { id: number; enabled: boolean }) =>
      apiClient.post<null>("/api/v1/site/enable", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点状态更新失败:", error),
  });
}

export function useDeleteSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) =>
      apiClient.delete<null>(`/api/v1/site/delete/${id}`),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点删除失败:", error),
  });
}

export function useArchiveSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) =>
      apiClient.post<null>(`/api/v1/site/archive/${id}`),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点归档失败:", error),
  });
}

export function useRestoreSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) =>
      apiClient.post<null>(`/api/v1/site/restore/${id}`),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点恢复失败:", error),
  });
}

export function useDetectSitePlatform() {
  return useMutation({
    mutationFn: async (url: string) =>
      apiClient.post<{ platform: string; default_route_type?: string }>("/api/v1/site/detect", { url }),
    onError: (error) => logger.error("平台检测失败:", error),
  });
}

export function useSiteAvailableModels(siteId: number | null) {
  return useQuery({
    queryKey: ["sites", "available-models", siteId],
    queryFn: async () =>
      apiClient.get<{ site_id: number; models: string[] }>(
        `/api/v1/site/${siteId}/available-models`,
      ),
    enabled: siteId != null && siteId > 0,
  });
}

export { SitePlatform, SiteCredentialType } from './site-types';
export type {
  CustomHeader,
  SiteRouteBaseURL,
  SiteToken,
  SiteUserGroup,
  SiteModel,
  SiteChannelBinding,
  SiteAccount,
  Site,
  SiteSyncResult,
  SiteManualSyncMode,
  SiteManualSyncFormat,
  SiteManualSyncRequest,
  SiteManualSyncPreviewGroup,
  SiteManualSyncPreview,
  SiteManualSyncApplyResult,
  SiteCheckinResult,
  AllAPIHubImportResult,
  MetAPIImportResult,
} from './site-types';
export {
  useCreateSiteAccount,
  useUpdateSiteAccount,
  useEnableSiteAccount,
  useDeleteSiteAccount,
  useSyncSiteAccount,
  usePreviewManualSiteSync,
  useApplyManualSiteSync,
  useCheckinSiteAccount,
} from './site-account';
export {
  useSyncAllSites,
  useCheckinAllSites,
  useSiteLastSyncTime,
  useSiteLastCheckinTime,
  useSiteBatchAction,
  useSiteBatchEdit,
} from './site-batch';
export { useImportAllAPIHub, useImportMetAPI } from './site-import';
