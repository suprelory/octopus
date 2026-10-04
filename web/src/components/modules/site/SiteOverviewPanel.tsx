'use client';

import { PageOverview } from '@/components/common/PageOverview';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { useTranslations } from 'next-intl';
import type { SiteActiveFilterStatus, SiteFilterStatus, SiteStatusSummary } from './site-status';

const FILTERS: Array<{ key: SiteFilterStatus; label: string; activeClassName: string; inactiveClassName: string }> = [
    {
        key: 'all', label: '全部',
        activeClassName: 'border-primary/30 bg-primary text-primary-foreground',
        inactiveClassName: 'border-border bg-background text-foreground',
    },
    {
        key: 'normal', label: '正常',
        activeClassName: 'border-emerald-500/30 bg-emerald-500 text-white',
        inactiveClassName: 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
    },
    {
        key: 'abnormal', label: '异常',
        activeClassName: 'border-destructive/30 bg-destructive text-white',
        inactiveClassName: 'border-destructive/20 bg-destructive/10 text-destructive',
    },
    {
        key: 'disabled', label: '停用',
        activeClassName: 'border-slate-500/30 bg-slate-700 text-white dark:bg-slate-200 dark:text-slate-900',
        inactiveClassName: 'border-slate-500/20 bg-slate-500/10 text-slate-700 dark:text-slate-300',
    },
];

export function SiteOverviewPanel({
    inventory,
    statusSummary,
    activeFilterStatuses,
    onFilterChange,
    visibleSiteCount,
    visibleAccountCount,
    allTags,
    activeTags,
    onTagFilterChange,
    hasActiveFilters,
    onClearFilters,
}: {
    inventory: { totalBalance: number; totalBalanceUsed: number; enabledAccounts: number; totalAccounts: number };
    statusSummary: SiteStatusSummary;
    activeFilterStatuses: SiteActiveFilterStatus[];
    onFilterChange: (status: SiteFilterStatus) => void;
    visibleSiteCount: number;
    visibleAccountCount: number;
    allTags: Array<{ tag: string; count: number }>;
    activeTags: string[];
    onTagFilterChange: (tag: string) => void;
    hasActiveFilters: boolean;
    onClearFilters: () => void;
}) {
    const t = useTranslations('workspace.site');
    return <>
        <PageOverview title={t('title')} description={t('description')} metrics={[
            { label: '当前余额', value: `$${inventory.totalBalance.toFixed(2)}`, accent: true },
            { label: '累计消耗', value: `$${inventory.totalBalanceUsed.toFixed(2)}` },
            { label: '启用账号', value: `${inventory.enabledAccounts} / ${inventory.totalAccounts}` },
        ]}>
            <div className="flex w-full flex-wrap items-center gap-2 border-t border-border/60 pt-3">
                <span className="mr-auto text-xs text-muted-foreground">
                    当前结果 <span className="font-medium tabular-nums text-foreground">{visibleSiteCount} 站点 / {visibleAccountCount} 账号</span>
                </span>
                {allTags.map(({ tag, count }) => <button key={tag} type="button" aria-pressed={activeTags.includes(tag)} onClick={() => onTagFilterChange(tag)} className={cn('rounded-full border px-3 py-1.5 text-xs transition-colors focus-visible:outline-2 focus-visible:outline-ring', activeTags.includes(tag) ? 'border-primary/30 bg-primary text-primary-foreground' : 'border-border/60 bg-background hover:bg-muted/60')}>
                    {tag} · {count}
                </button>)}
                {hasActiveFilters ? <Button variant="ghost" size="sm" className="rounded-xl text-xs" onClick={onClearFilters}>清空筛选</Button> : null}
            </div>
        </PageOverview>
        <div role="group" aria-label="站点状态筛选" className="flex flex-wrap gap-2 rounded-2xl border border-border/60 bg-card/60 px-4 py-4 sm:px-5">
            {FILTERS.map(filter => {
                const active = filter.key === 'all'
                    ? activeFilterStatuses.length === 0
                    : activeFilterStatuses.includes(filter.key);
                return <button
                    key={filter.key}
                    type="button"
                    aria-pressed={active}
                    onClick={() => onFilterChange(filter.key)}
                    className={cn(
                        'inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors focus-visible:outline-2 focus-visible:outline-ring',
                        active ? filter.activeClassName : filter.inactiveClassName,
                    )}
                >
                    <span className="tabular-nums">{statusSummary[filter.key]}</span>
                    <span>{filter.label}</span>
                </button>;
            })}
        </div>
    </>;
}
