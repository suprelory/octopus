'use client';

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
    return <section aria-label={t('title')} className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
                <h2 className="text-lg font-semibold">{t('title')}</h2>
                <p className="text-sm text-muted-foreground">{t('description')}</p>
            </div>
            <span className="text-xs text-muted-foreground">当前结果 {visibleSiteCount} 站点 / {visibleAccountCount} 账号</span>
        </div>
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
            {[
                ['当前余额', `$${inventory.totalBalance.toFixed(2)}`],
                ['累计消耗', `$${inventory.totalBalanceUsed.toFixed(2)}`],
                ['启用账号', `${inventory.enabledAccounts} / ${inventory.totalAccounts}`],
            ].map(([label, value]) => <div key={label} className="page-card p-4">
                <div className="text-xs text-muted-foreground">{label}</div>
                <div className="mt-3 truncate text-2xl font-semibold tabular-nums" title={value}>{value}</div>
            </div>)}
        </div>
        {allTags.length > 0 || hasActiveFilters ? <div className="flex flex-wrap items-center gap-2">
            {allTags.map(({ tag, count }) => <button key={tag} type="button" aria-pressed={activeTags.includes(tag)} onClick={() => onTagFilterChange(tag)} className={cn('rounded-full border px-3 py-1.5 text-xs', activeTags.includes(tag) ? 'bg-primary text-primary-foreground' : 'bg-card')}>
                {tag} · {count}
            </button>)}
            {hasActiveFilters ? <Button variant="ghost" size="sm" onClick={onClearFilters}>清空筛选</Button> : null}
        </div> : null}
    </section>;
}
