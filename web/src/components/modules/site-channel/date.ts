import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';
import 'dayjs/locale/zh-tw';
import relativeTime from 'dayjs/plugin/relativeTime';

dayjs.extend(relativeTime);

export const DAYJS_LOCALE_MAP: Record<'zh_hans' | 'zh_hant' | 'en', string> = {
    zh_hans: 'zh-cn',
    zh_hant: 'zh-tw',
    en: 'en',
};

export { dayjs };
