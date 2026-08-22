'use client';

import { lazy, Suspense, useEffect, useState } from 'react';

const Toaster = lazy(() =>
    import('@/components/ui/sonner').then((module) => ({ default: module.Toaster }))
);

export function DeferredToaster() {
    const [ready, setReady] = useState(false);

    useEffect(() => {
        const showToaster = () => setReady(true);

        if ('requestIdleCallback' in window) {
            const idleId = window.requestIdleCallback(showToaster, { timeout: 1000 });
            return () => window.cancelIdleCallback(idleId);
        }

        const timer = globalThis.setTimeout(showToaster, 0);
        return () => globalThis.clearTimeout(timer);
    }, []);

    if (!ready) return null;

    return (
        <Suspense fallback={null}>
            <Toaster />
        </Suspense>
    );
}
