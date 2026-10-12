import { translateApiErrorCode } from './error-i18n';
import type { ApiError, ApiErrorParams } from './types';
import { HttpStatus } from './types';
import { API_BASE_URL } from './config';
import { assertAuthIdentity, ensureFreshAuthSession, getAuthSession, rejectAuthCredential } from './auth-session';

export { API_BASE_URL } from './config';
export { setAuthStoreGetter } from './auth-session';

const publicPaths = new Set(['/api/v1/user/login', '/api/v1/user/bootstrap']);

// All authenticated transports share renewal and identity guards. Parsing is
// inside the guard so a slow upload/download body cannot escape into a new login.
export async function authenticatedFetch<T = Response>(
    path: string,
    init: RequestInit = {},
    readResponse: (response: Response) => Promise<T> = async (response) => response as T,
): Promise<T> {
    const expected = getAuthSession();
    const anonymous = publicPaths.has(path.split('?')[0]);
    assertAuthIdentity(expected);
    if (!anonymous) await ensureFreshAuthSession();
    assertAuthIdentity(expected);
    const session = getAuthSession();
    const headers = new Headers(init.headers);
    if (!anonymous && session?.token) headers.set('Authorization', `Bearer ${session.token}`);
    else headers.delete('Authorization');

    const response = await fetch(`${API_BASE_URL}${path}`, { ...init, headers });
    assertAuthIdentity(expected);
    if (!anonymous && response.status === HttpStatus.UNAUTHORIZED && session) rejectAuthCredential(session);
    const result = await readResponse(response);
    if (response.status !== HttpStatus.UNAUTHORIZED) assertAuthIdentity(expected);
    return result;
}

function isApiErrorParams(value: unknown): value is ApiErrorParams {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
        return false;
    }
    return Object.values(value).every((item) => (
        item === null ||
        item === undefined ||
        typeof item === 'string' ||
        typeof item === 'number' ||
        typeof item === 'boolean'
    ));
}

/**
 * 处理响应
 */
async function handleResponse<T>(response: Response): Promise<T> {
    if (response.status === 204) return undefined as T;
    const contentType = response.headers.get('content-type');
    const isJson = contentType?.includes('application/json');

    let data: unknown;
    if (isJson) {
        data = await response.json();
    } else {
        data = await response.text();
    }

    if (!response.ok) {
        const rawMessage = (data && typeof data === 'object' && 'message' in data && typeof data.message === 'string')
            ? data.message
            : (typeof data === 'string' ? data : response.statusText);
        const errorCode = (data && typeof data === 'object' && 'error_code' in data && typeof data.error_code === 'string')
            ? data.error_code
            : undefined;
        const errorParams = (data && typeof data === 'object' && 'params' in data && isApiErrorParams(data.params))
            ? data.params
            : undefined;
        const error: ApiError = {
            code: response.status,
            errorCode,
            rawMessage,
            params: errorParams,
            message: translateApiErrorCode(errorCode, rawMessage, errorParams),
        };

        console.error('API Error:', error);
        throw error;
    }

    // 如果是标准的 ApiResponse 格式，返回 data 字段
    if (data && typeof data === 'object' && 'data' in data) {
        return data.data as T;
    }

    return data as T;
}

/**
 * 发送请求
 */
async function request<T>(
    method: string,
    path: string,
    body?: BodyInit,
    params?: Record<string, string | number | boolean>,
    options?: Pick<RequestInit, 'signal' | 'cache'>,
): Promise<T> {
    // 构建 URL
    const searchParams = params ? new URLSearchParams(
        Object.entries(params).map(([k, v]) => [k, String(v)])
    ).toString() : '';
    const url = `${path}${searchParams ? `?${searchParams}` : ''}`;

    // 构建请求头
    const headers = new Headers();

    // 只在有 body 时设置 Content-Type
    if (body) {
        headers.set('Content-Type', 'application/json');
    }

    return authenticatedFetch(url, {
        ...options,
        method,
        headers,
        body,
    }, handleResponse<T>);
}

/**
 * API 客户端 - 基础 HTTP 方法
 */
export const apiClient = {
    /**
     * GET 请求
     */
    get: <T>(path: string, params?: Record<string, string | number | boolean>, options?: Pick<RequestInit, 'signal' | 'cache'>): Promise<T> =>
        request<T>('GET', path, undefined, params, options),

    /**
     * POST 请求
     */
    post: <T>(path: string, data?: unknown, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('POST', path, data ? JSON.stringify(data) : undefined, params),

    /**
     * PUT 请求
     */
    put: <T>(path: string, data?: unknown, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('PUT', path, data ? JSON.stringify(data) : undefined, params),

    /**
     * DELETE 请求
     */
    delete: <T>(path: string, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('DELETE', path, undefined, params),

    /**
     * PATCH 请求
     */
    patch: <T>(path: string, data?: unknown, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('PATCH', path, data ? JSON.stringify(data) : undefined, params),
};
