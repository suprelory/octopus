import { useTranslations } from 'use-intl';
import { Info, Tag, AlertTriangle, Download, Loader2 } from 'lucide-react';
import Github from '@thesvg/react/github';
import { useLatestInfo, useBuildInfo, useUpdateCore } from '@/api/update';
import { Button } from '@/components/ui/button';
import { toast } from 'sonner';

const APP_VERSION = import.meta.env.VITE_APP_VERSION || 'dev'; // 与普通 Go 构建的默认版本一致。

// SettingInfo 展示版本信息，并在更新后清理本项目的浏览器缓存。
export function SettingInfo() {
    const t = useTranslations('setting');
    const latestInfoQuery = useLatestInfo();
    const buildInfoQuery = useBuildInfo();
    const updateCore = useUpdateCore();

    const backendNowVersion = buildInfoQuery.data?.version || '';
    const latestVersion = latestInfoQuery.data?.tag_name || '';
    const repositoryURL = buildInfoQuery.data?.repository_url;
    const releaseStatus = latestInfoQuery.data?.status;

    // 前端版本与后端当前版本不一致 → 浏览器缓存问题
    const isCacheMismatch = !!backendNowVersion && backendNowVersion !== APP_VERSION;
    // 由后端统一比较版本和检查平台发布包，避免降级或覆盖开发版本。
    const hasNewVersion = latestInfoQuery.data?.update_available === true && !latestInfoQuery.isError;

    // clearCacheAndReload 清理当前应用目录的缓存和 Service Worker 注册后刷新页面。
    const clearCacheAndReload = async () => {
        const scope = new URL('./', document.baseURI); // 当前应用的 Service Worker 作用域及缓存所属目录。
        if ('caches' in window) {
            const names = await caches.keys();
            await Promise.all(names.filter((name) => name.startsWith(`octopus-${encodeURIComponent(scope.pathname)}-`)).map((name) => caches.delete(name)));
        }

        if ('serviceWorker' in navigator) {
            const registration = await navigator.serviceWorker.getRegistration(scope.href);
            if (registration && registration.scope === scope.href) await registration.unregister();
        }

        window.location.reload();
    };

    // handleForceRefresh 立即清理缓存并重新加载当前页面。
    const handleForceRefresh = () => {
        void clearCacheAndReload();
    };

    // handleUpdate 更新服务端程序，成功后清理旧前端缓存。
    const handleUpdate = () => {
        if (!hasNewVersion || !latestVersion) return;
        updateCore.mutate(latestVersion, {
            onSuccess: () => {
                toast.success(t('info.updateSuccess'));
                // 更新成功后清理缓存并刷新
                setTimeout(() => {
                    void clearCacheAndReload();
                }, 1500);
            },
            onError: () => {
                toast.error(t('info.updateFailed'));
            }
        });
    };

    return (
        <div className="rounded-3xl border border-border bg-card p-6 space-y-5">
            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                <Info className="h-5 w-5" />
                {t('info.title')}
            </h2>
            {/* GitHub 仓库 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Github variant="mono" className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('info.github')}</span>
                </div>
                {repositoryURL ? <a
                    href={repositoryURL}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-sm text-primary hover:underline"
                >
                    {buildInfoQuery.data?.repository}
                </a> : <span className="text-sm text-muted-foreground">{t('info.unknown')}</span>}
            </div>
            {/* 当前版本 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Tag className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('info.currentVersion')}</span>
                </div>
                <div className="flex items-center gap-2">
                    {buildInfoQuery.isLoading ? (
                        <Loader2 className="size-4 animate-spin text-muted-foreground" />
                    ) : (
                        <code className="text-sm font-mono text-muted-foreground">
                            {backendNowVersion || t('info.unknown')}
                        </code>
                    )}
                </div>
            </div>

            {/* 最新版本 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Download className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('info.latestVersion')}</span>
                </div>
                <div className="flex items-center gap-2">
                    {latestInfoQuery.isLoading ? (
                        <Loader2 className="size-4 animate-spin text-muted-foreground" />
                    ) : (
                        <code className="text-sm font-mono text-muted-foreground">
                            {latestInfoQuery.isError ? t('info.checkFailed') : releaseStatus === 'no_release' ? t('info.noRelease') : latestVersion || t('info.unknown')}
                        </code>
                    )}
                </div>
            </div>

            {(releaseStatus === 'development' || releaseStatus === 'unsupported_platform' || releaseStatus === 'asset_missing') && (
                <p className="text-sm text-muted-foreground">
                    {releaseStatus === 'development' ? t('info.developmentBuild') : t('info.noPlatformAsset')}
                </p>
            )}
            {latestInfoQuery.isError && (
                <Button variant="outline" size="sm" onClick={() => void latestInfoQuery.refetch()} disabled={latestInfoQuery.isFetching}>
                    {t('info.retryCheck')}
                </Button>
            )}

            {/* 浏览器缓存问题警告 */}
            {isCacheMismatch && (
                <div className="p-3 bg-destructive/10 border border-destructive/20 rounded-xl space-y-2">
                    <div className="flex items-start gap-3">
                        <AlertTriangle className="h-5 w-5 text-destructive shrink-0 mt-0.5" />
                        <div className="flex-1 space-y-1">
                            <p className="text-sm text-destructive font-medium">
                                {t('info.versionMismatch')}
                            </p>
                            <p className="text-xs text-muted-foreground">
                                {t('info.versionMismatchHint', { frontend: APP_VERSION, backend: backendNowVersion })}
                            </p>
                        </div>
                    </div>
                    <div className="flex justify-end">
                        <Button
                            variant="destructive"
                            size="sm"
                            onClick={handleForceRefresh}
                            className="rounded-xl"
                        >
                            {t('info.forceRefresh')}
                        </Button>
                    </div>
                </div>
            )}

            {/* 有新版本可更新 */}
            {hasNewVersion && (
                <div className="p-3 bg-primary/10 border border-primary/20 rounded-xl space-y-2">
                    <div className="flex items-start gap-3">
                        <Download className="h-5 w-5 text-primary shrink-0 mt-0.5" />
                        <div className="flex-1 space-y-1">
                            <p className="text-sm text-primary font-medium">
                                {t('info.newVersionAvailable')}
                            </p>
                            <p className="text-xs text-muted-foreground">
                                {t('info.newVersionAvailableHint')}
                            </p>
                        </div>
                    </div>
                    <div className="flex justify-end">
                        <Button
                            variant="default"
                            size="sm"
                            onClick={handleUpdate}
                            disabled={updateCore.isPending}
                            className="rounded-xl"
                        >
                            {updateCore.isPending ? t('info.updating') : t('info.updateNow')}
                        </Button>
                    </div>
                </div>
            )}
        </div>
    );
}
