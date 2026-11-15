import { useTranslations } from 'use-intl';
import { Input } from '@/components/ui/input';
import { type TurnstileSiteOptions } from '@/api/checkin';
import { CheckinField, CheckinSelect } from './common';

export function TurnstileSiteFields({
    value,
    onChange,
}: {
    value?: TurnstileSiteOptions;
    onChange: (value: TurnstileSiteOptions) => void;
}) {
    const t = useTranslations('checkin');
    const options: TurnstileSiteOptions = value ?? {
        site_key: '',
        page_path: '/',
        action: '',
        cdata: '',
        token_location: 'query',
        token_field: 'turnstile',
    };
    return (
        <details className="rounded-2xl border p-4">
            <summary className="cursor-pointer text-sm font-semibold">
                {t('turnstile.siteTitle')}
            </summary>
            <div className="mt-4 space-y-3">
                <p className="text-xs text-muted-foreground">{t('turnstile.siteHint')}</p>
                <div className="grid gap-3 sm:grid-cols-2">
                    <CheckinField label={t('turnstile.siteKey')} hint={t('turnstile.siteKeyHint')}>
                        <Input
                            value={options.site_key}
                            placeholder="0x4AAAA…"
                            maxLength={256}
                            onChange={(event) =>
                                onChange({ ...options, site_key: event.target.value })
                            }
                        />
                    </CheckinField>
                    <CheckinField
                        label={t('turnstile.pagePath')}
                        hint={t('turnstile.pagePathHint')}
                    >
                        <Input
                            value={options.page_path}
                            placeholder="/"
                            maxLength={2048}
                            onChange={(event) =>
                                onChange({ ...options, page_path: event.target.value })
                            }
                        />
                    </CheckinField>
                    <CheckinField label={t('turnstile.action')}>
                        <Input
                            value={options.action}
                            maxLength={32}
                            onChange={(event) =>
                                onChange({ ...options, action: event.target.value })
                            }
                        />
                    </CheckinField>
                    <CheckinField label={t('turnstile.cdata')}>
                        <Input
                            value={options.cdata}
                            maxLength={255}
                            onChange={(event) =>
                                onChange({ ...options, cdata: event.target.value })
                            }
                        />
                    </CheckinField>
                    <CheckinField label={t('turnstile.tokenLocation')}>
                        <CheckinSelect
                            value={options.token_location}
                            onChange={(event) =>
                                onChange({
                                    ...options,
                                    token_location: event.target
                                        .value as TurnstileSiteOptions['token_location'],
                                })
                            }
                        >
                            <option value="query">{t('turnstile.query')}</option>
                            <option value="json">{t('turnstile.json')}</option>
                            <option value="header">{t('turnstile.header')}</option>
                        </CheckinSelect>
                    </CheckinField>
                    <CheckinField label={t('turnstile.tokenField')}>
                        <Input
                            value={options.token_field}
                            placeholder="turnstile"
                            maxLength={64}
                            onChange={(event) =>
                                onChange({ ...options, token_field: event.target.value })
                            }
                        />
                    </CheckinField>
                </div>
            </div>
        </details>
    );
}
