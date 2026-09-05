'use client';

import { useSiteChannelList } from '@/api/endpoints/site-channel';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { KeyRound } from 'lucide-react';
import { useMemo, useState } from 'react';
import { UnifiedCompletionDialog } from './UnifiedCompletionDialog';
import { collectPendingCompletionSites } from './utils';

export function SiteChannelCompletionAction() {
    const { data } = useSiteChannelList();
    const [completionDialogOpen, setCompletionDialogOpen] = useState(false);

    const pendingCompletionSites = useMemo(() => collectPendingCompletionSites(data ?? []), [data]);
    const totalPendingCompletionCount = useMemo(
        () => pendingCompletionSites.reduce((sum, site) => sum + site.pending_count, 0),
        [pendingCompletionSites],
    );
    const effectiveCompletionDialogOpen = completionDialogOpen && totalPendingCompletionCount > 0;

    if (totalPendingCompletionCount === 0) return null;

    return (
        <>
            <Button
                type="button"
                variant="outline"
                className="h-10 rounded-2xl px-3"
                onClick={() => setCompletionDialogOpen(true)}
            >
                <KeyRound className="size-4 text-primary" />
                统一补全 Key
                <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                    {totalPendingCompletionCount}
                </Badge>
            </Button>
            <UnifiedCompletionDialog
                open={effectiveCompletionDialogOpen}
                onOpenChange={setCompletionDialogOpen}
                sites={pendingCompletionSites}
            />
        </>
    );
}

// 新增：用于在 SiteChannelSection 中同步状态到 store
export function useCompletionStateSync() {
    const { data } = useSiteChannelList();

    const pendingCompletionSites = useMemo(() => collectPendingCompletionSites(data ?? []), [data]);

    const totalPendingCompletionCount = useMemo(
        () => pendingCompletionSites.reduce((sum, site) => sum + site.pending_count, 0),
        [pendingCompletionSites],
    );

    return { pendingCompletionSites, totalPendingCompletionCount };
}
