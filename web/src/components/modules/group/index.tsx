'use client';

import { useMemo } from 'react';
import { Layers3, SearchX } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { GroupCard } from './Card';
import { useGroupList } from '@/api/endpoints/group';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { PageOverview } from '@/components/common/PageOverview';
import { CardGridSkeleton, PageEmptyState } from '@/components/common/PageState';

export function Group() {
    const t = useTranslations('group');
    const tOverview = useTranslations('workspace');
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
        return <CardGridSkeleton label={t('state.loading')} />;
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

    const overview = <PageOverview className="mb-4" title={tOverview('group.title')} description={tOverview('group.description')} metrics={[
        { label: tOverview('total'), value: groups?.length ?? 0 },
        { label: tOverview('group.members'), value: (groups ?? []).reduce((count, group) => count + (group.items?.length ?? 0), 0), accent: true },
        { label: tOverview('shown'), value: visibleGroups.length },
    ]} />;

    return (
        <div className="flex h-full min-h-0 flex-col overflow-hidden">
            <section className="relative min-h-0 flex-1">
                {visibleGroups.length > 0 ? (
                    <VirtualizedGrid
                        items={visibleGroups}
                        header={overview}
                        columns={{ default: 1, sm: 2, md: 2, lg: 3 }}
                        estimateItemHeight={140}
                        gap={16}
                        getItemKey={(group, index) => group.id ?? `group-${index}`}
                        renderItem={(group) => <GroupCard group={group} />}
                    />
                ) : (
                    <div className="page-scroll-area">
                        {overview}
                        <PageEmptyState icon={searchTerm.trim() ? SearchX : Layers3}
                            title={searchTerm.trim() ? t('state.noResults') : t('emptyState.title')}
                            description={searchTerm.trim() ? tOverview('searchHint') : t('emptyState.description')} />
                    </div>
                )}
            </section>
        </div>
    );
}
