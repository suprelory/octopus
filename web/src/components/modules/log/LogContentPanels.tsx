'use client';

import { useMemo, type ReactNode } from 'react';
import { ChevronsDownUp, ChevronsUpDown, MessageSquare, Send } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Badge } from '@/components/ui/badge';
import { CopyIconButton } from '@/components/common/CopyButton';
import { DeferredJsonContent } from './DeferredJsonContent';
import { formatJsonForCopy } from './display';

export function LogContentPanels({
    requestContent,
    responseContent,
    requestTokens,
    responseTokens,
    detailLoading,
    requestCollapsed,
    responseCollapsed,
    onToggleRequest,
    onToggleResponse,
}: {
    requestContent?: string;
    responseContent?: string;
    requestTokens: number;
    responseTokens: number;
    detailLoading: boolean;
    requestCollapsed: boolean;
    responseCollapsed: boolean;
    onToggleRequest: () => void;
    onToggleResponse: () => void;
}) {
    const t = useTranslations('log.card');
    const requestCopyText = useMemo(() => formatJsonForCopy(requestContent), [requestContent]);
    const responseCopyText = useMemo(() => formatJsonForCopy(responseContent), [responseContent]);

    return (
        <div className="flex-1 min-h-0 overflow-hidden">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 h-full min-h-0">
                <LogContentPanel
                    icon={<Send className="size-4 text-green-500" />}
                    title={t('requestContent')}
                    tokens={requestTokens}
                    content={requestContent}
                    copyText={requestCopyText}
                    fallbackText={t('noRequestContent')}
                    isLoading={detailLoading}
                    collapsed={requestCollapsed}
                    onToggle={onToggleRequest}
                />
                <LogContentPanel
                    icon={<MessageSquare className="size-4 text-purple-500" />}
                    title={t('responseContent')}
                    tokens={responseTokens}
                    content={responseContent}
                    copyText={responseCopyText}
                    fallbackText={t('noResponseContent')}
                    isLoading={detailLoading}
                    collapsed={responseCollapsed}
                    onToggle={onToggleResponse}
                />
            </div>
        </div>
    );
}

function LogContentPanel({
    icon,
    title,
    tokens,
    content,
    copyText,
    fallbackText,
    isLoading,
    collapsed,
    onToggle,
}: {
    icon: ReactNode;
    title: string;
    tokens: number;
    content?: string;
    copyText: string;
    fallbackText: string;
    isLoading: boolean;
    collapsed: boolean;
    onToggle: () => void;
}) {
    const t = useTranslations('log.card');
    return (
        <div className="flex flex-col rounded-2xl border border-border bg-muted/30 overflow-hidden min-h-0">
            <div className="flex items-center gap-2 px-3 md:px-4 py-2.5 md:py-3 border-b border-border bg-muted/50 shrink-0">
                {icon}
                <span className="text-sm font-medium text-card-foreground">{title}</span>
                <div className="ml-auto flex items-center gap-1">
                    <Badge variant="secondary" className="text-xs">{tokens.toLocaleString()} {t('tokens')}</Badge>
                    {content ? (
                        <>
                            <button type="button" onClick={onToggle} className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground" title={collapsed ? t('expandAll') : t('collapseAll')}>
                                {collapsed ? <ChevronsUpDown className="size-3.5" /> : <ChevronsDownUp className="size-3.5" />}
                            </button>
                            <CopyIconButton text={copyText} className="text-muted-foreground hover:text-foreground" />
                        </>
                    ) : null}
                </div>
            </div>
            <div className="flex-1 overflow-auto min-h-0">
                <DeferredJsonContent content={content} fallbackText={fallbackText} isLoading={isLoading} collapsed={collapsed} />
            </div>
        </div>
    );
}
