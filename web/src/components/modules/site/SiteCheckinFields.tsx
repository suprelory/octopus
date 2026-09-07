'use client';

import { Input } from '@/components/ui/input';
import type { SiteFormFieldsProps } from './site-form';

export function SiteCheckinFields({ siteForm, setSiteForm }: SiteFormFieldsProps) {
    return (
        <>
            <label className="grid gap-2 text-sm">
                <span className="font-medium">手动签到 URL</span>
                <Input
                    value={siteForm.external_checkin_url}
                    onChange={(event) =>
                        setSiteForm((current) => ({
                            ...current,
                            external_checkin_url: event.target.value,
                        }))
                    }
                    placeholder="可选：例如 https://example.com/signin"
                    className="rounded-xl"
                />
                <span className="text-xs text-muted-foreground">
                    配置后可在站点总览中一键打开此页面进行手动签到。
                </span>
            </label>

            <div className="grid gap-4 rounded-xl border border-border/60 bg-muted/20 p-4 md:grid-cols-2">
                <label className="grid gap-2 text-sm md:col-span-2">
                    <span className="font-medium">自动签到时区</span>
                    <Input
                        value={siteForm.checkin_timezone}
                        onChange={(event) =>
                            setSiteForm((current) => ({
                                ...current,
                                checkin_timezone: event.target.value,
                            }))
                        }
                        list="site-checkin-timezones"
                        placeholder="Asia/Shanghai"
                        className="rounded-xl"
                    />
                    <datalist id="site-checkin-timezones">
                        <option value="Asia/Shanghai" />
                        <option value="Asia/Hong_Kong" />
                        <option value="Asia/Tokyo" />
                        <option value="Europe/London" />
                        <option value="America/New_York" />
                        <option value="UTC" />
                    </datalist>
                </label>
                <label className="grid gap-2 text-sm">
                    <span className="font-medium">开始时间</span>
                    <Input
                        type="time"
                        value={siteForm.checkin_window_start}
                        onChange={(event) =>
                            setSiteForm((current) => ({
                                ...current,
                                checkin_window_start: event.target.value,
                            }))
                        }
                        className="rounded-xl"
                    />
                </label>
                <label className="grid gap-2 text-sm">
                    <span className="font-medium">结束时间</span>
                    <Input
                        type="time"
                        value={siteForm.checkin_window_end}
                        onChange={(event) =>
                            setSiteForm((current) => ({
                                ...current,
                                checkin_window_end: event.target.value,
                            }))
                        }
                        className="rounded-xl"
                    />
                </label>
                <p className="text-xs text-muted-foreground md:col-span-2">
                    自动签到只会在该站点当地时间窗口内执行；例如仅允许 8 点后签到时，将开始时间设为 08:00。
                </p>
            </div>
        </>
    );
}
