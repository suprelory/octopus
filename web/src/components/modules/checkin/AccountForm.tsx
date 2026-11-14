import { useState, type FormEvent } from 'react';
import { useTranslations } from 'use-intl';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    type AccountWrite,
    type CheckinAccount,
    type CheckinSite,
    useCheckinMutation,
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
    const [type, setType] = useState<'cookie' | 'bearer'>(account?.credential_type ?? 'cookie');
    const [credential, setCredential] = useState('');
    const [userID, setUserID] = useState(account?.user_id ?? '');
    const [proxyMode, setProxyMode] = useState(account?.proxy_mode ?? 'inherit');
    const [proxyURL, setProxyURL] = useState(account?.proxy_url ?? '');
    const [error, setError] = useState<unknown>(null);
    const save = useCheckinMutation();

    async function submit(event: FormEvent) {
        event.preventDefault();
        setError(null);
        try {
            if (
                account &&
                !credential &&
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
            if (!account || credential)
                body.credentials = {
                    type,
                    user_id: userID,
                    ...(type === 'cookie' ? { cookie: credential } : { access_token: credential }),
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
                            setType(event.target.value as 'cookie' | 'bearer');
                            setCredential('');
                        }}
                    >
                        <option value="cookie">Cookie</option>
                        <option value="bearer">{t('accessToken')}</option>
                    </CheckinSelect>
                </CheckinField>
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
                <CheckinField label={t('userID')} hint={t('userIDHint')}>
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
                <div className="flex justify-end gap-2 border-t pt-4">
                    <Button type="button" variant="outline" onClick={onClose}>
                        {t('cancel')}
                    </Button>
                    <Button type="submit" disabled={save.isPending}>
                        {t('save')}
                    </Button>
                </div>
            </form>
        </CheckinDialog>
    );
}
