import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test from 'node:test';
import ts from 'typescript';
import { QueryClient } from '@tanstack/react-query';
import * as querySession from '../src/api/query-session.ts';

const require = createRequire(import.meta.url);

test('anonymous authentication errors preserve the session while expired credentials log out', async (t) => {
    const { store, apiClient } = loadAuth();
    t.mock.method(globalThis, 'fetch', async () => new Response(JSON.stringify({ message: 'unauthorized' }), {
        status: 401, headers: { 'content-type': 'application/json' },
    }));
    t.mock.method(console, 'error', () => {});
    const version = store.getState().sessionVersion;
    await assert.rejects(apiClient.post('/api/v1/user/login', { username: 'invalid' }));
    assert.equal(store.getState().sessionVersion, version);
    store.getState().setAuth('expired-token', '2099-01-01T00:00:00Z');
    await assert.rejects(apiClient.get('/api/v1/private'));
    assert.equal(store.getState().token, null);
    assert.equal(store.getState().isAuthenticated, false);
});

// Execute the real store/client with browser storage and presentation-only
// dependencies supplied locally; no browser or backend service is required.
function loadAuth(storage = new Map()) {
    const localStorage = {
        getItem: (key) => storage.get(key) ?? null,
        setItem: (key, value) => storage.set(key, value),
        removeItem: (key) => storage.delete(key),
    };
    const documentListeners = new Map();
    const windowListeners = new Map();
    const document = {
        visibilityState: 'visible',
        addEventListener: (name, callback) => documentListeners.set(name, callback),
        removeEventListener: (name) => documentListeners.delete(name),
    };
    const window = {
        localStorage,
        addEventListener: (name, callback) => windowListeners.set(name, callback),
        removeEventListener: (name) => windowListeners.delete(name),
    };
    const compile = (path, dependencies = {}) => {
        const source = readFileSync(new URL(path, import.meta.url), 'utf8');
        const { outputText } = ts.transpileModule(source, {
            compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true },
        });
        const loadedModule = { exports: {} };
        new Function('require', 'module', 'exports', 'window', 'localStorage', 'document', outputText)(
            (name) => dependencies[name] ?? require(name), loadedModule, loadedModule.exports, window, localStorage, document,
        );
        return loadedModule.exports;
    };
    const config = compile('../src/api/config.ts');
    const authSession = compile('../src/api/auth-session.ts', { './config': config });
    const client = compile('../src/api/client.ts', {
        './config': config,
        './auth-session': authSession,
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
        '../auth-session': authSession,
        '@/lib/logger': { logger: { error() {}, log() {} } },
    });
    authSession.startAuthSessionListeners();
    return {
        store: useAuthStore, ...client, ...authSession, storage, localStorage, document, compile,
        interact: (type = 'pointerdown', isTrusted = true) => documentListeners.get(type)?.({ isTrusted }),
        storageEvent: () => windowListeners.get('storage')?.({ key: 'auth-storage', storageArea: localStorage }),
    };
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
    await settle();
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
    await settle();
    store.getState().setAPIKeyAuth('key-B');
    respond(new Response(JSON.stringify({ data: { info: { api_key: 'key-A' } } }), { headers: { 'content-type': 'application/json' } }));
    await assert.rejects(pending, /Authentication changed/);
    assert.equal(store.getState().token, 'key-B');
});

const DAY = 24 * 60 * 60;
const NOW = Date.UTC(2026, 9, 12);
const settle = () => new Promise((resolve) => setImmediate(resolve));
const json = (data, status = 200) => new Response(JSON.stringify({ data }), {
    status, headers: { 'content-type': 'application/json' },
});
function credential(overrides = {}) {
    const now = Math.floor(Date.now() / 1000);
    const claims = {
        ver: 2, sid: 'login-A', auth_time: now - 25 * DAY, renewable: true,
        iat: now - 25 * DAY, nbf: now - 25 * DAY, exp: now + 5 * DAY, ...overrides,
    };
    return {
        token: `${Buffer.from('{"alg":"HS256"}').toString('base64url')}.${Buffer.from(JSON.stringify(claims)).toString('base64url')}.signature`,
        expire_at: new Date(claims.exp * 1000).toISOString(),
    };
}
function renewal(previous, overrides = {}) {
    const payload = JSON.parse(Buffer.from(previous.token.split('.')[1], 'base64url').toString());
    const now = Math.floor(Date.now() / 1000);
    return credential({ ...payload, iat: now, nbf: now, exp: Math.min(now + 30 * DAY, payload.auth_time + 90 * DAY), ...overrides });
}
function login(store, auth) { store.getState().setAuth(auth.token, auth.expire_at); }

test('concurrent requests renew once, preserve cached data and use the renewed credential', async (t) => {
    t.mock.timers.enable({ apis: ['Date'], now: NOW });
    const auth = loadAuth();
    const original = credential();
    const renewed = renewal(original);
    login(auth.store, original);
    const cache = new QueryClient();
    const unregister = querySession.registerSessionQueryClient(cache);
    t.after(unregister);
    cache.setQueryData(['private'], { draft: 'keep me' });
    const version = auth.store.getState().sessionVersion;
    let respondRefresh;
    let refreshes = 0;
    const businessHeaders = [];
    t.mock.method(globalThis, 'fetch', async (url, init) => {
        if (url.endsWith('/refresh')) {
            refreshes++;
            assert.equal(init.method, 'POST');
            assert.equal(init.body, '{}');
            assert.equal(new Headers(init.headers).get('Content-Type'), 'application/json');
            return new Promise((resolve) => { respondRefresh = resolve; });
        }
        businessHeaders.push(new Headers(init.headers).get('Authorization'));
        return json('ok');
    });
    auth.interact();
    const requests = Array.from({ length: 10 }, () => auth.apiClient.get('/api/v1/private'));
    await settle();
    assert.equal(refreshes, 1);
    assert.equal(businessHeaders.length, 0);
    respondRefresh(json(renewed));
    assert.deepEqual(await Promise.all(requests), Array(10).fill('ok'));
    assert.ok(businessHeaders.every((header) => header === `Bearer ${renewed.token}`));
    assert.equal(auth.store.getState().sessionVersion, version);
    assert.deepEqual(cache.getQueryData(['private']), { draft: 'keep me' });
    assert.equal(JSON.parse(auth.localStorage.getItem('auth-storage')).state.token, renewed.token);
});

for (const status of [200, 401]) {
    test(`a late ${status} from the previous JWT preserves the renewed login`, async (t) => {
        t.mock.method(console, 'error', () => {});
        const auth = loadAuth();
        const original = credential();
        login(auth.store, original);
        let respond;
        t.mock.method(globalThis, 'fetch', () => new Promise((resolve) => { respond = resolve; }));
        const pending = auth.apiClient.get('/api/v1/private');
        await settle();
        const renewed = renewal(original);
        assert.equal(auth.store.getState().applyRenewedToken(renewed, auth.store.getState()), true);
        respond(json('old request result', status));
        if (status === 200) assert.equal(await pending, 'old request result');
        else await assert.rejects(pending, (error) => error.code === 401);
        assert.equal(auth.store.getState().token, renewed.token);
        assert.equal(auth.store.getState().isAuthenticated, true);
    });
}

for (const change of ['logout', 'new-login', 'api-key']) {
    test(`renewal cannot overwrite ${change} in another tab before its storage event arrives`, () => {
        const auth = loadAuth();
        const original = credential();
        login(auth.store, original);
        const expected = auth.store.getState();
        const other = loadAuth(auth.storage);
        if (change === 'logout') other.store.getState().logout();
        if (change === 'new-login') login(other.store, credential({ sid: 'login-B' }));
        if (change === 'api-key') other.store.getState().setAPIKeyAuth('key-B');
        const latest = other.store.getState().token;
        assert.equal(auth.store.getState().token, original.token);
        assert.equal(auth.store.getState().applyRenewedToken(renewal(original), expected), false);
        assert.equal(auth.store.getState().token, latest);
        assert.equal(JSON.parse(auth.localStorage.getItem('auth-storage')).state.token, latest);
        assert.ok(auth.store.getState().sessionVersion > expected.sessionVersion);
    });
}

test('storage renewal preserves the login generation and ignores older tokens', () => {
    const auth = loadAuth();
    const original = credential();
    login(auth.store, original);
    const version = auth.store.getState().sessionVersion;
    const other = loadAuth(auth.storage);
    const renewed = renewal(original);
    assert.equal(other.store.getState().applyRenewedToken(renewed, other.store.getState()), true);
    auth.storageEvent();
    assert.equal(auth.store.getState().token, renewed.token);
    assert.equal(auth.store.getState().sessionVersion, version);
    assert.equal(auth.store.getState().isAuthenticated, true);
    auth.localStorage.setItem('auth-storage', JSON.stringify({ state: {
        token: original.token, expireAt: original.expire_at, isAPIKeyAuth: false,
    } }));
    auth.storageEvent();
    assert.equal(auth.store.getState().token, renewed.token);
});

test('a refresh response cannot revive logout or overwrite a subsequent local login', async (t) => {
    const auth = loadAuth();
    const original = credential();
    login(auth.store, original);
    let respond;
    t.mock.method(globalThis, 'fetch', () => new Promise((resolve) => { respond = resolve; }));
    auth.interact();
    const flight = auth.ensureFreshAuthSession();
    auth.store.getState().logout();
    const next = credential({ sid: 'login-B' });
    login(auth.store, next);
    respond(json(renewal(original)));
    await flight;
    assert.equal(auth.store.getState().token, next.token);
});

test('renewal validates the original session, authentication time and expiration', () => {
    const auth = loadAuth();
    const original = credential();
    login(auth.store, original);
    const expected = auth.store.getState();
    for (const response of [
        original,
        renewal(original, { sid: 'login-B' }),
        renewal(original, { auth_time: Math.floor(Date.now() / 1000) }),
        renewal(original, { renewable: false }),
        { ...renewal(original), expire_at: original.expire_at },
    ]) assert.equal(auth.store.getState().applyRenewedToken(response, expected), false);
    assert.equal(auth.store.getState().token, original.token);
});

for (const failure of [204, 429, 503, 'network']) {
    test(`renewal ${failure} retains credentials, sends business requests once and cools down`, async (t) => {
        t.mock.timers.enable({ apis: ['Date'], now: NOW });
        const auth = loadAuth();
        const original = credential();
        login(auth.store, original);
        let refreshes = 0;
        let mutations = 0;
        t.mock.method(globalThis, 'fetch', async (url, init) => {
            if (url.endsWith('/refresh')) {
                refreshes++;
                if (failure === 'network') throw new TypeError('offline');
                return new Response(null, { status: failure });
            }
            mutations++;
            assert.equal(new Headers(init.headers).get('Authorization'), `Bearer ${original.token}`);
            return json('saved');
        });
        auth.interact();
        assert.equal(await auth.apiClient.post('/api/v1/private', { change: true }), 'saved');
        await auth.ensureFreshAuthSession();
        assert.equal(refreshes, 1);
        assert.equal(mutations, 1);
        assert.equal(auth.store.getState().token, original.token);
        t.mock.timers.tick(60_000);
        auth.interact();
        await auth.ensureFreshAuthSession();
        assert.equal(refreshes, 2);
    });
}

test('a hung refresh times out, retries after cooldown and discards its late response', async (t) => {
    t.mock.timers.enable({ apis: ['Date', 'setTimeout'], now: NOW });
    const auth = loadAuth();
    const original = credential();
    login(auth.store, original);
    let respondLate;
    let attempts = 0;
    let firstSignal;
    t.mock.method(globalThis, 'fetch', async (_url, init) => {
        attempts++;
        if (attempts === 1) {
            firstSignal = init.signal;
            return new Promise((resolve) => { respondLate = resolve; });
        }
        return json(renewal(original));
    });
    auth.interact();
    const flight = auth.ensureFreshAuthSession();
    t.mock.timers.tick(5000);
    await flight;
    assert.equal(firstSignal.aborted, true);
    assert.equal(auth.store.getState().token, original.token);
    t.mock.timers.tick(60_000);
    auth.interact();
    await auth.ensureFreshAuthSession();
    assert.equal(attempts, 2);
    const accepted = auth.store.getState().token;
    respondLate(json(renewal(original, { exp: Math.floor(Date.now() / 1000) + 30 * DAY })));
    await settle();
    assert.equal(auth.store.getState().token, accepted);
});

test('only recent real interaction in a visible page can renew', async (t) => {
    t.mock.timers.enable({ apis: ['Date'], now: NOW });
    const auth = loadAuth();
    login(auth.store, credential());
    let calls = 0;
    t.mock.method(globalThis, 'fetch', async () => { calls++; return new Response(null, { status: 204 }); });
    await auth.ensureFreshAuthSession();
    auth.interact('focus');
    auth.interact('pointerdown', false);
    await auth.ensureFreshAuthSession();
    assert.equal(calls, 0);
    auth.document.visibilityState = 'hidden';
    auth.interact();
    await auth.ensureFreshAuthSession();
    assert.equal(calls, 0);
    auth.document.visibilityState = 'visible';
    auth.interact('keydown');
    await auth.ensureFreshAuthSession();
    assert.equal(calls, 1);
    t.mock.timers.tick(6 * 60_000);
    await auth.ensureFreshAuthSession();
    assert.equal(calls, 1);
});

test('API keys, legacy tokens, fixed lifetimes, expiry, the seven-day boundary and the cap skip renewal', async (t) => {
    t.mock.timers.enable({ apis: ['Date'], now: NOW });
    const auth = loadAuth();
    let calls = 0;
    t.mock.method(globalThis, 'fetch', async () => { calls++; return json(null); });
    const now = NOW / 1000;
    for (const credentials of [
        { token: 'legacy-token', expire_at: '2099-01-01T00:00:00Z' },
        credential({ renewable: false }),
        credential({ auth_time: now - 23 * DAY, iat: now - 23 * DAY, nbf: now - 23 * DAY, exp: now + 7 * DAY }),
        credential({ exp: now }),
        credential({ auth_time: now - 85 * DAY, iat: now - 25 * DAY, exp: now + 5 * DAY }),
    ]) {
        login(auth.store, credentials);
        auth.interact();
        await auth.ensureFreshAuthSession();
    }
    auth.store.getState().setAPIKeyAuth('key-A');
    auth.interact();
    await auth.ensureFreshAuthSession();
    assert.equal(calls, 0);
});

test('initial verification retains credentials on outages, ignores the local clock and can retry', async (t) => {
    const auth = loadAuth();
    login(auth.store, { token: 'stored-token', expire_at: '2000-01-01T00:00:00Z' });
    const version = auth.store.getState().sessionVersion;
    let unavailable = true;
    t.mock.method(console, 'error', () => {});
    t.mock.method(globalThis, 'fetch', async () => unavailable ? json(null, 503) : json('ok'));
    await auth.store.getState().checkAuth();
    assert.equal(auth.store.getState().token, 'stored-token');
    assert.equal(auth.store.getState().authUnavailable, true);
    assert.equal(auth.store.getState().isAuthenticated, false);
    assert.equal(auth.store.getState().isLoading, false);
    unavailable = false;
    await auth.store.getState().checkAuth();
    assert.equal(auth.store.getState().isAuthenticated, true);
    assert.equal(auth.store.getState().authUnavailable, false);
    assert.equal(auth.store.getState().sessionVersion, version);
});

test('a current refresh 401 stops the waiting write and clears credentials', async (t) => {
    const auth = loadAuth();
    login(auth.store, credential());
    const calls = [];
    t.mock.method(globalThis, 'fetch', async (url) => { calls.push(url); return json(null, 401); });
    auth.interact();
    await assert.rejects(auth.apiClient.post('/api/v1/private', { change: true }), /Authentication changed/);
    assert.equal(auth.store.getState().token, null);
    assert.equal(calls.length, 1);
    assert.ok(calls[0].endsWith('/refresh'));
});

test('ordinary forbidden responses preserve authentication and writes are never replayed', async (t) => {
    const auth = loadAuth();
    login(auth.store, credential());
    t.mock.method(console, 'error', () => {});
    let calls = 0;
    t.mock.method(globalThis, 'fetch', async () => { calls++; return json(null, 403); });
    await assert.rejects(auth.apiClient.post('/api/v1/private', {}), (error) => error.code === 403);
    assert.equal(calls, 1);
    assert.equal(auth.store.getState().isAuthenticated, true);
});

test('file transport uses renewed credentials without setting multipart Content-Type', async (t) => {
    const auth = loadAuth();
    const original = credential();
    const renewed = renewal(original);
    login(auth.store, original);
    const form = new FormData();
    form.append('file', new Blob(['{}'], { type: 'application/json' }), 'import.json');
    let imports = 0;
    t.mock.method(globalThis, 'fetch', async (url, init) => {
        if (url.endsWith('/refresh')) return json(renewed);
        imports++;
        const headers = new Headers(init.headers);
        assert.equal(headers.get('Authorization'), `Bearer ${renewed.token}`);
        assert.equal(headers.has('Content-Type'), false);
        assert.equal(init.body, form);
        return json({ imported: true });
    });
    auth.interact();
    const result = await auth.authenticatedFetch('/api/v1/setting/import', { method: 'POST', body: form }, (response) => response.json());
    assert.equal(result.data.imported, true);
    assert.equal(imports, 1);
});

test('a slow download body cannot return private data to a different login', async (t) => {
    const auth = loadAuth();
    login(auth.store, credential());
    let finish;
    t.mock.method(globalThis, 'fetch', async () => new Response('private export'));
    const pending = auth.authenticatedFetch('/api/v1/setting/export', {}, () => new Promise((resolve) => { finish = resolve; }));
    await settle();
    auth.store.getState().setAPIKeyAuth('key-B');
    finish(new Blob(['private export']));
    await assert.rejects(pending, /Authentication changed/);
});
