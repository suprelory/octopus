import { useEffect, type ReactNode } from 'react';
import { IntlProvider } from 'use-intl';
import { useSettingStore, type Locale } from '@/stores/setting';

import zh_hansMessages from '@/locales/zh_hans.json';
import zh_hantMessages from '@/locales/zh_hant.json';
import enMessages from '@/locales/en.json';

import checkinZhHans from '@/locales/checkin/zh_hans.json';
import checkinZhHant from '@/locales/checkin/zh_hant.json';
import checkinEn from '@/locales/checkin/en.json';

const messages: Record<Locale, typeof zh_hansMessages & { checkin: typeof checkinZhHans }> = {
    zh_hans: { ...zh_hansMessages, checkin: checkinZhHans },
    zh_hant: { ...zh_hantMessages, checkin: checkinZhHant },
    en: { ...enMessages, checkin: checkinEn },
};

const languageTags: Record<Locale, string> = {
    zh_hans: 'zh-Hans',
    zh_hant: 'zh-Hant',
    en: 'en',
};

export function LocaleProvider({ children }: { children: ReactNode }) {
    const locale = useSettingStore((state) => state.locale);

    useEffect(() => {
        document.documentElement.lang = languageTags[locale];
    }, [locale]);

    return (
        <IntlProvider
            locale={languageTags[locale]}
            messages={messages[locale]}
            timeZone="Asia/Shanghai"
        >
            {children}
        </IntlProvider>
    );
}
