'use client';

import { useCallback, useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { KeyRound, Plus, Loader, X, Maximize2 } from 'lucide-react';
import {
    MorphingDialog,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogTrigger,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { useAPIKeyList, useCreateAPIKey, useUpdateAPIKey, useDeleteAPIKey, type APIKey } from '@/api/endpoints/apikey';
import { useSettingValue, SettingKey } from '@/api/endpoints/setting';
import { APIKeyExportOverlay } from './APIKeyExport';
import { toast } from '@/components/common/Toast';
import type { ApiError } from '@/api/types';
import { APIKeyFormOverlay } from './APIKeyForm';
import { APIKeyStatsCard } from './APIKeyStatsCard';
import { APIKeyKeyItem } from './APIKeyItem';

function APIKeyPanelBase({
    containerClassName,
    listClassName,
    renderHeaderExtra,
}: {
    containerClassName: string;
    listClassName: string;
    renderHeaderExtra?: (ctx: {
        disabled: boolean;
        onCloseAllOverlays: () => void;
    }) => React.ReactNode;
}) {
    const t = useTranslations('setting');
    const { data: apiKeys, isLoading: apiKeysLoading, error: apiKeysError } = useAPIKeyList();
    const createAPIKey = useCreateAPIKey();
    const updateAPIKey = useUpdateAPIKey();
    const deleteAPIKey = useDeleteAPIKey();
    const { value: apiBaseUrl } = useSettingValue(SettingKey.ApiBaseUrl);
    const canExport = apiBaseUrl.trim() !== '';

    const [isAdding, setIsAdding] = useState(false);
    const [viewingStats, setViewingStats] = useState<APIKey | null>(null);
    const [editingKey, setEditingKey] = useState<APIKey | null>(null);
    const [exportingKey, setExportingKey] = useState<APIKey | null>(null);
    const [deletingId, setDeletingId] = useState<number | null>(null);

    const sortedApiKeys = useMemo(() => {
        if (!apiKeys) return [];
        return [...apiKeys].sort((a, b) => a.id - b.id);
    }, [apiKeys]);

    const handleDelete = useCallback((id: number) => {
        setDeletingId(id);
        deleteAPIKey.mutate(id, {
            onSuccess: () => {
                toast.success(t('apiKey.toast.deleteSuccess'));
            },
            onError: (error) => {
                const msg = (error as unknown as ApiError)?.message;
                toast.error(t('apiKey.toast.deleteError'), { description: msg });
            },
            onSettled: () => setDeletingId((cur) => (cur === id ? null : cur)),
        });
    }, [deleteAPIKey, t]);

    const closeAllOverlays = useCallback(() => {
        setIsAdding(false);
        setViewingStats(null);
        setEditingKey(null);
        setExportingKey(null);
    }, []);

    const disabledHeaderActions = createAPIKey.isPending || isAdding || !!viewingStats || !!editingKey || !!exportingKey;

    const handleCreate = useCallback((data: Omit<APIKey, 'id' | 'api_key'>) => {
        createAPIKey.mutate(data, {
            onSuccess: () => {
                toast.success(t('apiKey.toast.createSuccess'));
                setIsAdding(false);
            },
            onError: (error) => {
                const msg = (error as unknown as ApiError)?.message;
                toast.error(t('apiKey.toast.createError'), { description: msg });
            },
        });
    }, [createAPIKey, t]);

    const handleUpdate = useCallback((apiKey: APIKey, data: Omit<APIKey, 'id' | 'api_key'>) => {
        updateAPIKey.mutate({ id: apiKey.id, ...data }, {
            onSuccess: () => {
                toast.success(t('apiKey.toast.updateSuccess'));
                setEditingKey(null);
            },
            onError: (error) => {
                const msg = (error as unknown as ApiError)?.message;
                toast.error(t('apiKey.toast.updateError'), { description: msg });
            },
        });
    }, [t, updateAPIKey]);

    return (
        <div className={containerClassName}>
            <div className="flex items-center justify-between gap-3">
                <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                    <KeyRound className="h-5 w-5" />
                    {t('apiKey.title')}
                </h2>
                <div className="flex items-center gap-2">
                    <button
                        type="button"
                        onClick={() => setIsAdding(true)}
                        disabled={disabledHeaderActions}
                        className="h-9 w-9 flex items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted disabled:opacity-50"
                        title={t('apiKey.add')}
                    >
                        <Plus className="size-4" />
                    </button>
                    {renderHeaderExtra?.({ disabled: disabledHeaderActions, onCloseAllOverlays: closeAllOverlays })}
                </div>
            </div>

            {isAdding && (
                <APIKeyFormOverlay
                    isPending={createAPIKey.isPending}
                    submitLabel={t('apiKey.form.create')}
                    onSubmit={handleCreate}
                    onClose={() => setIsAdding(false)}
                />
            )}

            {viewingStats && (
                <APIKeyStatsCard apiKey={viewingStats} onClose={() => setViewingStats(null)} />
            )}

            {editingKey && (
                <APIKeyFormOverlay
                    apiKey={editingKey}
                    isPending={updateAPIKey.isPending}
                    submitLabel={t('apiKey.form.save')}
                    onSubmit={(data) => handleUpdate(editingKey, data)}
                    onClose={() => setEditingKey(null)}
                />
            )}

            {exportingKey && (
                <APIKeyExportOverlay
                    apiKey={exportingKey}
                    baseUrl={apiBaseUrl}
                    onClose={() => setExportingKey(null)}
                />
            )}

            <div className={listClassName}>
                {apiKeysLoading ? (
                    <div className="h-full flex items-center justify-center text-sm text-muted-foreground">
                        <Loader className="size-4 animate-spin" />
                    </div>
                ) : apiKeysError ? (
                    <div className="h-full flex items-center justify-center text-sm text-destructive">
                        {t('apiKey.loadFailed')}
                    </div>
                ) : apiKeys?.length === 0 ? (
                    <div className="h-full flex items-center justify-center text-sm text-muted-foreground">
                        {t('apiKey.empty')}
                    </div>
                ) : (
                    sortedApiKeys.map((apiKey) => (
                        <APIKeyKeyItem
                            key={apiKey.id}
                            apiKey={apiKey}
                            onViewStats={() => {
                                closeAllOverlays();
                                setViewingStats(apiKey);
                            }}
                            onEdit={() => {
                                closeAllOverlays();
                                setEditingKey(apiKey);
                            }}
                            onExport={canExport ? () => {
                                closeAllOverlays();
                                setExportingKey(apiKey);
                            } : undefined}
                            onDelete={() => handleDelete(apiKey.id)}
                            isDeleting={deleteAPIKey.isPending && deletingId === apiKey.id}
                        />
                    ))
                )}
            </div>
        </div>
    );
}

function APIKeyDialogPanel() {
    const { setIsOpen } = useMorphingDialog();
    return (
        <APIKeyPanelBase
            containerClassName="page-card relative w-screen max-w-full space-y-5 p-6 md:max-w-xl"
            listClassName="space-y-2 h-[calc(100vh-10rem)] overflow-y-auto"
            renderHeaderExtra={() => (
                <button
                    type="button"
                    onClick={() => setIsOpen(false)}
                    className="h-9 w-9 flex items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted"
                    title="Close"
                >
                    <X className="size-4" />
                </button>
            )}
        />
    );
}

export function SettingAPIKey() {
    return (
        <APIKeyPanelBase
            containerClassName="page-card relative space-y-5 p-6"
            listClassName="space-y-2 h-36 overflow-y-auto"
            renderHeaderExtra={() => (
                <MorphingDialog>
                    <MorphingDialogTrigger className="h-9 w-9 flex items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted">
                        <Maximize2 className="size-4" />
                    </MorphingDialogTrigger>
                    <MorphingDialogContainer>
                        <MorphingDialogContent className="relative">
                            <APIKeyDialogPanel />
                        </MorphingDialogContent>
                    </MorphingDialogContainer>
                </MorphingDialog>
            )}
        />
    );
}
