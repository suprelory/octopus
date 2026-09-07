'use client';

import { Plus, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion';
import { type SiteFormFieldsProps, ROUTE_BASE_URL_OPTIONS } from './site-form';

export function SiteAdvancedFields({ siteForm, setSiteForm }: SiteFormFieldsProps) {
    return (
        <Accordion type="single" collapsible className="w-full rounded-xl border bg-card">
            <AccordionItem value="advanced" className="border-none">
                <AccordionTrigger className="rounded-xl px-4 py-3 text-sm font-medium text-card-foreground transition-colors hover:bg-muted/30 hover:no-underline">
                    高级设置
                </AccordionTrigger>
                <AccordionContent className="space-y-4 border-t px-4 pb-4 pt-4">
                    <div className="space-y-2">
                        <div className="flex items-center justify-between">
                            <label className="text-sm font-medium text-card-foreground">
                                自定义 Header {siteForm.custom_header.length > 0 ? `(${siteForm.custom_header.length})` : ''}
                            </label>
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                onClick={() =>
                                    setSiteForm((current) => ({
                                        ...current,
                                        custom_header: [
                                            ...current.custom_header,
                                            { header_key: '', header_value: '' },
                                        ],
                                    }))
                                }
                                className="h-6 px-2 text-xs text-muted-foreground/70 hover:bg-transparent hover:text-muted-foreground"
                            >
                                <Plus className="mr-1 h-3 w-3" />
                                添加
                            </Button>
                        </div>
                        <div className="space-y-2">
                            {siteForm.custom_header.map((item, index) => (
                                <div key={`site-hdr-${index}`} className="flex items-center gap-2">
                                    <Input
                                        value={item.header_key}
                                        onChange={(event) =>
                                            setSiteForm((current) => ({
                                                ...current,
                                                custom_header: current.custom_header.map(
                                                    (header, headerIndex) =>
                                                        headerIndex === index
                                                            ? { ...header, header_key: event.target.value }
                                                            : header,
                                                ),
                                            }))
                                        }
                                        placeholder="Header Key"
                                        className="flex-1 rounded-xl"
                                    />
                                    <Input
                                        value={item.header_value}
                                        onChange={(event) =>
                                            setSiteForm((current) => ({
                                                ...current,
                                                custom_header: current.custom_header.map(
                                                    (header, headerIndex) =>
                                                        headerIndex === index
                                                            ? {
                                                                  ...header,
                                                                  header_value: event.target.value,
                                                              }
                                                            : header,
                                                ),
                                            }))
                                        }
                                        placeholder="Header Value"
                                        className="flex-1 rounded-xl"
                                    />
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        onClick={() =>
                                            setSiteForm((current) => ({
                                                ...current,
                                                custom_header: current.custom_header.filter(
                                                    (_, headerIndex) => headerIndex !== index,
                                                ),
                                            }))
                                        }
                                        disabled={siteForm.custom_header.length <= 1}
                                        className="h-8 w-8 rounded-xl p-0 text-muted-foreground hover:bg-transparent hover:text-destructive disabled:opacity-40"
                                        title="Remove"
                                    >
                                        <X className="h-4 w-4" />
                                    </Button>
                                </div>
                            ))}
                        </div>
                    </div>
                    <div className="space-y-2">
                        <div className="flex items-center justify-between">
                            <label className="text-sm font-medium text-card-foreground">
                                协议路径覆盖 {siteForm.route_base_urls.length > 0 ? `(${siteForm.route_base_urls.length})` : ''}
                            </label>
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                onClick={() =>
                                    setSiteForm((current) => ({
                                        ...current,
                                        route_base_urls: [
                                            ...current.route_base_urls,
                                            { route_type: '', base_url: '' },
                                        ],
                                    }))
                                }
                                className="h-6 px-2 text-xs text-muted-foreground/70 hover:bg-transparent hover:text-muted-foreground"
                            >
                                <Plus className="mr-1 h-3 w-3" />
                                添加
                            </Button>
                        </div>
                        <p className="text-xs text-muted-foreground/70">
                            按协议覆盖请求地址，例如 Anthropic 填 https://example.com/anthropic/v1，留空则用站点地址默认推断。
                        </p>
                        <div className="space-y-2">
                            {siteForm.route_base_urls.map((item, index) => (
                                <div key={`site-route-${index}`} className="flex items-center gap-2">
                                    <Select
                                        value={item.route_type || undefined}
                                        onValueChange={(value) =>
                                            setSiteForm((current) => ({
                                                ...current,
                                                route_base_urls: current.route_base_urls.map(
                                                    (route, routeIndex) =>
                                                        routeIndex === index
                                                            ? { ...route, route_type: value }
                                                            : route,
                                                ),
                                            }))
                                        }
                                    >
                                        <SelectTrigger className="w-40 rounded-xl">
                                            <SelectValue placeholder="协议类型" />
                                        </SelectTrigger>
                                        <SelectContent>
                                            {ROUTE_BASE_URL_OPTIONS.map((option) => (
                                                <SelectItem key={option.value} value={option.value}>
                                                    {option.label}
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                    <Input
                                        value={item.base_url}
                                        onChange={(event) =>
                                            setSiteForm((current) => ({
                                                ...current,
                                                route_base_urls: current.route_base_urls.map(
                                                    (route, routeIndex) =>
                                                        routeIndex === index
                                                            ? { ...route, base_url: event.target.value }
                                                            : route,
                                                ),
                                            }))
                                        }
                                        placeholder="https://example.com/anthropic/v1"
                                        className="flex-1 rounded-xl"
                                    />
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        onClick={() =>
                                            setSiteForm((current) => ({
                                                ...current,
                                                route_base_urls: current.route_base_urls.filter(
                                                    (_, routeIndex) => routeIndex !== index,
                                                ),
                                            }))
                                        }
                                        className="h-8 w-8 rounded-xl p-0 text-muted-foreground hover:bg-transparent hover:text-destructive disabled:opacity-40"
                                        title="Remove"
                                    >
                                        <X className="h-4 w-4" />
                                    </Button>
                                </div>
                            ))}
                        </div>
                    </div>
                </AccordionContent>
            </AccordionItem>
        </Accordion>
    );
}
