'use client';

import { useRef } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { BACKDROP_ENTRANCE } from '@/lib/animations/css-entrances';
import { cn } from '@/lib/utils';

// Radix owns focus trapping, background accessibility and Escape for nested
// popovers. Keep the slots used by MorphingDialog to recognize an upper layer.
export function OverlayPortal({ onClose, title, className, children }: {
    onClose: () => void;
    title: string;
    className?: string;
    children: React.ReactNode;
}) {
    const returnFocus = useRef<HTMLElement | null>(null);
    return (
        <Dialog.Root open onOpenChange={(open) => { if (!open) onClose(); }}>
            <Dialog.Portal>
                <Dialog.Overlay data-slot="dialog-overlay"
                    className={cn('fixed inset-0 z-50 bg-white/40 backdrop-blur-xs dark:bg-black/40', BACKDROP_ENTRANCE)} />
                <Dialog.Content data-slot="dialog-content" className={className} aria-describedby={undefined}
                    onOpenAutoFocus={() => { returnFocus.current = document.activeElement as HTMLElement | null; }}
                    onCloseAutoFocus={(event) => {
                        event.preventDefault();
                        returnFocus.current?.focus();
                    }}>
                    <Dialog.Title className="sr-only">{title}</Dialog.Title>
                    {children}
                </Dialog.Content>
            </Dialog.Portal>
        </Dialog.Root>
    );
}
