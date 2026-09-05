'use client';

import { Download, RefreshCw } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import type { WebDAVBackupInfo } from '@/api/endpoints/setting';
import { SettingSection } from './shared';

function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function WebDAVBackupList({
    backups,
    isPending,
    isError,
    isFetching,
    onRefresh,
    restoringFile,
    onRestore,
}: {
    backups: WebDAVBackupInfo[] | null;
    isPending: boolean;
    isError: boolean;
    isFetching: boolean;
    onRefresh: () => void;
    restoringFile: string | null;
    onRestore: (filename: string) => void;
}) {
    const t = useTranslations('setting.webdavBackup');

    return (
        <>
            <SettingSection title={t('backupList')} />
            <div className="space-y-2">
                <div className="flex justify-end">
                    <Button variant="ghost" size="sm" onClick={onRefresh} disabled={isFetching} className="rounded-xl">
                        <RefreshCw className={`size-4 ${isFetching ? 'animate-spin' : ''}`} />
                        {t('refresh')}
                    </Button>
                </div>
                {isPending ? (
                    <p className="text-sm text-muted-foreground">{t('loading')}</p>
                ) : isError ? (
                    <p className="text-sm text-red-500">{t('loadError')}</p>
                ) : backups && backups.length === 0 ? (
                    <p className="text-sm text-muted-foreground">{t('noBackups')}</p>
                ) : backups ? (
                    <div className="space-y-1">
                        {backups.map((backup) => (
                            <div key={backup.name} className="flex items-center justify-between gap-2 rounded-xl border border-border p-2.5 text-sm">
                                <div className="min-w-0 flex-1">
                                    <div className="truncate font-mono text-xs">{backup.name}</div>
                                    <div className="text-xs text-muted-foreground">
                                        {formatBytes(backup.size)} &middot; {new Date(backup.modified_at).toLocaleString()}
                                    </div>
                                </div>
                                <Button variant="outline" size="sm" className="shrink-0 rounded-xl" onClick={() => onRestore(backup.name)} disabled={restoringFile !== null}>
                                    <Download className="size-3.5" />
                                    {restoringFile === backup.name ? t('restoring') : t('restore')}
                                </Button>
                            </div>
                        ))}
                    </div>
                ) : null}
            </div>
        </>
    );
}
