'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { useBootstrapUser, useLogin } from '@/api/endpoints/user';
import { AuthShell } from '@/components/common/AuthShell';
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
        <AuthShell title={t('title')} description={t('description')}>
            <form onSubmit={handleSubmit} className="space-y-5">
                <Field>
                    <FieldLabel htmlFor="bootstrap-username">{t('username')}</FieldLabel>
                    <Input
                        id="bootstrap-username"
                        autoComplete="username"
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
                        autoComplete="new-password"
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
                        autoComplete="new-password"
                        type="password"
                        value={confirmPassword}
                        onChange={(event) => setConfirmPassword(event.target.value)}
                        required
                        minLength={MIN_PASSWORD_LENGTH}
                        disabled={isPending}
                    />
                </Field>

                {error && <FieldDescription role="alert" className="rounded-xl border border-destructive/20 bg-destructive/5 p-3 text-destructive">{error}</FieldDescription>}
                <Button type="submit" className="h-10 w-full rounded-xl" disabled={isPending || !username.trim() || !token.trim()}>
                    {isPending ? t('button.loading') : t('button.submit')}
                </Button>
            </form>
        </AuthShell>
    );
}
