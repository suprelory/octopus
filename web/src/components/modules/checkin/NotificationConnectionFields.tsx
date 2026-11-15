import { useTranslations } from 'use-intl';
import type { NotificationConnection, NotificationKind } from '@/api/checkin';
import { Input } from '@/components/ui/input';
import { CheckinField, CheckinSelect, CheckinTextarea } from './common';

export function NotificationConnectionFields({
    kind,
    value,
    onChange,
    headers,
    onHeaders,
}: {
    kind: NotificationKind;
    value: NotificationConnection;
    onChange: (value: NotificationConnection) => void;
    headers: string;
    onHeaders: (value: string) => void;
}) {
    const t = useTranslations('checkin.notifications');
    const update = (patch: Partial<NotificationConnection>) => onChange({ ...value, ...patch });
    const field = (
        key: 'url' | 'token' | 'chat_id' | 'send_key' | 'secret' | 'base_url' | 'topic',
        required = true,
        secret = false,
        placeholder = '',
    ) => (
        <CheckinField label={t(`fields.${key}`)} hint={!required ? t('optional') : undefined}>
            <Input
                required={required}
                type={secret ? 'password' : 'text'}
                autoComplete={secret ? 'new-password' : 'off'}
                value={value[key] ?? ''}
                maxLength={key === 'url' || key === 'base_url' ? 4096 : 8192}
                placeholder={placeholder}
                onChange={(event) => update({ [key]: event.target.value })}
            />
        </CheckinField>
    );
    const smtp = value.smtp ?? {
        host: '',
        port: 587,
        tls: 'starttls',
        username: '',
        password: '',
        from: '',
        to: [],
    };
    const updateSMTP = (patch: Partial<NonNullable<NotificationConnection['smtp']>>) =>
        update({ smtp: { ...smtp, ...patch } });
    return (
        <div className="space-y-4">
            {['webhook', 'bark', 'feishu', 'dingtalk', 'wecom'].includes(kind) &&
                field('url', true, true, 'https://…')}
            {(kind === 'feishu' || kind === 'dingtalk') && field('secret', false, true)}
            {kind === 'telegram' && (
                <>
                    {field('token', true, true, '123456:…')}
                    {field('chat_id', true, false, '-1001234567890')}
                    {field('base_url', false, false, 'https://api.telegram.org')}
                </>
            )}
            {kind === 'serverchan' && (
                <>
                    {field('send_key', true, true)}
                    <p className="text-xs text-muted-foreground">{t('serverchanHint')}</p>
                </>
            )}
            {kind === 'ntfy' && (
                <>
                    {field('base_url', false, false, 'https://ntfy.sh')}
                    {field('topic')}
                    {field('token', false, true)}
                    <CheckinField label={t('fields.priority')}>
                        <CheckinSelect
                            value={value.priority ?? 3}
                            onChange={(event) => update({ priority: Number(event.target.value) })}
                        >
                            {[1, 2, 3, 4, 5].map((priority) => (
                                <option key={priority} value={priority}>
                                    {priority}
                                </option>
                            ))}
                        </CheckinSelect>
                    </CheckinField>
                </>
            )}
            {kind === 'webhook' && (
                <details className="rounded-xl border p-3">
                    <summary className="cursor-pointer text-sm font-medium">
                        {t('webhookAdvanced')}
                    </summary>
                    <div className="mt-4 space-y-4">
                        <CheckinField label={t('fields.headers')} hint={t('headersHint')}>
                            <CheckinTextarea
                                value={headers}
                                maxLength={16384}
                                placeholder={'{"Authorization": "Bearer …"}'}
                                onChange={(event) => onHeaders(event.target.value)}
                            />
                        </CheckinField>
                        <CheckinField label={t('fields.payload')} hint={t('payloadHint')}>
                            <CheckinTextarea
                                value={value.payload ?? ''}
                                maxLength={16384}
                                placeholder={'{"title": "{{title}}", "message": "{{message}}"}'}
                                onChange={(event) => update({ payload: event.target.value })}
                            />
                        </CheckinField>
                    </div>
                </details>
            )}
            {kind === 'smtp' && (
                <>
                    <div className="grid gap-4 sm:grid-cols-2">
                        <CheckinField label={t('fields.host')}>
                            <Input
                                required
                                value={smtp.host}
                                autoComplete="off"
                                onChange={(event) => updateSMTP({ host: event.target.value })}
                            />
                        </CheckinField>
                        <CheckinField label={t('fields.port')}>
                            <Input
                                required
                                type="number"
                                min={1}
                                max={65535}
                                value={smtp.port}
                                onChange={(event) =>
                                    updateSMTP({ port: Number(event.target.value) })
                                }
                            />
                        </CheckinField>
                        <CheckinField label={t('fields.tls')}>
                            <CheckinSelect
                                value={smtp.tls}
                                onChange={(event) => {
                                    const tls = event.target.value as typeof smtp.tls;
                                    updateSMTP({
                                        tls,
                                        port: tls === 'tls' ? 465 : tls === 'none' ? 25 : 587,
                                    });
                                }}
                            >
                                <option value="starttls">STARTTLS</option>
                                <option value="tls">TLS</option>
                                <option value="none">{t('noTLS')}</option>
                            </CheckinSelect>
                        </CheckinField>
                        <CheckinField label={t('fields.username')} hint={t('optional')}>
                            <Input
                                value={smtp.username}
                                autoComplete="off"
                                onChange={(event) => updateSMTP({ username: event.target.value })}
                            />
                        </CheckinField>
                        <CheckinField label={t('fields.password')} hint={t('optional')}>
                            <Input
                                type="password"
                                value={smtp.password}
                                autoComplete="new-password"
                                onChange={(event) => updateSMTP({ password: event.target.value })}
                            />
                        </CheckinField>
                        <CheckinField label={t('fields.from')}>
                            <Input
                                required
                                value={smtp.from}
                                autoComplete="off"
                                placeholder="Octopus <notify@example.com>"
                                onChange={(event) => updateSMTP({ from: event.target.value })}
                            />
                        </CheckinField>
                    </div>
                    <CheckinField label={t('fields.to')} hint={t('recipientsHint')}>
                        <CheckinTextarea
                            required
                            className="font-sans"
                            value={smtp.to.join('\n')}
                            onChange={(event) => updateSMTP({ to: event.target.value.split('\n') })}
                        />
                    </CheckinField>
                    <p className="text-xs text-muted-foreground">{t('smtpHint')}</p>
                </>
            )}
        </div>
    );
}
