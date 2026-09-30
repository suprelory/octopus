import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test from 'node:test';
import ts from 'typescript';
import { QueryClient } from '@tanstack/react-query';
import * as querySession from '../src/api/query-session.ts';

const require = createRequire(import.meta.url);

// Execute the real store/client with browser storage and presentation-only
// dependencies supplied locally; no browser or backend service is required.
function loadAuth() {
    const storage = new Map();
    const localStorage = {
        getItem: (key) => storage.get(key) ?? null,
        setItem: (key, value) => storage.set(key, value),
        removeItem: (key) => storage.delete(key),
    };
    const compile = (path, dependencies = {}) => {
        const source = readFileSync(new URL(path, import.meta.url), 'utf8');
        const { outputText } = ts.transpileModule(source, {
            compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true },
        });
        const loadedModule = { exports: {} };
        new Function('require', 'module', 'exports', 'window', 'localStorage', outputText)(
            (name) => dependencies[name] ?? require(name), loadedModule, loadedModule.exports, {}, localStorage,
        );
        return loadedModule.exports;
    };
    const client = compile('../src/api/client.ts', {
        './types': compile('../src/api/types.ts'),
        './error-i18n': { translateApiErrorCode: (_code, fallback) => fallback },
    });
    const { useAuthStore } = compile('../src/api/endpoints/user.ts', {
        'zustand/middleware': {
            ...require('zustand/middleware'),
            persist: (creator, options) => require('zustand/middleware').persist(creator, {
                ...options,
                storage: require('zustand/middleware').createJSONStorage(() => localStorage),
            }),
        },
        '../client': client,
        '../query-session': querySession,
        '@/lib/logger': { logger: { error() {}, log() {} } },
    });
    return { store: useAuthStore, ...client };
}

test('API key, admin login and logout all discard private cached credentials', () => {
    const { store } = loadAuth();
    const client = new QueryClient();
    const unregister = querySession.registerSessionQueryClient(client);
    try {
        store.getState().setAPIKeyAuth('key-A');
        const oldVersion = store.getState().sessionVersion;
        for (const transition of [
            () => store.getState().setAPIKeyAuth('key-B'),
            () => store.getState().setAuth('admin-token', '2099-01-01T00:00:00Z'),
            () => store.getState().logout(),
        ]) {
            client.setQueryData(['apikey', 'dashboard', 'stats'], { info: { api_key: 'secret-A' } });
            transition();
            assert.equal(client.getQueryCache().getAll().length, 0);
        }
        assert.ok(store.getState().sessionVersion > oldVersion);
        assert.equal(store.getState().token, null);
    } finally { unregister(); }
});

test('late 401 and auth validation for key A cannot log out key B', async (t) => {
    const { store } = loadAuth();
    let respond;
    t.mock.method(globalThis, 'fetch', () => new Promise((resolve) => { respond = resolve; }));
    t.mock.method(console, 'error', () => {});
    store.getState().setAPIKeyAuth('key-A');
    const checking = store.getState().checkAuth();
    store.getState().setAPIKeyAuth('key-B');
    respond(new Response(JSON.stringify({ message: 'unauthorized' }), { status: 401, headers: { 'content-type': 'application/json' } }));
    await checking;
    assert.equal(store.getState().token, 'key-B');
    assert.equal(store.getState().isAuthenticated, true);
});

test('late successful private responses are rejected after identity changes', async (t) => {
    const { store, apiClient } = loadAuth();
    let respond;
    t.mock.method(globalThis, 'fetch', () => new Promise((resolve) => { respond = resolve; }));
    store.getState().setAPIKeyAuth('key-A');
    const pending = apiClient.get('/api/v1/apikey/stats');
    store.getState().setAPIKeyAuth('key-B');
    respond(new Response(JSON.stringify({ data: { info: { api_key: 'key-A' } } }), { headers: { 'content-type': 'application/json' } }));
    await assert.rejects(pending, /Authentication changed/);
    assert.equal(store.getState().token, 'key-B');
});
