import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiRequest } from './client';

export type ProxyMode = 'direct' | 'system' | 'custom' | 'pool' | 'inherit';
export type ProxySelection = { proxy_mode: ProxyMode; proxy_config_id?: number | null; proxy_url?: string };
export type ProxyNode = {
    id: number; name: string; url: string; enabled: boolean; remark: string;
    revision: number; route_revision: number; has_auth: boolean; reference_count: number;
};
export type ProxyWrite = Pick<ProxyNode, 'name' | 'url' | 'enabled' | 'remark' | 'revision'> & { clear_auth?: boolean };
export type ProxyReference = { kind: 'channel' | 'checkin_site' | 'checkin_account' | 'checkin_notification'; id: string; name: string; site_id?: string; site_name?: string; inherited: boolean };
export type ProxyTestResult = { connected: boolean; http_status: number; duration_ms: number; message: string };
const path = '/api/v1/proxy-pool';

export function useProxyPool() {
    return useQuery({ queryKey: ['proxy-pool'], queryFn: ({ signal }) => apiRequest<ProxyNode[]>(path, { signal }), refetchInterval: 30_000 });
}

export function useProxyReferences(id: number | null) {
    return useQuery({ queryKey: ['proxy-references', id], queryFn: ({ signal }) => apiRequest<ProxyReference[]>(`${path}/${id}/references`, { signal }), enabled: id !== null });
}

export function useProxyMutation() {
    const client = useQueryClient();
    return useMutation({
        mutationFn: (input: { id?: number; value?: ProxyWrite; remove?: boolean }) => apiRequest<ProxyNode>(`${path}${input.id ? `/${input.id}` : ''}`, {
            method: input.remove ? 'DELETE' : input.id ? 'PATCH' : 'POST', body: input.value,
        }),
        onSuccess: () => {
            for (const key of ['proxy-pool', 'proxy-references', 'channels', 'checkin']) void client.invalidateQueries({ queryKey: [key] });
        },
    });
}

export function testProxy(input: { id?: number; url?: string; target_url: string }) {
    return apiRequest<ProxyTestResult>(`${path}/test`, { method: 'POST', body: input });
}

export function siteProxySelection(config: { proxy_mode?: ProxyMode; proxy_config_id?: number | null; proxy_url: string }): ProxySelection {
    return { proxy_mode: config.proxy_mode || (config.proxy_url ? 'custom' : 'direct'), proxy_config_id: config.proxy_config_id, proxy_url: config.proxy_url };
}
