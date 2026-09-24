'use client';

import { useTranslations } from 'next-intl';
import { useTheme } from 'next-themes';
import { toast } from '@/components/common/Toast';
import { useAPIKeyDashboardStats } from '@/api/endpoints/apikey';
import { useAuthStore } from '@/api/endpoints/user';
import { useSettingStore } from '@/stores/setting';
import { AnimatedNumber } from '@/components/common/AnimatedNumber';
import Logo from '@/components/modules/logo';
import { PageWrapper } from '@/components/common/PageWrapper';
import { PageOverview } from '@/components/common/PageOverview';
import { CardGridSkeleton } from '@/components/common/PageState';
import { CopyIconButton } from '@/components/common/CopyButton';
import { useCopyToClipboard } from '@uidotdev/usehooks';
import { useCallback } from 'react';
import type { JSX } from 'react';
import {
    ArrowDownToLine,
    ArrowUpFromLine,
    DollarSign,
    CheckCircle,
    XCircle,
    KeyRound,
    LogOut,
    Calendar,
    Wallet,
    Sun,
    Moon,
    Languages,
    Zap,
    Layers,
    Clock
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Progress } from '@/components/ui/progress';
import dayjs from 'dayjs';

export function APIKeyDashboard() {
    const t = useTranslations('apiKeyDashboard');
    const tOverview = useTranslations('workspace.dashboard');
    const { data, error, isLoading } = useAPIKeyDashboardStats();
    const { logout } = useAuthStore();
    const { theme, setTheme } = useTheme();
    const { locale, setLocale } = useSettingStore();
    const [, copyToClipboard] = useCopyToClipboard();

    const copyWithToast = useCallback(
        async (text: string, label: string) => {
            try {
                await copyToClipboard(text);
                toast.success(`${label} copied`);
                return true;
            } catch {
                toast.error(t('error'));
                return false;
            }
        },
        [copyToClipboard, t]
    );

    if (isLoading) {
        return <main className="mx-auto max-w-6xl px-4 py-10"><CardGridSkeleton label={tOverview('loading')} /></main>;
    }

    if (error || !data) {
        return (
            <div className="flex min-h-dvh items-center justify-center px-4">
                <div className="page-card space-y-4 p-6 text-center">
                    <p className="text-destructive font-medium">{t('error')}</p>
                    <Button onClick={logout} variant="outline" className="rounded-xl">
                        {t('logout')}
                    </Button>
                </div>
            </div>
        );
    }

    const { stats, info } = data;

    // Quota calculations
    const usedCost = stats.total_cost.raw;
    const maxCost = info.max_cost || 0;

    // Expiry calculations
    const expireAt = info.expire_at ? dayjs.unix(info.expire_at) : null;
    const isExpired = expireAt ? expireAt.isBefore(dayjs()) : false;
    const daysUntilExpire = expireAt ? expireAt.diff(dayjs(), 'day') : null;

    const supportedModels = info.supported_models
        ? info.supported_models
            .split(',')
            .map((m) => m.trim())
            .filter(Boolean)
        : [];

    const supportedModelButtons: JSX.Element[] = supportedModels.map((model) => (
        <Button
            key={model}
            variant="secondary"
            size="sm"
            className="h-auto min-h-8 max-w-full whitespace-normal break-all rounded-lg px-3 py-1.5 text-left text-sm transition-colors hover:bg-primary hover:text-primary-foreground"
            onClick={() => void copyWithToast(model, model)}
        >
            {model}
        </Button>
    ));

    const toggleTheme = () => setTheme(theme === 'dark' ? 'light' : 'dark');
    const toggleLanguage = () => {
        if (locale === 'zh_hans') setLocale('zh_hant');
        else if (locale === 'zh_hant') setLocale('en');
        else setLocale('zh_hans');
    };

    return (
        <div className="mx-auto max-w-6xl px-3 md:px-6">
            {/* Header - Consistent with app.tsx */}
            <header className="my-6 flex items-center gap-2 px-2">
                <Logo size={48} />
                <h1 className="ml-2 flex-1 truncate text-2xl font-bold tracking-tight">octopus</h1>
                <div className="flex items-center gap-2">
                    <Button variant="ghost" size="icon" aria-label={tOverview('theme')} onClick={toggleTheme} className="rounded-xl hover:bg-muted">
                        <Sun className="size-4 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
                        <Moon className="absolute size-4 rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100" />
                    </Button>
                    <Button variant="ghost" size="icon" aria-label={tOverview('language')} onClick={toggleLanguage} className="rounded-xl hover:bg-muted">
                        <Languages className="size-4" />
                    </Button>
                    <div className="w-px h-6 bg-border mx-1" />
                    <Button variant="ghost" size="icon" aria-label={t('logout')} onClick={logout} className="rounded-xl hover:bg-destructive/10 hover:text-destructive">
                        <LogOut className="size-4" />
                    </Button>
                </div>
            </header>

            <main className="mb-10">
                <PageWrapper className="space-y-4" animateChildren={false}>
                    <PageOverview title={tOverview('title')} description={tOverview('description')} />
                    {/* Hero: Identity + Limits */}
                    <div className="page-card overflow-hidden">
                        <div className="grid grid-cols-1 md:grid-cols-2">
                            {/* Left: Key Info */}
                            <div className="relative flex min-w-0 flex-col p-5 sm:p-6">
                                <KeyRound aria-hidden="true" className="absolute right-6 top-6 size-5 text-primary" />
                                <h2 className="truncate pr-8 text-lg font-semibold" title={info.name}>{info.name}</h2>
                                <div className="mt-4 flex items-center gap-2 rounded-xl border border-border/50 bg-muted/50 p-3">
                                    <code className="flex-1 font-mono text-sm truncate">
                                        {info.api_key.slice(0, 11)}********{info.api_key.slice(-4)}
                                    </code>
                                    <CopyIconButton
                                        text={info.api_key}
                                        className="flex size-8 items-center justify-center rounded-lg bg-primary/10 text-primary transition-all hover:bg-primary hover:text-primary-foreground active:scale-95"
                                        copyIconClassName="size-4"
                                        checkIconClassName="size-4"
                                    />
                                </div>
                                {/* Expiry & Quota inline */}
                                <div className="mt-auto pt-6 text-sm">
                                    <div className="flex flex-wrap items-center justify-between gap-2">
                                        <span className="flex items-center gap-2 text-muted-foreground"><Calendar className="w-4 h-4" />{t('expireAt')}</span>
                                        {expireAt ? (
                                            <span className={`font-medium ${isExpired ? 'text-destructive' : ''}`}>
                                                {expireAt.format('YYYY-MM-DD')}
                                                {!isExpired && daysUntilExpire !== null && <span className="ml-2 text-xs bg-secondary px-2 py-0.5 rounded-full">{daysUntilExpire} {t('daysLeft')}</span>}
                                                {isExpired && <span className="ml-2 text-xs bg-destructive/10 text-destructive px-2 py-0.5 rounded-full">{t('expired')}</span>}
                                            </span>
                                        ) : (
                                            <span className="font-medium">{t('neverExpire')}</span>
                                        )}
                                    </div>
                                </div>
                            </div>
                            {/* Right: Quota visual */}
                            <div className="relative flex min-w-0 flex-col justify-center border-t border-border/60 bg-primary/5 p-5 sm:p-6 md:border-l md:border-t-0">
                                <Wallet aria-hidden="true" className="absolute right-6 top-6 size-5 text-primary" />
                                <div className="mb-3 text-xs text-muted-foreground">{t('totalCost')}</div>
                                <div className="text-4xl font-semibold tracking-tight tabular-nums text-primary">
                                    <span className="mr-1 text-lg font-normal">$</span>
                                    <AnimatedNumber value={stats.total_cost.formatted.value} />
                                    <span className="ml-1 text-sm font-normal text-muted-foreground">{stats.total_cost.formatted.unit.replace('$', '')}</span>
                                </div>
                                {maxCost > 0 && (
                                    <div className="mt-4">
                                        <Progress aria-label={t('totalCost')} value={Math.min(100, (usedCost / maxCost) * 100)} className="h-2 *:data-[slot=progress-indicator]:bg-primary" />
                                        <div className="flex justify-between text-sm text-muted-foreground mt-1">
                                            <span>0</span>
                                            <span>{maxCost.toFixed(2)} $</span>
                                        </div>
                                    </div>
                                )}
                            </div>
                        </div>
                    </div>

                    {/* Row 2: Request Health */}
                    <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
                        <div className="page-card p-5">
                            <div className="mb-3 flex items-center gap-2 text-xs text-muted-foreground">
                                <CheckCircle className="size-4 text-chart-2" />
                                {t('successRequests')}
                            </div>
                            <div className="text-2xl font-bold">
                                <AnimatedNumber value={stats.request_success.formatted.value} />
                                <span className="ml-1 text-sm font-normal text-muted-foreground">{stats.request_success.formatted.unit}</span>
                            </div>
                        </div>

                        <div className="page-card p-5">
                            <div className="mb-3 flex items-center gap-2 text-xs text-muted-foreground">
                                <XCircle className="size-4 text-destructive" />
                                {t('failedRequests')}
                            </div>
                            <div className="text-2xl font-bold">
                                <AnimatedNumber value={stats.request_failed.formatted.value} />
                                <span className="ml-1 text-sm font-normal text-muted-foreground">{stats.request_failed.formatted.unit}</span>
                            </div>
                        </div>

                        <div className="page-card p-5">
                            <div className="mb-3 flex items-center gap-2 text-xs text-muted-foreground">
                                <Zap className="size-4 text-chart-4" />
                                {t('requestCount')}
                            </div>
                            <div className="text-2xl font-bold">
                                <AnimatedNumber value={stats.request_count.formatted.value} />
                                <span className="ml-1 text-sm font-normal text-muted-foreground">{stats.request_count.formatted.unit}</span>
                            </div>
                        </div>

                        <div className="page-card p-5">
                            <div className="mb-3 flex items-center gap-2 text-xs text-muted-foreground">
                                <Clock className="size-4 text-chart-5" />
                                {t('timeConsumed')}
                            </div>
                            <div className="text-2xl font-bold">
                                <AnimatedNumber value={stats.wait_time.formatted.value} />
                                <span className="ml-1 text-sm font-normal text-muted-foreground">{stats.wait_time.formatted.unit}</span>
                            </div>
                        </div>
                    </div>

                    {/* Row 3: Token & Cost breakdown */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        {/* Token breakdown */}
                        <div className="page-card p-6">
                            <div className="flex items-center gap-2 mb-4">
                                <Zap className="w-5 h-5 text-chart-4" />
                                <span className="font-semibold">{t('totalToken')}</span>
                                <span className="ml-auto text-2xl font-bold"><AnimatedNumber value={stats.total_token.formatted.value} /><span className="text-sm font-normal text-muted-foreground ml-1">{stats.total_token.formatted.unit}</span></span>
                            </div>
                            <div className="grid grid-cols-2 gap-4 pt-4 border-t border-border/50">
                                <div>
                                    <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1"><ArrowDownToLine className="w-3.5 h-3.5" />{t('inputTokens')}</div>
                                    <div className="text-lg font-semibold"><AnimatedNumber value={stats.input_token.formatted.value} /><span className="text-xs font-normal text-muted-foreground ml-1">{stats.input_token.formatted.unit}</span></div>
                                </div>
                                <div>
                                    <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1"><ArrowUpFromLine className="w-3.5 h-3.5" />{t('outputTokens')}</div>
                                    <div className="text-lg font-semibold"><AnimatedNumber value={stats.output_token.formatted.value} /><span className="text-xs font-normal text-muted-foreground ml-1">{stats.output_token.formatted.unit}</span></div>
                                </div>
                            </div>
                        </div>
                        {/* Cost breakdown */}
                        <div className="page-card p-6">
                            <div className="flex items-center gap-2 mb-4">
                                <DollarSign className="w-5 h-5 text-chart-1" />
                                <span className="font-semibold">{t('totalCost')}</span>
                                <span className="ml-auto text-2xl font-bold"><AnimatedNumber value={stats.total_cost.formatted.value} /><span className="text-sm font-normal text-muted-foreground ml-1">{stats.total_cost.formatted.unit}</span></span>
                            </div>
                            <div className="grid grid-cols-2 gap-4 pt-4 border-t border-border/50">
                                <div>
                                    <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1"><ArrowDownToLine className="w-3.5 h-3.5" />{t('inputCost')}</div>
                                    <div className="text-lg font-semibold"><AnimatedNumber value={stats.input_cost.formatted.value} /><span className="text-xs font-normal text-muted-foreground ml-1">{stats.input_cost.formatted.unit}</span></div>
                                </div>
                                <div>
                                    <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1"><ArrowUpFromLine className="w-3.5 h-3.5" />{t('outputCost')}</div>
                                    <div className="text-lg font-semibold"><AnimatedNumber value={stats.output_cost.formatted.value} /><span className="text-xs font-normal text-muted-foreground ml-1">{stats.output_cost.formatted.unit}</span></div>
                                </div>
                            </div>
                        </div>
                    </div>

                    {/* Supported Models */}
                    {info.supported_models && info.supported_models.trim().length > 0 && (
                        <div className="page-card p-6">
                            <div className="flex items-center gap-2 mb-4">
                                <Layers className="w-5 h-5 text-chart-3" />
                                <span className="font-semibold">{t('supportedModels')}</span>
                            </div>
                            <div className="flex flex-wrap gap-2">
                                {supportedModelButtons}
                            </div>
                        </div>
                    )}
                </PageWrapper>
            </main>
        </div>
    );
}
