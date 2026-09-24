import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';

export function PageEmptyState({ icon: Icon, title, description, children }: {
    icon: LucideIcon;
    title: string;
    description: string;
    children?: ReactNode;
}) {
    return (
        <section className="page-empty-state flex flex-col items-center">
            <span className="mb-4 rounded-2xl bg-primary/8 p-3 text-primary"><Icon aria-hidden className="size-6" /></span>
            <h3 className="text-sm font-semibold text-foreground">{title}</h3>
            <p className="mt-2 max-w-md text-xs leading-relaxed">{description}</p>
            {children && <div className="mt-4">{children}</div>}
        </section>
    );
}

export function CardGridSkeleton({ label }: { label: string }) {
    return (
        <div role="status" aria-label={label} className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }, (_, index) => <div key={index} className="page-card space-y-4 p-5 motion-safe:animate-pulse">
                <div className="flex items-center gap-3"><div className="size-10 rounded-xl bg-muted" /><div className="h-4 w-1/2 rounded bg-muted" /></div>
                <div className="h-16 rounded-xl bg-muted/60" />
            </div>)}
        </div>
    );
}
