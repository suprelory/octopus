
'use client';

import { Suspense, useState, useEffect, useRef } from 'react';
import { AnimatePresence } from 'motion/react';
import { getBootstrapStatus, useAuth, useAuthStore } from '@/api/endpoints/user';
import { useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import { logger } from '@/lib/logger';
import { lazyWithPreload } from '@/route/lazy-with-preload';

const LoginForm = lazyWithPreload(() =>
    import('@/components/modules/login').then((module) => ({ default: module.LoginForm }))
);
const BootstrapForm = lazyWithPreload(() =>
    import('@/components/modules/bootstrap').then((module) => ({ default: module.BootstrapForm }))
);
const APIKeyDashboard = lazyWithPreload(() =>
    import('@/components/modules/apikey-dashboard').then((module) => ({ default: module.APIKeyDashboard }))
);
const AuthenticatedApp = lazyWithPreload(() =>
    import('@/components/authenticated-app').then((module) => ({ default: module.AuthenticatedApp }))
);

function AppShellSkeleton() {
    return (
        <div className="mx-auto flex h-dvh max-w-6xl flex-col overflow-hidden px-3 md:grid md:grid-cols-[auto_1fr] md:gap-6 md:px-6">
            <div className="hidden items-center justify-center md:flex md:min-h-screen">
                <div className="flex flex-col gap-4">
                    {Array.from({ length: 7 }, (_, index) => (
                        <div key={index} className="size-10 animate-pulse rounded-2xl bg-muted" />
                    ))}
                </div>
            </div>
            <main className="flex min-h-0 w-full min-w-0 flex-1 flex-col pb-[calc(6rem+env(safe-area-inset-bottom))] md:pb-0">
                <header className="my-6 flex flex-none items-center gap-3 px-2">
                    <div className="size-12 animate-pulse rounded-full bg-muted" />
                    <div className="h-9 w-40 animate-pulse rounded-md bg-muted" />
                </header>
                <div className="min-h-0 flex-1 space-y-4 px-2">
                    <div className="h-12 w-full animate-pulse rounded-xl bg-muted" />
                    <div className="grid gap-4 sm:grid-cols-2">
                        <div className="h-32 animate-pulse rounded-xl bg-muted" />
                        <div className="h-32 animate-pulse rounded-xl bg-muted" />
                    </div>
                    <div className="h-64 animate-pulse rounded-xl bg-muted" />
                </div>
            </main>
        </div>
    );
}

function AuthFormSkeleton() {
    return (
        <div className="min-h-screen flex items-center justify-center px-6">
            <div className="w-full max-w-sm space-y-6">
                <div className="mx-auto size-12 animate-pulse rounded-full bg-muted" />
                <div className="h-10 w-full animate-pulse rounded-xl bg-muted" />
                <div className="h-11 w-full animate-pulse rounded-md bg-muted" />
                <div className="h-11 w-full animate-pulse rounded-md bg-muted" />
                <div className="h-10 w-full animate-pulse rounded-md bg-muted" />
            </div>
        </div>
    );
}

export function AppContainer() {
    const { isAuthenticated, isAPIKeyAuth, isLoading: authLoading } = useAuth();
    const storedToken = useAuthStore((state) => state.token);
    const storedAPIKeyAuth = useAuthStore((state) => state.isAPIKeyAuth);
    const queryClient = useQueryClient();

    const [bootstrapLoading, setBootstrapLoading] = useState(true);
    const [bootstrapRequired, setBootstrapRequired] = useState(false);
    const bootstrapStartedRef = useRef(false);

    useEffect(() => {
        if (!storedToken) return;

        if (storedAPIKeyAuth) {
            void APIKeyDashboard.preload();
            return;
        }

        void AuthenticatedApp.preload();
    }, [storedAPIKeyAuth, storedToken]);

    // 首屏最早的 server-rendered loader：一旦客户端开始渲染，就淡出移除
    useEffect(() => {
        const el = document.getElementById('initial-loader');
        if (!el) return;

        el.classList.add('octo-hide');
        const timer = setTimeout(() => el.remove(), 220);
        return () => clearTimeout(timer);
    }, []);

    useEffect(() => {
        if (authLoading || isAuthenticated) {
            setBootstrapLoading(false);
            setBootstrapRequired(false);
            return;
        }

        let active = true;
        setBootstrapLoading(true);
        getBootstrapStatus()
            .then((status) => {
                if (active) setBootstrapRequired(status.required);
            })
            .catch((error) => {
                logger.warn('administrator bootstrap status check failed:', error);
                if (active) setBootstrapRequired(false);
            })
            .finally(() => {
                if (active) setBootstrapLoading(false);
            });
        return () => {
            active = false;
        };
    }, [authLoading, isAuthenticated]);

    // 后台预取数据 — 不阻塞内容渲染，React Query 缓存就绪后自动触发组件重渲染
    useEffect(() => {
        if (authLoading) return;
        if (!isAuthenticated) return;

        if (bootstrapStartedRef.current) return;
        bootstrapStartedRef.current = true;

        const prefetches: Array<Promise<unknown>> = [];

        // API Key 认证模式：预取 dashboard stats
        if (isAPIKeyAuth) {
            prefetches.push(
                queryClient.prefetchQuery({
                    queryKey: ['apikey', 'dashboard', 'stats'],
                    queryFn: async () => apiClient.get('/api/v1/apikey/stats'),
                })
            );
        }

        // 后台静默运行，不阻塞渲染
        Promise.allSettled(prefetches).catch((e) => {
            logger.warn('bootstrap prefetch failed:', e);
        });
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [authLoading, isAuthenticated]);

    // 认证状态未知时立即展示应用壳，避免固定动画阻塞首屏。
    if (authLoading) {
        return <AppShellSkeleton />;
    }

    // API Key 认证模式 - 显示 API Key Dashboard
    if (isAPIKeyAuth) {
        return (
            <Suspense fallback={<AppShellSkeleton />}>
                <AnimatePresence mode="wait">
                    <APIKeyDashboard key="apikey-dashboard" />
                </AnimatePresence>
            </Suspense>
        );
    }

    if (bootstrapLoading) {
        return <AuthFormSkeleton />;
    }

    if (bootstrapRequired) {
        return (
            <Suspense fallback={<AuthFormSkeleton />}>
                <AnimatePresence mode="wait">
                    <BootstrapForm key="bootstrap" onComplete={() => setBootstrapRequired(false)} />
                </AnimatePresence>
            </Suspense>
        );
    }

    // 登录页面
    if (!isAuthenticated) {
        return (
            <Suspense fallback={<AuthFormSkeleton />}>
                <AnimatePresence mode="wait">
                    <LoginForm key="login" />
                </AnimatePresence>
            </Suspense>
        );
    }

    return (
        <Suspense fallback={<AppShellSkeleton />}>
            <AuthenticatedApp />
        </Suspense>
    );
}
