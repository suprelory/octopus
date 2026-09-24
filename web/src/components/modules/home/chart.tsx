'use client';

import { useStatsDaily, useStatsHourly, useStatsTotal } from '@/api/endpoints/stats';
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart';
import { useId, useMemo, type ReactNode } from 'react';
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts';
import { useTranslations } from 'next-intl';
import { cn, formatCount, formatMoney, formatTime } from '@/lib/utils';
import { ChartNoAxesCombined, Clock3, Coins, Layers3, MessagesSquare, type LucideIcon } from 'lucide-react';
import dayjs from 'dayjs';
import { AnimatedNumber } from '@/components/common/AnimatedNumber';
import { Tabs, TabsList, TabsTrigger } from '@/components/animate-ui/components/animate/tabs';
import { useHomeViewStore, type ChartPeriod } from '@/components/modules/home/store';
import { useReducedMotion } from 'motion/react';

type Formatted = { value: string; unit: string };

type MetricsRow = {
    requests: Formatted;
    tokens: Formatted;
    waitTime: Formatted;
};

type HeroValue = {
    value: string | undefined;
    unit: string;
};

type ChartPoint = { date: string; total_cost: number };

const PERIOD_KEY: Record<ChartPeriod, 'today' | 'last7Days' | 'last30Days' | 'allTime'> = {
    '1': 'today',
    '7': 'last7Days',
    '30': 'last30Days',
    all: 'allTime',
};

export function StatsChart({ children }: { children: ReactNode }) {
    const t = useTranslations('home.summary');
    const gradientId = useId();
    const reducedMotion = useReducedMotion();

    const { data: statsTotal, isLoading: totalLoading, isError: totalError } = useStatsTotal();
    const { data: statsDaily, isLoading: dailyLoading, isError: dailyError } = useStatsDaily();
    const { data: statsHourly, isLoading: hourlyLoading, isError: hourlyError } = useStatsHourly();

    const period = useHomeViewStore((state) => state.chartPeriod);
    const setChartPeriod = useHomeViewStore((state) => state.setChartPeriod);

    const sortedDaily = useMemo(() => {
        if (!statsDaily) return [];
        return [...statsDaily].sort((a, b) => a.date.localeCompare(b.date));
    }, [statsDaily]);

    const { hero, metrics, chartData } = useMemo<{
        hero: HeroValue;
        metrics: MetricsRow;
        chartData: ChartPoint[];
    }>(() => {
        const emptyMetrics: MetricsRow = {
            requests: formatCount(0).formatted,
            tokens: formatCount(0).formatted,
            waitTime: formatTime(0).formatted,
        };
        const emptyHero: HeroValue = { value: undefined, unit: '' };

        if (period === 'all') {
            // 累计档：优先使用 statsTotal；否则 fallback 到 statsDaily 全量聚合
            const points: ChartPoint[] = sortedDaily.map((stat) => ({
                date: dayjs(stat.date).format('MM/DD'),
                total_cost: stat.total_cost.raw,
            }));

            if (statsTotal) {
                return {
                    hero: {
                        value: statsTotal.total_cost.formatted.value,
                        unit: statsTotal.total_cost.formatted.unit,
                    },
                    metrics: {
                        requests: statsTotal.request_count.formatted,
                        tokens: statsTotal.total_token.formatted,
                        waitTime: statsTotal.wait_time.formatted,
                    },
                    chartData: points,
                };
            }

            if (sortedDaily.length === 0) {
                return { hero: emptyHero, metrics: emptyMetrics, chartData: [] };
            }

            const cost = sortedDaily.reduce((acc, s) => acc + s.total_cost.raw, 0);
            const requests = sortedDaily.reduce((acc, s) => acc + s.request_count.raw, 0);
            const tokens = sortedDaily.reduce((acc, s) => acc + s.total_token.raw, 0);
            const wait = sortedDaily.reduce((acc, s) => acc + s.wait_time.raw, 0);
            const costFmt = formatMoney(cost).formatted;
            return {
                hero: { value: costFmt.value, unit: costFmt.unit },
                metrics: {
                    requests: formatCount(requests).formatted,
                    tokens: formatCount(tokens).formatted,
                    waitTime: formatTime(wait).formatted,
                },
                chartData: points,
            };
        }

        if (period === '1') {
            // 今日档：聚合 statsHourly
            if (!statsHourly) {
                return { hero: emptyHero, metrics: emptyMetrics, chartData: [] };
            }
            const points: ChartPoint[] = statsHourly.map((stat) => ({
                date: `${stat.hour}:00`,
                total_cost: stat.total_cost.raw,
            }));
            const cost = statsHourly.reduce((acc, s) => acc + s.total_cost.raw, 0);
            const requests = statsHourly.reduce((acc, s) => acc + s.request_count.raw, 0);
            const tokens = statsHourly.reduce((acc, s) => acc + s.total_token.raw, 0);
            const wait = statsHourly.reduce((acc, s) => acc + s.wait_time.raw, 0);
            const costFmt = formatMoney(cost).formatted;
            return {
                hero: { value: costFmt.value, unit: costFmt.unit },
                metrics: {
                    requests: formatCount(requests).formatted,
                    tokens: formatCount(tokens).formatted,
                    waitTime: formatTime(wait).formatted,
                },
                chartData: points,
            };
        }

        // 7 / 30 天：聚合 statsDaily
        const days = Number(period);
        const recent = sortedDaily.slice(-days);
        const points: ChartPoint[] = recent.map((stat) => ({
            date: dayjs(stat.date).format('MM/DD'),
            total_cost: stat.total_cost.raw,
        }));

        if (recent.length === 0) {
            return { hero: emptyHero, metrics: emptyMetrics, chartData: [] };
        }

        const cost = recent.reduce((acc, s) => acc + s.total_cost.raw, 0);
        const requests = recent.reduce((acc, s) => acc + s.request_count.raw, 0);
        const tokens = recent.reduce((acc, s) => acc + s.total_token.raw, 0);
        const wait = recent.reduce((acc, s) => acc + s.wait_time.raw, 0);
        const costFmt = formatMoney(cost).formatted;
        return {
            hero: { value: costFmt.value, unit: costFmt.unit },
            metrics: {
                requests: formatCount(requests).formatted,
                tokens: formatCount(tokens).formatted,
                waitTime: formatTime(wait).formatted,
            },
            chartData: points,
        };
    }, [period, statsTotal, statsHourly, sortedDaily]);

    const chartConfig = useMemo(
        () => ({
            total_cost: { label: t('headline.allTime') },
        }),
        [t]
    );

    const isLoading = period === '1' ? hourlyLoading : period === 'all' ? totalLoading && dailyLoading : dailyLoading;
    const hasError = period === '1' ? hourlyError : period === 'all' ? totalError && dailyError : dailyError;

    return (
        <section className="space-y-5" aria-label={t('title')}>
            <header className="flex flex-col gap-4 px-1 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h2 className="text-lg font-semibold tracking-tight">{t('title')}</h2>
                    <p className="mt-1 text-sm text-muted-foreground">{t('description')}</p>
                </div>
                <Tabs value={period} onValueChange={(v) => setChartPeriod(v as ChartPeriod)}>
                    <TabsList aria-label={t('periodLabel')} className="h-10 w-full border border-border/60 bg-card/70 p-1 sm:w-auto">
                        <TabsTrigger value="1">{t('periods.today')}</TabsTrigger>
                        <TabsTrigger value="7">{t('periods.last7Days')}</TabsTrigger>
                        <TabsTrigger value="30">{t('periods.last30Days')}</TabsTrigger>
                        <TabsTrigger value="all">{t('periods.allTime')}</TabsTrigger>
                    </TabsList>
                </Tabs>
            </header>

            {hasError && <p role="alert" className="rounded-xl border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive">{t('loadFailed')}</p>}

            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4" aria-busy={isLoading}>
                <StatItem label={t(`headline.${PERIOD_KEY[period]}`)} icon={Coins} accent monetary loading={isLoading}
                    value={hasError ? undefined : { value: hero.value ?? '0.00', unit: hero.unit || '$' }} />
                <StatItem label={t('metrics.requests')} value={hasError ? undefined : metrics.requests} icon={MessagesSquare} loading={isLoading} />
                <StatItem label={t('metrics.tokens')} value={hasError ? undefined : metrics.tokens} icon={Layers3} loading={isLoading} />
                <StatItem label={t('totalDuration')} value={hasError ? undefined : metrics.waitTime} icon={Clock3} loading={isLoading} />
            </div>

            <div className="grid gap-4 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
                <div className="min-w-0 overflow-hidden rounded-2xl border border-border/70 bg-card shadow-sm">
                    <div className="flex items-start justify-between gap-3 p-5">
                        <div>
                            <h3 className="font-semibold">{t('trend')}</h3>
                            <p className="mt-1 text-xs text-muted-foreground">{t(`headline.${PERIOD_KEY[period]}`)}</p>
                        </div>
                        <span className="inline-flex items-center gap-1.5 rounded-full bg-primary/8 px-2.5 py-1 text-xs font-medium text-primary">
                            <span className="size-1.5 rounded-full bg-primary" /> USD
                        </span>
                    </div>
                    {isLoading ? (
                        <div className="mx-5 mb-5 h-60 animate-pulse rounded-xl bg-muted/60" />
                    ) : chartData.length === 0 ? (
                        <div className="flex h-65 flex-col items-center justify-center gap-2 px-5 text-center">
                            <span className="mb-1 rounded-2xl bg-muted/60 p-3"><ChartNoAxesCombined className="size-6 text-muted-foreground" /></span>
                            <p className="text-sm font-medium">{hasError ? t('loadFailed') : t('noData')}</p>
                            {!hasError && <p className="max-w-64 text-xs leading-relaxed text-muted-foreground">{t('noDataHint')}</p>}
                        </div>
                    ) : (
                        <ChartContainer config={chartConfig} className="h-65 w-full px-2 pb-4 sm:pr-5">
                            <AreaChart accessibilityLayer data={chartData} margin={{ top: 12, right: 8, left: -16, bottom: 0 }}>
                                <defs>
                                    <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                                        <stop offset="0%" stopColor="var(--primary)" stopOpacity={0.24} />
                                        <stop offset="100%" stopColor="var(--primary)" stopOpacity={0.01} />
                                    </linearGradient>
                                </defs>
                                <CartesianGrid strokeDasharray="4 4" vertical={false} strokeOpacity={0.6} />
                                <XAxis dataKey="date" tickLine={false} axisLine={false} tickMargin={10} minTickGap={28} />
                                <YAxis tickLine={false} axisLine={false} tickMargin={8} tickCount={4}
                                    tickFormatter={(value) => {
                                        const formatted = formatMoney(value).formatted;
                                        return `$${formatted.value}${formatted.unit.replace('$', '')}`;
                                    }} />
                                <ChartTooltip cursor={false} content={<ChartTooltipContent indicator="line" />} />
                                <Area type="monotone" dataKey="total_cost" stroke="var(--primary)" strokeWidth={2.5} isAnimationActive={!reducedMotion}
                                    fill={`url(#${gradientId})`} activeDot={{ r: 5, stroke: 'var(--card)', strokeWidth: 3 }} />
                            </AreaChart>
                        </ChartContainer>
                    )}
                </div>
                {children}
            </div>
        </section>
    );
}

function StatItem({ label, value, icon: Icon, accent, monetary, loading }: {
    label: string;
    value: Formatted | undefined;
    icon: LucideIcon;
    accent?: boolean;
    monetary?: boolean;
    loading: boolean;
}) {
    const reducedMotion = useReducedMotion();
    return (
        <div className={cn('min-w-0 rounded-2xl border p-4 shadow-sm sm:p-5', accent ? 'border-primary/20 bg-primary/8' : 'border-border/70 bg-card')}>
            <div className="mb-4 flex items-center justify-between gap-2">
                <span className="text-xs font-medium text-muted-foreground">{label}</span>
                <Icon aria-hidden className={cn('size-4 shrink-0', accent ? 'text-primary' : 'text-muted-foreground/70')} />
            </div>
            <div className="text-2xl font-semibold tracking-tight tabular-nums sm:text-3xl">
                {loading ? <span className="block h-8 w-20 animate-pulse rounded bg-muted" /> : value ? (
                    <>
                        {monetary && <span className="mr-0.5 text-lg font-normal text-muted-foreground">$</span>}
                        <AnimatedNumber value={value.value} duration={reducedMotion ? 0 : 500} />
                        {value.unit && (
                            <span className="ml-1 text-sm font-normal text-muted-foreground">{monetary ? value.unit.replace('$', '') : value.unit}</span>
                        )}
                    </>
                ) : (
                    <span className="text-muted-foreground">—</span>
                )}
            </div>
        </div>
    );
}
