'use client';

import { useEffect } from 'react';
import { SW_MESSAGE_TYPE } from '@/lib/sw';

export function ServiceWorkerRegister() {
    useEffect(() => {
        if (typeof window === 'undefined') return;
        if (!('serviceWorker' in navigator)) return;
        if (process.env.NODE_ENV !== 'production') return;

        let hasRefreshed = false;
        let disposed = false;
        let idleId: number | undefined;
        let timer: ReturnType<typeof globalThis.setTimeout> | undefined;
        const onControllerChange = () => {
            if (hasRefreshed) return;
            hasRefreshed = true;
            window.location.reload();
        };

        const activateUpdate = (registration: ServiceWorkerRegistration) => {
            // First install: no existing controller, so no need to force activation/reload.
            if (!navigator.serviceWorker.controller) return;

            // Prefer `waiting` worker (already installed & waiting to activate).
            const worker = registration.waiting || registration.installing;
            worker?.postMessage({ type: SW_MESSAGE_TYPE.SKIP_WAITING });
        };

        navigator.serviceWorker.addEventListener('controllerchange', onControllerChange);

        const register = () => {
            if (disposed) return;

            navigator.serviceWorker
                .register('/sw.js', { scope: '/' })
                .then((registration) => {
                    // If an update is already waiting, activate it immediately.
                    if (registration.waiting) {
                        activateUpdate(registration);
                    }

                    registration.addEventListener('updatefound', () => {
                        const installing = registration.installing;
                        if (!installing) return;
                        installing.addEventListener('statechange', () => {
                            if (installing.state === 'installed') {
                                // When installed + controller exists => an update is ready (likely in `waiting`)
                                activateUpdate(registration);
                            }
                        });
                    });
                })
                .catch(() => {
                    // ignore
                });
        };

        const scheduleRegistration = () => {
            if ('requestIdleCallback' in window) {
                idleId = window.requestIdleCallback(register, { timeout: 2000 });
                return;
            }

            timer = globalThis.setTimeout(register, 0);
        };

        if (document.readyState === 'complete') {
            scheduleRegistration();
        } else {
            window.addEventListener('load', scheduleRegistration, { once: true });
        }

        return () => {
            disposed = true;
            window.removeEventListener('load', scheduleRegistration);
            navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange);
            if (idleId !== undefined) window.cancelIdleCallback(idleId);
            if (timer !== undefined) globalThis.clearTimeout(timer);
        };
    }, []);

    return null;
}
