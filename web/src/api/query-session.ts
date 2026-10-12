import type { QueryClient } from '@tanstack/react-query';

const clients = new Set<QueryClient>();

export function shouldRetrySessionQuery(failureCount: number, error: unknown): boolean {
    // Authentication failures are final for that request, including a late
    // 401 from a token replaced by renewal. Transient query failures may retry.
    return failureCount < 3 && !(error && typeof error === 'object' && 'code' in error && error.code === 401);
}

export function registerSessionQueryClient(client: QueryClient) {
    clients.add(client);
    return () => {
        clients.delete(client);
        client.clear();
    };
}

export function clearSessionQueries() {
    for (const client of clients) {
        // clear destroys queries and cancels their retryers, preventing late
        // responses from repopulating a previous identity's cache.
        void client.cancelQueries();
        client.clear();
    }
}
