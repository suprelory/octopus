import { useEffect } from 'react';
import { useMutation } from '@tanstack/react-query';
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { apiClient, setAuthStoreGetter } from '../client';
import { logger } from '@/lib/logger';
import { clearSessionQueries } from '../query-session';
import {
    AUTH_STORAGE_KEY, decodeJWTSession, sameAuthCredential, sameAuthIdentity,
    sameJWTSession, startAuthSessionListeners, type AuthSnapshot, type RenewedToken,
} from '../auth-session';

/**
 * 用户登录请求
 */
export interface UserLoginRequest {
    username: string;
    password: string;
    expire: number; // -1 为可续期登录；0 使用默认有效期，正数指定固定分钟数
}

/**
 * 用户登录响应
 */
export interface UserLoginResponse {
    token: string;
    expire_at: string; // ISO 8601 格式
}

export interface UserBootstrapRequest {
    username: string;
    password: string;
    token: string;
}

export interface UserBootstrapStatus {
    required: boolean;
}

/**
 * 修改密码请求
 */
export interface ChangePasswordRequest {
    old_password: string;
    new_password: string;
}

/**
 * 修改用户名请求
 */
export interface ChangeUsernameRequest {
    new_username: string;
}

/**
 * 认证状态 Store
 */
interface AuthState {
    sessionVersion: number;
    isAuthenticated: boolean;
    isLoading: boolean;
    authUnavailable: boolean;
    isAPIKeyAuth: boolean;
    token: string | null;
    expireAt: string | null;

    // Actions
    setAuth: (token: string, expireAt: string) => void;
    setAPIKeyAuth: (apiKey: string) => void;
    applyRenewedToken: (response: RenewedToken, expected: AuthSnapshot) => boolean;
    syncAuthFromStorage: () => void;
    checkAuth: () => Promise<void>;
    logout: () => void;
}

type StoredAuth = Pick<AuthState, 'token' | 'expireAt' | 'isAPIKeyAuth'>;

function readStoredAuth(): StoredAuth | undefined {
    if (typeof localStorage === 'undefined') return undefined;
    try {
        const raw = localStorage.getItem(AUTH_STORAGE_KEY);
        if (!raw) return { token: null, expireAt: null, isAPIKeyAuth: false };
        const state = JSON.parse(raw)?.state;
        if (!state || (state.token !== null && typeof state.token !== 'string') ||
            (state.expireAt !== null && typeof state.expireAt !== 'string') ||
            typeof state.isAPIKeyAuth !== 'boolean') return undefined;
        return { token: state.token, expireAt: state.expireAt, isAPIKeyAuth: state.isAPIKeyAuth };
    } catch {
        // Storage may be unavailable. Keep the in-memory login in that case.
        return undefined;
    }
}

/**
 * 认证状态管理 Store（使用 zustand + persist）
 */
export const useAuthStore = create<AuthState>()(
    persist(
        (set, get) => ({
            sessionVersion: 0,
            isAuthenticated: false,
            isLoading: true,
            authUnavailable: false,
            isAPIKeyAuth: false,
            token: null,
            expireAt: null,

            setAuth: (token: string, expireAt: string) => {
                clearSessionQueries();
                set({
                    sessionVersion: get().sessionVersion + 1,
                    isAuthenticated: true,
                    isAPIKeyAuth: false,
                    token,
                    expireAt,
                    isLoading: false,
                    authUnavailable: false,
                });
            },

            setAPIKeyAuth: (apiKey: string) => {
                clearSessionQueries();
                set({
                    sessionVersion: get().sessionVersion + 1,
                    isAuthenticated: true,
                    isAPIKeyAuth: true,
                    token: apiKey,
                    expireAt: null,
                    isLoading: false,
                    authUnavailable: false,
                });
            },

            applyRenewedToken: (response, expected) => {
                const current = get();
                if (!sameAuthCredential(expected, current) || current.isAPIKeyAuth) return false;
                const previous = decodeJWTSession(current.token);
                const renewed = decodeJWTSession(response.token);
                if (!previous?.renewable || !renewed || !sameJWTSession(previous, renewed) ||
                    renewed.exp <= previous.exp || renewed.iat < previous.iat ||
                    Date.parse(response.expire_at) !== renewed.exp * 1000) return false;

                // A storage event can still be queued when a refresh completes.
                // Consult persisted credentials before writing, including logout
                // and a newer login from another tab.
                const stored = readStoredAuth();
                if (stored) {
                    const persisted = !stored.isAPIKeyAuth ? decodeJWTSession(stored.token) : null;
                    if (!sameJWTSession(previous, persisted) ||
                        (persisted && (persisted.exp > previous.exp ||
                            (persisted.exp === previous.exp && persisted.iat > previous.iat)))) {
                        get().syncAuthFromStorage();
                        return false;
                    }
                }
                set({ token: response.token, expireAt: response.expire_at });
                return true;
            },

            syncAuthFromStorage: () => {
                const stored = readStoredAuth();
                const current = get();
                if (!stored || (stored.token === current.token && stored.isAPIKeyAuth === current.isAPIKeyAuth)) return;
                const previous = !current.isAPIKeyAuth ? decodeJWTSession(current.token) : null;
                const incoming = !stored.isAPIKeyAuth ? decodeJWTSession(stored.token) : null;
                if (sameJWTSession(previous, incoming) && previous && incoming) {
                    if (incoming.iat >= previous.iat && (incoming.exp > previous.exp ||
                        (incoming.exp === previous.exp && incoming.iat > previous.iat))) {
                        set({ token: stored.token, expireAt: new Date(incoming.exp * 1000).toISOString() });
                    }
                    return;
                }
                clearSessionQueries();
                set({
                    ...stored,
                    sessionVersion: current.sessionVersion + 1,
                    isAuthenticated: false,
                    isLoading: !!stored.token,
                    authUnavailable: false,
                });
            },

            checkAuth: async () => {
                get().syncAuthFromStorage();
                const expected = get();
                const { token, isAPIKeyAuth } = expected;

                if (!token) {
                    set({ isAuthenticated: false, isLoading: false, authUnavailable: false });
                    return;
                }

                set({ isLoading: true, authUnavailable: false });

                try {
                    // Browser clocks only schedule renewal; the server decides
                    // expiry for both JWT and API key credentials.
                    const endpoint = isAPIKeyAuth ? '/api/v1/apikey/login' : '/api/v1/user/status';
                    await apiClient.get<unknown>(endpoint, undefined, { signal: AbortSignal.timeout(10_000), cache: 'no-store' });
                    get().syncAuthFromStorage();
                    if (!sameAuthIdentity(expected, get())) return;
                    set({ isAuthenticated: true, isLoading: false, authUnavailable: false });
                } catch (error) {
                    get().syncAuthFromStorage();
                    if (!sameAuthIdentity(expected, get())) return;
                    if (get().token !== expected.token) {
                        await get().checkAuth();
                        return;
                    }
                    logger.error('认证验证失败:', error);
                    set({ isAuthenticated: false, isLoading: false, authUnavailable: true });
                }
            },

            logout: () => {
                clearSessionQueries();
                set({
                    sessionVersion: get().sessionVersion + 1,
                    isAuthenticated: false,
                    isAPIKeyAuth: false,
                    token: null,
                    expireAt: null,
                    isLoading: false,
                    authUnavailable: false,
                });
            }
        }),
        {
            name: AUTH_STORAGE_KEY,
            partialize: (state) => ({
                token: state.token,
                expireAt: state.expireAt,
                isAPIKeyAuth: state.isAPIKeyAuth,
            })
        }
    )
);

// 注册 auth store getter 到 apiClient
if (typeof window !== 'undefined') {
    setAuthStoreGetter(() => useAuthStore.getState());
}

/**
 * 用户登录 Hook
 * 
 * @example
 * const login = useLogin();
 * login.mutate({ username: 'admin', password: 'strong-password', expire: -1 });
 * 
 * if (login.isPending) return <Loading />;
 * if (login.isError) return <Error message={login.error.message} />;
 */
export function useLogin() {
    const { setAuth } = useAuthStore();

    return useMutation({
        mutationFn: async (data: UserLoginRequest) => {
            return apiClient.post<UserLoginResponse>('/api/v1/user/login', data);
        },
        onSuccess: (data) => {
            // 保存到 zustand store
            setAuth(data.token, data.expire_at);
        },
        onError: (error) => {
            logger.error('登录失败:', error);
        },
    });
}

export async function getBootstrapStatus(): Promise<UserBootstrapStatus> {
    return apiClient.get<UserBootstrapStatus>('/api/v1/user/bootstrap');
}

export function useBootstrapUser() {
    return useMutation({
        mutationFn: async (data: UserBootstrapRequest) => {
            return apiClient.post<string>('/api/v1/user/bootstrap', data);
        },
        onError: (error) => {
            logger.error('管理员初始化失败:', error);
        },
    });
}

/**
 * 修改密码 Hook
 * 
 * @example
 * const changePassword = useChangePassword();
 * changePassword.mutate({ oldPassword: '123', newPassword: '456' });
 */
export function useChangePassword() {
    return useMutation({
        mutationFn: async (data: { oldPassword: string; newPassword: string }) => {
            const payload: ChangePasswordRequest = {
                old_password: data.oldPassword,
                new_password: data.newPassword,
            };
            return apiClient.post<string>('/api/v1/user/change-password', payload);
        },
        onSuccess: (message) => {
            logger.log('密码修改成功:', message);
            useAuthStore.getState().logout();
        },
        onError: (error) => {
            logger.error('密码修改失败:', error);
        },
    });
}

/**
 * 修改用户名 Hook
 * 
 * @example
 * const changeUsername = useChangeUsername();
 * changeUsername.mutate({ newUsername: 'newname' });
 */
export function useChangeUsername() {
    return useMutation({
        mutationFn: async (data: { newUsername: string }) => {
            const payload: ChangeUsernameRequest = {
                new_username: data.newUsername,
            };
            return apiClient.post<string>('/api/v1/user/change-username', payload);
        },
        onSuccess: (message) => {
            logger.log('用户名修改成功:', message);
        },
        onError: (error) => {
            logger.error('用户名修改失败:', error);
        },
    });
}

/**
 * 认证状态和方法 Hook
 * 
 * @example
 * const auth = useAuth();
 * 
 * if (auth.isAuthenticated) {
 *   // 已登录
 * }
 * 
 * auth.logout(); // 登出
 */
export function useAuth() {
    const store = useAuthStore();
    const { checkAuth, isLoading } = store;

    useEffect(() => startAuthSessionListeners(), []);

    // 只在首次挂载时检查认证状态
    useEffect(() => {
        if (isLoading) {
            checkAuth();
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []); // 有意只在挂载时执行一次

    return {
        isAuthenticated: store.isAuthenticated,
        isAPIKeyAuth: store.isAPIKeyAuth,
        isLoading: store.isLoading,
        authUnavailable: store.authUnavailable,
        retryAuth: store.checkAuth,
        logout: store.logout,
    };
}
