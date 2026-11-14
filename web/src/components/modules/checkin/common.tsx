import { cloneElement, isValidElement, useId, type ComponentProps, type ReactNode } from 'react';
import { Dialog } from 'radix-ui';
import { X } from 'lucide-react';
import { useTranslations } from 'use-intl';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

export function CheckinDialog({
    title,
    description,
    children,
    onClose,
}: {
    title: string;
    description?: string;
    children: ReactNode;
    onClose: () => void;
}) {
    const t = useTranslations('checkin');
    return (
        <Dialog.Root
            open
            onOpenChange={(open) => {
                if (!open) onClose();
            }}
        >
            <Dialog.Portal>
                <Dialog.Overlay className="fixed inset-0 z-[70] bg-black/45 backdrop-blur-sm" />
                <Dialog.Content className="fixed left-1/2 top-1/2 z-[80] flex max-h-[90dvh] w-[calc(100%-1.5rem)] max-w-3xl -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-3xl border bg-card shadow-xl">
                    <div className="flex items-start gap-4 border-b p-5">
                        <div className="min-w-0 flex-1">
                            <Dialog.Title className="text-lg font-semibold">{title}</Dialog.Title>
                            <Dialog.Description
                                className={cn(
                                    'mt-1 text-sm text-muted-foreground',
                                    !description && 'sr-only',
                                )}
                            >
                                {description || title}
                            </Dialog.Description>
                        </div>
                        <Dialog.Close asChild>
                            <Button variant="ghost" size="icon" aria-label={t('close')}>
                                <X className="size-4" />
                            </Button>
                        </Dialog.Close>
                    </div>
                    <div className="overflow-y-auto p-5">{children}</div>
                </Dialog.Content>
            </Dialog.Portal>
        </Dialog.Root>
    );
}

export function CheckinField({
    label,
    hint,
    children,
}: {
    label: string;
    hint?: string;
    children: ReactNode;
}) {
    const id = useId();
    const control = isValidElement<{ id?: string; 'aria-describedby'?: string }>(children)
        ? children
        : null;
    const controlID = control?.props.id ?? id;
    return (
        <div className="flex min-w-0 flex-col gap-1.5 text-sm">
            <label htmlFor={controlID} className="font-medium">
                {label}
            </label>
            {control
                ? cloneElement(control, {
                      id: controlID,
                      'aria-describedby':
                          [control.props['aria-describedby'], hint ? `${id}-hint` : undefined]
                              .filter(Boolean)
                              .join(' ') || undefined,
                  })
                : children}
            {hint && (
                <span id={`${id}-hint`} className="text-xs text-muted-foreground">
                    {hint}
                </span>
            )}
        </div>
    );
}

export function CheckinSelect({ className, ...props }: ComponentProps<'select'>) {
    return (
        <select
            {...props}
            className={cn(
                'h-9 min-w-0 rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring',
                className,
            )}
        />
    );
}

export function CheckinTextarea({ className, ...props }: ComponentProps<'textarea'>) {
    return (
        <textarea
            {...props}
            className={cn(
                'min-h-24 w-full rounded-md border border-input bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring',
                className,
            )}
        />
    );
}

export function CheckinToggle({
    label,
    checked,
    onChange,
}: {
    label: string;
    checked: boolean;
    onChange: (value: boolean) => void;
}) {
    const id = useId();
    return (
        <label htmlFor={id} className="flex items-center gap-2 text-sm">
            <input
                id={id}
                type="checkbox"
                checked={checked}
                onChange={(event) => onChange(event.target.checked)}
                className="size-4 accent-[var(--primary)]"
            />
            {label}
        </label>
    );
}

export function CheckinStatus({ value }: { value: string }) {
    const t = useTranslations('checkin');
    const state = value || 'idle';
    return (
        <span
            className={cn(
                'inline-flex rounded-full border px-2 py-0.5 text-xs',
                (state === 'success' ||
                    state === 'completed' ||
                    state === 'already_checked_in' ||
                    state === 'supported') &&
                    'border-emerald-500/25 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
                (state === 'failed' ||
                    state === 'completed_with_errors' ||
                    state === 'unsupported') &&
                    'border-destructive/30 bg-destructive/10 text-destructive',
                (state === 'pending' ||
                    state === 'manual_required' ||
                    state === 'running' ||
                    state === 'queued') &&
                    'border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-400',
            )}
        >
            {t.has(`states.${state}`) ? t(`states.${state}`) : state}
        </span>
    );
}

export function CheckinError({ error }: { error: unknown }) {
    if (!error) return null;
    return (
        <p role="alert" className="rounded-xl bg-destructive/10 p-3 text-sm text-destructive">
            {error instanceof Error ? error.message : String(error)}
        </p>
    );
}
