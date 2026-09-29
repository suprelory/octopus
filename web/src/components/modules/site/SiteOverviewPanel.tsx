'use client';

import { PageOverview } from '@/components/common/PageOverview';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { useTranslations } from 'next-intl';

export function SiteOverviewPanel({ inventory, visibleSiteCount, visibleAccountCount, allTags, activeTags, onTagFilterChange, hasActiveFilters, onClearFilters }: {
    inventory: { totalBalance: number; totalBalanceUsed: number; enabledAccounts: number; totalAccounts: number };
    visibleSiteCount: number;
    visibleAccountCount: number;
    allTags: Array<{ tag: string; count: number }>;
    activeTags: string[];
    onTagFilterChange: (tag: string) => void;
    hasActiveFilters: boolean;
    onClearFilters: () => void;
}) {
    const t = useTranslations('workspace.site');
    return <PageOverview title={t('title')} description={t('description')} metrics={[
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
    </PageOverview>;
}
