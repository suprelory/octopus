import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiRequest } from './client';

/**
 * 后端 /api/v1/update 返回的最新发布信息
 */
interface LatestInfo {
    tag_name: string;
    published_at: string;
    body: string;
    html_url: string;
    status: 'no_release' | 'development' | 'up_to_date' | 'unsupported_platform' | 'asset_missing' | 'update_available';
    update_available: boolean;
}

interface BuildInfo {
    version: string;
    commit: string;
    build_time: string;
    author: string;
    repository: string;
    repository_url: string;
}

/**
 * 获取最新发布信息 Hook
 * 
 * @example
 * const { data: latestInfo, isLoading, error } = useLatestInfo();
 * 
 * if (isLoading) return <Loading />;
 * if (error) return <Error message={error.message} />;
 * 
 * console.log('Latest tag:', latestInfo?.tag_name);
 */
export function useLatestInfo() {
    return useQuery({
        queryKey: ['update', 'latest'],
        queryFn: () => apiRequest<LatestInfo>('/api/v1/update'),
        refetchInterval: 3600000, // 1 小时
        refetchOnMount: 'always',
    });
}

/**
 * 获取后端构建信息；仓库链接与服务端更新源保持一致。
 *
 * 后端: GET /api/v1/update/metadata -> BuildInfo
 */
export function useBuildInfo() {
    return useQuery({
        queryKey: ['update', 'metadata'],
        queryFn: () => apiRequest<BuildInfo>('/api/v1/update/metadata'),
        refetchInterval: 3600000, // 1 小时
        refetchOnMount: 'always',
    });
}

/**
 * 执行更新 Hook
 * 
 * @example
 * const updateCore = useUpdateCore();
 * 
 * updateCore.mutate(latestInfo.tag_name, {
 *   onSuccess: () => {
 *     console.log('Update started successfully');
 *   },
 * });
 */
export function useUpdateCore() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (tagName: string) => apiRequest<string>('/api/v1/update', {
            method: 'POST',
            body: { tag_name: tagName },
        }),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['update', 'latest'] });
            queryClient.invalidateQueries({ queryKey: ['update', 'metadata'] });
        },
    });
}
