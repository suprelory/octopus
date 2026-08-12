'use client';

import { cn } from '@/lib/utils';

/** 日志卡片骨架屏：结构对齐 LogCard，避免首屏数据到达时布局跳动。 */
export function LogCardSkeleton() {
    return (
        <div className="rounded-3xl border border-border bg-card">
            <div className="grid grid-cols-[auto_1fr] items-center gap-2.5 p-2.5 sm:gap-4 sm:p-4">
                <div className="size-9 shrink-0 animate-pulse rounded-full bg-muted sm:size-10" />
                <div className="flex min-w-0 flex-col gap-2">
                    <div className="flex items-center gap-2">
                        <div className="h-4 w-32 animate-pulse rounded bg-muted" />
                        <div className="h-4 w-16 animate-pulse rounded bg-muted/70" />
                        <div className="hidden h-4 w-24 animate-pulse rounded bg-muted/70 sm:block" />
                    </div>
                    <div className="grid grid-cols-2 gap-x-3 gap-y-1.5 md:grid-cols-7">
                        {Array.from({ length: 7 }).map((_, index) => (
                            <div key={index} className="h-3 animate-pulse rounded bg-muted/60" />
                        ))}
                    </div>
                </div>
            </div>
        </div>
    );
}

export function LogListSkeleton({ count = 6, className }: { count?: number; className?: string }) {
    return (
        <div className={cn('flex flex-col gap-4', className)}>
            {Array.from({ length: count }).map((_, index) => (
                <LogCardSkeleton key={index} />
            ))}
        </div>
    );
}
