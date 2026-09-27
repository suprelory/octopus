import { useQuery } from '@tanstack/react-query';
import { apiClient } from '../client';

export type SiteCheckinLog = {
  id: number;
  site_id: number;
  account_id: number;
  site_name: string;
  account_name: string;
  platform: string;
  source: string;
  status: 'success' | 'failed' | 'skipped';
  reason: string;
  message: string;
  reward: string;
  duration_ms: number;
  started_at: string;
  finished_at: string;
};

export type SiteCheckinLogPage = {
  items: SiteCheckinLog[];
  next_before_id?: number;
};

export function useSiteCheckinLogs(filters: Record<string, string | number>, enabled = true) {
  return useQuery({
    queryKey: ['sites', 'checkin-logs', filters],
    queryFn: () => apiClient.get<SiteCheckinLogPage>('/api/v1/site/checkin-logs', filters),
    enabled,
    refetchInterval: enabled ? 15000 : false,
  });
}
