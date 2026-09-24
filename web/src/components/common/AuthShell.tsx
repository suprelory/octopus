import type { ReactNode } from 'react';
import Logo from '@/components/modules/logo';

export function AuthShell({ title, description, children }: { title: string; description: string; children: ReactNode }) {
    return (
        <main className="flex min-h-dvh items-center justify-center px-4 py-10 text-foreground">
            <div className="w-full max-w-md space-y-6">
                <div className="flex items-center justify-center gap-3"><Logo size={44} /><span className="text-2xl font-semibold tracking-tight">Octopus</span></div>
                <section className="page-card p-6 sm:p-8">
                    <header className="mb-6">
                        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
                        <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{description}</p>
                    </header>
                    {children}
                </section>
            </div>
        </main>
    );
}
