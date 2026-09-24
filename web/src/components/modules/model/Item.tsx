'use client';

import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Pencil, Trash2, ArrowDownToLine, ArrowUpFromLine } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useUpdateModel, useDeleteModel, type LLMInfo } from '@/api/endpoints/model';
import { getModelIcon } from '@/lib/model-icons';
import { toast } from '@/components/common/Toast';
import { ModelDeleteOverlay, ModelEditOverlay } from './ItemOverlays';
import { useIsClient } from '@/hooks/useIsClient';
import { cn } from '@/lib/utils';
import { createPortal } from 'react-dom';

interface ModelItemProps {
    model: LLMInfo;
    layout?: 'grid' | 'list';
}

const priceFormat = new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 8 });

export const ModelItem = memo(function ModelItem({ model, layout = 'grid' }: ModelItemProps) {
    const t = useTranslations('model');
    const tOverview = useTranslations('workspace.model');
    const isClient = useIsClient();
    const isListLayout = layout === 'list';
    const [isEditOpen, setIsEditOpen] = useState(false);
    const [confirmDelete, setConfirmDelete] = useState(false);
    const [overlayRect, setOverlayRect] = useState<{ top: number; left: number; width: number } | null>(null);
    const cardRef = useRef<HTMLElement | null>(null);
    const editButtonRef = useRef<HTMLButtonElement | null>(null);
    const editOverlayRef = useRef<HTMLDivElement | null>(null);
    const [editValues, setEditValues] = useState(() => ({
        input: model.input.toString(),
        output: model.output.toString(),
        cache_read: model.cache_read.toString(),
        cache_write: model.cache_write.toString(),
    }));

    const updateModel = useUpdateModel();
    const deleteModel = useDeleteModel();

    const { Avatar: ModelAvatar, color: brandColor } = useMemo(() => getModelIcon(model.name), [model.name]);

    const updateOverlayRect = useCallback(() => {
        const card = cardRef.current;
        if (!card) return;
        const rect = card.getBoundingClientRect();
        setOverlayRect((prev) => {
            if (prev && prev.top === rect.top && prev.left === rect.left && prev.width === rect.width) {
                return prev;
            }
            return { top: rect.top, left: rect.left, width: rect.width };
        });
    }, []);

    const closeEdit = useCallback(() => {
        setIsEditOpen(false);
    }, []);

    const handleEditClick = () => {
        setConfirmDelete(false);
        setEditValues({
            input: model.input.toString(),
            output: model.output.toString(),
            cache_read: model.cache_read.toString(),
            cache_write: model.cache_write.toString(),
        });
        // Measure before opening so the overlay is anchored on its first paint.
        updateOverlayRect();
        setIsEditOpen(true);
    };

    const handleCancelEdit = () => {
        closeEdit();
    };

    const handleSaveEdit = () => {
        updateModel.mutate({
            name: model.name,
            input: parseFloat(editValues.input) || 0,
            output: parseFloat(editValues.output) || 0,
            cache_read: parseFloat(editValues.cache_read) || 0,
            cache_write: parseFloat(editValues.cache_write) || 0,
        }, {
            onSuccess: () => {
                closeEdit();
                toast.success(t('toast.updated'));
            },
            onError: (error) => {
                toast.error(t('toast.updateFailed'), { description: error.message });
            }
        });
    };

    const handleDeleteClick = () => {
        closeEdit();
        setConfirmDelete(true);
    };
    const handleCancelDelete = () => setConfirmDelete(false);
    const handleConfirmDelete = () => {
        deleteModel.mutate(model.name, {
            onSuccess: () => {
                setConfirmDelete(false);
                toast.success(t('toast.deleted'));
            },
            onError: (error) => {
                setConfirmDelete(false);
                toast.error(t('toast.deleteFailed'), { description: error.message });
            }
        });
    };

    useEffect(() => {
        if (!isEditOpen) return;

        const handlePointerDown = (event: PointerEvent) => {
            const target = event.target as Node | null;
            if (!target) return;
            if (editOverlayRef.current?.contains(target)) return;
            if (editButtonRef.current?.contains(target)) return;
            closeEdit();
        };

        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') closeEdit();
        };

        updateOverlayRect();
        window.addEventListener('resize', updateOverlayRect);
        window.addEventListener('scroll', updateOverlayRect, true);
        document.addEventListener('pointerdown', handlePointerDown);
        document.addEventListener('keydown', handleKeyDown);

        return () => {
            window.removeEventListener('resize', updateOverlayRect);
            window.removeEventListener('scroll', updateOverlayRect, true);
            document.removeEventListener('pointerdown', handlePointerDown);
            document.removeEventListener('keydown', handleKeyDown);
        };
    }, [isEditOpen, updateOverlayRect, closeEdit]);

    return (
        <article
            ref={cardRef}
            className={cn(
                'page-card group relative flex h-full flex-col gap-4 p-5 transition-colors hover:border-primary/35',
                isListLayout && 'md:flex-row md:items-center md:gap-6',
                (isEditOpen || confirmDelete) && 'z-50'
            )}
        >
            <header className={cn('flex min-w-0 items-center gap-3', isListLayout && 'md:flex-1')}>
                <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-muted/50"><ModelAvatar size={32} /></span>
                <div className="min-w-0 flex-1">
                    <h3 className="truncate text-base font-semibold tracking-tight" title={model.name}>{model.name}</h3>
                    <p className="mt-1 text-xs text-muted-foreground">{tOverview('unit')}</p>
                </div>
            </header>

            <dl className={cn('grid grid-cols-2 gap-x-5 gap-y-3 rounded-xl bg-muted/35 p-3.5', isListLayout && 'md:flex-1 lg:grid-cols-4')}>
                {([
                    ['input', model.input, ArrowDownToLine],
                    ['output', model.output, ArrowUpFromLine],
                    ['cacheRead', model.cache_read, ArrowDownToLine],
                    ['cacheWrite', model.cache_write, ArrowUpFromLine],
                ] as const).map(([label, value, Icon]) => <div key={label} className="min-w-0">
                    <dt className="flex items-center gap-1 text-[11px] text-muted-foreground"><Icon aria-hidden className="size-3" />{t(`overlay.${label}`)}</dt>
                    <dd className="mt-1 break-all text-sm font-semibold tabular-nums" title={String(value)}><span className="mr-0.5 font-normal text-muted-foreground">$</span>{priceFormat.format(value)}</dd>
                </div>)}
            </dl>

            <div className={cn('flex items-center justify-end gap-1 border-t border-border/60 pt-3', isListLayout && 'md:shrink-0 md:border-t-0 md:pt-0', (isEditOpen || confirmDelete) && 'invisible pointer-events-none')}>
                <button
                    ref={editButtonRef}
                    type="button"
                    onClick={handleEditClick}
                    disabled={isEditOpen || confirmDelete}
                    className="flex h-8 items-center gap-1.5 rounded-lg px-2 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50"
                    title={t('card.edit')}
                >
                    <Pencil className="size-4" />
                    {t('card.edit')}
                </button>

                <button
                    type="button"
                    onClick={handleDeleteClick}
                    disabled={isEditOpen || confirmDelete}
                    className="flex size-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50"
                    title={t('card.delete')}
                >
                    <Trash2 className="size-4" />
                </button>
            </div>

            {confirmDelete && (
                <ModelDeleteOverlay
                    isPending={deleteModel.isPending}
                    onCancel={handleCancelDelete}
                    onConfirm={handleConfirmDelete}
                />
            )}

            {isEditOpen && overlayRect && isClient
                ? createPortal(
                    <div
                        ref={editOverlayRef}
                        className="fixed z-[90]"
                        style={{
                            top: `${overlayRect.top}px`,
                            left: `${overlayRect.left}px`,
                            width: `${overlayRect.width}px`,
                        }}
                    >
                        <div className="relative">
                            <ModelEditOverlay
                                modelName={model.name}
                                brandColor={brandColor}
                                editValues={editValues}
                                isPending={updateModel.isPending}
                                onChange={setEditValues}
                                onCancel={handleCancelEdit}
                                onSave={handleSaveEdit}
                            />
                        </div>
                    </div>,
                    document.body
                )
                : null}
        </article>
    );
});
