import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';

export function PageOverview({ title, description, metrics = [], children, className }: {
    title: string;
    description: string;
    metrics?: { label: string; value: ReactNode; accent?: boolean }[];
    children?: ReactNode;
    className?: string;
}) {
    return (
        <section aria-label={title} className={cn('flex flex-wrap items-center justify-between gap-x-6 gap-y-4 rounded-2xl border border-border/60 bg-card/60 px-4 py-4 sm:px-5', className)}>
            <div className="min-w-0">
                <h2 className="text-sm font-semibold">{title}</h2>
                <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{description}</p>
            </div>
            {metrics.length > 0 && <dl className="flex flex-wrap items-center gap-x-5 gap-y-2">
                {metrics.map(({ label, value, accent }) => <div key={label} className="flex items-baseline gap-2">
                    <dt className="text-xs text-muted-foreground">{label}</dt>
                    <dd className={cn('text-base font-semibold tabular-nums', accent && 'text-primary')}>{value}</dd>
                </div>)}
            </dl>}
            {children}
        </section>
    );
}
