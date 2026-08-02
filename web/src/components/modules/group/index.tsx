'use client';

import { useMemo } from 'react';
import { Layers3 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { GroupCard } from './Card';
import { useGroupList } from '@/api/endpoints/group';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';

export function Group() {
    const t = useTranslations('group');
    const { data: groups, isLoading, isError, refetch } = useGroupList();
    const pageKey = 'group' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const sortField = useToolbarViewOptionsStore((s) => s.getSortField(pageKey));
    const sortOrder = useToolbarViewOptionsStore((s) => s.getSortOrder(pageKey));

    const sortedGroups = useMemo(() => {
        if (!groups) return [];
        return [...groups].sort((a, b) => {
            // 置顶优先：pinned 组排在前面，组内按 pinned_at desc
            if (!!a.pinned !== !!b.pinned) return a.pinned ? -1 : 1;
            if (a.pinned && b.pinned) {
                const ta = a.pinned_at ? new Date(a.pinned_at).getTime() : 0;
                const tb = b.pinned_at ? new Date(b.pinned_at).getTime() : 0;
                if (ta !== tb) return tb - ta;
            }
            const diff = sortField === 'name'
                ? a.name.localeCompare(b.name)
                : (a.id || 0) - (b.id || 0);
            return sortOrder === 'asc' ? diff : -diff;
        });
    }, [groups, sortField, sortOrder]);

    const visibleGroups = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        return !term
            ? sortedGroups
            : sortedGroups.filter((group) =>
                group.name.toLowerCase().includes(term)
                || (group.items ?? []).some((item) => item.model_name.toLowerCase().includes(term))
            );
    }, [sortedGroups, searchTerm]);

    if (isLoading) {
        return (
            <div className="flex h-full min-h-0 items-center justify-center rounded-t-xl">
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <span className="size-2 animate-pulse rounded-full bg-primary" />
                    {t('state.loading')}
                </div>
            </div>
        );
    }

    if (isError) {
        return (
            <div className="flex h-full min-h-0 items-center justify-center rounded-t-xl px-3">
                <div className="w-full max-w-md rounded-xl border border-border bg-card p-5 text-center text-card-foreground">
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

    if (groups && groups.length === 0) {
        return (
            <div className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-xl px-3 py-4 md:px-4 md:py-6">
                <section className="mx-auto flex w-full max-w-3xl flex-col items-center rounded-xl border border-dashed border-border bg-card px-6 py-12 text-center text-card-foreground">
                    <div className="grid size-16 place-items-center rounded-full border border-border/50 bg-muted/30 text-primary">
                        <Layers3 className="size-7" />
                    </div>
                    <h2 className="mt-5 text-xl font-semibold tracking-tight">{t('emptyState.title')}</h2>
                    <p className="mt-2 max-w-xl text-sm leading-6 text-muted-foreground">{t('emptyState.description')}</p>
                </section>
            </div>
        );
    }

    return (
        <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-t-xl pb-3 md:pb-4">
            <section className="relative min-h-0 flex-1">
                {visibleGroups.length > 0 ? (
                    <VirtualizedGrid
                        items={visibleGroups}
                        columns={{ default: 1, sm: 2, md: 2, lg: 3 }}
                        estimateItemHeight={72}
                        gap={12}
                        getItemKey={(group, index) => group.id ?? `group-${index}`}
                        renderItem={(group) => <GroupCard group={group} />}
                    />
                ) : (
                    <div className="flex h-full items-center justify-center px-3">
                        <div className="rounded-xl border border-dashed border-border bg-card px-6 py-8 text-center text-sm text-muted-foreground">
                            {t('state.noResults')}
                        </div>
                    </div>
                )}
            </section>
        </div>
    );
}
