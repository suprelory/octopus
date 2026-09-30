import type { QueryClient } from '@tanstack/react-query';

const clients = new Set<QueryClient>();

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
