'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Trash2, X, Info, Pencil, Share2 } from 'lucide-react';
import { type APIKey } from '@/api/endpoints/apikey';
import { OVERLAY_ENTRANCE } from '@/lib/animations/css-entrances';
import { cn } from '@/lib/utils';
import { CopyIconButton } from '@/components/common/CopyButton';

export function APIKeyKeyItem({
    apiKey,
    onViewStats,
    onEdit,
    onDelete,
    onExport,
    isDeleting,
}: {
    apiKey: APIKey;
    onViewStats: () => void;
    onEdit: () => void;
    onDelete: () => void;
    onExport?: () => void;
    isDeleting: boolean;
}) {
    const t = useTranslations('setting');
    const [confirmDelete, setConfirmDelete] = useState(false);

    return (
        <div className={cn('group relative flex items-center justify-between gap-3 p-3 rounded-xl bg-muted/50 overflow-hidden origin-top', OVERLAY_ENTRANCE)}>
            <span className="text-sm font-medium truncate">{apiKey.name}</span>

            <div className="flex items-center gap-1.5">
                <button
                    type="button"
                    onClick={onViewStats}
                    className="flex size-8 items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground active:scale-95"
                    title="Stats"
                >
                    <Info className="size-4" />
                </button>
                <button
                    type="button"
                    onClick={onEdit}
                    className="flex size-8 items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground active:scale-95"
                    title="Edit"
                >
                    <Pencil className="size-4" />
                </button>
                {onExport && (
                    <button
                        type="button"
                        onClick={onExport}
                        className="flex size-8 items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground active:scale-95"
                        title="Export"
                    >
                        <Share2 className="size-4" />
                    </button>
                )}
                <CopyIconButton
                    text={apiKey.api_key}
                    className="flex size-8 items-center justify-center rounded-lg bg-primary/10 text-primary transition-all hover:bg-primary hover:text-primary-foreground active:scale-95"
                    copyIconClassName="size-4"
                    checkIconClassName="size-4"
                />

                {!confirmDelete && (
                    <button
                        type="button"
                        onClick={() => setConfirmDelete(true)}
                        className="flex size-8 items-center justify-center rounded-lg bg-destructive/10 text-destructive transition-colors hover:bg-destructive hover:text-destructive-foreground"
                    >
                        <Trash2 className="size-4" />
                    </button>
                )}
            </div>

            {confirmDelete && (
                <div className={cn('absolute inset-0 flex items-center justify-center gap-2 bg-destructive p-3 rounded-xl', OVERLAY_ENTRANCE)}>
                    <button
                        type="button"
                        onClick={() => setConfirmDelete(false)}
                        className="flex size-8 items-center justify-center rounded-lg bg-destructive-foreground/20 text-destructive-foreground transition-all hover:bg-destructive-foreground/30 active:scale-95"
                    >
                        <X className="size-4" />
                    </button>
                    <button
                        type="button"
                        onClick={onDelete}
                        disabled={isDeleting}
                        className="flex-1 h-8 flex items-center justify-center gap-1.5 rounded-lg bg-destructive-foreground text-destructive text-sm font-medium transition-all hover:bg-destructive-foreground/90 active:scale-[0.98] disabled:opacity-50"
                    >
                        <Trash2 className="size-3.5" />
                        {isDeleting ? '...' : t('apiKey.form.confirm')}
                    </button>
                </div>
            )}
        </div>
    );
}
