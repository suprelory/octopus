import assert from 'node:assert/strict';
import test from 'node:test';
import { QueryClient } from '@tanstack/react-query';
import { clearSessionQueries, registerSessionQueryClient, shouldRetrySessionQuery } from '../src/api/query-session.ts';

test('identity changes clear secrets and prevent late requests restoring old data', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const unregister = registerSessionQueryClient(client);
    try {
        client.setQueryData(['apikey', 'dashboard', 'stats', 1], { info: { api_key: 'key-A' } });
        let finish;
        const pending = client.fetchQuery({
            queryKey: ['private-pending'],
            queryFn: () => new Promise((resolve) => { finish = resolve; }),
        });
        const canceled = assert.rejects(pending);
        clearSessionQueries();
        assert.equal(client.getQueryCache().getAll().length, 0);
        client.setQueryData(['apikey', 'dashboard', 'stats', 2], { info: { api_key: 'key-B' } });
        finish({ info: { api_key: 'key-A' } });
        await canceled;
        await new Promise((resolve) => setImmediate(resolve));
        assert.equal(client.getQueryData(['private-pending']), undefined);
        assert.equal(client.getQueryData(['apikey', 'dashboard', 'stats', 1]), undefined);
        assert.equal(client.getQueryData(['apikey', 'dashboard', 'stats', 2]).info.api_key, 'key-B');
    } finally {
        unregister();
    }
});

test('query retries never replay a 401 but still recover from a transient failure', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: shouldRetrySessionQuery, retryDelay: 0 } } });
    let unauthorizedAttempts = 0;
    let transientAttempts = 0;
    try {
        await assert.rejects(client.fetchQuery({
            queryKey: ['unauthorized'],
            queryFn: async () => { unauthorizedAttempts++; throw { code: 401, message: 'expired' }; },
        }));
        assert.equal(unauthorizedAttempts, 1);
        const result = await client.fetchQuery({
            queryKey: ['temporary'],
            queryFn: async () => {
                if (++transientAttempts === 1) throw { code: 503, message: 'unavailable' };
                return 'recovered';
            },
        });
        assert.equal(result, 'recovered');
        assert.equal(transientAttempts, 2);
    } finally { client.clear(); }
});
