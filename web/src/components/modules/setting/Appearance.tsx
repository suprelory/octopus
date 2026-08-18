'use client';

import { useTheme } from 'next-themes';
import { useTranslations } from 'next-intl';
import { Sun, Moon, Monitor, Languages } from 'lucide-react';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useSettingStore, type Locale } from '@/stores/setting';
import { SETTING_CONTROL_WIDTH, SettingCard, SettingRow } from './shared';

export function SettingAppearance() {
    const t = useTranslations('setting');
    const { theme, setTheme } = useTheme();
    const { locale, setLocale } = useSettingStore();

    return (
        <SettingCard icon={Sun} title={t('appearance')}>
            {/* 主题 */}
            <SettingRow icon={theme === 'dark' ? Moon : Sun} label={t('theme.label')}>
                <Select value={theme} onValueChange={setTheme}>
                    <SelectTrigger className={`${SETTING_CONTROL_WIDTH} rounded-xl`}>
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="rounded-xl">
                        <SelectItem value="light" className="rounded-xl">
                            <Sun className="size-4" />
                            {t('theme.light')}
                        </SelectItem>
                        <SelectItem value="dark" className="rounded-xl">
                            <Moon className="size-4" />
                            {t('theme.dark')}
                        </SelectItem>
                        <SelectItem value="system" className="rounded-xl">
                            <Monitor className="size-4" />
                            {t('theme.system')}
                        </SelectItem>
                    </SelectContent>
                </Select>
            </SettingRow>

            {/* 语言 */}
            <SettingRow icon={Languages} label={t('language.label')}>
                <Select value={locale} onValueChange={(v) => setLocale(v as Locale)}>
                    <SelectTrigger className={`${SETTING_CONTROL_WIDTH} rounded-xl`}>
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="rounded-xl">
                        <SelectItem value="zh_hans" className="rounded-xl">{t('language.zh_hans')}</SelectItem>
                        <SelectItem value="zh_hant" className="rounded-xl">{t('language.zh_hant')}</SelectItem>
                        <SelectItem value="en" className="rounded-xl">{t('language.en')}</SelectItem>
                    </SelectContent>
                </Select>
            </SettingRow>
        </SettingCard>
    );
}
