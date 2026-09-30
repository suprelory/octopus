import assert from 'node:assert/strict';
import test from 'node:test';
import { QueryClient } from '@tanstack/react-query';
import { clearSessionQueries, registerSessionQueryClient } from '../src/api/query-session.ts';

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
