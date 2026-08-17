'use client';

import { Activity } from './activity';
import { StatsChart } from './chart';
import { Rank } from './rank';
import { PageWrapper } from '@/components/common/PageWrapper';

export function Home() {
    return (
        <PageWrapper className="page-scroll-area space-y-6">
            <StatsChart />
            <Activity />
            <Rank />
        </PageWrapper>
    );
}
