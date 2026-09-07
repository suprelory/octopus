import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../client";
import { logger } from "@/lib/logger";
import { CustomHeader } from './site-types';
import { invalidateSiteQueries } from './site-query';

export function useSyncAllSites() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => apiClient.post<null>("/api/v1/site/sync-all", {}),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点批量同步失败:", error),
  });
}

export function useCheckinAllSites() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      apiClient.post<null>("/api/v1/site/checkin-all", {}),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点批量签到失败:", error),
  });
}

export function useSiteLastSyncTime() {
  return useQuery({
    queryKey: ["sites", "last-sync-time"],
    queryFn: async () => apiClient.get<string>("/api/v1/site/last-sync-time"),
    refetchInterval: 30000,
  });
}

export function useSiteLastCheckinTime() {
  return useQuery({
    queryKey: ["sites", "last-checkin-time"],
    queryFn: async () =>
      apiClient.get<string>("/api/v1/site/last-checkin-time"),
    refetchInterval: 30000,
  });
}

export function useSiteBatchAction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: { ids: number[]; action: string }) =>
      apiClient.post<{
        success_ids: number[];
        failed_items: Array<{ id: number; message: string }>;
      }>("/api/v1/site/batch", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("批量操作失败:", error),
  });
}

export function useSiteBatchEdit() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: {
      ids: number[];
      add_tags: string[];
      remove_tags: string[];
      upserts: CustomHeader[];
      delete_keys: string[];
    }) =>
      apiClient.post<{
        success_ids: number[];
        failed_items: Array<{ id: number; message: string }>;
      }>("/api/v1/site/batch/edit", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("批量编辑失败:", error),
  });
}
