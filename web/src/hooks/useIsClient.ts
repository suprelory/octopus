import { useSyncExternalStore } from 'react';

const subscribeToNothing = () => () => {};

/**
 * True once running on the client, false during the static export render.
 *
 * Prefer this over `typeof window !== 'undefined'` when the result decides what to
 * render: a bare `typeof` check is read during hydration too, so the first client
 * render can disagree with the server HTML. `useSyncExternalStore` returns the
 * server snapshot for the hydration pass and re-renders after, which is hydration-safe.
 */
export function useIsClient() {
    return useSyncExternalStore(
        subscribeToNothing,
        () => true,
        () => false
    );
}
