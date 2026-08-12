import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type LogFieldName =
    | 'endpointType'
    | 'channelName'
    | 'actualModel'
    | 'apiKeyName'
    | 'clientIP'
    | 'cost'
    | 'tps'
    | 'cacheHitRate'
    | 'reasoningEffort'
    | 'reasoningTokens';

export type LogFieldVisibility = Record<LogFieldName, boolean>;

export const DEFAULT_LOG_FIELD_VISIBILITY: LogFieldVisibility = {
    endpointType: true,
    channelName: true,
    actualModel: true,
    apiKeyName: true,
    clientIP: true,
    cost: true,
    tps: true,
    cacheHitRate: true,
    reasoningEffort: true,
    reasoningTokens: true,
};

type LogFieldVisibilityState = {
    visibility: LogFieldVisibility;
    toggleField: (field: LogFieldName) => void;
    resetFields: () => void;
};

export const useLogFieldVisibilityStore = create<LogFieldVisibilityState>()(
    persist(
        (set) => ({
            visibility: { ...DEFAULT_LOG_FIELD_VISIBILITY },
            toggleField: (field) => set((state) => ({
                visibility: { ...state.visibility, [field]: !state.visibility[field] },
            })),
            resetFields: () => set({ visibility: { ...DEFAULT_LOG_FIELD_VISIBILITY } }),
        }),
        { name: 'log-field-visibility-storage', partialize: (state) => ({ visibility: state.visibility }) },
    ),
);

export function useLogFieldVisibility() {
    return useLogFieldVisibilityStore((state) => state.visibility);
}

export const DEFAULT_LOG_PAGE_SIZE = 20;

interface LogUIState {
    refreshRequestId: number;
    isRefreshing: boolean;
    page: number;
    pageSize: number;
    /** 实时推送开关；仅在第 1 页且无筛选时才真正生效。 */
    liveEnabled: boolean;
    requestRefresh: () => void;
    setRefreshing: (value: boolean) => void;
    setPage: (value: number) => void;
    setPageSize: (value: number) => void;
    setLiveEnabled: (value: boolean) => void;
}

export const useLogUIStore = create<LogUIState>()(
    persist(
        (set) => ({
            refreshRequestId: 0,
            isRefreshing: false,
            page: 1,
            pageSize: DEFAULT_LOG_PAGE_SIZE,
            liveEnabled: true,
            requestRefresh: () => set((state) => ({ refreshRequestId: state.refreshRequestId + 1 })),
            setRefreshing: (value) => set({ isRefreshing: value }),
            setPage: (value) => set({ page: Math.max(1, Math.floor(value)) }),
            // 每页条数变化会让原页码失去意义，直接回到第 1 页。
            setPageSize: (value) => set({ pageSize: value, page: 1 }),
            setLiveEnabled: (value) => set({ liveEnabled: value }),
        }),
        {
            name: 'log-ui-storage',
            // 页码是会话内状态，只持久化用户偏好。
            partialize: (state) => ({ pageSize: state.pageSize, liveEnabled: state.liveEnabled }),
        },
    ),
);
