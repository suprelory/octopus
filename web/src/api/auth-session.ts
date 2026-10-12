import { API_BASE_URL } from './config';

export const AUTH_STORAGE_KEY = 'auth-storage';
const DAY = 24 * 60 * 60;
const ACTIVE_WINDOW_MS = 5 * 60 * 1000;
const REFRESH_TIMEOUT_MS = 5000;
const REFRESH_COOLDOWN_MS = 60 * 1000;

export interface AuthSnapshot {
    token: string | null;
    sessionVersion: number;
    isAPIKeyAuth: boolean;
    isAuthenticated: boolean;
}

export interface RenewedToken {
    token: string;
    expire_at: string;
}

interface AuthSession extends AuthSnapshot {
    logout: () => void;
    applyRenewedToken: (response: RenewedToken, expected: AuthSnapshot) => boolean;
    syncAuthFromStorage: () => void;
    checkAuth: () => Promise<void>;
    isLoading: boolean;
}

export interface JWTSession {
    ver: 2;
    sid: string;
    auth_time: number;
    renewable: boolean;
    iat: number;
    nbf: number;
    exp: number;
}

// Decoding only schedules renewal and identifies a login. The server remains
// responsible for validating signatures and deciding whether a token is valid.
export function decodeJWTSession(token: string | null): JWTSession | null {
    if (!token) return null;
    try {
        const parts = token.split('.');
        if (parts.length !== 3) return null;
        const payload = parts[1].replace(/-/g, '+').replace(/_/g, '/');
        const claims = JSON.parse(atob(payload.padEnd(Math.ceil(payload.length / 4) * 4, '=')));
        if (!claims || claims.ver !== 2 || typeof claims.sid !== 'string' || !claims.sid ||
            typeof claims.renewable !== 'boolean' ||
            ![claims.auth_time, claims.iat, claims.nbf, claims.exp].every(Number.isSafeInteger) ||
            claims.auth_time <= 0 || claims.auth_time > claims.iat || claims.iat > claims.nbf ||
            claims.nbf >= claims.exp || claims.exp - claims.iat > 30 * DAY ||
            claims.exp > claims.auth_time + 90 * DAY || !Number.isFinite(new Date(claims.exp * 1000).getTime())) return null;
        return claims as JWTSession;
    } catch {
        return null;
    }
}

export function sameJWTSession(left: JWTSession | null, right: JWTSession | null): boolean {
    return !!left && !!right && left.sid === right.sid && left.auth_time === right.auth_time &&
        left.renewable === right.renewable;
}

export function sameAuthIdentity(left: AuthSnapshot, right: AuthSnapshot | undefined): boolean {
    if (!right || left.sessionVersion !== right.sessionVersion || left.isAPIKeyAuth !== right.isAPIKeyAuth) return false;
    if (left.token === right.token) return true;
    return !left.isAPIKeyAuth && sameJWTSession(decodeJWTSession(left.token), decodeJWTSession(right.token));
}

export function sameAuthCredential(left: AuthSnapshot, right: AuthSnapshot | undefined): boolean {
    return sameAuthIdentity(left, right) && left.token === right?.token;
}

let getAuthStore: (() => AuthSession) | undefined;

export function setAuthStoreGetter(getter: () => AuthSession) {
    getAuthStore = getter;
}

export function getAuthSession(): AuthSession | undefined {
    return getAuthStore?.();
}

export function assertAuthIdentity(expected: AuthSnapshot | undefined) {
    getAuthStore?.().syncAuthFromStorage();
    if (expected && !sameAuthIdentity(expected, getAuthStore?.())) {
        throw new Error('Authentication changed while request was in flight');
    }
}

export function rejectAuthCredential(expected: AuthSnapshot) {
    getAuthStore?.().syncAuthFromStorage();
    const current = getAuthStore?.();
    if (expected.token && current && sameAuthCredential(expected, current)) current.logout();
}

let lastActivity: number | null = null;
let inFlight: { session: AuthSnapshot; promise: Promise<void> } | undefined;
let cooldown: { session: AuthSnapshot; until: number } | undefined;

export async function ensureFreshAuthSession(): Promise<void> {
    getAuthStore?.().syncAuthFromStorage();
    const session = getAuthStore?.();
    if (!session?.token || session.isAPIKeyAuth || !session.isAuthenticated) return;
    const claims = decodeJWTSession(session.token);
    const now = Date.now();
    if (!claims?.renewable || claims.exp * 1000 <= now || claims.exp * 1000 - now >= 7 * DAY * 1000 ||
        claims.exp >= claims.auth_time + 90 * DAY || lastActivity === null ||
        now - lastActivity < 0 || now - lastActivity > ACTIVE_WINDOW_MS ||
        typeof document === 'undefined' || document.visibilityState !== 'visible') return;

    if (inFlight && sameAuthCredential(inFlight.session, session)) return inFlight.promise;
    if (cooldown && sameAuthCredential(cooldown.session, session) && now < cooldown.until) return;

    const flight = { session, promise: Promise.resolve() };
    inFlight = flight;
    flight.promise = refreshAuthSession(session).finally(() => {
        if (inFlight === flight) {
            cooldown = { session, until: Date.now() + REFRESH_COOLDOWN_MS };
            inFlight = undefined;
        }
    });
    return flight.promise;
}

async function refreshAuthSession(session: AuthSession): Promise<void> {
    const controller = new AbortController();
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const timeout = new Promise<never>((_, reject) => {
        timer = setTimeout(() => {
            active = false;
            controller.abort();
            reject(new Error('Authentication renewal timed out'));
        }, REFRESH_TIMEOUT_MS);
    });
    const refresh = async () => {
        const response = await fetch(`${API_BASE_URL}/api/v1/user/refresh`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${session.token}`, 'Content-Type': 'application/json' },
            body: '{}',
            cache: 'no-store',
            signal: controller.signal,
        });
        if (!active) return;
        if (response.status === 401) {
            rejectAuthCredential(session);
            return;
        }
        if (response.status !== 200) return;
        const payload: unknown = await response.json();
        if (!active || !payload || typeof payload !== 'object' || !('data' in payload)) return;
        const data = payload.data;
        if (!data || typeof data !== 'object' || !('token' in data) || typeof data.token !== 'string' ||
            !('expire_at' in data) || typeof data.expire_at !== 'string') return;
        getAuthStore?.().applyRenewedToken({ token: data.token, expire_at: data.expire_at }, session);
    };
    try {
        // Race the entire response, including its body. A late response after
        // timeout must never mutate credentials even if fetch ignores abort.
        await Promise.race([refresh(), timeout]);
    } catch {
        // Transient failures leave the current token usable for the caller.
    } finally {
        active = false;
        clearTimeout(timer);
        controller.abort();
    }
}

export function startAuthSessionListeners(): () => void {
    const onActivity = (event: Event) => {
        if (!event.isTrusted || document.visibilityState !== 'visible') return;
        lastActivity = Date.now();
        void ensureFreshAuthSession();
    };
    const onStorage = (event: StorageEvent) => {
        if (event.key !== AUTH_STORAGE_KEY && event.key !== null) return;
        if (event.storageArea && event.storageArea !== window.localStorage) return;
        getAuthStore?.().syncAuthFromStorage();
        const current = getAuthStore?.();
        if (current?.token && current.isLoading) void current.checkAuth();
    };
    document.addEventListener('pointerdown', onActivity, { passive: true });
    document.addEventListener('keydown', onActivity, { passive: true });
    window.addEventListener('storage', onStorage);
    return () => {
        document.removeEventListener('pointerdown', onActivity);
        document.removeEventListener('keydown', onActivity);
        window.removeEventListener('storage', onStorage);
    };
}
