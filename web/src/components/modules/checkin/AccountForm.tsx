import { useState, type FormEvent } from 'react';
import { useTranslations } from 'use-intl';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    type AccountWrite,
    type Capability,
    type CredentialType,
    type CheckinAccount,
    type CheckinSite,
    useCheckinMutation,
    useCheckinQuery,
} from '@/api/checkin';
import {
    CheckinDialog,
    CheckinError,
    CheckinField,
    CheckinSelect,
    CheckinTextarea,
    CheckinToggle,
} from './common';

export function AccountForm({
    site,
    account,
    onClose,
}: {
    site: CheckinSite;
    account?: CheckinAccount;
    onClose: () => void;
}) {
    const t = useTranslations('checkin');
    const [name, setName] = useState(account?.name ?? '');
    const [enabled, setEnabled] = useState(account?.enabled ?? true);
    const [automatic, setAutomatic] = useState(account?.auto_checkin ?? false);
    const capabilities = useCheckinQuery<Capability[]>('/capabilities');
    const platform = capabilities.data?.find((item) => item.id === site.config.adapter);
    const availableTypes = platform?.credential_types ?? [];
    const sub2api = platform?.token_refresh ?? false;
    const [chosenType, setType] = useState<CredentialType | ''>(account?.credential_type ?? '');
    const type = chosenType || availableTypes[0] || 'cookie';
    const [credential, setCredential] = useState('');
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [refreshToken, setRefreshToken] = useState('');
    const [expiresAt, setExpiresAt] = useState('');
    const [userID, setUserID] = useState(account?.user_id ?? '');
    const [proxyMode, setProxyMode] = useState(account?.proxy_mode ?? 'inherit');
    const [proxyURL, setProxyURL] = useState(account?.proxy_url ?? '');
    const [error, setError] = useState<unknown>(null);
    const save = useCheckinMutation();

    async function submit(event: FormEvent) {
        event.preventDefault();
        setError(null);
        try {
            const replacing = type === 'password' ? !!(username || password) : !!credential;
            if (type === 'password' && (!account || replacing) && (!username.trim() || !password))
                throw new Error(t('replacePasswordHint'));
            if (!credential && (refreshToken || expiresAt))
                throw new Error(t('replaceSessionHint'));
            if (
                account &&
                !replacing &&
                (type !== account.credential_type || userID !== (account.user_id ?? ''))
            )
                throw new Error(t('replaceCredentialHint'));
            const body: AccountWrite = {
                name,
                enabled,
                auto_checkin: automatic,
                proxy_mode: proxyMode,
                proxy_url: proxyURL,
                revision: account?.revision ?? 0,
                credential_version: account?.credential_version ?? 0,
            };
            if (!account || replacing)
                body.credentials = {
                    type,
                    user_id: userID,
                    ...(type === 'password'
                        ? { username, password }
                        : type === 'cookie'
                          ? { cookie: credential }
                          : { access_token: credential }),
                    ...(sub2api && type === 'bearer'
                        ? {
                              refresh_token: refreshToken,
                              ...(expiresAt
                                  ? { expires_at: new Date(expiresAt).toISOString() }
                                  : {}),
                          }
                        : {}),
                };
            await save.mutateAsync({
                path: account ? `/accounts/${account.id}` : `/sites/${site.id}/accounts`,
                method: account ? 'PATCH' : 'POST',
                body,
            });
            onClose();
        } catch (error) {
            setError(error);
        }
    }

    return (
        <CheckinDialog
            title={t(account ? 'editAccount' : 'addAccount')}
            description={site.config.name}
            onClose={onClose}
        >
            <form
                onSubmit={(event) => {
                    void submit(event);
                }}
                className="space-y-5"
            >
                <CheckinField label={t('accountName')}>
                    <Input
                        required
                        value={name}
                        onChange={(event) => setName(event.target.value)}
                    />
                </CheckinField>
                <div className="flex flex-wrap gap-4">
                    <CheckinToggle
                        label={t('accountEnabled')}
                        checked={enabled}
                        onChange={setEnabled}
                    />
                    <CheckinToggle
                        label={t('autoCheckin')}
                        checked={automatic}
                        onChange={setAutomatic}
                    />
                </div>
                <p className="text-xs text-muted-foreground">
                    {t('autoHint', {
                        timezone: site.config.timezone,
                        start: site.config.window_start,
                        end: site.config.window_end,
                    })}
                </p>
                <CheckinField label={t('credentialType')}>
                    <CheckinSelect
                        value={type}
                        onChange={(event) => {
                            setType(event.target.value as CredentialType);
                            setCredential('');
                            setUsername('');
                            setPassword('');
                            setRefreshToken('');
                            setExpiresAt('');
                        }}
                    >
                        {availableTypes.map((value) => (
                            <option key={value} value={value}>
                                {value === 'cookie'
                                    ? 'Cookie'
                                    : t(value === 'password' ? 'usernamePassword' : 'accessToken')}
                            </option>
                        ))}
                    </CheckinSelect>
                </CheckinField>
                {type === 'password' ? (
                    <>
                        <CheckinField
                            label={t('username')}
                            hint={t(account ? 'loginCredentialsSaved' : 'loginCredentialsHint')}
                        >
                            <Input
                                required={!account || !!password}
                                autoComplete="off"
                                value={username}
                                onChange={(event) => setUsername(event.target.value)}
                                placeholder={account ? '••••••••' : ''}
                            />
                        </CheckinField>
                        <CheckinField label={t('password')}>
                            <Input
                                type="password"
                                required={!account || !!username}
                                autoComplete="new-password"
                                value={password}
                                onChange={(event) => setPassword(event.target.value)}
                                placeholder={account ? '••••••••' : ''}
                            />
                        </CheckinField>
                        <p className="text-xs text-muted-foreground">
                            {t(
                                platform?.execution === 'login'
                                    ? 'agentrouterAccountHint'
                                    : 'anyrouterAccountHint',
                            )}
                        </p>
                    </>
                ) : (
                    <CheckinField
                        label={type === 'cookie' ? 'Cookie' : t('accessToken')}
                        hint={t(account ? 'credentialSaved' : 'credentialHint')}
                    >
                        {type === 'cookie' ? (
                            <CheckinTextarea
                                required={!account}
                                autoComplete="off"
                                spellCheck={false}
                                value={credential}
                                onChange={(event) => setCredential(event.target.value)}
                                placeholder={account ? '••••••••' : 'session=...'}
                            />
                        ) : (
                            <Input
                                type="password"
                                required={!account}
                                autoComplete="new-password"
                                value={credential}
                                onChange={(event) => setCredential(event.target.value)}
                                placeholder={account ? '••••••••' : ''}
                            />
                        )}
                    </CheckinField>
                )}
                {sub2api && type === 'bearer' && (
                    <>
                        <CheckinField label={t('refreshToken')} hint={t('refreshTokenHint')}>
                            <Input
                                type="password"
                                autoComplete="new-password"
                                value={refreshToken}
                                onChange={(event) => setRefreshToken(event.target.value)}
                                placeholder={account?.has_refresh_token ? '••••••••' : ''}
                            />
                        </CheckinField>
                        <CheckinField label={t('tokenExpiry')} hint={t('tokenExpiryHint')}>
                            <Input
                                type="datetime-local"
                                value={expiresAt}
                                onChange={(event) => setExpiresAt(event.target.value)}
                            />
                        </CheckinField>
                        <p className="text-xs text-muted-foreground">
                            {t('sessionCredentialHint')}
                        </p>
                    </>
                )}
                <CheckinField
                    label={t('userID')}
                    hint={t(
                        site.config.adapter === 'anyrouter' || site.config.adapter === 'agentrouter'
                            ? 'routerUserIDHint'
                            : 'userIDHint',
                    )}
                >
                    <Input value={userID} onChange={(event) => setUserID(event.target.value)} />
                </CheckinField>
                <div className="grid gap-3 sm:grid-cols-2">
                    <CheckinField label={t('proxy')}>
                        <CheckinSelect
                            value={proxyMode}
                            onChange={(event) =>
                                setProxyMode(event.target.value as CheckinAccount['proxy_mode'])
                            }
                        >
                            <option value="inherit">{t('inherit')}</option>
                            <option value="direct">{t('direct')}</option>
                            <option value="custom">{t('custom')}</option>
                        </CheckinSelect>
                    </CheckinField>
                    {proxyMode === 'custom' && (
                        <CheckinField label={t('proxyURL')} hint={t('directHint')}>
                            <Input
                                required
                                value={proxyURL}
                                onChange={(event) => setProxyURL(event.target.value)}
                            />
                        </CheckinField>
                    )}
                </div>
                <CheckinError error={error} />
                <CheckinError error={capabilities.error} />
                <div className="flex justify-end gap-2 border-t pt-4">
                    <Button type="button" variant="outline" onClick={onClose}>
                        {t('cancel')}
                    </Button>
                    <Button type="submit" disabled={save.isPending || !availableTypes.length}>
                        {t('save')}
                    </Button>
                </div>
            </form>
        </CheckinDialog>
    );
}
