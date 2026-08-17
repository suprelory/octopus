'use client';

import { useMemo } from 'react';
import { PackageSearch } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useModelList } from '@/api/endpoints/model';
import { ModelItem } from './Item';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';

export function Model() {
    const t = useTranslations('model');
    const { data: models, isLoading, isError, refetch } = useModelList();
    const pageKey = 'model' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const layout = useToolbarViewOptionsStore((s) => s.getLayout(pageKey));
    const sortOrder = useToolbarViewOptionsStore((s) => s.getSortOrder(pageKey));

    const sortedModels = useMemo(() => {
        if (!models) return [];
        return [...models].sort((a, b) =>
            sortOrder === 'asc' ? a.name.localeCompare(b.name) : b.name.localeCompare(a.name)
        );
    }, [models, sortOrder]);

    const visibleModels = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        return !term ? sortedModels : sortedModels.filter((m) => m.name.toLowerCase().includes(term));
    }, [sortedModels, searchTerm]);

    if (isLoading) {
        return (
            <div className="flex h-full min-h-0 items-center justify-center">
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <span className="size-2 animate-pulse rounded-full bg-primary" />
                    {t('state.loading')}
                </div>
            </div>
        );
    }

    if (isError) {
        return (
            <div className="flex h-full min-h-0 items-center justify-center px-3">
                <div className="page-card w-full max-w-md p-5 text-center">
                    <p className="text-sm font-semibold">{t('state.errorTitle')}</p>
                    <p className="mt-1 text-sm text-muted-foreground">{t('state.errorDescription')}</p>
                    <button
                        type="button"
                        onClick={() => refetch()}
                        className="mt-4 h-9 rounded-lg border border-border bg-card px-4 text-sm font-medium transition-colors hover:bg-muted"
                    >
                        {t('state.retry')}
                    </button>
                </div>
            </div>
        );
    }

    if (visibleModels.length === 0) {
        return (
            <div className="page-scroll-area px-3 pt-4 md:px-4 md:pt-6">
                <section className="page-empty-state mx-auto flex w-full max-w-3xl flex-col items-center py-12 text-card-foreground">
                    <div className="grid size-16 place-items-center rounded-full border border-border/50 bg-muted/30 text-primary">
                        <PackageSearch className="size-7" />
                    </div>
                    <h2 className="mt-5 text-xl font-semibold">{t('state.emptyTitle')}</h2>
                    <p className="mt-2 max-w-xl text-sm leading-6 text-muted-foreground">{t('state.emptyDescription')}</p>
                </section>
            </div>
        );
    }

    return (
        <VirtualizedGrid
            items={visibleModels}
            layout={layout}
            columns={{ default: 1, md: 2, lg: 3 }}
            estimateItemHeight={112}
            getItemKey={(model) => `model-${model.name}`}
            renderItem={(model) => <ModelItem model={model} layout={layout} />}
        />
    );
}
