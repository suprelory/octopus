import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../client";
import { logger } from "@/lib/logger";
import {
  SiteAccount,
  SiteSyncResult,
  SiteManualSyncRequest,
  SiteManualSyncPreview,
  SiteManualSyncApplyResult,
  SiteCheckinResult,
} from './site-types';
import { invalidateSiteQueries } from './site-query';

export function useCreateSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      data: Omit<
        SiteAccount,
        | "id"
        | "tokens"
        | "user_groups"
        | "models"
        | "channel_bindings"
        | "last_sync_at"
        | "last_checkin_at"
        | "last_checkin_success_at"
        | "checkin_failure_count"
        | "last_sync_status"
        | "last_checkin_status"
        | "last_sync_message"
        | "last_checkin_message"
        | "balance"
        | "balance_used"
        | "today_income"
      >,
    ) => apiClient.post<SiteAccount>("/api/v1/site/account/create", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号创建失败:", error),
  });
}

export function useUpdateSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      data: Partial<
        Omit<
          SiteAccount,
          "tokens" | "user_groups" | "models" | "channel_bindings"
        >
      > & { id: number },
    ) => apiClient.post<SiteAccount>("/api/v1/site/account/update", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号更新失败:", error),
  });
}

export function useEnableSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: { id: number; enabled: boolean }) =>
      apiClient.post<null>("/api/v1/site/account/enable", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号状态更新失败:", error),
  });
}

export function useDeleteSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) =>
      apiClient.delete<null>(`/api/v1/site/account/delete/${id}`),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号删除失败:", error),
  });
}

export function useSyncSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) =>
      apiClient.post<SiteSyncResult>(`/api/v1/site/account/sync/${id}`, {}),
    onSettled: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号同步失败:", error),
  });
}

export function usePreviewManualSiteSync() {
  return useMutation({
    mutationFn: async (data: {
      id: number;
      request: SiteManualSyncRequest;
    }) =>
      apiClient.post<SiteManualSyncPreview>(
        `/api/v1/site/account/manual-sync/preview/${data.id}`,
        data.request,
      ),
    onError: (error) => logger.error("手动同步数据预览失败:", error),
  });
}

export function useApplyManualSiteSync() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: {
      id: number;
      request: SiteManualSyncRequest;
    }) =>
      apiClient.post<SiteManualSyncApplyResult>(
        `/api/v1/site/account/manual-sync/${data.id}`,
        data.request,
      ),
    onSettled: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("手动同步数据应用失败:", error),
  });
}

export function useCheckinSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) =>
      apiClient.post<SiteCheckinResult>(
        `/api/v1/site/account/checkin/${id}`,
        {},
      ),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号签到失败:", error),
  });
}
