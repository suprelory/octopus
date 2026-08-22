'use client';

import { lazy, Suspense, useEffect, useState } from 'react';

const ServiceWorkerRegister = lazy(() =>
    import('@/components/sw-register').then((module) => ({ default: module.ServiceWorkerRegister }))
);

export function DeferredServiceWorkerRegister() {
    const [ready, setReady] = useState(false);

    useEffect(() => {
        const loadServiceWorker = () => setReady(true);

        if ('requestIdleCallback' in window) {
            const idleId = window.requestIdleCallback(loadServiceWorker, { timeout: 2000 });
            return () => window.cancelIdleCallback(idleId);
        }

        const timer = globalThis.setTimeout(loadServiceWorker, 0);
        return () => globalThis.clearTimeout(timer);
    }, []);

    if (!ready) return null;

    return (
        <Suspense fallback={null}>
            <ServiceWorkerRegister />
        </Suspense>
    );
}
