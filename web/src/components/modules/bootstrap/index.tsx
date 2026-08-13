'use client';

import { useState } from 'react';
import { motion } from 'motion/react';
import { useTranslations } from 'next-intl';
import { ShieldCheck } from 'lucide-react';
import { useBootstrapUser, useLogin } from '@/api/endpoints/user';
import Logo from '@/components/modules/logo';
import { Button } from '@/components/ui/button';
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';

const MIN_PASSWORD_LENGTH = 12;

export function BootstrapForm({ onComplete }: { onComplete?: () => void }) {
    const t = useTranslations('bootstrap');
    const [username, setUsername] = useState('admin');
    const [token, setToken] = useState('');
    const [password, setPassword] = useState('');
    const [confirmPassword, setConfirmPassword] = useState('');
    const [error, setError] = useState<string | null>(null);
    const bootstrap = useBootstrapUser();
    const login = useLogin();

    const handleSubmit = async (event: React.FormEvent) => {
        event.preventDefault();
        setError(null);

        if (Array.from(password).length < MIN_PASSWORD_LENGTH) {
            setError(t('error.tooShort', { length: MIN_PASSWORD_LENGTH }));
            return;
        }
        if (password !== confirmPassword) {
            setError(t('error.mismatch'));
            return;
        }

        try {
            await bootstrap.mutateAsync({ username: username.trim(), password, token: token.trim() });
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : t('error.generic'));
            return;
        }

        try {
            await login.mutateAsync({ username: username.trim(), password, expire: 1440 });
        } catch {
            // useLogin already records the error; fall back to the normal login form.
        } finally {
            // The bootstrap token is consumed once the account is created. If
            // automatic login fails, leave setup and show the normal login form.
            onComplete?.();
        }
    };

    const isPending = bootstrap.isPending || login.isPending;

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="min-h-screen flex items-center justify-center px-6 text-foreground"
        >
            <form onSubmit={handleSubmit} className="w-full max-w-sm space-y-7">
                <header className="flex flex-col items-center gap-3 text-center">
                    <Logo size={48} />
                    <div className="flex items-center gap-2">
                        <ShieldCheck className="size-5" />
                        <h1 className="text-2xl font-bold">{t('title')}</h1>
                    </div>
                    <p className="text-sm text-muted-foreground">{t('description')}</p>
                </header>

                <Field>
                    <FieldLabel htmlFor="bootstrap-username">{t('username')}</FieldLabel>
                    <Input
                        id="bootstrap-username"
                        value={username}
                        onChange={(event) => setUsername(event.target.value)}
                        required
                        disabled={isPending}
                    />
                </Field>
                <Field>
                    <FieldLabel htmlFor="bootstrap-token">{t('token')}</FieldLabel>
                    <Input
                        id="bootstrap-token"
                        value={token}
                        onChange={(event) => setToken(event.target.value)}
                        required
                        autoComplete="one-time-code"
                        disabled={isPending}
                    />
                    <FieldDescription>{t('tokenDescription')}</FieldDescription>
                </Field>
                <Field>
                    <FieldLabel htmlFor="bootstrap-password">{t('password')}</FieldLabel>
                    <Input
                        id="bootstrap-password"
                        type="password"
                        value={password}
                        onChange={(event) => setPassword(event.target.value)}
                        required
                        minLength={MIN_PASSWORD_LENGTH}
                        disabled={isPending}
                    />
                </Field>
                <Field>
                    <FieldLabel htmlFor="bootstrap-password-confirm">{t('confirmPassword')}</FieldLabel>
                    <Input
                        id="bootstrap-password-confirm"
                        type="password"
                        value={confirmPassword}
                        onChange={(event) => setConfirmPassword(event.target.value)}
                        required
                        minLength={MIN_PASSWORD_LENGTH}
                        disabled={isPending}
                    />
                </Field>

                {error && <FieldDescription className="text-destructive">{error}</FieldDescription>}
                <Button type="submit" className="w-full" disabled={isPending || !username.trim() || !token.trim()}>
                    {isPending ? t('button.loading') : t('button.submit')}
                </Button>
            </form>
        </motion.div>
    );
}
