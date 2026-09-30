'use client';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { useAuthStore } from '@/api/endpoints/user';
import { registerSessionQueryClient } from '@/api/query-session';

export default function QueryProvider({ children }: { children: React.ReactNode }) {
    const sessionVersion = useAuthStore((state) => state.sessionVersion);
    return <SessionQueryProvider key={sessionVersion}>{children}</SessionQueryProvider>;
}

function SessionQueryProvider({ children }: { children: React.ReactNode }) {
    const [queryClient] = useState(
        () =>
            new QueryClient({
                defaultOptions: {
                    queries: {
                        staleTime: 60 * 1000,
                        refetchOnWindowFocus: false,
                    },
                },
            })
    );

    useEffect(() => registerSessionQueryClient(queryClient), [queryClient]);

    return (
        <QueryClientProvider client={queryClient}>
            {children}
        </QueryClientProvider>
    );
}
