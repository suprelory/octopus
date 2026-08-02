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

interface LogUIState {
    refreshRequestId: number;
    isRefreshing: boolean;
    requestRefresh: () => void;
    setRefreshing: (value: boolean) => void;
}

export const useLogUIStore = create<LogUIState>((set) => ({
    refreshRequestId: 0,
    isRefreshing: false,
    requestRefresh: () => set((state) => ({ refreshRequestId: state.refreshRequestId + 1 })),
    setRefreshing: (value) => set({ isRefreshing: value }),
}));
